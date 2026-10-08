package ooxml

import (
	"bytes"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAplicarRecuoPrimeiraLinhaEscreveTwipsNoParagrafoSelecionado(t *testing.T) {
	fonte := alinhamentoFixture(`<w:p><w:r><w:t>selecionado 😀 café</w:t></w:r></w:p><w:tbl><w:tr><w:tc><w:p/></w:tc></w:tr></w:tbl><w:p><w:r><w:t>outro</w:t></w:r></w:p>`)
	original := montarZip(t, append(partesBaseDocx(fonte), entradaZip{nome: "word/media/imagem.bin", conteudo: []byte{0, 255, 17}, metodo: 0}))
	doc := abrirParaSubstituicao(t, original)
	require.NoError(t, doc.AplicarRecuoPrimeiraLinha([]int{2}, 0.5))
	pacoteSaida := salvarSubstituicao(t, doc)
	saidaBytes := lerParte(t, pacoteSaida, "word/document.xml")
	saida := string(saidaBytes)
	assert.Contains(t, saida, `<w:p><w:pPr><w:ind w:firstLine="283"/></w:pPr><w:r><w:t>outro`)
	assert.NotContains(t, saida, `<w:p><w:pPr><w:ind w:firstLine="283"/></w:pPr><w:r><w:t>selecionado`)
	assert.Equal(t, extrairTextosWT(t, fonte), extrairTextosWT(t, saidaBytes), "texto do documento permanece íntegro")
	for _, parte := range listarPartes(t, original) {
		if parte != "word/document.xml" {
			assert.Equal(t, lerParte(t, original, parte), lerParte(t, pacoteSaida, parte), "parte não alvo %s", parte)
		}
	}
}

func TestAplicarRecuoPrimeiraLinhaRejeitaAlvoInvalidoAtomicamente(t *testing.T) {
	casos := []struct {
		nome string
		refs []int
		cm   float64
		xml  string
	}{{"referência negativa", []int{-1}, 0.5, `<w:p/>`}, {"referência duplicada", []int{0, 0}, 0.5, `<w:p/>`}, {"alvo é tabela", []int{0}, 0.5, `<w:tbl/>`}, {"referência fora do corpo", []int{1}, 0.5, `<w:p/>`}, {"medida inválida com seleção vazia", nil, -0.1, `<w:p/>`}, {"mais que o teto de referências", make([]int, 100001), 0.5, `<w:p/>`}}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			fonte := alinhamentoFixture(caso.xml)
			doc := abrirParaSubstituicao(t, docxSintetico(t, fonte))
			require.NoError(t, doc.SubstituirParte("word/document.xml", fonte), "pré-condição: delta de documento válido")
			antes := salvarSubstituicao(t, doc)
			require.Error(t, doc.AplicarRecuoPrimeiraLinha(caso.refs, caso.cm), "caso deve atingir a rejeição indicada pela entrada")
			assert.Equal(t, antes, salvarSubstituicao(t, doc), "rejeição preserva o delta anterior")
		})
	}
}

func TestAplicarRecuoPrimeiraLinhaRejeitaConflitosDiretosSemAlterarDelta(t *testing.T) {
	casos := []struct{ nome, ind string }{
		{"hanging zero", `<w:ind w:hanging="0"/>`},
		{"hangingChars", `<w:ind w:hangingChars="120"/>`},
		{"firstLineChars", `<w:ind w:firstLineChars="120"/>`},
		{"numPr", `<w:numPr><w:numId w:val="0"/></w:numPr>`},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			fonte := alinhamentoFixture(`<w:p><w:pPr>` + caso.ind + `</w:pPr></w:p>`)
			doc := abrirParaSubstituicao(t, docxSintetico(t, fonte))
			require.NoError(t, doc.SubstituirParte("word/document.xml", fonte), "pré-condição: delta XML com conflito direto válido")
			antes := salvarSubstituicao(t, doc)
			require.Error(t, doc.AplicarRecuoPrimeiraLinha([]int{0}, 0.5), "preflight deve recusar o conflito indicado")
			assert.Equal(t, antes, salvarSubstituicao(t, doc), "rejeição preserva o delta anterior")
		})
	}
}

