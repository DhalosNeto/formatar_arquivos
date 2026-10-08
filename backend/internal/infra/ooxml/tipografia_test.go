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

func TestAplicarTipografiaFormataRunsDiretosEPreservaPartes(t *testing.T) {
	fonte := alinhamentoFixture(`<w:p><w:r><w:t xml:space="preserve">  café 😀 café </w:t></w:r><w:r><w:rPr><w:b/><w:i/><w:color w:val="123456"/></w:rPr><w:t>segundo</w:t></w:r><w:r><w:delText>apagado</w:delText></w:r></w:p><w:tbl><w:tr><w:tc><w:p><w:r><w:t>tabela</w:t></w:r></w:p></w:tc></w:tr></w:tbl><w:p><w:r><w:t>alvo</w:t></w:r></w:p>`)
	original := montarZip(t, append(partesBaseDocx(fonte), entradaZip{nome: "word/media/imagem.bin", conteudo: []byte{0, 255, 17}, metodo: 0}))
	doc := abrirParaSubstituicao(t, original)
	require.NoError(t, doc.AplicarTipografia([]int{2, 0}, "A&B <C> \"D\" 'E'", 12))
	saida := salvarSubstituicao(t, doc)
	parte := lerParte(t, saida, "word/document.xml")
	propriedades := propriedadesTipografiaDireta(t, parte)
	require.Len(t, propriedades, 3, "pré-condição: três runs diretos com w:t devem estar presentes")
	for _, p := range propriedades {
		assert.Equal(t, "A&B <C> \"D\" 'E'", p["rFonts.ascii"])
		assert.Equal(t, "A&B <C> \"D\" 'E'", p["rFonts.hAnsi"])
		assert.Equal(t, "A&B <C> \"D\" 'E'", p["rFonts.eastAsia"])
		assert.Equal(t, "A&B <C> \"D\" 'E'", p["rFonts.cs"])
		assert.Equal(t, "24", p["sz.val"])
		assert.Equal(t, "24", p["szCs.val"])
	}
	assert.Contains(t, string(parte), `<w:b/>`)
	assert.Contains(t, string(parte), `<w:i/>`)
	assert.Contains(t, string(parte), `<w:color w:val="123456"/>`)
	assert.Contains(t, string(parte), `xml:space="preserve"`)
	assert.Equal(t, extrairTextosWT(t, fonte), extrairTextosWT(t, parte), "sequência de w:t deve permanecer idêntica")
	for _, nome := range listarPartes(t, original) {
		if nome != "word/document.xml" {
			assert.Equal(t, lerParte(t, original, nome), lerParte(t, saida, nome), "parte não alvo %s", nome)
		}
	}
	antesRepeticao := saida
	require.NoError(t, doc.AplicarTipografia([]int{0, 2}, "A&B <C> \"D\" 'E'", 12))
	assert.Equal(t, antesRepeticao, salvarSubstituicao(t, doc), "repetição deve ser byte-idempotente")
}

func TestAplicarTipografiaPreencheRPrAusenteOuVazio(t *testing.T) {
	for _, tc := range []struct{ nome, run string }{{"ausente", `<w:r><w:t>x</w:t></w:r>`}, {"vazio", `<w:r><w:rPr></w:rPr><w:t>x</w:t></w:r>`}} {
		t.Run(tc.nome, func(t *testing.T) {
			doc := abrirParaSubstituicao(t, docxSintetico(t, alinhamentoFixture(`<w:p>`+tc.run+`</w:p>`)))
			require.NoError(t, doc.AplicarTipografia([]int{0}, "Fonte", 12))
			p := propriedadesTipografiaDireta(t, lerParte(t, salvarSubstituicao(t, doc), "word/document.xml"))
			require.Len(t, p, 1)
			assert.Equal(t, "Fonte", p[0]["rFonts.ascii"])
			assert.Equal(t, "24", p[0]["sz.val"])
		})
	}
}

