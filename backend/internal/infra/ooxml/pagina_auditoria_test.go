package ooxml

import (
	"bytes"
	"strings"
	"testing"

	"github.com/daniel-halos/formatador/internal/domain/ruleset"
	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/stretchr/testify/require"
)

func TestAplicarPaginaAuditoriaMargemNegativa(t *testing.T) {
	p := paginaSintetica()
	p.Margens.EsquerdaCM = -0.01
	_, err := converterPagina(p, MargensComplementares{})
	require.Error(t, err)
}

func TestAplicarPaginaAuditoriaRejeitaMedidasEComplementosAtomicamente(t *testing.T) {
	casos := []struct {
		nome         string
		pagina       ruleset.Pagina
		complementos MargensComplementares
	}{
		{"largura zero", func() ruleset.Pagina { p := paginaSintetica(); p.LarguraCM = 0; return p }(), MargensComplementares{}},
		{"altura zero", func() ruleset.Pagina { p := paginaSintetica(); p.AlturaCM = 0; return p }(), MargensComplementares{}},
		{"altura negativa", func() ruleset.Pagina { p := paginaSintetica(); p.AlturaCM = -1; return p }(), MargensComplementares{}},
		{"margem superior negativa", func() ruleset.Pagina { p := paginaSintetica(); p.Margens.SuperiorCM = -1; return p }(), MargensComplementares{}},
		{"margem inferior negativa", func() ruleset.Pagina { p := paginaSintetica(); p.Margens.InferiorCM = -1; return p }(), MargensComplementares{}},
		{"margem esquerda negativa", func() ruleset.Pagina { p := paginaSintetica(); p.Margens.EsquerdaCM = -1; return p }(), MargensComplementares{}},
		{"margem direita negativa", func() ruleset.Pagina { p := paginaSintetica(); p.Margens.DireitaCM = -1; return p }(), MargensComplementares{}},
		{"dimensão arredonda para zero", func() ruleset.Pagina {
			p := paginaSintetica()
			p.LarguraCM = 0.0001
			p.Margens = ruleset.Margens{}
			return p
		}(), MargensComplementares{}},
		{"cabeçalho negativo", paginaSintetica(), MargensComplementares{CabecalhoTwips: -1}},
		{"rodapé negativo", paginaSintetica(), MargensComplementares{RodapeTwips: -1}},
		{"medianiz negativa", paginaSintetica(), MargensComplementares{MedianizTwips: -1}},
		{"cabeçalho acima do limite", paginaSintetica(), MargensComplementares{CabecalhoTwips: 31681}},
		{"rodapé acima do limite", paginaSintetica(), MargensComplementares{RodapeTwips: 31681}},
		{"medianiz acima do limite", paginaSintetica(), MargensComplementares{MedianizTwips: 31681}},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			doc := abrirParaSubstituicao(t, docxSintetico(t, documentoXMLMinimo("base")))
			antes := salvarSubstituicao(t, doc)
			err := doc.AplicarPagina(caso.pagina, caso.complementos)
			require.Error(t, err)
			var validacao *errors.ErroValidacao
			require.True(t, errors.Como(err, &validacao))
			require.Equal(t, antes, salvarSubstituicao(t, doc))
		})
	}
}

func TestAplicarPaginaAuditoriaPaisagemEComplementoPreservadoInvalido(t *testing.T) {
	p := paginaSintetica()
	p.LarguraCM = 29.7
	p.AlturaCM = 21
	fonte := []byte(`<w:document xmlns:w="` + namespaceWord + `"><w:body><w:sectPr><w:pgMar w:top="1" w:right="1" w:bottom="1" w:left="1" w:header="-1"/></w:sectPr></w:body></w:document>`)
	doc := abrirParaSubstituicao(t, docxSintetico(t, fonte))
	antes := salvarSubstituicao(t, doc)
	err := doc.AplicarPagina(p, MargensComplementares{})
	require.Error(t, err, "header existente inválido deve alcançar a validação do atributo")
	require.Equal(t, antes, salvarSubstituicao(t, doc))
}

func TestAplicarPaginaAuditoriaPaisagem(t *testing.T) {
	p := paginaSintetica()
	p.LarguraCM, p.AlturaCM = 29.7, 21
	doc := abrirParaSubstituicao(t, docxSintetico(t, documentoXMLMinimo("base")))
	require.NoError(t, doc.AplicarPagina(p, MargensComplementares{}))
	saida := string(lerParte(t, salvarSubstituicao(t, doc), parteDocumentoPrincipal))
	require.Contains(t, saida, `orient="landscape"`)
}

func TestAplicarPaginaAuditoriaRejeitaRodapePreservadoInvalido(t *testing.T) {
	fonte := []byte(`<w:document xmlns:w="` + namespaceWord + `"><w:body><w:sectPr><w:pgMar w:top="1" w:right="1" w:bottom="1" w:left="1" w:footer="31681"/></w:sectPr></w:body></w:document>`)
	doc := abrirParaSubstituicao(t, docxSintetico(t, fonte))
	antes := salvarSubstituicao(t, doc)
	require.Error(t, doc.AplicarPagina(paginaSintetica(), MargensComplementares{}))
	require.Equal(t, antes, salvarSubstituicao(t, doc))
}

