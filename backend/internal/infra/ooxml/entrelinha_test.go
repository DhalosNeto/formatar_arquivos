package ooxml

import (
	"bytes"
	"encoding/xml"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAplicarEntrelinhaGravaLineERegraAuto(t *testing.T) {
	for _, caso := range []struct {
		nome     string
		multiplo float64
		line     string
	}{{"simples", 1, "240"}, {"uma e meia", 1.5, "360"}, {"dupla", 2, "480"}, {"arredondamento", 1.001, "240"}} {
		t.Run(caso.nome, func(t *testing.T) {
			doc := abrirParaSubstituicao(t, docxSintetico(t, alinhamentoFixture(`<w:p/>`)))
			require.NoError(t, doc.AplicarEntrelinha([]int{0}, caso.multiplo))
			xmlSaida := lerParte(t, salvarSubstituicao(t, doc), "word/document.xml")
			attrs := atributosEntrelinhaAtual(t, xmlSaida)
			assert.Equal(t, map[string]string{"line": caso.line, "lineRule": "auto"}, attrs)
		})
	}
}

func TestAplicarEntrelinhaPreservaEspacamentoHistoricoTextoEDelta(t *testing.T) {
	fonte := alinhamentoFixture(`<w:p><w:pPr><w:spacing w:before="120" w:after="240" w:beforeLines="2" w:afterLines="3" w:beforeAutospacing="1" w:afterAutospacing="1" w:line="480" w:lineRule="exact"/><w:pPrChange><w:pPr><w:spacing w:line="777" w:lineRule="atLeast"/></w:pPr></w:pPrChange></w:pPr><w:r><w:t xml:space="preserve"> texto 😀 café </w:t></w:r></w:p><w:p><w:r><w:t>inalterado</w:t></w:r></w:p>`)
	original := montarZip(t, append(partesBaseDocx(fonte), entradaZip{nome: "word/media/imagem.bin", conteudo: []byte{0, 255, 17}, metodo: 0}))
	doc := abrirParaSubstituicao(t, original)
	delta := fonte
	require.NoError(t, doc.SubstituirParte("word/document.xml", delta))
	require.NoError(t, doc.AplicarEntrelinha([]int{0}, 1.5))
	saida := salvarSubstituicao(t, doc)
	xmlSaida := lerParte(t, saida, "word/document.xml")
	assert.Equal(t, []rune(strings.Join(extrairTextosWT(t, fonte), "")), []rune(strings.Join(extrairTextosWT(t, xmlSaida), "")))
	texto := string(xmlSaida)
	assert.Contains(t, texto, `w:before="120"`)
	assert.Contains(t, texto, `w:after="240"`)
	assert.Contains(t, texto, `w:beforeLines="2"`)
	assert.Contains(t, texto, `w:afterLines="3"`)
	assert.Contains(t, texto, `w:beforeAutospacing="1"`)
	assert.Contains(t, texto, `w:afterAutospacing="1"`)
	assert.Contains(t, texto, `>inalterado<`)
	assert.Contains(t, texto, `> texto 😀 café </w:t>`)
	for _, parte := range listarPartes(t, original) {
		if parte != "word/document.xml" {
			assert.Equal(t, lerParte(t, original, parte), lerParte(t, saida, parte), "parte não-alvo %s", parte)
		}
	}
	antesRepeticao := saida
	for range 10 {
		require.NoError(t, doc.AplicarEntrelinha([]int{0}, 1.5))
	}
	assert.Equal(t, antesRepeticao, salvarSubstituicao(t, doc))
}

func TestAplicarEntrelinhaInsereSpacingNaOrdemDoPPr(t *testing.T) {
	fonte := alinhamentoFixture(`<w:p><w:pPr><w:snapToGrid/><w:ind w:left="120"/></w:pPr></w:p>`)
	doc := abrirParaSubstituicao(t, docxSintetico(t, fonte))
	require.NoError(t, doc.AplicarEntrelinha([]int{0}, 2))
	saida := string(lerParte(t, salvarSubstituicao(t, doc), "word/document.xml"))
	iSnap := strings.Index(saida, `<w:snapToGrid/>`)
	iSpacing := strings.Index(saida, `<w:spacing`)
	iInd := strings.Index(saida, `<w:ind`)
	require.NotEqual(t, -1, iSnap, "pré-condição: snapToGrid presente")
	require.NotEqual(t, -1, iSpacing, "pré-condição: spacing inserido")
	require.NotEqual(t, -1, iInd, "pré-condição: ind presente")
	assert.Less(t, iSnap, iSpacing)
	assert.Less(t, iSpacing, iInd)
}

func TestAplicarEntrelinhaAceitaNamespacesBOMEPreservacaoLexical(t *testing.T) {
	casos := []struct {
		nome  string
		fonte []byte
	}{
		{"prefixo alternativo com BOM", []byte("\xef\xbb\xbf<?xml version=\"1.0\" encoding=\"UTF-8\"?><x:document xmlns:x=\"" + espacoNomesW + "\"><x:body><x:p><x:pPr><x:spacing x:before=\"10\" x:after=\"20\"> \t\r\n </x:spacing></x:pPr></x:p></x:body></x:document>")},
		{"namespace default", []byte(`<document xmlns="` + espacoNomesW + `"><body><p><pPr><spacing before="10" after="20"> ` + "\t\r\n" + ` </spacing></pPr></p></body></document>`)},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			doc := abrirParaSubstituicao(t, docxSintetico(t, caso.fonte))
			require.NoError(t, doc.AplicarEntrelinha([]int{0}, 1.5))
			saida := lerParte(t, salvarSubstituicao(t, doc), "word/document.xml")
			if bytes.HasPrefix(caso.fonte, []byte{0xef, 0xbb, 0xbf}) {
				assert.True(t, bytes.HasPrefix(saida, []byte{0xef, 0xbb, 0xbf}), "BOM deve permanecer")
			}
			attrs := atributosEntrelinhaAtual(t, saida)
			assert.Equal(t, map[string]string{"line": "360", "lineRule": "auto"}, attrs)
			assert.Contains(t, string(saida), `before="10"`)
			assert.Contains(t, string(saida), `after="20"`)
		})
	}
}