func TestAplicarTipografiaRunSemTextoAtualNaoCriaPropriedades(t *testing.T) {
	fonte := alinhamentoFixture(`<w:p><w:r><w:delText>apagado</w:delText></w:r></w:p>`)
	original := docxSintetico(t, fonte)
	doc := abrirParaSubstituicao(t, original)
	require.NoError(t, doc.AplicarTipografia([]int{0}, "Fonte", 12))
	saida := salvarSubstituicao(t, doc)
	assert.Equal(t, lerParte(t, original, "word/document.xml"), lerParte(t, saida, "word/document.xml"))
	assert.Equal(t, original, saida, "parágrafo sem w:t atual deve permanecer no-op integral")
}

func TestAplicarTipografiaGravaValoresFracionariosESubstituiAlvos(t *testing.T) {
	fonte := alinhamentoFixture(`<w:p><w:r><w:rPr><w:rStyle w:val="Corpo"/><w:rFonts w:ascii="Old" w:hAnsi="Old"/><w:b/><w:sz w:val="20"/><w:szCs w:val="20"/></w:rPr><w:t>texto</w:t></w:r></w:p>`)
	doc := abrirParaSubstituicao(t, docxSintetico(t, fonte))
	require.NoError(t, doc.AplicarTipografia([]int{0}, "Nova", 10.25))
	p := propriedadesTipografiaDireta(t, lerParte(t, salvarSubstituicao(t, doc), "word/document.xml"))
	require.Len(t, p, 1)
	assert.Equal(t, map[string]string{"rFonts.ascii": "Nova", "rFonts.hAnsi": "Nova", "rFonts.eastAsia": "Nova", "rFonts.cs": "Nova", "sz.val": "21", "szCs.val": "21"}, p[0])
	saida := string(lerParte(t, salvarSubstituicao(t, doc), "word/document.xml"))
	require.NotEqual(t, -1, strings.Index(saida, `<w:rStyle w:val="Corpo"/>`), "pré-condição: estilo de run preservado")
	require.NotEqual(t, -1, strings.Index(saida, `<w:b/>`), "pré-condição: negrito preservado")
	assert.Less(t, strings.Index(saida, `<w:rStyle`), strings.Index(saida, `<w:rFonts`))
	assert.Less(t, strings.Index(saida, `<w:rFonts`), strings.Index(saida, `<w:b/>`))
	assert.Less(t, strings.Index(saida, `<w:b/>`), strings.Index(saida, `<w:sz `))
	assert.Less(t, strings.Index(saida, `<w:sz `), strings.Index(saida, `<w:szCs `))
}

func TestAplicarTipografiaValidaEntradasAntesDeIO(t *testing.T) {
	t.Run("receptor nulo precede valores inválidos", func(t *testing.T) {
		var doc *Documento
		err := doc.AplicarTipografia(nil, "", math.NaN())
		var nulo *errors.ErroArgumentoNulo
		require.ErrorAs(t, err, &nulo)
		assert.Equal(t, "documento", nulo.Argumento)
	})
	t.Run("seleção vazia valida entradas e não lê XML", func(t *testing.T) {
		doc := abrirParaSubstituicao(t, docxSintetico(t, alinhamentoFixture(`<w:p/>`)))
		malformado := []byte(`<w:document incompleto`)
		require.NoError(t, doc.SubstituirParte("word/document.xml", malformado), "pré-condição: delta opaco instalado")
		antes := salvarSubstituicao(t, doc)
		require.NoError(t, doc.AplicarTipografia([]int{}, "Fonte", 12))
		assert.Equal(t, antes, salvarSubstituicao(t, doc))
		for _, entrada := range []struct {
			fonte   string
			tamanho float64
		}{{"", 12}, {"Fonte", 0}, {"Fonte", math.NaN()}} {
			err := doc.AplicarTipografia(nil, entrada.fonte, entrada.tamanho)
			var validacao *errors.ErroValidacao
			require.ErrorAs(t, err, &validacao)
			assert.Equal(t, antes, salvarSubstituicao(t, doc))
		}
	})
	for _, tc := range []struct {
		nome, fonte string
		tamanho     float64
	}{
		{"controle", "F\x01", 12}, {"UTF-8 inválido", string([]byte{0xff}), 12}, {"U+FFFE", "F\ufffe", 12}, {"U+FFFF", "F\uffff", 12},
		{"fonte extensa", strings.Repeat("f", 101), 12}, {"negativo", "F", -1}, {"arredonda a zero", "F", 0.1},
		{"infinito", "F", math.Inf(1)}, {"overflow", "F", float64(math.MaxInt32)},
	} {
		t.Run(tc.nome, func(t *testing.T) {
			doc := abrirParaSubstituicao(t, docxSintetico(t, []byte(`<w:document`)))
			antes := salvarSubstituicao(t, doc)
			err := doc.AplicarTipografia([]int{0}, tc.fonte, tc.tamanho)
			var validacao *errors.ErroValidacao
			require.ErrorAs(t, err, &validacao, "valor inválido deve ser rejeitado antes de ler o XML malformado")
			assert.Equal(t, antes, salvarSubstituicao(t, doc))
		})
	}
}

