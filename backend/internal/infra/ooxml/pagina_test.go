package ooxml

import (
	"bytes"
	"encoding/xml"
	errosPadrao "errors"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/daniel-halos/formatador/internal/domain/ruleset"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func paginaSintetica() ruleset.Pagina {
	return ruleset.Pagina{
		LarguraCM: 21, AlturaCM: 29.7,
		Margens: ruleset.Margens{SuperiorCM: 2, InferiorCM: 2, EsquerdaCM: 3, DireitaCM: 2},
	}
}

func documentoPagina(xml string) []byte {
	return []byte(xml)
}

// Confere o contrato de uma seção sintética, incluindo namespace dos atributos.
func conferirPaginaSinteticaXML(t *testing.T, dados []byte, complementos map[string]string) {
	t.Helper()
	const namespace = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	esperados := map[string]map[string]string{
		"pgSz":  {"w": "11906", "h": "16838", "orient": "portrait"},
		"pgMar": {"top": "1134", "bottom": "1134", "left": "1701", "right": "1134"},
	}
	for nome, valor := range complementos {
		esperados["pgMar"][nome] = valor
	}
	encontrados := map[string]int{}
	decodificador := xml.NewDecoder(bytes.NewReader(dados))
	for {
		token, err := decodificador.Token()
		if errosPadrao.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err, "saída deve ser XML válido")
		inicio, ok := token.(xml.StartElement)
		if !ok || esperados[inicio.Name.Local] == nil {
			continue
		}
		require.Equal(t, xml.Name{Space: namespace, Local: inicio.Name.Local}, inicio.Name)
		encontrados[inicio.Name.Local]++
		atributos := map[xml.Name]string{}
		for _, atributo := range inicio.Attr {
			_, duplicado := atributos[atributo.Name]
			require.False(t, duplicado, "atributo duplicado: %v", atributo.Name)
			atributos[atributo.Name] = atributo.Value
			if _, alvo := esperados[inicio.Name.Local][atributo.Name.Local]; alvo && atributo.Name.Space != "xmlns" {
				assert.Equal(t, namespace, atributo.Name.Space, "namespace de %s", atributo.Name.Local)
			}
		}
		for nome, valor := range esperados[inicio.Name.Local] {
			assert.Equal(t, valor, atributos[xml.Name{Space: namespace, Local: nome}], "%s/@%s", inicio.Name.Local, nome)
		}
	}
	assert.Equal(t, map[string]int{"pgSz": 1, "pgMar": 1}, encontrados)
}

func TestAplicarPaginaAlteraMedidasPreservaAtributosComplementares(t *testing.T) {
	xml := documentoPagina(`<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:sectPr><w:pgSz w:w="1" w:h="2" w:orient="landscape"/><w:pgMar w:top="1" w:right="2" w:bottom="3" w:left="4" w:header="321" w:footer="654" w:gutter="77"/><w:cols w:num="2"/></w:sectPr></w:body></w:document>`)
	doc := abrirParaSubstituicao(t, docxSintetico(t, xml))
	err := doc.AplicarPagina(paginaSintetica(), MargensComplementares{CabecalhoTwips: 900, RodapeTwips: 901, MedianizTwips: 902})
	require.NoError(t, err)
	saida := lerParte(t, salvarSubstituicao(t, doc), "word/document.xml")
	conferirPaginaSinteticaXML(t, saida, map[string]string{"header": "321", "footer": "654", "gutter": "77"})
	for _, esperado := range []string{`w:w="11906"`, `w:h="16838"`, `w:orient="portrait"`, `w:top="1134"`, `w:right="1134"`, `w:bottom="1134"`, `w:left="1701"`, `w:header="321"`, `w:footer="654"`, `w:gutter="77"`, `<w:cols w:num="2"/>`} {
		assert.Contains(t, string(saida), esperado)
	}
	assert.NotContains(t, string(saida), `w:top="1"`)
}