func TestAplicarRecuoPrimeiraLinhaAtualizaFirstLinePreservandoRecuosLaterais(t *testing.T) {
	fonte := alinhamentoFixture(`<w:p><w:pPr><w:ind w:left="240" w:right="120" w:start="80" w:end="40" w:firstLine="360"/></w:pPr><w:r><w:t>texto</w:t></w:r></w:p>`)
	doc := abrirParaSubstituicao(t, docxSintetico(t, fonte))
	require.NoError(t, doc.AplicarRecuoPrimeiraLinha([]int{0}, 0.5))
	saida := string(lerParte(t, salvarSubstituicao(t, doc), "word/document.xml"))
	assert.Contains(t, saida, `<w:ind w:left="240" w:right="120" w:start="80" w:end="40" w:firstLine="283"/>`)
}

const relStylesCanonica = `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rStyles" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`
const stylesSemRegras = `<w:styles xmlns:w="` + espacoNomesW + `"/>`

func docxComRelacoesStyles(t *testing.T, documento, relacoes, estilos []byte) []byte {
	t.Helper()
	partes := partesBaseDocx(documento)
	if relacoes != nil {
		partes = append(partes, entradaZip{nome: "word/_rels/document.xml.rels", conteudo: relacoes})
	}
	if estilos != nil {
		partes = append(partes, entradaZip{nome: "word/styles.xml", conteudo: estilos})
	}
	return montarZip(t, partes)
}

func executarRecuoComStyles(t *testing.T, documento, relacoes, estilos []byte) (*Documento, []byte) {
	t.Helper()
	doc := abrirParaSubstituicao(t, docxComRelacoesStyles(t, documento, relacoes, estilos))
	require.NoError(t, doc.SubstituirParte("word/document.xml", documento), "pré-condição: document.xml deve ser substituível")
	if relacoes != nil {
		require.NoError(t, doc.SubstituirParte("word/_rels/document.xml.rels", relacoes), "pré-condição: relationships deve ser parte existente")
	}
	if estilos != nil {
		require.NoError(t, doc.SubstituirParte("word/styles.xml", estilos), "pré-condição: styles deve ser parte existente")
	}
	return doc, salvarSubstituicao(t, doc)
}