func TestAplicarTipografiaRejeitaReferenciasSemDeltaParcial(t *testing.T) {
	for _, tc := range []struct {
		nome string
		refs []int
		xml  string
	}{
		{"negativa", []int{-1}, `<w:p><w:r><w:t>a</w:t></w:r></w:p>`},
		{"duplicada", []int{0, 0}, `<w:p><w:r><w:t>a</w:t></w:r></w:p>`},
		{"tabela conta como ordinal", []int{1}, `<w:p><w:r><w:t>a</w:t></w:r></w:p><w:tbl/>`},
		{"fora do documento", []int{1}, `<w:p><w:r><w:t>a</w:t></w:r></w:p>`},
		{"segundo alvo não é parágrafo", []int{0, 1}, `<w:p><w:r><w:t>a</w:t></w:r></w:p><w:tbl/>`},
		{"segundo run inválido", []int{0}, `<w:p><w:r><w:t>a</w:t></w:r><w:r><w:rPr><w:b/><w:b/></w:rPr><w:t>b</w:t></w:r></w:p>`},
		{"segundo alvo contém wrapper", []int{0, 1}, `<w:p><w:r><w:t>a</w:t></w:r></w:p><w:p><w:hyperlink><w:r><w:t>b</w:t></w:r></w:hyperlink></w:p>`},
	} {
		t.Run(tc.nome, func(t *testing.T) {
			fonte := alinhamentoFixture(tc.xml)
			doc := abrirParaSubstituicao(t, docxSintetico(t, fonte))
			require.NoError(t, doc.SubstituirParte("word/document.xml", fonte), "pré-condição: delta XML válido deve estar instalado")
			antes := salvarSubstituicao(t, doc)
			require.Error(t, doc.AplicarTipografia(tc.refs, "Fonte", 12), "entrada deve atingir a rejeição indicada")
			assert.Equal(t, antes, salvarSubstituicao(t, doc), "lote inválido não deixa edição parcial")
		})
	}
}

func TestAplicarTipografiaRejeitaAmbiguidadeDeAtributosAtomica(t *testing.T) {
	casos := []struct{ nome, rpr string }{
		{"asciiTheme", `<w:rFonts w:asciiTheme="majorHAnsi"/>`}, {"hAnsiTheme", `<w:rFonts w:hAnsiTheme="majorHAnsi"/>`},
		{"eastAsiaTheme", `<w:rFonts w:eastAsiaTheme="majorEastAsia"/>`}, {"cstheme", `<w:rFonts w:cstheme="majorBidi"/>`},
		{"csTheme", `<w:rFonts w:csTheme="majorBidi"/>`}, {"asciiTheme sem namespace", `<w:rFonts asciiTheme="majorHAnsi"/>`}, {"hAnsiTheme sem namespace", `<w:rFonts hAnsiTheme="majorHAnsi"/>`}, {"eastAsiaTheme sem namespace", `<w:rFonts eastAsiaTheme="majorEastAsia"/>`}, {"cstheme sem namespace", `<w:rFonts cstheme="majorBidi"/>`}, {"csTheme sem namespace", `<w:rFonts csTheme="majorBidi"/>`}, {"ascii sem namespace", `<w:rFonts ascii="Old"/>`},
		{"hAnsi sem namespace", `<w:rFonts hAnsi="Old"/>`}, {"eastAsia sem namespace", `<w:rFonts eastAsia="Old"/>`},
		{"cs sem namespace", `<w:rFonts cs="Old"/>`}, {"sz val sem namespace", `<w:sz val="24"/>`},
		{"szCs val sem namespace", `<w:szCs val="24"/>`},
	}
	for _, tc := range casos {
		t.Run(tc.nome, func(t *testing.T) {
			fonte := alinhamentoFixture(`<w:p><w:r><w:rPr>` + tc.rpr + `</w:rPr><w:t>x</w:t></w:r></w:p>`)
			doc := abrirParaSubstituicao(t, docxSintetico(t, fonte))
			antes := salvarSubstituicao(t, doc)
			require.Error(t, doc.AplicarTipografia([]int{0}, "Fonte", 12))
			assert.Equal(t, antes, salvarSubstituicao(t, doc))
		})
	}
}

