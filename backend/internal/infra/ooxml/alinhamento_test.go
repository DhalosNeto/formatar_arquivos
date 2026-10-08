package ooxml

import (
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"testing"

	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAplicarAlinhamentoMapeiaQuatroValores(t *testing.T) {
	casos := []struct{ nome, entrada, xml string }{
		{"esquerda", "esquerda", "left"},
		{"direita", "direita", "right"},
		{"centralizado", "centralizado", "center"},
		{"justificado", "justificado", "both"},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			fonte := []byte(`<w:document xmlns:w="` + espacoNomesW + `"><w:body><w:p><w:r><w:t>texto</w:t></w:r></w:p></w:body></w:document>`)
			doc := abrirParaSubstituicao(t, docxSintetico(t, fonte))
			require.NoError(t, doc.AplicarAlinhamento([]int{0}, caso.entrada))
			saida := lerParte(t, salvarSubstituicao(t, doc), "word/document.xml")
			conferirJc(t, saida, caso.xml, 1)
		})
	}
}

func alinhamentoFixture(xmlCorpo string) []byte {
	return []byte(`<w:document xmlns:w="` + espacoNomesW + `"><w:body>` + xmlCorpo + `</w:body></w:document>`)
}

func conferirJc(t *testing.T, conteudo []byte, alinhamento string, quantidade int) {
	t.Helper()
	valores := valoresJc(t, conteudo)
	assert.Len(t, valores, quantidade)
	for _, valor := range valores {
		assert.Equal(t, alinhamento, valor)
	}
}

func valoresJc(t *testing.T, conteudo []byte) []string {
	t.Helper()
	decodificador := xml.NewDecoder(bytes.NewReader(conteudo))
	valoresEncontrados := []string{}
	for {
		token, err := decodificador.Token()
		if errors.E(err, io.EOF) {
			break
		}
		require.NoError(t, err, "XML resultante deve ser válido")
		inicio, ok := token.(xml.StartElement)
		if !ok || inicio.Name.Local != "jc" {
			continue
		}
		require.Equal(t, espacoNomesW, inicio.Name.Space, "QName de jc")
		quantidadeValores := 0
		for _, atributo := range inicio.Attr {
			if atributo.Name.Local == "val" && atributo.Name.Space == espacoNomesW {
				quantidadeValores++
				valoresEncontrados = append(valoresEncontrados, atributo.Value)
			}
		}
		assert.Equal(t, 1, quantidadeValores, "jc deve ter exatamente um w:val")
	}
	return valoresEncontrados
}

func TestAplicarAlinhamentoResolveOrdinalDeParagrafosETabelas(t *testing.T) {
	fonte := alinhamentoFixture(`<w:p><w:r><w:t>primeiro</w:t></w:r></w:p><w:sectPr/><w:bookmarkStart w:id="1" w:name="x"/><w:tbl><w:tr><w:tc><w:p/></w:tc></w:tr></w:tbl><w:p><w:r><w:t>selecionado</w:t></w:r></w:p><w:p><w:pPr><w:jc w:val="left"/></w:pPr><w:r><w:t>preservado</w:t></w:r></w:p>`)
	doc := abrirParaSubstituicao(t, docxSintetico(t, fonte))
	require.NoError(t, doc.AplicarAlinhamento([]int{2, 0}, "centralizado"))
	saida := lerParte(t, salvarSubstituicao(t, doc), "word/document.xml")
	assert.Equal(t, []string{"center", "center", "left"}, valoresJc(t, saida))
	texto := string(saida)
	assert.Contains(t, texto, `<w:sectPr/>`)
	assert.Contains(t, texto, `<w:bookmarkStart w:id="1" w:name="x"/>`)
	assert.Contains(t, texto, `<w:tbl>`)
	assert.Contains(t, texto, `selecionado`)
	assert.Contains(t, texto, `preservado`)
	assert.Contains(t, texto, `<w:jc w:val="left"/>`)
}