func TestAplicarPaginaAuditoriaMedianizPreservadaConsomeLargura(t *testing.T) {
	fonte := []byte(`<w:document xmlns:w="` + namespaceWord + `"><w:body><w:sectPr><w:pgMar w:top="1" w:right="1" w:bottom="1" w:left="1" w:gutter="10000"/></w:sectPr></w:body></w:document>`)
	doc := abrirParaSubstituicao(t, docxSintetico(t, fonte))
	antes := salvarSubstituicao(t, doc)
	require.Error(t, doc.AplicarPagina(paginaSintetica(), MargensComplementares{}))
	require.Equal(t, antes, salvarSubstituicao(t, doc))
}

func TestAplicarPaginaAuditoriaRejeicoesDeEstruturaAtomicas(t *testing.T) {
	casos := map[string]string{
		"Strict":             `<w:document xmlns:w="http://purl.oclc.org/ooxml/wordprocessingml/main"><w:body><w:sectPr/></w:body></w:document>`,
		"AlternateContent":   `<w:document xmlns:w="` + namespaceWord + `" xmlns:mc="urn:mc"><w:body><mc:AlternateContent><mc:Choice Requires="w"><w:sectPr/></mc:Choice></mc:AlternateContent></w:body></w:document>`,
		"textbox":            `<w:document xmlns:w="` + namespaceWord + `"><w:body><w:p><w:r><w:txbxContent><w:sectPr/></w:txbxContent></w:r></w:p></w:body></w:document>`,
		"duas secoes finais": `<w:document xmlns:w="` + namespaceWord + `"><w:body><w:sectPr/><w:sectPr/></w:body></w:document>`,
		"profundidade 257":   `<w:body>` + strings.Repeat(`<w:sdt>`, 255) + strings.Repeat(`</w:sdt>`, 255) + `</w:body>`,
	}
	for nome, corpo := range casos {
		t.Run(nome, func(t *testing.T) {
			fonte := corpo
			if !strings.HasPrefix(corpo, "<w:document") && !strings.HasPrefix(corpo, "<document") {
				fonte = `<w:document xmlns:w="` + namespaceWord + `">` + corpo + `</w:document>`
			}
			doc := abrirParaSubstituicao(t, docxSintetico(t, []byte(fonte)))
			antes := salvarSubstituicao(t, doc)
			err := doc.AplicarPagina(paginaSintetica(), MargensComplementares{})
			require.Error(t, err)
			require.Equal(t, antes, salvarSubstituicao(t, doc))
		})
	}
}

func TestAplicarPaginaAuditoriaAceitaVariacoesLexicaisValidas(t *testing.T) {
	casos := map[string]string{
		"self closing com espaços":                  `<w:document xmlns:w="` + namespaceWord + `"><w:body><w:sectPr   /></w:body></w:document>`,
		"atributo antes da declaracao do namespace": `<w:document xmlns:w="` + namespaceWord + `"><w:body><w:sectPr><w:pgSz a:w="1" xmlns:a="` + namespaceWord + `"/></w:sectPr></w:body></w:document>`,
	}
	for nome, fonte := range casos {
		t.Run(nome, func(t *testing.T) {
			doc := abrirParaSubstituicao(t, docxSintetico(t, []byte(fonte)))
			err := doc.AplicarPagina(paginaSintetica(), MargensComplementares{})
			require.NoError(t, err)
			saida := string(lerParte(t, salvarSubstituicao(t, doc), parteDocumentoPrincipal))
			require.Contains(t, saida, `pgSz`)
			require.Contains(t, saida, `pgMar`)
			require.Contains(t, saida, `11906`)
		})
	}
}

func TestAplicarPaginaAuditoriaProfundidade256Aceita(t *testing.T) {
	fonte := []byte(`<w:document xmlns:w="` + namespaceWord + `"><w:body>` + strings.Repeat(`<w:sdt>`, 254) + strings.Repeat(`</w:sdt>`, 254) + `</w:body></w:document>`)
	doc := abrirParaSubstituicao(t, docxSintetico(t, fonte))
	require.NoError(t, doc.AplicarPagina(paginaSintetica(), MargensComplementares{}))
}

func TestAplicarPaginaAuditoriaNamespaceDefaultSemPrefixoW(t *testing.T) {
	fonte := []byte(`<document xmlns="` + namespaceWord + `"><body><sectPr><pgSz/><pgMar/></sectPr></body></document>`)
	doc := abrirParaSubstituicao(t, docxSintetico(t, fonte))
	err := doc.AplicarPagina(paginaSintetica(), MargensComplementares{})
	require.NoError(t, err)
	saida := string(lerParte(t, salvarSubstituicao(t, doc), parteDocumentoPrincipal))
	require.Contains(t, saida, `<pgSz`)
	conferirPaginaSinteticaXML(t, []byte(saida), map[string]string{"header": "0", "footer": "0", "gutter": "0"})
}