func TestAplicarTipografiaRejeitaEstruturasDeRPrAmbiguas(t *testing.T) {
	for _, tc := range []struct{ nome, rpr string }{
		{"rPr duplicado", `<w:rPr><w:b/></w:rPr><w:rPr><w:i/></w:rPr>`},
		{"rPr fora da primeira posição", `<w:t>x</w:t><w:rPr><w:b/></w:rPr>`},
		{"filhos fora de ordem", `<w:rPr><w:sz w:val="20"/><w:b/></w:rPr>`},
		{"filho repetido", `<w:rPr><w:b/><w:b/></w:rPr>`},
		{"filho desconhecido", `<w:rPr><w:unknown/></w:rPr>`},
		{"filho estrangeiro", `<w:rPr><x:b xmlns:x="urn:foreign"/></w:rPr>`},
		{"texto NBSP", `<w:rPr><w:b/> <w:i/></w:rPr>`},
		{"NBSP em rFonts", `<w:rPr><w:rFonts> </w:rFonts></w:rPr>`},
		{"NBSP em sz", `<w:rPr><w:sz> </w:sz></w:rPr>`},
		{"NBSP em szCs", `<w:rPr><w:szCs> </w:szCs></w:rPr>`},
	} {
		t.Run(tc.nome, func(t *testing.T) {
			fonte := alinhamentoFixture(`<w:p><w:r>` + tc.rpr + `<w:t>x</w:t></w:r></w:p>`)
			doc := abrirParaSubstituicao(t, docxSintetico(t, fonte))
			require.NoError(t, doc.SubstituirParte("word/document.xml", fonte), "pré-condição: fixture deve ser XML válido")
			antes := salvarSubstituicao(t, doc)
			require.Error(t, doc.AplicarTipografia([]int{0}, "Fonte", 12))
			assert.Equal(t, antes, salvarSubstituicao(t, doc))
		})
	}
}

func TestAplicarTipografiaRecusaTextoDeWrappersMasIgnoraHistorico(t *testing.T) {
	for _, wrapper := range []string{"hyperlink", "sdt", "ins", "moveTo"} {
		t.Run(wrapper, func(t *testing.T) {
			fonte := alinhamentoFixture(`<w:p><w:r><w:t>direto</w:t></w:r><w:` + wrapper + `><w:r><w:t>envolvido</w:t></w:r></w:` + wrapper + `></w:p>`)
			doc := abrirParaSubstituicao(t, docxSintetico(t, fonte))
			antes := salvarSubstituicao(t, doc)
			require.Error(t, doc.AplicarTipografia([]int{0}, "Fonte", 12))
			assert.Equal(t, antes, salvarSubstituicao(t, doc))
		})
	}
	t.Run("AlternateContent com texto atual é recusado", func(t *testing.T) {
		fonte := alinhamentoFixture(`<w:p><w:r><w:t>direto</w:t></w:r><mc:AlternateContent xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006"><mc:Choice Requires="w"><w:r><w:t>alternativo</w:t></w:r></mc:Choice></mc:AlternateContent></w:p>`)
		doc := abrirParaSubstituicao(t, docxSintetico(t, fonte))
		antes := salvarSubstituicao(t, doc)
		require.Error(t, doc.AplicarTipografia([]int{0}, "Fonte", 12))
		assert.Equal(t, antes, salvarSubstituicao(t, doc))
	})
	t.Run("delText e histórico não são alvos", func(t *testing.T) {
		fonte := alinhamentoFixture(`<w:p><w:pPr><w:pPrChange w:id="1"><w:pPr><w:keepNext/></w:pPr></w:pPrChange></w:pPr><w:r><w:rPr><w:b/><w:rPrChange w:id="2"><w:rPr><w:rFonts w:asciiTheme="majorHAnsi"/></w:rPr></w:rPrChange></w:rPr><w:delText>apagado</w:delText><w:t>atual</w:t></w:r></w:p>`)
		doc := abrirParaSubstituicao(t, docxSintetico(t, fonte))
		require.NoError(t, doc.AplicarTipografia([]int{0}, "Fonte", 12))
		saida := string(lerParte(t, salvarSubstituicao(t, doc), "word/document.xml"))
		assert.Contains(t, saida, `<w:rPrChange w:id="2"><w:rPr><w:rFonts w:asciiTheme="majorHAnsi"/></w:rPr></w:rPrChange>`)
		assert.Contains(t, saida, `<w:pPrChange w:id="1"><w:pPr><w:keepNext/></w:pPr></w:pPrChange>`)
		assert.Contains(t, saida, `<w:delText>apagado</w:delText>`)
		assert.Contains(t, saida, `<w:t>atual</w:t>`)
	})
}