func TestAplicarPaginaAtualizaTodasSecoesCorrentesEIgnoraHistorico(t *testing.T) {
	xml := documentoPagina(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:pPr><w:sectPr><w:pgSz w:w="10" w:h="20"/><w:pgMar w:top="1" w:right="1" w:bottom="1" w:left="1"/></w:sectPr><w:pPrChange><w:pPr><w:sectPr><w:pgSz w:w="70" w:h="80"/><w:pgMar w:top="8" w:right="8" w:bottom="8" w:left="8"/></w:sectPr></w:pPr></w:pPrChange></w:pPr></w:p><w:sectPr><w:pgSz w:w="30" w:h="40"/><w:pgMar w:top="2" w:right="2" w:bottom="2" w:left="2"/><w:sectPrChange><w:sectPr><w:pgSz w:w="50" w:h="60"/><w:pgMar w:top="9" w:right="9" w:bottom="9" w:left="9"/></w:sectPr></w:sectPrChange></w:sectPr></w:body></w:document>`)
	doc := abrirParaSubstituicao(t, docxSintetico(t, xml))
	err := doc.AplicarPagina(paginaSintetica(), MargensComplementares{CabecalhoTwips: 42, RodapeTwips: 43, MedianizTwips: 44})
	require.NoError(t, err)
	saida := string(lerParte(t, salvarSubstituicao(t, doc), "word/document.xml"))
	assert.Equal(t, 2, strings.Count(saida, `w:w="11906"`))
	assert.Equal(t, 2, strings.Count(saida, `w:h="16838"`))
	assert.Contains(t, saida, `w:w="50" w:h="60"`)
	assert.Contains(t, saida, `w:top="9" w:right="9" w:bottom="9" w:left="9"`)
	assert.Contains(t, saida, `w:w="70" w:h="80"`)
	assert.Contains(t, saida, `w:top="8" w:right="8" w:bottom="8" w:left="8"`)
	assert.Contains(t, saida, `w:header="42"`)
	assert.Contains(t, saida, `w:footer="43"`)
	assert.Contains(t, saida, `w:gutter="44"`)
}

func TestAplicarPaginaCriaSecaoFinalENosNaOrdemOOXML(t *testing.T) {
	xml := documentoPagina(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>texto 😀 café</w:t></w:r></w:p></w:body></w:document>`)
	doc := abrirParaSubstituicao(t, docxSintetico(t, xml))
	require.NoError(t, doc.AplicarPagina(paginaSintetica(), MargensComplementares{CabecalhoTwips: 10, RodapeTwips: 11, MedianizTwips: 12}))
	saida := string(lerParte(t, salvarSubstituicao(t, doc), "word/document.xml"))
	assert.Contains(t, saida, `<w:sectPr><w:pgSz`)
	assert.Less(t, strings.Index(saida, `<w:pgSz`), strings.Index(saida, `<w:pgMar`))
	assert.Contains(t, saida, `<w:pgMar`)
	assert.Contains(t, saida, `texto 😀 café`)
}