func TestAplicarAlinhamentoCriaPPrEJcNaOrdemECriaExpansao(t *testing.T) {
	fonte := alinhamentoFixture(`<w:p><w:r><w:t>sem propriedades</w:t></w:r></w:p><w:p><w:pPr/><w:r><w:t>vazio</w:t></w:r></w:p><w:p><w:pPr><w:keepNext/><w:spacing/><w:ind/></w:pPr></w:p>`)
	doc := abrirParaSubstituicao(t, docxSintetico(t, fonte))
	require.NoError(t, doc.AplicarAlinhamento([]int{0, 1, 2}, "direita"))
	saida := string(lerParte(t, salvarSubstituicao(t, doc), "word/document.xml"))
	assert.Contains(t, saida, `<w:p><w:pPr><w:jc w:val="right"/></w:pPr><w:r>`)
	assert.Contains(t, saida, `<w:pPr><w:jc w:val="right"/></w:pPr>`)
	inicioTerceiroPPr := strings.Index(saida, `<w:keepNext/>`)
	require.NotEqual(t, -1, inicioTerceiroPPr, "pré-condição: terceiro pPr presente")
	fimTerceiroPPr := strings.Index(saida[inicioTerceiroPPr:], `</w:pPr>`)
	require.NotEqual(t, -1, fimTerceiroPPr, "pré-condição: fechamento do terceiro pPr presente")
	terceiroPPr := saida[inicioTerceiroPPr : inicioTerceiroPPr+fimTerceiroPPr]
	assert.Less(t, strings.Index(terceiroPPr, `<w:spacing/>`), strings.Index(terceiroPPr, `<w:ind/>`))
	assert.Less(t, strings.Index(terceiroPPr, `<w:ind/>`), strings.Index(terceiroPPr, `<w:jc w:val="right"/>`))
	conferirJc(t, []byte(saida), "right", 3)
}

func TestAplicarAlinhamentoPreservaNamespaceHistoricoTextoEPartes(t *testing.T) {
	fonte := []byte("\xef\xbb\xbf" + `<?xml version="1.0" encoding="UTF-8"?><document xmlns="` + espacoNomesW + `" xmlns:a="` + espacoNomesW + `"><body><p><pPr><pStyle a:val="Corpo"/><keepNext/><spacing/><ind/><jc a:val="left"/><rPr><b/></rPr><pPrChange><pPr><jc a:val="right"/></pPr></pPrChange></pPr><r><t xml:space="preserve"> texto 😀 café 𠀀 </t></r></p></body></document>`)
	textos := extrairTextosWT(t, fonte)
	require.Equal(t, []string{" texto 😀 café 𠀀 "}, textos, "fixture deve ter texto Unicode, NFD e espaços")
	original := montarZip(t, append(partesBaseDocx(fonte), entradaZip{nome: "word/media/imagem.bin", conteudo: []byte{0, 255, 17}, metodo: 0}))
	doc := abrirParaSubstituicao(t, original)
	require.NoError(t, doc.AplicarAlinhamento([]int{0}, "centralizado"))
	saida := salvarSubstituicao(t, doc)
	xmlSaida := lerParte(t, saida, "word/document.xml")
	require.True(t, bytes.HasPrefix(xmlSaida, []byte{0xef, 0xbb, 0xbf}), "BOM presente na entrada")
	assert.Equal(t, []rune(strings.Join(textos, "")), []rune(strings.Join(extrairTextosWT(t, xmlSaida), "")))
	assert.Contains(t, string(xmlSaida), `<pPrChange><pPr><jc a:val="right"/></pPr></pPrChange>`)
	assert.Contains(t, string(xmlSaida), `<pStyle a:val="Corpo"/>`)
	assert.Contains(t, string(xmlSaida), `<jc a:val="center"/>`)
	for _, parte := range listarPartes(t, original) {
		if parte != "word/document.xml" {
			assert.Equal(t, lerParte(t, original, parte), lerParte(t, saida, parte), "parte não-alvo %s", parte)
		}
	}
	assert.Equal(t, []string{"center", "right"}, valoresJc(t, xmlSaida), "jc atual muda e jc do histórico permanece")
}

func TestAplicarAlinhamentoPrefixoAlternativoENamespaceDeclaradoEntreIrmaos(t *testing.T) {
	fonte := []byte(`<x:document xmlns:x="` + espacoNomesW + `"><x:body><x:p xmlns:y="` + espacoNomesW + `"><y:pPr><y:jc y:val="left"/></y:pPr></x:p><x:p><x:pPr/></x:p></x:body></x:document>`)
	blocos, err := abrirParaSubstituicao(t, docxSintetico(t, fonte)).ExtrairBlocos()
	require.NoError(t, err, "pré-condição: prefixo alternativo com declaração local é XML WordprocessingML válido")
	require.Len(t, blocos, 2, "pré-condição: os dois parágrafos diretos da fixture são extraídos")
	require.Equal(t, []int{0, 1}, []int{blocos[0].Indice, blocos[1].Indice})
	doc := abrirParaSubstituicao(t, docxSintetico(t, fonte))
	require.NoError(t, doc.AplicarAlinhamento([]int{1, 0}, "direita"))
	saida := lerParte(t, salvarSubstituicao(t, doc), "word/document.xml")
	assert.Equal(t, []string{"right", "right"}, valoresJc(t, saida), "comparar QName, sem depender do prefixo de saída")

	// A declaração de y no primeiro irmão não pode autorizar uso no segundo.
	semDeclaracao := bytes.Replace(fonte, []byte(`<x:p><x:pPr/></x:p>`), []byte(`<x:p><y:pPr/></x:p>`), 1)
	require.NotEqual(t, fonte, semDeclaracao, "pré-condição: fixture adversarial mudou")
	invalido := abrirParaSubstituicao(t, docxSintetico(t, fonte))
	require.NoError(t, invalido.SubstituirParte("word/document.xml", semDeclaracao))
	antes := salvarSubstituicao(t, invalido)
	require.Error(t, invalido.AplicarAlinhamento([]int{1}, "direita"))
	assert.Equal(t, antes, salvarSubstituicao(t, invalido), "namespace do irmão não vaza e falha preserva delta")
}