func TestAplicarPaginaAuditoriaLimiteXMLComDelta(t *testing.T) {
	for _, tamanho := range []int{limiteXMLPagina, limiteXMLPagina + 1} {
		t.Run(map[bool]string{true: "exatamente 32MiB", false: "32MiB mais um"}[tamanho == limiteXMLPagina], func(t *testing.T) {
			// O caso positivo já contém todos os alvos; a edição não cresce além do limite.
			base := []byte(`<w:document xmlns:w="` + namespaceWord + `"><w:body><w:sectPr><w:pgSz w:w="11906" w:h="16838" w:orient="portrait"/><w:pgMar w:top="1134" w:right="1134" w:bottom="1134" w:left="1701" w:header="1" w:footer="2" w:gutter="3"/></w:sectPr></w:body></w:document>`)
			require.LessOrEqual(t, len(base), tamanho)
			fonte := append([]byte(nil), base[:len(base)-len(`</w:document>`)]...)
			fonte = append(fonte, bytes.Repeat([]byte(" "), tamanho-len(base))...)
			fonte = append(fonte, []byte(`</w:document>`)...)
			require.Len(t, fonte, tamanho)
			doc := abrirParaSubstituicao(t, docxSintetico(t, documentoXMLMinimo("base")))
			require.NoError(t, doc.SubstituirParte(parteDocumentoPrincipal, fonte), "fixture grande usa delta para não passar pelo limite de ingestão ZIP")
			antes := salvarSubstituicao(t, doc)
			err := doc.AplicarPagina(paginaSintetica(), MargensComplementares{})
			if tamanho == limiteXMLPagina {
				require.NoError(t, err)
				resultado := salvarSubstituicao(t, doc)
				require.NoError(t, doc.AplicarPagina(paginaSintetica(), MargensComplementares{CabecalhoTwips: 1, RodapeTwips: 2, MedianizTwips: 3}))
				require.Equal(t, resultado, salvarSubstituicao(t, doc))
			} else {
				require.Error(t, err)
				require.Equal(t, antes, salvarSubstituicao(t, doc))
			}
		})
	}
}

func TestAplicarPaginaAuditoriaEdicaoPreservaLexicoENaoAlvos(t *testing.T) {
	fonte := []byte(`<?xml version="1.0"?><w:document xmlns:w="` + namespaceWord + `"><w:body><w:p><w:r><w:t>fora do alvo 😀</w:t></w:r></w:p><w:sectPr><w:pgSz w:w="1" w:h="2"/><w:pgMar w:top="1" w:right="2" w:bottom="3" w:left="4" w:header="17" w:footer="18" w:gutter="19" /></w:sectPr></w:body></w:document>`)
	zipOriginal := montarZip(t, append(partesBaseDocx(fonte), entradaZip{nome: "word/media/a.bin", conteudo: []byte{0, 1, 2, 255}, metodo: 0}))
	doc := abrirParaSubstituicao(t, zipOriginal)
	require.NoError(t, doc.SubstituirParte(parteDocumentoPrincipal, fonte))
	require.NoError(t, doc.AplicarPagina(paginaSintetica(), MargensComplementares{}))
	resultado := salvarSubstituicao(t, doc)
	saida := lerParte(t, resultado, parteDocumentoPrincipal)
	inicioAlvo := bytes.Index(fonte, []byte(`<w:pgSz`))
	fimSecaoFonte := bytes.Index(fonte, []byte(`</w:sectPr>`))
	inicioAlvoSaida := bytes.Index(saida, []byte(`<w:pgSz`))
	fimSecaoSaida := bytes.Index(saida, []byte(`</w:sectPr>`))
	require.NotEqual(t, -1, inicioAlvo, "pré-condição: alvos presentes no XML de origem")
	require.NotEqual(t, -1, fimSecaoFonte, "pré-condição: seção presente no XML de origem")
	require.NotEqual(t, -1, inicioAlvoSaida, "pré-condição: alvos presentes no XML de saída")
	require.NotEqual(t, -1, fimSecaoSaida, "pré-condição: seção presente no XML de saída")
	require.Equal(t, fonte[:inicioAlvo], saida[:inicioAlvoSaida], "prefixo anterior aos alvos preservado byte a byte")
	require.Equal(t, fonte[fimSecaoFonte:], saida[fimSecaoSaida:], "sufixo posterior aos alvos preservado byte a byte")
	require.Contains(t, string(saida), `<w:t>fora do alvo 😀</w:t>`)
	require.Contains(t, string(saida), `w:header="17"`)
	require.Contains(t, string(saida), `w:footer="18"`)
	require.Contains(t, string(saida), `w:gutter="19"`)
	require.Contains(t, string(saida), `w:pgMar`)
	require.Equal(t, lerParte(t, zipOriginal, "word/media/a.bin"), lerParte(t, resultado, "word/media/a.bin"))
	for i := 0; i < 10; i++ {
		require.NoError(t, doc.AplicarPagina(paginaSintetica(), MargensComplementares{}))
		require.Equal(t, resultado, salvarSubstituicao(t, doc), "repetição %d", i+1)
	}
}
