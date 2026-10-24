package ooxml

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"strconv"
	"strings"

	"github.com/daniel-halos/formatador/internal/domain/cdm"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// espacoNomesW é o namespace do WordprocessingML. Comparar pelo namespace, e
// não pelo prefixo literal "w:", porque o prefixo é escolha de quem gerou o
// arquivo — Word, LibreOffice e Pages não precisam usar o mesmo.
const espacoNomesW = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"

// TipoBlocoBruto distingue as duas formas de bloco de nível superior que o
// corpo de um DOCX admite.
type TipoBlocoBruto int

const (
	TipoBlocoBrutoParagrafo TipoBlocoBruto = iota
	TipoBlocoBrutoTabela
)

// BlocoBruto é o que a leitura do pacote enxerga, antes de qualquer
// interpretação semântica: a forma do nó, o estilo que o próprio documento
// declara, o texto concatenado e a posição ordinal no corpo.
type BlocoBruto struct {
	Tipo          TipoBlocoBruto
	EstiloNomeado string
	Texto         string
	Indice        int
	// RecuoEsquerdaTwips é o recuo à esquerda que o PRÓPRIO parágrafo declara
	// em w:pPr/w:ind, em twips. Zero quando não há recuo declarado e também
	// quando o valor não é um inteiro não negativo de twips — ver
	// recuoEsquerdaTwips.
	RecuoEsquerdaTwips int
}

// ExtrairBlocos lê word/document.xml e devolve os blocos de nível superior na
// ordem do documento.
//
// A leitura prioriza a última substituição aceita, inclusive vazia. O parser
// não escreve XML de volta, pois isso reescreveria os namespaces do pacote.
func (documento *Documento) ExtrairBlocos() ([]BlocoBruto, error) {
	conteudo, err := documento.abrirDocumentoPrincipal()
	if err != nil {
		return nil, err
	}
	defer func() { _ = conteudo.Close() }()

	decodificador := xml.NewDecoder(conteudo)

	for {
		token, err := decodificador.Token()
		if errors.E(err, io.EOF) {
			return nil, errors.NovoErroValidacao("arquivo", "word/document.xml não tem um corpo de documento")
		}
		if err != nil {
			return nil, erroDocumentoMalformado()
		}
		if inicio, ok := token.(xml.StartElement); ok && ehElementoW(inicio.Name, "body") {
			return lerCorpo(decodificador)
		}
	}
}

func (documento *Documento) abrirDocumentoPrincipal() (io.ReadCloser, error) {
	if conteudo, substituida := documento.partesSubstituidas[parteDocumentoPrincipal]; substituida {
		return io.NopCloser(bytes.NewReader(conteudo)), nil
	}
	arquivo := documento.parte(parteDocumentoPrincipal)
	if arquivo == nil {
		return nil, errors.NovoErroValidacao("arquivo", "o pacote não contém word/document.xml")
	}
	conteudo, err := arquivo.Open()
	if err != nil {
		return nil, errors.NovoErroValidacao("arquivo", "não foi possível ler word/document.xml do pacote")
	}
	return conteudo, nil
}

// parte localiza uma entrada do ZIP pelo nome.
func (d *Documento) parte(nome string) *zip.File {
	for _, arquivo := range d.arquivos {
		if arquivo.Name == nome {
			return arquivo
		}
	}
	return nil
}

// lerCorpo consome os filhos de w:body até o fechamento dele, transformando
// cada w:p e w:tbl em um bloco e ignorando o resto (w:sectPr, por exemplo).
func lerCorpo(decodificador *xml.Decoder) ([]BlocoBruto, error) {
	blocos := make([]BlocoBruto, 0, 64)

	for {
		token, err := decodificador.Token()
		if err != nil {
			return nil, erroDocumentoMalformado()
		}

		switch elemento := token.(type) {
		case xml.EndElement:
			if ehElementoW(elemento.Name, "body") {
				return blocos, nil
			}
		case xml.StartElement:
			tipo, ehBloco := tipoDoElemento(elemento.Name)
			if !ehBloco {
				if err := decodificador.Skip(); err != nil {
					return nil, erroDocumentoMalformado()
				}
				continue
			}
			bloco, err := lerBloco(decodificador, tipo, len(blocos))
			if err != nil {
				return nil, err
			}
			blocos = append(blocos, bloco)
		}
	}
}