func TestAplicarAlinhamentoPreservaNoOpELeUltimoDelta(t *testing.T) {
	fonte := alinhamentoFixture(`<w:p><w:r><w:t>base</w:t></w:r></w:p>`)
	doc := abrirParaSubstituicao(t, docxSintetico(t, fonte))
	antes := salvarSubstituicao(t, doc)
	require.NoError(t, doc.AplicarAlinhamento(nil, "justificado"))
	require.Equal(t, antes, salvarSubstituicao(t, doc))
	require.NoError(t, doc.AplicarAlinhamento([]int{}, "justificado"))
	require.Equal(t, antes, salvarSubstituicao(t, doc))
	delta := alinhamentoFixture(`<w:p><w:r><w:t>delta 😀</w:t></w:r></w:p>`)
	require.NoError(t, doc.SubstituirParte("word/document.xml", delta))
	require.NoError(t, doc.AplicarAlinhamento([]int{0}, "justificado"))
	saida := lerParte(t, salvarSubstituicao(t, doc), "word/document.xml")
	assert.Contains(t, string(saida), `delta 😀`)
	assert.NotContains(t, string(saida), `>base<`)
	antesRepeticao := salvarSubstituicao(t, doc)
	for range 10 {
		require.NoError(t, doc.AplicarAlinhamento([]int{0}, "justificado"))
	}
	assert.Equal(t, antesRepeticao, salvarSubstituicao(t, doc))
}

func TestAplicarAlinhamentoRejeitaEntradasSemAlterarDocumento(t *testing.T) {
	casos := []struct {
		nome        string
		indices     []int
		alinhamento string
		xml         string
	}{
		{"negativa", []int{-1}, "esquerda", `<w:p/>`},
		{"duplicada", []int{0, 0}, "esquerda", `<w:p/>`},
		{"acima do máximo", make([]int, 100001), "esquerda", `<w:p/>`},
		{"tabela", []int{0}, "esquerda", `<w:tbl/>`},
		{"fora do corpo", []int{1}, "esquerda", `<w:p/>`},
		{"alinhamento inválido", []int{0}, "Esquerda", `<w:p/>`},
		{"segundo alvo inválido", []int{0, 1}, "esquerda", `<w:p/><w:tbl/>`},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			fonte := alinhamentoFixture(caso.xml)
			doc := abrirParaSubstituicao(t, docxSintetico(t, fonte))
			require.NoError(t, doc.SubstituirParte("word/document.xml", fonte), "pré-condição: delta XML válido e existente deve ser aceito")
			antes := salvarSubstituicao(t, doc)
			err := doc.AplicarAlinhamento(caso.indices, caso.alinhamento)
			require.Error(t, err, "fixture deve alcançar a rejeição indicada pelo caso")
			assert.Equal(t, antes, salvarSubstituicao(t, doc), "rejeição preserva delta anterior")
		})
	}
	var nulo *Documento
	require.Error(t, nulo.AplicarAlinhamento(nil, "esquerda"))
	validacao := abrirParaSubstituicao(t, docxSintetico(t, alinhamentoFixture(`<w:p/>`)))
	antesValidacao := salvarSubstituicao(t, validacao)
	require.Error(t, validacao.AplicarAlinhamento(nil, "Esquerda"), "referências vazias não dispensam validação do alinhamento")
	assert.Equal(t, antesValidacao, salvarSubstituicao(t, validacao))
}