func TestAplicarRecuoPrimeiraLinhaRelacionamentoStyles(t *testing.T) {
	paragrafo := alinhamentoFixture(`<w:p><w:r><w:t>texto</w:t></w:r></w:p>`)
	casos := []struct {
		nome   string
		rels   []byte
		styles []byte
		aceita bool
	}{
		{"relacionamento canônico", []byte(relStylesCanonica), []byte(stylesSemRegras), true},
		{"ausência conjunta sem pStyle", nil, nil, true},
		{"relacionamentos ausentes mas parte styles presente", nil, []byte(stylesSemRegras), false},
		{"parte styles ausente sem relação styles", []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"/>`), nil, true},
		{"parte styles sem relação", []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="r1" Type="urn:other" Target="other.xml"/></Relationships>`), []byte(stylesSemRegras), false},
		{"filho Relationship estrangeiro", []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><x:Relationship xmlns:x="urn:foreign" Id="r1" Type="urn:other" Target="other.xml"/></Relationships>`), nil, false},
		{"filho não Relationship", []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Other Id="r1" Type="urn:other" Target="other.xml"/></Relationships>`), nil, false},
		{"QName raiz estrangeiro", []byte(`<x:Relationships xmlns:x="urn:foreign"/>`), nil, false},
		{"Type qualificado sem Type não qualificado", []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships" xmlns:x="urn:x"><Relationship Id="r1" x:Type="urn:other" Target="other.xml"/></Relationships>`), nil, false},
		{"Target qualificado sem Target não qualificado", []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships" xmlns:x="urn:x"><Relationship Id="r1" Type="urn:other" x:Target="other.xml"/></Relationships>`), nil, false},
		{"Id vazio", []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="" Type="urn:other" Target="other.xml"/></Relationships>`), nil, false},
		{"Id repetido entre relações não styles", []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="r1" Type="urn:one" Target="one.xml"/><Relationship Id="r1" Type="urn:two" Target="two.xml"/></Relationships>`), nil, false},
		{"TargetMode qualificado", []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships" xmlns:x="urn:x"><Relationship Id="r1" Type="urn:other" Target="other.xml" x:TargetMode="Internal"/></Relationships>`), nil, false},
		{"TargetMode inválido", []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="r1" Type="urn:other" Target="other.xml" TargetMode="Local"/></Relationships>`), nil, false},
		{"styles externo", []byte(strings.Replace(relStylesCanonica, `Target="styles.xml"`, `Target="styles.xml" TargetMode="External"`, 1)), []byte(stylesSemRegras), false},
		{"Target alternativo", []byte(strings.Replace(relStylesCanonica, `Target="styles.xml"`, `Target="./styles.xml"`, 1)), []byte(stylesSemRegras), false},
		{"Target absoluto", []byte(strings.Replace(relStylesCanonica, `Target="styles.xml"`, `Target="/word/styles.xml"`, 1)), []byte(stylesSemRegras), false},
		{"duas relações styles", []byte(strings.Replace(relStylesCanonica, `</Relationships>`, `<Relationship Id="r2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`, 1)), []byte(stylesSemRegras), false},
		{"relação styles sem parte", []byte(relStylesCanonica), nil, false},
		{"XML de relações malformado", []byte(`<Relationships`), nil, false},
		{"duplicata de atributo Id", []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="r1" Id="r2" Type="urn:other" Target="other.xml"/></Relationships>`), nil, false},
		{"atributos Type duplicados por aliases do mesmo QName", []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships" xmlns:a="urn:x" xmlns:b="urn:x"><Relationship Id="r1" Type="urn:other" a:Type="one" b:Type="two" Target="other.xml"/></Relationships>`), nil, false},
		{"Type XML duplicado", []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="r1" Type="urn:one" Type="urn:two" Target="other.xml"/></Relationships>`), nil, false},
		{"Target XML duplicado", []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="r1" Type="urn:other" Target="one.xml" Target="two.xml"/></Relationships>`), nil, false},
		{"Id duplicado por aliases do mesmo QName", []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships" xmlns:a="urn:x" xmlns:b="urn:x"><Relationship Id="r1" a:Id="one" b:Id="two" Type="urn:other" Target="other.xml"/></Relationships>`), nil, false},
		{"Target duplicado por aliases do mesmo QName", []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships" xmlns:a="urn:x" xmlns:b="urn:x"><Relationship Id="r1" Type="urn:other" Target="other.xml" a:Target="one" b:Target="two"/></Relationships>`), nil, false},
		{"externa não styles não seguida", []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="r1" Type="urn:external" Target="https://example.invalid/x" TargetMode="External"/></Relationships>`), nil, true},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			doc, antes := executarRecuoComStyles(t, paragrafo, caso.rels, caso.styles)
			err := doc.AplicarRecuoPrimeiraLinha([]int{0}, 0.5)
			if caso.aceita {
				require.NoError(t, err)
				depois := salvarSubstituicao(t, doc)
				for _, parte := range listarPartes(t, antes) {
					if parte != "word/document.xml" {
						assert.Equal(t, lerParte(t, antes, parte), lerParte(t, depois, parte), "relationship preflight não deve mutar %s", parte)
					}
				}
			} else {
				require.Error(t, err, "fixture deve alcançar a rejeição do relacionamento indicada")
				assert.Equal(t, antes, salvarSubstituicao(t, doc), "rejeição preserva todos os deltas")
			}
		})
	}
}

func stylesXML(inner string) []byte {
	return []byte(`<w:styles xmlns:w="` + espacoNomesW + `">` + inner + `</w:styles>`)
}

func documentoComEstilo(id string) []byte {
	return alinhamentoFixture(`<w:p><w:pPr><w:pStyle w:val="` + id + `"/></w:pPr><w:r><w:t>texto</w:t></w:r></w:p>`)
}

func TestAplicarRecuoPrimeiraLinhaEscolheDefaultParagraph(t *testing.T) {
	casos := []struct {
		nome, flag string
		aceita     bool
	}{
		{"default ausente", ``, true},
		{"default false", ` w:default="false"`, true},
		{"default zero", ` w:default="0"`, true},
		{"default off", ` w:default="off"`, true},
		{"default true", ` w:default="true"`, true},
		{"default um", ` w:default="1"`, true},
		{"default on", ` w:default="on"`, true},
		{"default inválido", ` w:default="yes"`, false},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			estilos := stylesXML(`<w:style w:type="paragraph" w:styleId="Normal"` + caso.flag + `/>`)
			doc, antes := executarRecuoComStyles(t, alinhamentoFixture(`<w:p><w:r><w:t>texto</w:t></w:r></w:p>`), []byte(relStylesCanonica), estilos)
			err := doc.AplicarRecuoPrimeiraLinha([]int{0}, 0.5)
			if caso.aceita {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Equal(t, antes, salvarSubstituicao(t, doc))
			}
		})
	}
	t.Run("dois defaults paragraph", func(t *testing.T) {
		estilos := stylesXML(`<w:style w:type="paragraph" w:styleId="N1" w:default="1"/><w:style w:type="paragraph" w:styleId="N2" w:default="on"/>`)
		doc, antes := executarRecuoComStyles(t, alinhamentoFixture(`<w:p/>`), []byte(relStylesCanonica), estilos)
		require.Error(t, doc.AplicarRecuoPrimeiraLinha([]int{0}, 0.5))
		assert.Equal(t, antes, salvarSubstituicao(t, doc))
	})
}

func TestAplicarRecuoPrimeiraLinhaIgnoraDefaultNaoUsadoPorEstiloNomeado(t *testing.T) {
	casos := []struct {
		nome, extra string
	}{
		{"default conflitante", `<w:style w:type="paragraph" w:styleId="Default" w:default="1"><w:pPr><w:ind w:hanging="0"/></w:pPr></w:style>`},
		{"default inválido não utilizado", `<w:style w:type="paragraph" w:styleId="Default" w:default="invalid"/>`},
		{"defaults duplicados não utilizados", `<w:style w:type="paragraph" w:styleId="D1" w:default="1"/><w:style w:type="paragraph" w:styleId="D2" w:default="on"/>`},
		{"styleId vazio em estilo não usado", `<w:style w:type="paragraph" w:styleId=""/>`},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			estilos := stylesXML(`<w:style w:type="paragraph" w:styleId="Body"/>` + caso.extra)
			doc, _ := executarRecuoComStyles(t, documentoComEstilo("Body"), []byte(relStylesCanonica), estilos)
			require.NoError(t, doc.AplicarRecuoPrimeiraLinha([]int{0}, 0.5), "pStyle nomeado raiz não deve anexar o default")
		})
	}
}

func TestAplicarRecuoPrimeiraLinhaValidaCadeiaBasedOn(t *testing.T) {
	casos := []struct {
		nome, estilos string
		aceita        bool
	}{
		{"cadeia válida", `<w:style w:type="paragraph" w:styleId="Child"><w:basedOn w:val="Parent"/></w:style><w:style w:type="paragraph" w:styleId="Parent"/>`, true},
		{"conflito herdado", `<w:style w:type="paragraph" w:styleId="Child"><w:basedOn w:val="Parent"/></w:style><w:style w:type="paragraph" w:styleId="Parent"><w:pPr><w:ind w:hanging="0"/></w:pPr></w:style>`, false},
		{"estilo requerido ausente", `<w:style w:type="paragraph" w:styleId="Other"/>`, false},
		{"estilo requerido duplicado", `<w:style w:type="paragraph" w:styleId="Body"/><w:style w:type="paragraph" w:styleId="Body"/>`, false},
		{"tipo incompatível", `<w:style w:type="character" w:styleId="Body"/>`, false},
		{"ciclo", `<w:style w:type="paragraph" w:styleId="Body"><w:basedOn w:val="Parent"/></w:style><w:style w:type="paragraph" w:styleId="Parent"><w:basedOn w:val="Body"/></w:style>`, false},
		{"pStyle val duplicado", `<w:style w:type="paragraph" w:styleId="Body"/>`, false},
		{"basedOn val duplicado", `<w:style w:type="paragraph" w:styleId="Body"><w:basedOn w:val="Parent" w:val="Other"/></w:style><w:style w:type="paragraph" w:styleId="Parent"/>`, false},
		{"styleId duplicado no nó", `<w:style w:type="paragraph" w:styleId="Body" w:styleId="Other"/>`, false},
		{"default duplicado no nó", `<w:style w:type="paragraph" w:styleId="Body" w:default="0" w:default="1"/>`, false},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			documento := documentoComEstilo("Body")
			if caso.nome == "cadeia válida" || caso.nome == "conflito herdado" {
				documento = documentoComEstilo("Child")
			}
			if caso.nome == "pStyle val duplicado" {
				documento = alinhamentoFixture(`<w:p><w:pPr><w:pStyle w:val="Body" w:val="Other"/></w:pPr></w:p>`)
			}
			doc, antes := executarRecuoComStyles(t, documento, []byte(relStylesCanonica), stylesXML(caso.estilos))
			err := doc.AplicarRecuoPrimeiraLinha([]int{0}, 0.5)
			if caso.aceita {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Equal(t, antes, salvarSubstituicao(t, doc), "erro preserva todos os deltas")
			}
		})
	}
	for _, limite := range []int{32, 33} {
		t.Run(fmt.Sprintf("cadeia %d", limite), func(t *testing.T) {
			var estilos strings.Builder
			for indice := 0; indice < limite; indice++ {
				fmt.Fprintf(&estilos, `<w:style w:type="paragraph" w:styleId="S%d">`, indice)
				if indice+1 < limite {
					fmt.Fprintf(&estilos, `<w:basedOn w:val="S%d"/>`, indice+1)
				}
				estilos.WriteString(`</w:style>`)
			}
			doc, antes := executarRecuoComStyles(t, documentoComEstilo("S0"), []byte(relStylesCanonica), stylesXML(estilos.String()))
			err := doc.AplicarRecuoPrimeiraLinha([]int{0}, 0.5)
			if limite == 32 {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Equal(t, antes, salvarSubstituicao(t, doc))
			}
		})
	}
}

func fixturePPrPorRota(rota, conteudo string, duplicado bool) ([]byte, []byte) {
	ppr := `<w:pPr>` + conteudo + `</w:pPr>`
	if duplicado {
		ppr += `<w:pPr/>`
	}
	documento := `<w:document xmlns:w="` + espacoNomesW + `"><w:body><w:p>`
	var estilos string
	switch rota {
	case "direto":
		documento += ppr
	case "docDefaults":
		estilos = `<w:docDefaults><w:pPrDefault>` + ppr + `</w:pPrDefault></w:docDefaults>`
	case "estilo ativo":
		documento += `<w:pPr><w:pStyle w:val="Body"/></w:pPr>`
		estilos = `<w:style w:type="paragraph" w:styleId="Body">` + ppr + `</w:style>`
	}
	documento += `<w:r><w:t>texto</w:t></w:r></w:p></w:body></w:document>`
	if estilos != "" {
		estilos = `<w:styles xmlns:w="` + espacoNomesW + `">` + estilos + `</w:styles>`
		return []byte(documento), []byte(estilos)
	}
	return []byte(documento), []byte(stylesSemRegras)
}

func TestAplicarRecuoPrimeiraLinhaRejeitaPPrDuplicadoEmCadaRota(t *testing.T) {
	for _, rota := range []string{"direto", "docDefaults", "estilo ativo"} {
		t.Run(rota, func(t *testing.T) {
			documento, estilos := fixturePPrPorRota(rota, `<w:spacing/>`, true)
			doc, antes := executarRecuoComStyles(t, documento, []byte(relStylesCanonica), estilos)
			require.Error(t, doc.AplicarRecuoPrimeiraLinha([]int{0}, 0.5), "rota deve alcançar pPr duplicado")
			assert.Equal(t, antes, salvarSubstituicao(t, doc))
		})
	}
}

func TestAplicarRecuoPrimeiraLinhaRejeitaOrdemPPrInvalidaEmCadaRota(t *testing.T) {
	for _, rota := range []string{"direto", "docDefaults", "estilo ativo"} {
		t.Run(rota, func(t *testing.T) {
			documento, estilos := fixturePPrPorRota(rota, `<w:ind/><w:spacing/>`, false)
			doc, antes := executarRecuoComStyles(t, documento, []byte(relStylesCanonica), estilos)
			require.Error(t, doc.AplicarRecuoPrimeiraLinha([]int{0}, 0.5), "rota deve alcançar filhos fora de ordem")
			assert.Equal(t, antes, salvarSubstituicao(t, doc))
		})
	}
}

func TestAplicarRecuoPrimeiraLinhaRejeitaIndComConteudoEmCadaRota(t *testing.T) {
	for _, rota := range []string{"direto", "docDefaults", "estilo ativo"} {
		for _, conteudo := range []struct{ nome, ind string }{
			{"texto", `<w:ind>texto</w:ind>`},
			{"elemento filho", `<w:ind><w:b/></w:ind>`},
		} {
			t.Run(rota+"/"+conteudo.nome, func(t *testing.T) {
				documento, estilos := fixturePPrPorRota(rota, conteudo.ind, false)
				doc, antes := executarRecuoComStyles(t, documento, []byte(relStylesCanonica), estilos)
				require.Error(t, doc.AplicarRecuoPrimeiraLinha([]int{0}, 0.5), "rota deve alcançar ind com conteúdo inválido")
				assert.Equal(t, antes, salvarSubstituicao(t, doc))
			})
		}
	}
}

func TestAplicarRecuoPrimeiraLinhaRejeitaFirstLineSemNamespaceEmCadaRota(t *testing.T) {
	for _, rota := range []string{"direto", "docDefaults", "estilo ativo"} {
		t.Run(rota, func(t *testing.T) {
			documento, estilos := fixturePPrPorRota(rota, `<w:ind firstLine="240"/>`, false)
			doc, antes := executarRecuoComStyles(t, documento, []byte(relStylesCanonica), estilos)
			require.Error(t, doc.AplicarRecuoPrimeiraLinha([]int{0}, 0.5), "rota deve rejeitar atributo firstLine ambíguo")
			assert.Equal(t, antes, salvarSubstituicao(t, doc))
		})
	}
}

func TestAplicarRecuoPrimeiraLinhaRejeitaAlternateContentEmCadaRota(t *testing.T) {
	for _, rota := range []string{"direto", "docDefaults", "estilo ativo"} {
		t.Run(rota, func(t *testing.T) {
			documento, estilos := fixturePPrPorRota(rota, `<mc:AlternateContent xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006"/>`, false)
			doc, antes := executarRecuoComStyles(t, documento, []byte(relStylesCanonica), estilos)
			require.Error(t, doc.AplicarRecuoPrimeiraLinha([]int{0}, 0.5), "rota deve recusar AlternateContent ativo")
			assert.Equal(t, antes, salvarSubstituicao(t, doc))
		})
	}
}

