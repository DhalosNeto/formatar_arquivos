package ooxml

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/stretchr/testify/require"
)

func TestAplicarPaginaBordasDeEstrutura(t *testing.T) {
	for _, corpo := range []string{
		"<w:body/>",
		"<w:body><w:sectPr/></w:body>",
		"<w:body><w:sectPr><w15:footnoteColumns xmlns:w15=\"http://schemas.microsoft.com/office/word/2012/wordml\"/></w:sectPr></w:body>",
		"<w:body><w:sectPr><w:pgMar w:header=\"0\" w:footer=\"0\" w:gutter=\"0\"/><w:cols/></w:sectPr></w:body>",
	} {
		t.Run(corpo, func(t *testing.T) {
			fonte := []byte("<w:document xmlns:w=\"" + espacoNomesW + "\">" + corpo + "</w:document>")
			doc := abrirParaSubstituicao(t, docxSintetico(t, fonte))
			require.NoError(t, doc.AplicarPagina(paginaSintetica(), MargensComplementares{}))
			saida := salvarSubstituicao(t, doc)
			xml := string(lerParte(t, saida, parteDocumentoPrincipal))
			require.Contains(t, xml, "pgSz")
			require.Contains(t, xml, "pgMar")
			require.Less(t, strings.Index(xml, ":pgSz"), strings.Index(xml, ":pgMar"))
			if strings.Contains(xml, ":footnoteColumns") {
				require.Less(t, strings.Index(xml, ":pgMar"), strings.Index(xml, ":footnoteColumns"))
			}
			require.NoError(t, doc.AplicarPagina(paginaSintetica(), MargensComplementares{}))
			require.Equal(t, saida, salvarSubstituicao(t, doc))
		})
	}
}
func TestAplicarPaginaRecusaBordasAtomicamente(t *testing.T) {
	for nome, corpo := range map[string]string{
		"atributo duplicado expandido": "<w:body><w:sectPr><w:pgSz xmlns:a=\"" + espacoNomesW + "\" w:w=\"10\" a:w=\"20\"/></w:sectPr></w:body>",
		"secao final fora de ordem":    "<w:body><w:sectPr/><w:p/></w:body>",
		"dois corpos":                  "<w:body/><w:body/>",
		"colunas explicitas":           "<w:body><w:sectPr><w:cols><w:col w:w=\"2000\"/></w:cols></w:sectPr></w:body>",
		"profundidade excessiva":       "<w:body>" + strings.Repeat("<w:sdt>", 260) + strings.Repeat("</w:sdt>", 260) + "</w:body>",
		"medianiz consome largura":     "<w:body><w:sectPr><w:pgMar w:header=\"0\" w:footer=\"0\" w:gutter=\"31680\"/></w:sectPr></w:body>",
	} {
		t.Run(nome, func(t *testing.T) {
			doc := abrirParaSubstituicao(t, docxSintetico(t, []byte("<w:document xmlns:w=\""+espacoNomesW+"\">"+corpo+"</w:document>")))
			antes := salvarSubstituicao(t, doc)
			err := doc.AplicarPagina(paginaSintetica(), MargensComplementares{})
			require.Error(t, err)
			var validacao *errors.ErroValidacao
			require.True(t, errors.Como(err, &validacao))
			require.Equal(t, antes, salvarSubstituicao(t, doc))
		})
	}
}
func TestAplicarPaginaLimiteLeituraXML(t *testing.T) {
	fonte := bytes.Repeat([]byte(" "), 32<<20+1)
	doc := abrirParaSubstituicao(t, docxSintetico(t, fonte))
	require.Error(t, doc.AplicarPagina(paginaSintetica(), MargensComplementares{}))
}

func TestAplicarPaginaLimitaRecursosXML(t *testing.T) {
	var atributos, declaracoes strings.Builder
	for i := 0; i < 129; i++ {
		atributos.WriteString(" a" + strconv.Itoa(i) + "=\"v\"")
	}
	// Declarações distribuídas: nenhum elemento excede o limite de atributos.
	for i := 0; i < 1024; i++ {
		declaracoes.WriteString("<w:p xmlns:a" + strconv.Itoa(i) + "=\"urn:teste\"/>")
	}
	for nome, corpo := range map[string]string{
		"atributos":   "<w:p" + atributos.String() + "/>",
		"declaracoes": declaracoes.String(),
		"nos":         strings.Repeat("<w:p/>", 100000),
	} {
		t.Run(nome, func(t *testing.T) {
			doc := abrirParaSubstituicao(t, docxSintetico(t, documentoXMLMinimo("base")))
			fonte := []byte("<w:document xmlns:w=\"" + espacoNomesW + "\"><w:body>" + corpo + "</w:body></w:document>")
			require.NoError(t, doc.SubstituirParte(parteDocumentoPrincipal, fonte))
			antes := salvarSubstituicao(t, doc)
			require.Error(t, doc.AplicarPagina(paginaSintetica(), MargensComplementares{}))
			require.Equal(t, antes, salvarSubstituicao(t, doc))
		})
	}
}

func TestAplicarPaginaMedianizEfetiva(t *testing.T) {
	doc := abrirParaSubstituicao(t, docxSintetico(t, documentoXMLMinimo("base")))
	antes := salvarSubstituicao(t, doc)
	require.Error(t, doc.AplicarPagina(paginaSintetica(), MargensComplementares{MedianizTwips: 31680}))
	require.Equal(t, antes, salvarSubstituicao(t, doc))
	fonte := []byte("<w:document xmlns:w=\"" + espacoNomesW + "\"><w:body><w:sectPr><w:pgMar w:gutter=\"0\"/></w:sectPr></w:body></w:document>")
	require.NoError(t, doc.SubstituirParte(parteDocumentoPrincipal, fonte))
	require.NoError(t, doc.AplicarPagina(paginaSintetica(), MargensComplementares{MedianizTwips: 31680}), "existente prevalece sobre fallback")
}