func TestAplicarPaginaPreservaLexicaPrefixosPartesENoOp(t *testing.T) {
	for nome, xml := range map[string]string{
		"prefixo alternativo com BOM e declaração": "\xef\xbb\xbf" + `<?xml version="1.0" encoding="UTF-8"?><x:document xmlns:x="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><!--comentário--><x:body><x:p><x:r><x:t xml:space="preserve"> texto 😀 café 𠀀 </x:t></x:r></x:p><x:sectPr><x:pgSz x:w="1" x:h="2"/><x:pgMar x:top="1" x:right="1" x:bottom="1" x:left="1"/></x:sectPr></x:body></x:document>`,
		"namespace default":                        `<document xmlns="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:a="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><!--comentário--><body><p><r><t xml:space="preserve"> texto 😀 café 𠀀 </t></r></p><sectPr><pgSz a:w="1" a:h="2"/><pgMar a:top="1" a:right="1" a:bottom="1" a:left="1"/></sectPr></body></document>`,
	} {
		t.Run(nome, func(t *testing.T) {
			originalXML := []byte(xml)
			textosAntes := extrairTextosWT(t, originalXML)
			require.Equal(t, []string{" texto 😀 café 𠀀 "}, textosAntes, "fixture deve conter texto Unicode e espaços")
			require.Contains(t, xml, "<!--comentário-->")
			entradas := append(partesBaseDocx(originalXML), entradaZip{nome: "word/media/imagem.bin", conteudo: []byte{0, 255, 17}, metodo: 0})
			original := montarZip(t, entradas)
			doc := abrirParaSubstituicao(t, original)
			require.NoError(t, doc.AplicarPagina(paginaSintetica(), MargensComplementares{CabecalhoTwips: 1, RodapeTwips: 2, MedianizTwips: 3}))
			saida := salvarSubstituicao(t, doc)
			documentoSaida := lerParte(t, saida, "word/document.xml")
			partes := listarPartes(t, original)
			require.Equal(t, partes, listarPartes(t, saida))
			for _, parte := range partes {
				if parte != "word/document.xml" {
					assert.Equal(t, lerParte(t, original, parte), lerParte(t, saida, parte), "parte não-alvo: %s", parte)
				}
			}
			assert.Equal(t, []rune(strings.Join(textosAntes, "")), []rune(strings.Join(extrairTextosWT(t, documentoSaida), "")))
			conferirPaginaSinteticaXML(t, documentoSaida, map[string]string{"header": "1", "footer": "2", "gutter": "3"})
			assert.Contains(t, string(documentoSaida), "<!--comentário-->")
			assert.Contains(t, string(documentoSaida), "xmlns")
			if strings.HasPrefix(xml, "\xef\xbb\xbf") {
				assert.True(t, bytes.HasPrefix(documentoSaida, []byte{0xef, 0xbb, 0xbf}))
				assert.Contains(t, string(documentoSaida), `<?xml version="1.0" encoding="UTF-8"?>`)
				assert.Contains(t, string(documentoSaida), `<x:pgSz`)
			} else {
				assert.Contains(t, string(documentoSaida), `<pgSz`)
				assert.Contains(t, string(documentoSaida), `a:w="11906"`)
				assert.Contains(t, string(documentoSaida), `a:h="16838"`)
			}
			require.NoError(t, doc.AplicarPagina(paginaSintetica(), MargensComplementares{CabecalhoTwips: 1, RodapeTwips: 2, MedianizTwips: 3}))
			assert.Equal(t, saida, salvarSubstituicao(t, doc))
		})
	}
}

func TestAplicarPaginaLeUltimoDeltaDoDocumento(t *testing.T) {
	doc := abrirParaSubstituicao(t, docxSintetico(t, documentoXMLMinimo("inicial")))
	primeiro := strings.Replace(string(documentoXMLMinimo("inicial")), `inicial`, `delta intermediário`, 1)
	require.NoError(t, doc.SubstituirParte("word/document.xml", []byte(primeiro)))
	xml := `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>último 😀 café</w:t></w:r></w:p></w:body></w:document>`
	require.NoError(t, doc.SubstituirParte("word/document.xml", []byte(xml)))
	require.NoError(t, doc.AplicarPagina(paginaSintetica(), MargensComplementares{CabecalhoTwips: 1, RodapeTwips: 2, MedianizTwips: 3}))
	saida := string(lerParte(t, salvarSubstituicao(t, doc), "word/document.xml"))
	assert.Contains(t, saida, "último 😀 café")
	assert.NotContains(t, saida, "inicial")
	assert.NotContains(t, saida, "intermediário")
}