func TestAplicarRecuoPrimeiraLinhaRejeitaAlternateContentDiretoEmEstiloAtivoSemPPrDireto(t *testing.T) {
	documento := documentoComEstilo("Body")
	estilos := stylesXML(`<w:style w:type="paragraph" w:styleId="Body"><mc:AlternateContent xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006"><mc:Choice Requires="w"><w:pPr><w:ind w:hanging="0"/></w:pPr></mc:Choice></mc:AlternateContent></w:style>`)
	doc, antes := executarRecuoComStyles(t, documento, []byte(relStylesCanonica), estilos)
	require.Error(t, doc.AplicarRecuoPrimeiraLinha([]int{0}, 0.5))
	assert.Equal(t, antes, salvarSubstituicao(t, doc), "AlternateContent ativo recusa atomicamente")
}

func TestAplicarRecuoPrimeiraLinhaIgnoraConflitoSomenteNoHistoricoEmCadaRota(t *testing.T) {
	historico := `<w:pPrChange w:author="histórico"><w:pPr><w:ind w:hanging="0"/></w:pPr></w:pPrChange>`
	for _, rota := range []string{"direto", "docDefaults", "estilo ativo"} {
		t.Run(rota, func(t *testing.T) {
			documento, estilos := fixturePPrPorRota(rota, historico, false)
			pacote := docxComRelacoesStyles(t, documento, []byte(relStylesCanonica), estilos)
			doc := abrirParaSubstituicao(t, pacote)
			_, errBlocos := doc.ExtrairBlocos()
			require.NoError(t, errBlocos, "pré-condição: fixture histórica é XML document válido")
			require.NoError(t, doc.SubstituirParte("word/document.xml", documento), "pré-condição: delta document válido")
			antesStyles := []byte(nil)
			if estilos != nil {
				require.NoError(t, doc.SubstituirParte("word/styles.xml", estilos), "pré-condição: styles existente")
				antesStyles = bytes.Clone(estilos)
			}
			require.NoError(t, doc.AplicarRecuoPrimeiraLinha([]int{0}, 0.5), "histórico não é conflito ativo")
			pacoteSaida := salvarSubstituicao(t, doc)
			saida := lerParte(t, pacoteSaida, "word/document.xml")
			if rota == "direto" {
				assert.Contains(t, string(saida), historico, "histórico direto permanece integral")
			} else {
				stylesSaida := lerParte(t, pacoteSaida, "word/styles.xml")
				assert.Contains(t, string(stylesSaida), historico, "histórico em styles permanece integral")
				assert.Equal(t, antesStyles, stylesSaida, "parte styles não alvo permanece idêntica")
			}
		})
	}
}