// lerBloco consome a subárvore de um w:p ou w:tbl já aberto, acumulando o
// texto de todos os w:t na ordem em que aparecem e, para parágrafo, o
// primeiro w:pStyle encontrado.
//
// O texto sai sem separador entre os w:t porque é isso que o OOXML significa:
// os runs de um parágrafo são fatias contíguas da mesma frase, quebradas por
// formatação. Inserir separador criaria caractere que o usuário não escreveu.
func lerBloco(decodificador *xml.Decoder, tipo TipoBlocoBruto, indice int) (BlocoBruto, error) {
	bloco := BlocoBruto{Tipo: tipo, Indice: indice}

	var texto strings.Builder
	var dentroDeTexto bool
	var recuoLido bool
	profundidade := 1

	for profundidade > 0 {
		token, err := decodificador.Token()
		if err != nil {
			return BlocoBruto{}, erroDocumentoMalformado()
		}

		switch elemento := token.(type) {
		case xml.StartElement:
			// w:pPrChange guarda as propriedades ANTIGAS de uma revisão
			// rastreada: o w:ind que vive lá dentro é o recuo REVOGADO, e
			// lê-lo devolveria a propriedade que a revisão já derrubou. A
			// subárvore inteira é descartada antes de contar profundidade —
			// ela nunca contém w:t, então nada de texto se perde.
			if ehElementoW(elemento.Name, "pPrChange") {
				if err := decodificador.Skip(); err != nil {
					return BlocoBruto{}, erroDocumentoMalformado()
				}
				continue
			}
			profundidade++
			switch {
			case ehElementoW(elemento.Name, "t"):
				dentroDeTexto = true
			case tipo == TipoBlocoBrutoParagrafo && bloco.EstiloNomeado == "" && ehElementoW(elemento.Name, "pStyle"):
				bloco.EstiloNomeado = atributoW(elemento, "val")
			// A guarda de tipo é a mesma de w:pStyle, e pelo mesmo motivo: um
			// w:ind de parágrafo dentro de célula contaminaria o BlocoBruto do
			// w:tbl que o contém. O primeiro w:ind vence, como o primeiro
			// w:pStyle.
			case tipo == TipoBlocoBrutoParagrafo && !recuoLido && ehElementoW(elemento.Name, "ind"):
				recuoLido = true
				bloco.RecuoEsquerdaTwips = recuoEsquerdaTwips(elemento)
			}
		case xml.EndElement:
			profundidade--
			if ehElementoW(elemento.Name, "t") {
				dentroDeTexto = false
			}
		case xml.CharData:
			if dentroDeTexto {
				texto.Write(elemento)
			}
		}
	}

	bloco.Texto = texto.String()
	return bloco, nil
}

// recuoEsquerdaTwips lê o recuo à esquerda de um w:ind.
//
// w:start tem precedência sobre w:left: medindo a conversão de um .fodt por
// `soffice --headless`, o LibreOffice emite <w:ind w:start="2268"/> — ler só
// w:left devolveria zero no produtor alvo. Essa precedência vale enquanto o
// w:start for um inteiro: se ele não parseia (ausente, "4cm", 20 dígitos), a
// leitura cai para w:left, porque descartar o único valor utilizável do
// elemento devolveria 0 para um recuo que o documento declarou. w:start que
// parseia mas é negativo continua vencendo, e o saneamento abaixo o zera.
//
// w:leftChars NÃO é lido: a unidade dele é centésimo de caractere, e
// interpretá-lo como twip daria um número sem sentido; parágrafo que só o
// tenha fica com 0, limite declarado.
//
// Saneamento: ST_SignedTwipsMeasure admite negativo e medida universal
// ("4cm"), e o valor vem de arquivo não confiável. Qualquer coisa que não
// seja inteiro não negativo — texto, unidade, overflow de strconv — vira 0.
// Nunca erro, nunca pânico: valor estranho é limite do formato, não falha do
// pacote. O saneamento é para baixo de propósito: devolver um número grande
// para entrada hostil classificaria como citação um bloco que não é.
func recuoEsquerdaTwips(elemento xml.StartElement) int {
	twips, err := strconv.Atoi(atributoW(elemento, "start"))
	if err != nil {
		twips, err = strconv.Atoi(atributoW(elemento, "left"))
	}
	if err != nil || twips < 0 {
		return 0
	}
	return twips
}