func TestAplicarEntrelinhaSelecionaOrdinaisForaDeOrdemEPreservanoOp(t *testing.T) {
	fonte := alinhamentoFixture(`<w:p><w:pPr><w:spacing w:before="1"/></w:pPr></w:p><w:tbl><w:tr><w:tc><w:p/></w:tc></w:tr></w:tbl><w:p><w:pPr><w:spacing w:after="2"/></w:pPr></w:p>`)
	doc := abrirParaSubstituicao(t, docxSintetico(t, fonte))
	require.NoError(t, doc.AplicarEntrelinha([]int{2, 0}, 1.5))
	saida := lerParte(t, salvarSubstituicao(t, doc), "word/document.xml")
	assert.Equal(t, []map[string]string{{"line": "360", "lineRule": "auto"}, {"line": "360", "lineRule": "auto"}}, todosAtributosEntrelinha(t, saida))
	assert.Contains(t, string(saida), `<w:tbl><w:tr><w:tc><w:p/></w:tc></w:tr></w:tbl>`, "parágrafo dentro da tabela permanece intacto")
	require.NoError(t, doc.SubstituirParte("word/document.xml", []byte(`<quebrado`)))
	delta := salvarSubstituicao(t, doc)
	require.NoError(t, doc.AplicarEntrelinha(nil, 1.5))
	assert.Equal(t, delta, salvarSubstituicao(t, doc), "no-op sem I/O mantém delta opaco")
	err := doc.AplicarEntrelinha([]int{}, 0)
	require.Error(t, err, "validar medida precede no-op")
	assert.Equal(t, delta, salvarSubstituicao(t, doc))
}

func TestAplicarEntrelinhaPreservaHistoricoAtualizandoDeltaPosterior(t *testing.T) {
	doc := abrirParaSubstituicao(t, docxSintetico(t, alinhamentoFixture(`<w:p/>`)))
	delta := alinhamentoFixture(`<w:p><w:pPr><w:spacing w:before="11" w:after="22" w:line="240" w:lineRule="atLeast"/><w:pPrChange w:id="7"><w:pPr><w:spacing w:before="33" w:after="44" w:line="777" w:lineRule="exact"/></w:pPr></w:pPrChange></w:pPr><w:r><w:t>delta posterior</w:t></w:r></w:p>`)
	require.NoError(t, doc.SubstituirParte("word/document.xml", delta))
	require.NoError(t, doc.AplicarEntrelinha([]int{0}, 2))
	saida := string(lerParte(t, salvarSubstituicao(t, doc), "word/document.xml"))
	assert.Contains(t, saida, `w:before="11"`)
	assert.Contains(t, saida, `w:after="22"`)
	assert.Contains(t, saida, `w:line="480" w:lineRule="auto"`)
	assert.Contains(t, saida, `<w:pPrChange w:id="7"><w:pPr><w:spacing w:before="33" w:after="44" w:line="777" w:lineRule="exact"/></w:pPr></w:pPrChange>`)
	assert.Contains(t, saida, `delta posterior`)
}