func TestAplicarAlinhamentoRejeitaEstruturasAmbiguasAtomicamente(t *testing.T) {
	casos := []struct {
		nome, xml         string
		documentoCompleto bool
	}{
		{"pPr duplicado", `<w:p><w:pPr/><w:pPr/></w:p>`, false},
		{"pPr não primeiro", `<w:p><w:r/><w:pPr/></w:p>`, false},
		{"jc duplicado", `<w:p><w:pPr><w:jc/><w:jc/></w:pPr></w:p>`, false},
		{"filho desconhecido", `<w:p><w:pPr><w:unknown/></w:pPr></w:p>`, false},
		{"filho fora de ordem", `<w:p><w:pPr><w:ind/><w:spacing/></w:pPr></w:p>`, false},
		{"jc com elemento", `<w:p><w:pPr><w:jc><w:b/></w:jc></w:pPr></w:p>`, false},
		{"jc com texto", `<w:p><w:pPr><w:jc>texto</w:jc></w:pPr></w:p>`, false},
		{"jc com entidade", `<w:p><w:pPr><w:jc>&#32;</w:jc></w:pPr></w:p>`, false},
		{"jc com comentário", `<w:p><w:pPr><w:jc><!--c--></w:jc></w:pPr></w:p>`, false},
		{"val sem namespace", `<w:p><w:pPr><w:jc val="left"/></w:pPr></w:p>`, false},
		{"Strict", `<s:document xmlns:s="http://purl.oclc.org/ooxml/wordprocessingml/main"><s:body><s:p/></s:body></s:document>`, true},
		{"DTD", `<!DOCTYPE w:document [<!ENTITY x "x">]><w:document xmlns:w="` + espacoNomesW + `"><w:body><w:p><w:t>&x;</w:t></w:p></w:body></w:document>`, true},
		{"XML truncado", `<w:document xmlns:w="` + espacoNomesW + `"><w:body><w:p>`, true},
		{"namespace indefinido", `<w:document><w:body><w:p/></w:body></w:document>`, true},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			fonte := alinhamentoFixture(caso.xml)
			if caso.documentoCompleto {
				fonte = []byte(caso.xml)
			}
			doc := abrirParaSubstituicao(t, docxSintetico(t, alinhamentoFixture(`<w:p/>`)))
			require.NoError(t, doc.SubstituirParte("word/document.xml", fonte), "pré-condição: delta malformado estruturalmente, mas opaco deve ser aceito")
			antes := salvarSubstituicao(t, doc)
			err := doc.AplicarAlinhamento([]int{0}, "centralizado")
			require.Error(t, err)
			assert.Equal(t, antes, salvarSubstituicao(t, doc))
			assert.NotContains(t, err.Error(), "w:pPr")
		})
	}
}

func TestAplicarAlinhamentoPermiteWhitespaceXMLLiteralEmJc(t *testing.T) {
	for _, branco := range []string{"", " ", "\t", "\r\n"} {
		fonte := alinhamentoFixture(`<w:p><w:pPr><w:jc>` + branco + `</w:jc></w:pPr></w:p>`)
		doc := abrirParaSubstituicao(t, docxSintetico(t, fonte))
		require.NoError(t, doc.AplicarAlinhamento([]int{0}, "centralizado"))
		conferirJc(t, lerParte(t, salvarSubstituicao(t, doc), "word/document.xml"), "center", 1)
	}
}

// Sequencial: o XML se aproxima do teto de 32 MiB e o caso retém um único delta grande.
func TestAplicarAlinhamentoRejeitaExpansaoAcima32MiB(t *testing.T) {
	const limite = 32 << 20
	prefixo := `<w:document xmlns:w="` + espacoNomesW + `"><w:body><w:p><w:r><w:t>`
	sufixo := `</w:t></w:r></w:p></w:body></w:document>`
	fonte := []byte(prefixo + strings.Repeat("x", limite-len(prefixo)-len(sufixo)) + sufixo)
	require.Equal(t, limite, len(fonte), "fixture deve alcançar exatamente o limite XML declarado")
	doc := abrirParaSubstituicao(t, docxSintetico(t, alinhamentoFixture(`<w:p/>`)))
	require.NoError(t, doc.SubstituirParte("word/document.xml", fonte), "pré-condição: SubstituirParte deve aceitar o XML no limite")
	antes := salvarSubstituicao(t, doc)
	err := doc.AplicarAlinhamento([]int{0}, "esquerda")
	require.Error(t, err, "inserir pPr/jc excede limite de saída")
	assert.Equal(t, antes, salvarSubstituicao(t, doc), "limite preserva delta grande anterior")
}

func TestAplicarAlinhamentoRejeitaReceptorNuloEErroTipado(t *testing.T) {
	var doc *Documento
	err := doc.AplicarAlinhamento([]int{0}, "esquerda")
	require.Error(t, err)
	var argumento *errors.ErroArgumentoNulo
	assert.ErrorAs(t, err, &argumento)
}