func ehElementoW(nome xml.Name, local string) bool {
	return nome.Space == espacoNomesW && nome.Local == local
}

func tipoDoElemento(nome xml.Name) (TipoBlocoBruto, bool) {
	switch {
	case ehElementoW(nome, "p"):
		return TipoBlocoBrutoParagrafo, true
	case ehElementoW(nome, "tbl"):
		return TipoBlocoBrutoTabela, true
	}
	return 0, false
}

func atributoW(elemento xml.StartElement, local string) string {
	for _, atributo := range elemento.Attr {
		if atributo.Name.Local == local && (atributo.Name.Space == espacoNomesW || atributo.Name.Space == "") {
			return atributo.Value
		}
	}
	return ""
}

// erroDocumentoMalformado classifica falha de parsing como culpa do arquivo
// enviado (HTTP 400), não do servidor. A mensagem é fixa de propósito: a do
// encoding/xml cita o trecho que falhou, e trecho é conteúdo do documento do
// usuário — que não pode ir para log nem para resposta (regra 7).
func erroDocumentoMalformado() error {
	return errors.NovoErroValidacao("arquivo", "o XML do documento está malformado")
}

// ---------------------------------------------------------------------------
// Camada 1 da classificação: os estilos nomeados que o próprio DOCX declara.
// ---------------------------------------------------------------------------

// TamanhoMaximoTextoResumo é o teto, em runas, do trecho guardado em
// cdm.Bloco.TextoResumo. O CDM é um índice semântico, não uma cópia do
// documento: o texto íntegro continua no pacote, alcançável por RefXML. O
// valor é o que a F5 manda enviar ao LLM — os primeiros ~200 caracteres de
// cada bloco candidato.
const TamanhoMaximoTextoResumo = 200

// Confiança da camada 1. Estilo nomeado é a evidência mais forte das três
// camadas; a ausência dele não classifica nada, só marca o bloco para a
// heurística da camada 2 olhar.
const (
	confiancaEstiloConhecido = 0.95
	confiancaSemEstilo       = 0.2
)

// confiancaRecuoCitacao é a confiança do bloco classificado como citação só
// pelo recuo. Valor fixo, entre confiancaSemEstilo (0,2) e
// confiancaEstiloConhecido (0,95), e obrigatoriamente ABAIXO de
// config.LimiteConsulta (default 0,7): o fallback só considera candidato o
// bloco com Confianca < LimiteConsulta, e recuo é palpite geométrico que
// precisa seguir aberto ao desempate do LLM — diferente do estilo nomeado,
// que o documento DECLARA.
//
// Esse mesmo palpite é o motivo de o caminho do recuo gravar
// cdm.OrigemHeuristica: confiança é quem decide se vale consultar o LLM,
// origem é quem decide se o motor pode mutar o parágrafo.
const confiancaRecuoCitacao = 0.5

// limiarRecuoCitacaoTwips é o recuo à esquerda acima do qual um parágrafo sem
// outro papel é tratado como citação em bloco. A aritmética: 1440 twips por
// polegada ÷ 2,54 = 566,93 twips por centímetro, logo 567 é 1 cm. O corpo dos
// dois fixtures tem recuo 0, e o recuo de bloco usual (4 cm) fica em 2268,
// bem acima do limiar. A comparação é ESTRITA: exatamente 567 não classifica.
const limiarRecuoCitacaoTwips = 567

const prefixoEstiloSecao = "Heading"

// ClassificarPorEstiloDocx traduz um bloco bruto em entrada do CDM usando o
// que o documento declara — o styleId e a forma do nó — e, só quando isso não
// determina papel nenhum, o recuo do próprio parágrafo. Nunca falha: estilo
// desconhecido vira Paragrafo com confiança baixa, que é um resultado
// legítimo, não um erro.
//
// A ORIGEM é decidida junto do papel, não fixada na camada: estilo nomeado,
// w:tbl e o default de estilo não reconhecido saem com cdm.OrigemEstiloDocx,
// porque em todos esses casos o papel vem do que o documento DECLARA. "Estilo
// não reconhecido" cobre os dois caminhos que caem no default: parágrafo sem
// nenhum w:pStyle e parágrafo com styleId que não está no switch, como
// "MinhaMarca". O caminho do recuo sai com cdm.OrigemHeuristica, porque recuo
// é palpite geométrico — e é a origem que o piso de autorização do plano lê
// para decidir se pode mutar o bloco.
func ClassificarPorEstiloDocx(bruto BlocoBruto) cdm.Bloco {
	papel, confianca, origem := papelPeloEstilo(bruto)

	return cdm.Bloco{
		Papel:       papel,
		TextoResumo: resumirTexto(bruto.Texto),
		Confianca:   confianca,
		Origem:      origem,
		RefXML:      bruto.Indice,
	}
}