func TestAplicarEntrelinhaRejeitaEstruturasDeSpacingAmbiguas(t *testing.T) {
	casos := []struct{ nome, xml string }{
		{"spacing duplicado", `<w:p><w:pPr><w:spacing/><w:spacing/></w:pPr></w:p>`},
		{"filho desconhecido", `<w:p><w:pPr><w:unknown/></w:pPr></w:p>`},
		{"filhos fora de ordem", `<w:p><w:pPr><w:ind/><w:spacing/></w:pPr></w:p>`},
		{"entidade em spacing", `<w:p><w:pPr><w:spacing>&#32;</w:spacing></w:pPr></w:p>`},
		{"comentário em spacing", `<w:p><w:pPr><w:spacing><!-- comentário --></w:spacing></w:pPr></w:p>`},
		{"atributo line duplicado", `<w:p><w:pPr><w:spacing w:line="1" w:line="2"/></w:pPr></w:p>`},
		{"pPr duplicado", `<w:p><w:pPr/><w:pPr/></w:p>`}, {"pPr não primeiro", `<w:p><w:r/><w:pPr/></w:p>`},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			fonte := alinhamentoFixture(caso.xml)
			doc := abrirParaSubstituicao(t, docxSintetico(t, fonte))
			require.NoError(t, doc.SubstituirParte("word/document.xml", fonte), "pré-condição: delta instalado")
			antes := salvarSubstituicao(t, doc)
			err := doc.AplicarEntrelinha([]int{0}, 1.5)
			require.Error(t, err, "fixture alcança rejeição estrutural")
			assert.Equal(t, antes, salvarSubstituicao(t, doc), "rejeição preserva delta")
		})
	}
}

func TestAplicarEntrelinhaPreservaDocumentoEmRejeicoes(t *testing.T) {
	casos := []struct {
		nome  string
		refs  []int
		valor float64
		xml   string
	}{
		{"negativa", []int{-1}, 1.5, `<w:p/>`}, {"duplicada", []int{0, 0}, 1.5, `<w:p/>`}, {"teto de referências", make([]int, 100001), 1.5, `<w:p/>`},
		{"alvo tabela", []int{0}, 1.5, `<w:tbl/>`}, {"segundo alvo inválido", []int{0, 1}, 1.5, `<w:p/><w:tbl/>`},
		{"referência fora do documento", []int{1}, 1.5, `<w:p/>`},
		{"zero", []int{0}, 0, `<w:p/>`}, {"arredonda para zero", []int{0}, .49 / 240, `<w:p/>`}, {"NaN", []int{0}, math.NaN(), `<w:p/>`}, {"infinito", []int{0}, math.Inf(1), `<w:p/>`},
		{"line sem namespace", []int{0}, 1.5, `<w:p><w:pPr><w:spacing line="240"/></w:pPr></w:p>`}, {"lineRule sem namespace", []int{0}, 1.5, `<w:p><w:pPr><w:spacing lineRule="auto"/></w:pPr></w:p>`},
		{"spacing com filho", []int{0}, 1.5, `<w:p><w:pPr><w:spacing><w:b/></w:spacing></w:pPr></w:p>`}, {"texto em spacing", []int{0}, 1.5, `<w:p><w:pPr><w:spacing>texto</w:spacing></w:pPr></w:p>`},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			fonte := alinhamentoFixture(caso.xml)
			doc := abrirParaSubstituicao(t, docxSintetico(t, fonte))
			require.NoError(t, doc.SubstituirParte("word/document.xml", fonte), "pré-condição: o delta XML da fixture existe")
			antes := salvarSubstituicao(t, doc)
			err := doc.AplicarEntrelinha(caso.refs, caso.valor)
			require.Error(t, err, "caso deve alcançar a guarda pretendida")
			assert.Equal(t, antes, salvarSubstituicao(t, doc), "rejeição preserva o delta anterior")
		})
	}
	var nulo *Documento
	require.Error(t, nulo.AplicarEntrelinha([]int{0}, 1.5))
}