func TestAplicarPaginaRejeitaMedidasSemAlterarDocumento(t *testing.T) {
	casos := []struct {
		nome   string
		pagina ruleset.Pagina
	}{
		{"nan", func() ruleset.Pagina { p := paginaSintetica(); p.LarguraCM = math.NaN(); return p }()},
		{"infinito", func() ruleset.Pagina { p := paginaSintetica(); p.AlturaCM = math.Inf(1); return p }()},
		{"largura acima de 31680 twips", func() ruleset.Pagina { p := paginaSintetica(); p.LarguraCM = 100; return p }()},
		{"margens consomem largura", func() ruleset.Pagina {
			p := paginaSintetica()
			p.Margens.EsquerdaCM = 11
			p.Margens.DireitaCM = 10
			return p
		}()},
		{"margens consomem altura", func() ruleset.Pagina {
			p := paginaSintetica()
			p.Margens.SuperiorCM = 15
			p.Margens.InferiorCM = 15
			return p
		}()},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			doc := abrirParaSubstituicao(t, docxSintetico(t, documentoXMLMinimo("base")))
			antes := salvarSubstituicao(t, doc)
			err := doc.AplicarPagina(caso.pagina, MargensComplementares{})
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "base")
			assert.Equal(t, antes, salvarSubstituicao(t, doc))
		})
	}
}

func TestAplicarPaginaRejeitaXMLAmbiguoSemAlterarDocumento(t *testing.T) {
	casos := []struct{ nome, xml string }{
		{"truncado", `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`},
		{"DTD", `<!DOCTYPE x [<!ENTITY x "texto">]><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>&x;</w:body></w:document>`},
		{"pgSz duplicado", `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:sectPr><w:pgSz w:w="1" w:h="2"/><w:pgSz w:w="3" w:h="4"/><w:pgMar w:top="1" w:right="1" w:bottom="1" w:left="1"/></w:sectPr></w:body></w:document>`},
		{"pgMar duplicado", `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:sectPr><w:pgSz w:w="1" w:h="2"/><w:pgMar w:top="1" w:right="1" w:bottom="1" w:left="1"/><w:pgMar w:top="2" w:right="2" w:bottom="2" w:left="2"/></w:sectPr></w:body></w:document>`},
		{"ordem inválida", `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:sectPr><w:pgMar w:top="1" w:right="1" w:bottom="1" w:left="1"/><w:pgSz w:w="1" w:h="2"/></w:sectPr></w:body></w:document>`},
		{"seção corrente em caminho não suportado", `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:sdt><w:sdtContent><w:p><w:pPr><w:sectPr><w:pgSz w:w="1" w:h="2"/><w:pgMar w:top="1" w:right="1" w:bottom="1" w:left="1"/></w:sectPr></w:pPr></w:p></w:sdtContent></w:sdt></w:body></w:document>`},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			doc := abrirParaSubstituicao(t, docxSintetico(t, []byte(caso.xml)))
			antes := salvarSubstituicao(t, doc)
			err := doc.AplicarPagina(paginaSintetica(), MargensComplementares{CabecalhoTwips: 1, RodapeTwips: 2, MedianizTwips: 3})
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "w:sectPr")
			assert.Equal(t, antes, salvarSubstituicao(t, doc))
		})
	}
}

func TestAplicarPaginaFalhaAtomicamenteNaSegundaSecao(t *testing.T) {
	xml := `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:pPr><w:sectPr><w:pgSz w:w="100" w:h="200"/><w:pgMar w:top="10" w:right="10" w:bottom="10" w:left="10"/></w:sectPr></w:pPr></w:p><w:sectPr><w:pgSz w:w="300" w:h="400"/><w:pgMar w:top="1" w:right="1" w:bottom="1" w:left="1"/><w:pgMar w:top="2" w:right="2" w:bottom="2" w:left="2"/></w:sectPr></w:body></w:document>`
	doc := abrirParaSubstituicao(t, docxSintetico(t, []byte(xml)))
	require.NoError(t, doc.SubstituirParte("word/document.xml", []byte(xml)))
	antes := salvarSubstituicao(t, doc)
	err := doc.AplicarPagina(paginaSintetica(), MargensComplementares{CabecalhoTwips: 1, RodapeTwips: 2, MedianizTwips: 3})
	require.Error(t, err)
	assert.Equal(t, antes, salvarSubstituicao(t, doc), "falha na segunda seção preserva integralmente o delta prévio")
}

func TestAplicarPaginaRejeitaReceptorNulo(t *testing.T) {
	var doc *Documento
	err := doc.AplicarPagina(paginaSintetica(), MargensComplementares{})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "<")
}
