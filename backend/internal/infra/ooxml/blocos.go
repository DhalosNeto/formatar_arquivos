package ooxml

import (
	"archive/zip"
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
}

// ExtrairBlocos lê word/document.xml e devolve os blocos de nível superior na
// ordem do documento.
//
// É um caminho paralelo, estritamente de LEITURA: o decoder consome uma cópia
// recém-aberta da entrada do ZIP e nada é escrito de volta em d.arquivos, que
// é o que Salvar recopia. Parsear para ler não pode virar parsear para
// escrever — o encoding/xml reescreveria namespaces e corromperia o pacote.
func (d *Documento) ExtrairBlocos() ([]BlocoBruto, error) {
	arquivo := d.parte(parteDocumentoPrincipal)
	if arquivo == nil {
		return nil, errors.NovoErroValidacao("arquivo", "o pacote não contém word/document.xml")
	}

	conteudo, err := arquivo.Open()
	if err != nil {
		return nil, errors.NovoErroValidacao("arquivo", "não foi possível ler word/document.xml do pacote")
	}
	defer func() { _ = conteudo.Close() }()

	decodificador := xml.NewDecoder(conteudo)

	for {
		token, err := decodificador.Token()
		if err == io.EOF {
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
	profundidade := 1

	for profundidade > 0 {
		token, err := decodificador.Token()
		if err != nil {
			return BlocoBruto{}, erroDocumentoMalformado()
		}

		switch elemento := token.(type) {
		case xml.StartElement:
			profundidade++
			switch {
			case ehElementoW(elemento.Name, "t"):
				dentroDeTexto = true
			case tipo == TipoBlocoBrutoParagrafo && bloco.EstiloNomeado == "" && ehElementoW(elemento.Name, "pStyle"):
				bloco.EstiloNomeado = atributoW(elemento, "val")
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

const prefixoEstiloSecao = "Heading"

// ClassificarPorEstiloDocx traduz um bloco bruto em entrada do CDM usando só
// o estilo que o documento declara. Nunca falha: estilo desconhecido vira
// Paragrafo com confiança baixa, que é um resultado legítimo, não um erro.
func ClassificarPorEstiloDocx(bruto BlocoBruto) cdm.Bloco {
	papel, confianca := papelPeloEstilo(bruto)

	return cdm.Bloco{
		Papel:       papel,
		TextoResumo: resumirTexto(bruto.Texto),
		Confianca:   confianca,
		Origem:      cdm.OrigemEstiloDocx,
		RefXML:      bruto.Indice,
	}
}

func papelPeloEstilo(bruto BlocoBruto) (cdm.Papel, float64) {
	// A forma do nó já é evidência direta: w:tbl é uma tabela independente do
	// nome de estilo que a célula por acaso use.
	if bruto.Tipo == TipoBlocoBrutoTabela {
		return cdm.Tabela, confiancaEstiloConhecido
	}

	switch bruto.EstiloNomeado {
	case "Title":
		return cdm.Titulo, confiancaEstiloConhecido
	case "Normal":
		return cdm.Paragrafo, confiancaEstiloConhecido
	}

	if nivel, ok := nivelDeSecao(bruto.EstiloNomeado); ok {
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