func TestAplicarRecuoPrimeiraLinhaRejeitaConflitosEmTodasAsRotas(t *testing.T) {
	conflitos := []struct{ nome, xml string }{
		{"hanging com valor zero", `<w:ind w:hanging="0"/>`},
		{"hangingChars", `<w:ind w:hangingChars="120"/>`},
		{"firstLineChars", `<w:ind w:firstLineChars="120"/>`},
		{"numPr numId zero", `<w:numPr><w:numId w:val="0"/></w:numPr>`},
	}
	for _, rota := range []string{"direto", "docDefaults", "estilo ativo"} {
		for _, conflito := range conflitos {
			t.Run(rota+"/"+conflito.nome, func(t *testing.T) {
				documento, estilos := fixturePPrPorRota(rota, conflito.xml, false)
				doc, antes := executarRecuoComStyles(t, documento, []byte(relStylesCanonica), estilos)
				require.Error(t, doc.AplicarRecuoPrimeiraLinha([]int{0}, 0.5), "rota deve rejeitar o conflito herdado/direto especificado")
				assert.Equal(t, antes, salvarSubstituicao(t, doc), "rejeição preserva todos os deltas")
			})
		}
	}
}

func TestAplicarRecuoPrimeiraLinhaSobrescreveFirstLineHerdado(t *testing.T) {
	for _, rota := range []string{"docDefaults", "estilo ativo"} {
		t.Run(rota, func(t *testing.T) {
			documento, estilos := fixturePPrPorRota(rota, `<w:ind w:left="240" w:firstLine="360"/>`, false)
			doc, _ := executarRecuoComStyles(t, documento, []byte(relStylesCanonica), estilos)
			require.NoError(t, doc.AplicarRecuoPrimeiraLinha([]int{0}, 0.5), "firstLine herdado é sobrescrito diretamente")
			pacoteSaida := salvarSubstituicao(t, doc)
			assert.Contains(t, string(lerParte(t, pacoteSaida, "word/document.xml")), `<w:ind w:firstLine="283"/>`)
			if estilos != nil {
				assert.Contains(t, string(lerParte(t, pacoteSaida, "word/styles.xml")), `w:firstLine="360"`, "estilo original permanece inalterado")
			}
		})
	}
}