func TestAplicarTipografiaPreservaBOMPrefixosEDeltaExistente(t *testing.T) {
	documento := []byte("\xef\xbb\xbf<?xml version=\"1.0\" encoding=\"UTF-8\"?><x:document xmlns:x=\"" + espacoNomesW + "\"><x:body><x:p><x:r><x:t>texto</x:t></x:r></x:p></x:body></x:document>")
	original := docxSintetico(t, documento)
	doc := abrirParaSubstituicao(t, original)
	antes := []byte("\xef\xbb\xbf<?xml version=\"1.0\" encoding=\"UTF-8\"?><x:document xmlns:x=\"" + espacoNomesW + "\"><x:body><x:p><x:r><x:t>anterior</x:t></x:r></x:p></x:body></x:document>")
	require.NoError(t, doc.SubstituirParte("word/document.xml", antes), "pré-condição: delta anterior substituível")
	require.NoError(t, doc.AplicarTipografia([]int{0}, "Fonte", 12))
	saida := lerParte(t, salvarSubstituicao(t, doc), "word/document.xml")
	assert.True(t, bytes.HasPrefix(saida, []byte{0xef, 0xbb, 0xbf}), "BOM preservado")
	assert.Contains(t, string(saida), `<x:rFonts`)
	assert.Contains(t, string(saida), `<x:t>anterior</x:t>`)
	assert.NotContains(t, string(saida), `<w:`)
}

func propriedadesTipografiaDireta(t *testing.T, conteudo []byte) []map[string]string {
	t.Helper()
	dec := xml.NewDecoder(bytes.NewReader(conteudo))
	var pilha []xml.Name
	var resultado []map[string]string
	for {
		token, err := dec.Token()
		if errors.E(err, io.EOF) {
			break
		}
		require.NoError(t, err, "XML de saída deve continuar válido")
		switch elemento := token.(type) {
		case xml.StartElement:
			pilha = append(pilha, elemento.Name)
			if len(pilha) >= 5 && pilha[len(pilha)-4].Local == "body" && pilha[len(pilha)-3].Local == "p" && pilha[len(pilha)-2].Local == "r" && elemento.Name.Space == espacoNomesW && elemento.Name.Local == "rPr" {
				resultado = append(resultado, map[string]string{"__emRun": "sim"})
			}
			if len(pilha) < 5 || pilha[len(pilha)-5].Local != "body" || pilha[len(pilha)-4].Local != "p" || pilha[len(pilha)-3].Local != "r" || pilha[len(pilha)-2].Local != "rPr" || elemento.Name.Space != espacoNomesW {
				continue
			}
			if elemento.Name.Local != "rFonts" && elemento.Name.Local != "sz" && elemento.Name.Local != "szCs" {
				continue
			}
			indice := len(resultado) - 1
			require.GreaterOrEqual(t, indice, 0, "propriedade direta requer rPr direto")
			for _, attr := range elemento.Attr {
				if attr.Name.Space == espacoNomesW {
					resultado[indice][elemento.Name.Local+"."+attr.Name.Local] = attr.Value
				}
			}
		case xml.EndElement:
			if len(pilha) > 0 {
				pilha = pilha[:len(pilha)-1]
			}
		}
	}
	for _, p := range resultado {
		delete(p, "__emRun")
	}
	return resultado
}
