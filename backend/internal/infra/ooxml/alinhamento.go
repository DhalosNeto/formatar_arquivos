package ooxml

import (
	"bytes"
	"io"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/daniel-halos/formatador/internal/domain/ruleset"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// AplicarAlinhamento escreve alinhamento direto nos parágrafos indicados pelos
// ordinais de ExtrairBlocos (tabelas contam, mas não são alvos válidos).
// Não escolhe papéis do CDM, não autoriza usuários e não modifica styles.xml.
// A operação é atômica; lista vazia é no-op após validar receptor e alinhamento.
func (documento *Documento) AplicarAlinhamento(referencias []int, alinhamento string) error {
	if documento == nil {
		return errors.NovoErroArgumentoNulo("documento")
	}
	if err := ruleset.ValidarAlinhamento(alinhamento); err != nil {
		return err
	}
	return documento.aplicarPropriedadeParagrafos(referencias, "jc", map[string]string{"val": valorAlinhamentoXML(alinhamento)})
}

// aplicarPropriedadeParagrafos recebe apenas nomes e valores produzidos pelos
// adaptadores internos; não aceita fragmentos XML nem faz autorização de usuário.
func (documento *Documento) aplicarPropriedadeParagrafos(referencias []int, local string, valores map[string]string) error {
	return documento.aplicarPropriedadeParagrafosComPreflight(referencias, local, valores, nil)
}

func (documento *Documento) aplicarPropriedadeParagrafosComPreflight(referencias []int, local string, valores map[string]string, preflight func([]byte, *noXMLPagina) error) error {
	if len(referencias) == 0 {
		return nil
	}
	if len(referencias) > limiteNosPagina {
		return erroReferenciasParagrafos()
	}
	vistas := make(map[int]bool, len(referencias))
	for _, referencia := range referencias {
		if referencia < 0 || vistas[referencia] {
			return erroReferenciasParagrafos()
		}
		vistas[referencia] = true
	}
	conteudo, err := documento.abrirDocumentoPrincipal()
	if err != nil {
		return errors.NovoErroAplicacao("não foi possível ler o documento principal")
	}
	original, errLeitura := io.ReadAll(io.LimitReader(conteudo, limiteXMLPagina+1))
	errFechamento := conteudo.Close()
	if errLeitura != nil || errFechamento != nil {
		return errors.NovoErroAplicacao("não foi possível ler o documento principal")
	}
	corpo, err := corpoXMLParagrafos(original)
	if err != nil {
		return err
	}
	var blocos []*noXMLPagina
	for _, filho := range corpo.filhos {
		if _, bloco := tipoDoElemento(filho.nome); bloco {
			blocos = append(blocos, filho)
		}
	}
	edicoes := make([]edicaoXMLPagina, 0, len(referencias))
	bytesPreparados := 0
	for _, referencia := range referencias {
		if referencia >= len(blocos) || !ehElementoW(blocos[referencia].nome, "p") {
			return erroReferenciasParagrafos()
		}
		if preflight != nil {
			if err := preflight(original, blocos[referencia]); err != nil {
				return err
			}
		}
		edicao, err := prepararPropriedadeParagrafo(original, blocos[referencia], local, valores)
		if err != nil {
			return err
		}
		// Os alvos são disjuntos: seus textos novos são um piso da saída final.
		// Limitar já aqui evita acumular aliases longos em milhares de buffers.
		if len(edicao.texto) > limiteXMLPagina-bytesPreparados {
			return erroXMLPagina()
		}
		bytesPreparados += len(edicao.texto)
		edicoes = append(edicoes, edicao)
	}
	tamanho := int64(len(original))
	for _, edicao := range edicoes {
		tamanho += int64(len(edicao.texto)) - int64(edicao.fim-edicao.inicio)
	}
	if tamanho > limiteXMLPagina {
		return erroXMLPagina()
	}
	alterado, err := aplicarEdicoesPagina(original, edicoes)
	if err != nil {
		return errors.NovoErroAplicacao("não foi possível aplicar a propriedade do parágrafo")
	}
	if _, err := corpoXMLParagrafos(alterado); err != nil {
		return err
	}
	if bytes.Equal(original, alterado) {
		return nil
	}
	return documento.SubstituirParte(parteDocumentoPrincipal, alterado)
}

func erroReferenciasParagrafos() error {
	return errors.NovoErroValidacao("referencias", "referências de parágrafos inválidas")
}

func valorAlinhamentoXML(alinhamento string) string {
	switch alinhamento {
	case "esquerda":
		return "left"
	case "direita":
		return "right"
	case "centralizado":
		return "center"
	default:
		return "both" // entrada já validada no domínio
	}
}

func corpoXMLParagrafos(dados []byte) (*noXMLPagina, error) {
	if len(dados) == 0 || len(dados) > limiteXMLPagina || !utf8.Valid(dados) || !declaracaoUTF8(dados) {
		return nil, erroXMLPagina()
	}
	raiz, _, err := analisarXMLPagina(dados)
	if err != nil || raiz == nil || !ehElementoW(raiz.nome, "document") {
		return nil, erroXMLPagina()
	}
	corpos := filhosW(raiz, "body")
	if len(corpos) != 1 {
		return nil, erroXMLPagina()
	}
	return corpos[0], nil
}

// CT_PPr: ordem conferida no schema Open XML SDK. O subconjunto selecionado
// rejeita extensões desconhecidas em vez de adivinhar sua posição.
const ordemPropriedadesParagrafo = "|pStyle|keepNext|keepLines|pageBreakBefore|framePr|widowControl|numPr|suppressLineNumbers|pBdr|shd|tabs|suppressAutoHyphens|kinsoku|wordWrap|overflowPunct|topLinePunct|autoSpaceDE|autoSpaceDN|bidi|adjustRightInd|snapToGrid|spacing|ind|contextualSpacing|mirrorIndents|suppressOverlap|jc|textDirection|textAlignment|textboxTightWrap|outlineLvl|divId|cnfStyle|rPr|sectPr|pPrChange|"

func ordemPropriedadeParagrafo(no *noXMLPagina) int {
	if no.nome.Space != espacoNomesW {
		return -1
	}
	return strings.Index(ordemPropriedadesParagrafo, "|"+no.nome.Local+"|")
}

func prepararPropriedadeParagrafo(dados []byte, paragrafo *noXMLPagina, local string, valores map[string]string) (edicaoXMLPagina, error) {
	propriedades := filhosW(paragrafo, "pPr")
	if len(propriedades) > 1 {
		return edicaoXMLPagina{}, erroXMLPagina()
	}
	if len(propriedades) == 0 {
		novo := novaPropriedadeParagrafo(paragrafo, local, valores)
		nome := nomeFilhoParagrafo(paragrafo, "pPr")
		return inserirConteudoParagrafo(dados, paragrafo, "<"+nome+">"+novo+"</"+nome+">"), nil
	}
	ppr := propriedades[0]
	if len(paragrafo.filhos) == 0 || paragrafo.filhos[0] != ppr {
		return edicaoXMLPagina{}, erroXMLPagina()
	}
	var alvo *noXMLPagina
	ultimo := -1
	posicao := ppr.fechoInicio
	rankAlvo := strings.Index(ordemPropriedadesParagrafo, "|"+local+"|")
	for _, filho := range ppr.filhos {
		rank := ordemPropriedadeParagrafo(filho)
		if rank < 0 || rank <= ultimo {
			return edicaoXMLPagina{}, erroXMLPagina()
		}
		ultimo = rank
		if rank > rankAlvo && posicao == ppr.fechoInicio {
			posicao = filho.inicio
		}
		if filho.nome.Local == local {
			alvo = filho
		}
	}
	if alvo != nil {
		if len(alvo.filhos) != 0 || (!alvo.vazio && strings.Trim(string(dados[alvo.fim:alvo.fechoInicio]), " \t\r\n") != "") {
			return edicaoXMLPagina{}, erroXMLPagina()
		}
		for _, atributo := range alvo.atributos {
			if _, alterado := valores[atributo.local]; alterado && atributo.namespace == "" {
				return edicaoXMLPagina{}, erroXMLPagina()
			}
		}
		texto, err := editarTagPagina(dados, alvo, valores)
		if err != nil {
			return edicaoXMLPagina{}, err
		}
		return edicaoXMLPagina{inicio: alvo.inicio, fim: alvo.fim, texto: texto}, nil
	}
	novo := novaPropriedadeParagrafo(ppr, local, valores)
	if ppr.vazio {
		return inserirConteudoParagrafo(dados, ppr, novo), nil
	}
	return edicaoXMLPagina{inicio: posicao, fim: posicao, texto: []byte(novo)}, nil
}

func nomeFilhoParagrafo(pai *noXMLPagina, local string) string {
	prefixo, _ := separarQName(pai.qname)
	if prefixo == "" {
		return local
	}
	return prefixo + ":" + local
}

func novaPropriedadeParagrafo(pai *noXMLPagina, local string, valores map[string]string) string {
	nome := nomeFilhoParagrafo(pai, local)
	prefixo, _ := separarQName(pai.qname)
	declaracao := ""
	if prefixo == "" {
		// Prefixo curto local: não repetir aliases arbitrariamente grandes da raiz.
		prefixo = "fmtw"
		declaracao = " xmlns:fmtw=\"" + espacoNomesW + "\""
	}
	locais := make([]string, 0, len(valores))
	for atributo := range valores {
		locais = append(locais, atributo)
	}
	sort.Strings(locais)
	var atributos strings.Builder
	for _, atributo := range locais {
		atributos.WriteString(" " + prefixo + ":" + atributo + "=\"" + valores[atributo] + "\"")
	}
	return "<" + nome + declaracao + atributos.String() + "/>"
}

func inserirConteudoParagrafo(dados []byte, pai *noXMLPagina, conteudo string) edicaoXMLPagina {
	if pai.vazio {
		abertura := string(dados[pai.inicio:pai.fim-2]) + ">"
		return edicaoXMLPagina{inicio: pai.inicio, fim: pai.fim, texto: []byte(abertura + conteudo + "</" + pai.qname + ">")}
	}
	return edicaoXMLPagina{inicio: pai.fim, fim: pai.fim, texto: []byte(conteudo)}
}
