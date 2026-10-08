package ooxml

import (
	"bytes"
	"io"
	"strconv"
	"strings"

	"github.com/daniel-halos/formatador/internal/domain/ruleset"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// AplicarTipografia grava fonte e tamanho diretamente nos runs de texto dos
// parágrafos indicados pelos ordinais de ExtrairBlocos.
func (documento *Documento) AplicarTipografia(referencias []int, fonte string, tamanhoPT float64) error {
	if documento == nil {
		return errors.NovoErroArgumentoNulo("documento")
	}
	if err := ruleset.ValidarFonteCorpo(fonte); err != nil {
		return err
	}
	if err := ruleset.ValidarTamanhoCorpoPT(tamanhoPT); err != nil {
		return err
	}
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
	tamanho, _ := vo.PontosParaMeiosPontos(tamanhoPT)
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
	var edicoes []edicaoXMLPagina
	bytesPreparados := 0
	for _, referencia := range referencias {
		if referencia >= len(blocos) || !ehElementoW(blocos[referencia].nome, "p") {
			return erroReferenciasParagrafos()
		}
		paragrafo := blocos[referencia]
		if temTextoEmWrapper(paragrafo, paragrafo) {
			return erroXMLPagina()
		}
		for _, run := range filhosW(paragrafo, "r") {
			if len(filhosW(run, "t")) == 0 {
				continue
			}
			edicao, err := prepararTipografiaRun(original, run, fonte, strconv.Itoa(tamanho))
			if err != nil {
				return err
			}
			if len(edicao.texto) > limiteXMLPagina-bytesPreparados {
				return erroXMLPagina()
			}
			bytesPreparados += len(edicao.texto)
			edicoes = append(edicoes, edicao)
		}
	}
	tamanhoSaida := int64(len(original))
	for _, edicao := range edicoes {
		tamanhoSaida += int64(len(edicao.texto)) - int64(edicao.fim-edicao.inicio)
		if tamanhoSaida > limiteXMLPagina {
			return erroXMLPagina()
		}
	}
	if tamanhoSaida > limiteXMLPagina {
		return erroXMLPagina()
	}
	alterado, err := aplicarEdicoesPagina(original, edicoes)
	if err != nil {
		return erroXMLPagina()
	}
	if _, err := corpoXMLParagrafos(alterado); err != nil {
		return err
	}
	if bytes.Equal(original, alterado) {
		return nil
	}
	return documento.SubstituirParte(parteDocumentoPrincipal, alterado)
}

// Apenas w:t sob w:r filho direto do parágrafo pode receber formatação.
// Histórico de propriedades e texto apagado não são texto corrente.
func temTextoEmWrapper(no, paragrafo *noXMLPagina) bool {
	if no.nome.Space == namespaceWord {
		switch no.nome.Local {
		case "pPrChange", "rPrChange":
			return false
		case "t":
			return no.pai == nil || no.pai.pai != paragrafo || !ehElementoW(no.pai.nome, "r")
		}
	}
	for _, filho := range no.filhos {
		if temTextoEmWrapper(filho, paragrafo) {
			return true
		}
	}
	return false
}

// CT_RPr: subconjunto estrito e em ordem; extensões são recusadas.
const ordemTipografiaRun = "|rStyle|rFonts|b|bCs|i|iCs|caps|smallCaps|strike|dstrike|outline|shadow|emboss|imprint|noProof|snapToGrid|vanish|webHidden|color|spacing|w|kern|position|sz|szCs|highlight|u|effect|bdr|shd|fitText|vertAlign|rtl|cs|em|lang|eastAsianLayout|specVanish|oMath|rPrChange|"

func prepararTipografiaRun(dados []byte, run *noXMLPagina, fonte, tamanho string) (edicaoXMLPagina, error) {
	rprs := filhosW(run, "rPr")
	if len(rprs) > 1 {
		return edicaoXMLPagina{}, erroXMLPagina()
	}
	fonteEscapada := escaparAtributoTipografia(fonte)
	valores := map[string]map[string]string{
		"rFonts": {"ascii": fonteEscapada, "hAnsi": fonteEscapada, "eastAsia": fonteEscapada, "cs": fonteEscapada},
		"sz":     {"val": tamanho},
		"szCs":   {"val": tamanho},
	}
	if len(rprs) == 0 {
		novo := montarPropriedadesRun(run, valores)
		return inserirConteudoParagrafo(dados, run, novo), nil
	}
	rpr := rprs[0]
	if len(run.filhos) == 0 || run.filhos[0] != rpr {
		return edicaoXMLPagina{}, erroXMLPagina()
	}
	if rpr.vazio {
		novo := montarFilhosTipografia(rpr, valores)
		return inserirConteudoParagrafo(dados, rpr, novo), nil
	}
	existentes := make(map[string]*noXMLPagina)
	ultimo := -1
	fimAnterior := rpr.fim
	for _, filho := range rpr.filhos {
		if strings.Trim(string(dados[fimAnterior:filho.inicio]), " \t\r\n") != "" {
			return edicaoXMLPagina{}, erroXMLPagina()
		}
		fimAnterior = filho.fechoFim
		if filho.nome.Space != namespaceWord {
			return edicaoXMLPagina{}, erroXMLPagina()
		}
		rank := strings.Index(ordemTipografiaRun, "|"+filho.nome.Local+"|")
		if rank < 0 || rank <= ultimo {
			return edicaoXMLPagina{}, erroXMLPagina()
		}
		ultimo = rank
		existentes[filho.nome.Local] = filho
	}
	if strings.Trim(string(dados[fimAnterior:rpr.fechoInicio]), " \t\r\n") != "" {
		return edicaoXMLPagina{}, erroXMLPagina()
	}
	var edicoes []edicaoXMLPagina
	for _, local := range []string{"rFonts", "sz", "szCs"} {
		if filho := existentes[local]; filho != nil {
			if err := validarAlvoTipografia(dados, filho); err != nil {
				return edicaoXMLPagina{}, err
			}
			texto, err := editarTagPagina(dados, filho, valores[local])
			if err != nil {
				return edicaoXMLPagina{}, err
			}
			edicoes = append(edicoes, edicaoXMLPagina{inicio: filho.inicio - rpr.inicio, fim: filho.fim - rpr.inicio, texto: texto})
			continue
		}
		posicao := rpr.fechoInicio
		rankAlvo := strings.Index(ordemTipografiaRun, "|"+local+"|")
		for _, filho := range rpr.filhos {
			if strings.Index(ordemTipografiaRun, "|"+filho.nome.Local+"|") > rankAlvo {
				posicao = filho.inicio
				break
			}
		}
		novo := novaPropriedadeParagrafo(rpr, local, valores[local])
		edicoes = append(edicoes, edicaoXMLPagina{inicio: posicao - rpr.inicio, fim: posicao - rpr.inicio, texto: []byte(novo)})
	}
	texto, err := aplicarEdicoesPagina(dados[rpr.inicio:rpr.fechoFim], edicoes)
	if err != nil {
		return edicaoXMLPagina{}, erroXMLPagina()
	}
	return edicaoXMLPagina{inicio: rpr.inicio, fim: rpr.fechoFim, texto: texto}, nil
}

func validarAlvoTipografia(dados []byte, filho *noXMLPagina) error {
	if len(filho.filhos) != 0 || (!filho.vazio && strings.Trim(string(dados[filho.fim:filho.fechoInicio]), " \t\r\n") != "") {
		return erroXMLPagina()
	}
	for _, atributo := range filho.atributos {
		if atributo.namespace != "" && atributo.namespace != namespaceWord {
			continue
		}
		switch filho.nome.Local {
		case "rFonts":
			switch atributo.local {
			case "ascii", "hAnsi", "eastAsia", "cs":
				if atributo.namespace == "" {
					return erroXMLPagina()
				}
			case "asciiTheme", "hAnsiTheme", "eastAsiaTheme", "cstheme", "csTheme":
				return erroXMLPagina()
			}
		case "sz", "szCs":
			if atributo.local == "val" && atributo.namespace == "" {
				return erroXMLPagina()
			}
		}
	}
	return nil
}

func montarPropriedadesRun(run *noXMLPagina, valores map[string]map[string]string) string {
	nome := nomeFilhoParagrafo(run, "rPr")
	return "<" + nome + ">" + montarFilhosTipografia(run, valores) + "</" + nome + ">"
}

func montarFilhosTipografia(pai *noXMLPagina, valores map[string]map[string]string) string {
	return novaPropriedadeParagrafo(pai, "rFonts", valores["rFonts"]) +
		novaPropriedadeParagrafo(pai, "sz", valores["sz"]) +
		novaPropriedadeParagrafo(pai, "szCs", valores["szCs"])
}

func escaparAtributoTipografia(valor string) string {
	var saida strings.Builder
	for _, caractere := range valor {
		switch caractere {
		case '&':
			saida.WriteString("&amp;")
		case '<':
			saida.WriteString("&lt;")
		case '>':
			saida.WriteString("&gt;")
		case '"':
			saida.WriteString("&quot;")
		case '\'':
			saida.WriteString("&apos;")
		default:
			saida.WriteRune(caractere)
		}
	}
	return saida.String()
}