func TestAplicarRecuoPrimeiraLinhaValidaReceptorAntesDaMedida(t *testing.T) {
	var doc *Documento
	err := doc.AplicarRecuoPrimeiraLinha(nil, math.NaN())
	require.Error(t, err)
	var argumento *errors.ErroArgumentoNulo
	require.ErrorAs(t, err, &argumento, "receptor nil deve preceder validação da medida")
}

func TestAplicarRecuoPrimeiraLinhaListaVaziaValidaEntradaSemIO(t *testing.T) {
	doc := &Documento{}
	antes := salvarSubstituicao(t, doc)
	require.NoError(t, doc.AplicarRecuoPrimeiraLinha(nil, 0), "lista vazia e zero são válidos sem consultar partes")
	require.NoError(t, doc.AplicarRecuoPrimeiraLinha([]int{}, 0.5), "lista vazia valida a medida e termina sem I/O")
	assert.Equal(t, antes, salvarSubstituicao(t, doc), "operação vazia não cria deltas nem abre partes")
	err := doc.AplicarRecuoPrimeiraLinha(nil, -1)
	require.Error(t, err, "seleção vazia não dispensa validação da medida")
	assert.Equal(t, antes, salvarSubstituicao(t, doc), "erro preserva o estado")
}

func TestAplicarRecuoPrimeiraLinhaAceitaZeroEConversaoArredondadaParaZero(t *testing.T) {
	for _, caso := range []struct {
		nome string
		cm   float64
	}{
		{"zero", 0},
		{"positivo arredondado para zero", 0.0001},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			doc := abrirParaSubstituicao(t, docxSintetico(t, alinhamentoFixture(`<w:p/>`)))
			require.NoError(t, doc.AplicarRecuoPrimeiraLinha([]int{0}, caso.cm))
			assert.Contains(t, string(lerParte(t, salvarSubstituicao(t, doc), "word/document.xml")), `<w:ind w:firstLine="0"/>`)
		})
	}
}

func TestAplicarRecuoPrimeiraLinhaPreparaTodosOsAlvosAntesDeSubstituir(t *testing.T) {
	fonte := alinhamentoFixture(`<w:p><w:r><w:t>primeiro</w:t></w:r></w:p><w:p><w:pPr><w:ind w:hanging="0"/></w:pPr><w:r><w:t>conflito</w:t></w:r></w:p>`)
	doc := abrirParaSubstituicao(t, docxSintetico(t, fonte))
	require.NoError(t, doc.SubstituirParte("word/document.xml", fonte), "pré-condição: delta com primeiro alvo válido e segundo conflitante")
	antes := salvarSubstituicao(t, doc)
	require.Error(t, doc.AplicarRecuoPrimeiraLinha([]int{0, 1}, 0.5), "o conflito no segundo alvo deve rejeitar a operação")
	assert.Equal(t, antes, salvarSubstituicao(t, doc), "preflight com erro não pode persistir edição parcial do primeiro alvo")
}