func papelPeloEstilo(bruto BlocoBruto) (cdm.Papel, float64, cdm.Origem) {
	// A forma do nó já é evidência direta: w:tbl é uma tabela independente do
	// nome de estilo que a célula por acaso use.
	if bruto.Tipo == TipoBlocoBrutoTabela {
		return cdm.Tabela, confiancaEstiloConhecido, cdm.OrigemEstiloDocx
	}

	papel, confianca := papelDoEstiloNomeado(bruto.EstiloNomeado)

	// Portão DERIVADO, não lista literal de estilos: o sinal geométrico só é
	// consultado quando o estilo não determinou outro papel — ou seja, quando
	// o papel resultante é o default Paragrafo. Derivar evita que o portão
	// deixe de valer em silêncio quando alguém acrescentar estilo ao switch.
	//
	// A confiança continua 0,5, abaixo de config.LimiteConsulta, e
	// OrigemHeuristica não entra nas exclusões de candidato do fallback, então
	// o bloco segue aberto ao desempate do LLM.
	if papel == cdm.Paragrafo && bruto.RecuoEsquerdaTwips > limiarRecuoCitacaoTwips {
		return cdm.Citacao, confiancaRecuoCitacao, cdm.OrigemHeuristica
	}

	return papel, confianca, cdm.OrigemEstiloDocx
}

// papelDoEstiloNomeado traduz o styleId declarado pelo documento. Comparação
// exata e sensível a caixa, no mesmo idioma de Title/Normal/HeadingN.
//
// A lista de estilos de citação é MEDIDA, não intuída: o LibreOffice descarta
// os caracteres não-ASCII do styleId, então "Citação" sai "Citao" e "Título 1"
// sai "Ttulo1user" — "Citacao" e "Citação" nunca chegam dele. Quote e
// IntenseQuote são os styleIds latent-English do Word. "BlockQuote" não entra:
// nenhuma ferramenta conhecida o emite.
//
// A lista é frágil por natureza, porque depende do idioma de quem salvou o
// arquivo; o caminho robusto é o recuo (limiarRecuoCitacaoTwips).
func papelDoEstiloNomeado(estilo string) (cdm.Papel, float64) {
	switch estilo {
	case "Title":
		return cdm.Titulo, confiancaEstiloConhecido
	case "Normal":
		return cdm.Paragrafo, confiancaEstiloConhecido
	case "Citao", "Blocodecitao", "Quote", "IntenseQuote":
		return cdm.Citacao, confiancaEstiloConhecido
	}

	if nivel, ok := nivelDeSecao(estilo); ok {
		return cdm.Secao(nivel), confiancaEstiloConhecido
	}

	return cdm.Paragrafo, confiancaSemEstilo
}

// nivelDeSecao lê HeadingN. Fora da faixa que cdm.Secao aceita, devolve
// false: inventar Secao(7) produziria um bloco que o domínio recusaria.
func nivelDeSecao(estilo string) (int, bool) {
	sufixo, encontrado := strings.CutPrefix(estilo, prefixoEstiloSecao)
	if !encontrado {
		return 0, false
	}
	nivel, err := strconv.Atoi(sufixo)
	if err != nil || nivel < cdm.NivelSecaoMinimo || nivel > cdm.NivelSecaoMaximo {
		return 0, false
	}
	return nivel, true
}

// resumirTexto corta em runas, nunca em bytes: corte por índice de byte
// partiria caractere multibyte ao meio e geraria UTF-8 inválido.
func resumirTexto(texto string) string {
	runas := []rune(texto)
	if len(runas) <= TamanhoMaximoTextoResumo {
		return texto
	}
	return string(runas[:TamanhoMaximoTextoResumo])
}