func TestAplicarEntrelinhaExpansaoAcimaDe32MiBPreservaDelta(t *testing.T) {
	fonte := alinhamentoFixture(`<w:p><w:pPr><w:keepNext/></w:pPr></w:p>`)
	doc := abrirParaSubstituicao(t, docxSintetico(t, fonte))
	prefixo := []byte(`<w:document xmlns:w="` + espacoNomesW + `"><w:body><w:p><w:pPr><w:keepNext/></w:pPr><w:r><w:t>`)
	sufixo := []byte(`</w:t></w:r></w:p></w:body></w:document>`)
	grande := append(bytes.Clone(prefixo), bytes.Repeat([]byte("x"), limiteXMLPagina-len(prefixo)-len(sufixo)-1)...)
	grande = append(grande, sufixo...)
	require.Len(t, grande, limiteXMLPagina-1, "pré-condição: entrada abaixo do teto")
	require.NoError(t, doc.SubstituirParte("word/document.xml", grande), "pré-condição: delta válido menor que 32 MiB")
	antes := salvarSubstituicao(t, doc)
	err := doc.AplicarEntrelinha([]int{0}, 1.5)
	require.Error(t, err)
	assert.Equal(t, antes, salvarSubstituicao(t, doc))
}

func TestAplicarEntrelinhaRejeitaCrescimentoPorPrefixoLongoPreservandoDelta(t *testing.T) {
	alias := strings.Repeat("W", 16384)
	var xmlEntrada strings.Builder
	xmlEntrada.WriteString(`<document xmlns="` + espacoNomesW + `" xmlns:` + alias + `="` + espacoNomesW + `"><body>`)
	for range 1100 {
		xmlEntrada.WriteString(`<p><pPr><spacing/></pPr></p>`)
	}
	xmlEntrada.WriteString(`</body></document>`)
	fonte := []byte(xmlEntrada.String())
	require.Less(t, len(fonte), limiteXMLPagina, "pré-condição: XML de entrada pequeno")
	doc := abrirParaSubstituicao(t, docxSintetico(t, alinhamentoFixture(`<w:p/>`)))
	require.NoError(t, doc.SubstituirParte("word/document.xml", fonte))
	antes := salvarSubstituicao(t, doc)
	refs := make([]int, 1100)
	for i := range refs {
		refs[i] = i
	}
	err := doc.AplicarEntrelinha(refs, 1.5)
	require.Error(t, err, "duas attrs de namespace longo por alvo excedem 32 MiB de saída")
	assert.Equal(t, antes, salvarSubstituicao(t, doc), "rejeição de expansão preserva o delta anterior")
}

func atributosEntrelinhaAtual(t *testing.T, conteudo []byte) map[string]string {
	t.Helper()
	dec := xml.NewDecoder(bytes.NewReader(conteudo))
	attrs := map[string]string{}
	historico := 0
	for {
		token, err := dec.Token()
		if errors.E(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		switch elemento := token.(type) {
		case xml.StartElement:
			if elemento.Name.Local == "pPrChange" {
				historico++
			}
			if elemento.Name.Local == "spacing" && historico == 0 {
				for _, attr := range elemento.Attr {
					if attr.Name.Space == espacoNomesW && (attr.Name.Local == "line" || attr.Name.Local == "lineRule") {
						attrs[attr.Name.Local] = attr.Value
					}
				}
			}
		case xml.EndElement:
			if elemento.Name.Local == "pPrChange" {
				historico--
			}
		}
	}
	return attrs
}

func todosAtributosEntrelinha(t *testing.T, conteudo []byte) []map[string]string {
	t.Helper()
	dec := xml.NewDecoder(bytes.NewReader(conteudo))
	var todos []map[string]string
	for {
		token, err := dec.Token()
		if errors.E(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		inicio, ok := token.(xml.StartElement)
		if !ok || inicio.Name.Local != "spacing" {
			continue
		}
		attrs := map[string]string{}
		for _, attr := range inicio.Attr {
			if attr.Name.Space == espacoNomesW && (attr.Name.Local == "line" || attr.Name.Local == "lineRule") {
				attrs[attr.Name.Local] = attr.Value
			}
		}
		todos = append(todos, attrs)
	}
	return todos
}
