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

func TestAplicarEspacamentoAntesGravaTwipsEPreservaDepois(t *testing.T) {
	casos := []struct {
		nome        string
		pontos      float64
		expectativa string
	}{{"zero", 0, "0"}, {"fracionário", 1.1, "22"}}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			fonte := alinhamentoFixture(`<w:p><w:pPr><w:spacing w:after="75"/></w:pPr></w:p>`)
			doc := abrirParaSubstituicao(t, docxSintetico(t, fonte))
			require.NoError(t, doc.AplicarEspacamentoAntes([]int{0}, caso.pontos))
			attrs := atributosEspacamentoAtual(t, lerParte(t, salvarSubstituicao(t, doc), "word/document.xml"))
			assert.Equal(t, caso.expectativa, attrs["before"])
			assert.Equal(t, "75", attrs["after"])
		})
	}
}

func TestAplicarEspacamentoDepoisGravaTwipsEPreservaAntes(t *testing.T) {
	casos := []struct {
		nome        string
		pontos      float64
		expectativa string
	}{{"zero", 0, "0"}, {"fracionário", 2.5, "50"}}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			fonte := alinhamentoFixture(`<w:p><w:pPr><w:spacing w:before="65"/></w:pPr></w:p>`)
			doc := abrirParaSubstituicao(t, docxSintetico(t, fonte))
			require.NoError(t, doc.AplicarEspacamentoDepois([]int{0}, caso.pontos))
			attrs := atributosEspacamentoAtual(t, lerParte(t, salvarSubstituicao(t, doc), "word/document.xml"))
			assert.Equal(t, "65", attrs["before"])
			assert.Equal(t, caso.expectativa, attrs["after"])
		})
	}
}

func atributosEspacamentoAtual(t *testing.T, conteudo []byte) map[string]string {
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
					if attr.Name.Space == espacoNomesW && (attr.Name.Local == "before" || attr.Name.Local == "after") {
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

func TestAplicarEspacamentoRejeitaMedidasInvalidasSemAlterarDocumento(t *testing.T) {
	metodos := []struct {
		nome    string
		aplicar func(*Documento, []int, float64) error
	}{
		{"antes", (*Documento).AplicarEspacamentoAntes},
		{"depois", (*Documento).AplicarEspacamentoDepois},
	}
	medidas := []struct {
		nome  string
		valor float64
	}{
		{"negativa", -0.01},
		{"NaN", math.NaN()},
		{"infinito positivo", math.Inf(1)},
		{"infinito negativo", math.Inf(-1)},
		{"overflow", float64(math.MaxInt32)/20 + 1},
	}
	for _, metodo := range metodos {
		for _, medida := range medidas {
			t.Run(metodo.nome+"/"+medida.nome, func(t *testing.T) {
				doc := abrirParaSubstituicao(t, docxSintetico(t, alinhamentoFixture(`<w:p><w:pPr><w:spacing w:before="10" w:after="20"/></w:pPr></w:p>`)))
				antes := salvarSubstituicao(t, doc)
				err := metodo.aplicar(doc, []int{0}, medida.valor)
				var validacao *errors.ErroValidacao
				require.ErrorAs(t, err, &validacao, "medida inválida deve falhar na validação antes de acessar o alvo")
				assert.Equal(t, antes, salvarSubstituicao(t, doc))
			})
		}
	}
}

func TestAplicarEspacamentoValidaReceptorNuloAntesDaMedida(t *testing.T) {
	metodos := []struct {
		nome    string
		aplicar func(*Documento, []int, float64) error
	}{
		{"antes", (*Documento).AplicarEspacamentoAntes},
		{"depois", (*Documento).AplicarEspacamentoDepois},
	}
	for _, metodo := range metodos {
		t.Run(metodo.nome, func(t *testing.T) {
			var doc *Documento
			err := metodo.aplicar(doc, nil, math.NaN())
			var nulo *errors.ErroArgumentoNulo
			require.ErrorAs(t, err, &nulo, "receptor nulo tem precedência sobre medida inválida")
			assert.Equal(t, "documento", nulo.Argumento)
		})
	}
}

func TestAplicarEspacamentoListaVaziaValidaMedidaENaoFazIO(t *testing.T) {
	metodos := []struct {
		nome    string
		aplicar func(*Documento, []int, float64) error
	}{
		{"antes", (*Documento).AplicarEspacamentoAntes},
		{"depois", (*Documento).AplicarEspacamentoDepois},
	}
	for _, metodo := range metodos {
		t.Run(metodo.nome, func(t *testing.T) {
			doc := abrirParaSubstituicao(t, docxSintetico(t, alinhamentoFixture(`<w:p/>`)))
			malformado := []byte(`<w:document incompleto`)
			require.NoError(t, doc.SubstituirParte("word/document.xml", malformado), "pré-condição: delta opaco instalado")
			antes := salvarSubstituicao(t, doc)
			require.NoError(t, metodo.aplicar(doc, []int{}, 0), "seleção vazia válida não deve ler/parsear document.xml")
			assert.Equal(t, antes, salvarSubstituicao(t, doc))
			err := metodo.aplicar(doc, nil, -1)
			var validacao *errors.ErroValidacao
			require.ErrorAs(t, err, &validacao, "medida inválida continua sendo validada antes do no-op")
			assert.Equal(t, antes, salvarSubstituicao(t, doc))
		})
	}
}

func TestAplicarEspacamentoRejeitaReferenciasAtomicamente(t *testing.T) {
	metodos := []struct {
		nome    string
		aplicar func(*Documento, []int, float64) error
	}{
		{"antes", (*Documento).AplicarEspacamentoAntes},
		{"depois", (*Documento).AplicarEspacamentoDepois},
	}
	casos := []struct {
		nome string
		refs []int
		xml  string
	}{
		{"negativa", []int{-1}, `<w:p/>`},
		{"duplicada", []int{0, 0}, `<w:p/>`},
		{"acima do máximo", make([]int, 100001), `<w:p/>`},
		{"alvo tabela", []int{0}, `<w:tbl/>`},
		{"alvo posterior inválido", []int{0, 1}, `<w:p/><w:tbl/>`},
		{"fora do documento", []int{1}, `<w:p/>`},
	}
	for _, metodo := range metodos {
		for _, caso := range casos {
			t.Run(metodo.nome+"/"+caso.nome, func(t *testing.T) {
				fonte := alinhamentoFixture(caso.xml)
				doc := abrirParaSubstituicao(t, docxSintetico(t, fonte))
				require.NoError(t, doc.SubstituirParte("word/document.xml", fonte), "pré-condição: documento XML e delta inicial válidos")
				antes := salvarSubstituicao(t, doc)
				err := metodo.aplicar(doc, caso.refs, 1.25)
				require.Error(t, err, "entrada deve alcançar a guarda do critério: %s", caso.nome)
				assert.Equal(t, antes, salvarSubstituicao(t, doc), "falha preserva o delta")
			})
		}
	}
}

func TestAplicarEspacamentoRejeitaXMLMalformadoAtomicamente(t *testing.T) {
	metodos := []struct {
		nome    string
		aplicar func(*Documento, []int, float64) error
	}{
		{"antes", (*Documento).AplicarEspacamentoAntes},
		{"depois", (*Documento).AplicarEspacamentoDepois},
	}
	for _, metodo := range metodos {
		t.Run(metodo.nome, func(t *testing.T) {
			doc := abrirParaSubstituicao(t, docxSintetico(t, alinhamentoFixture(`<w:p/>`)))
			malformado := []byte(`<w:document xmlns:w="` + espacoNomesW + `"><w:body><w:p>`)
			require.NoError(t, doc.SubstituirParte("word/document.xml", malformado), "pré-condição: delta XML malformado aceito como opaco")
			antes := salvarSubstituicao(t, doc)
			err := metodo.aplicar(doc, []int{0}, 1.5)
			require.Error(t, err, "seleção válida alcança parsing do XML truncado")
			assert.Equal(t, antes, salvarSubstituicao(t, doc))
		})
	}
}

func TestAplicarEspacamentoRepetidoEByteIdempotente(t *testing.T) {
	metodos := []struct {
		nome    string
		aplicar func(*Documento, []int, float64) error
	}{
		{"antes", (*Documento).AplicarEspacamentoAntes},
		{"depois", (*Documento).AplicarEspacamentoDepois},
	}
	for _, metodo := range metodos {
		t.Run(metodo.nome, func(t *testing.T) {
			doc := abrirParaSubstituicao(t, docxSintetico(t, alinhamentoFixture(`<w:p><w:pPr><w:spacing w:before="1" w:after="2"/></w:pPr></w:p>`)))
			require.NoError(t, metodo.aplicar(doc, []int{0}, 3.25))
			primeira := salvarSubstituicao(t, doc)
			for range 10 {
				require.NoError(t, metodo.aplicar(doc, []int{0}, 3.25))
			}
			assert.Equal(t, primeira, salvarSubstituicao(t, doc))
		})
	}
}

func TestAplicarEspacamentoPreservaAtributosHistoricoTextoEstilosEPartes(t *testing.T) {
	fonte := alinhamentoFixture(`<w:p><w:pPr><w:spacing w:before="12" w:after="24" w:beforeLines="2" w:afterLines="3" w:beforeAutospacing="1" w:afterAutospacing="1" w:line="480" w:lineRule="exact"/><w:contextualSpacing w:val="1"/><w:pPrChange w:id="7"><w:pPr><w:spacing w:before="33" w:after="44" w:beforeLines="4" w:afterLines="5" w:beforeAutospacing="0" w:afterAutospacing="0" w:line="777" w:lineRule="atLeast"/><w:contextualSpacing w:val="0"/></w:pPr></w:pPrChange></w:pPr><w:r><w:t xml:space="preserve"> texto 😀 café </w:t></w:r></w:p><w:p><w:r><w:t>inalterado</w:t></w:r></w:p>`)
	textos := extrairTextosWT(t, fonte)
	require.Equal(t, []string{" texto 😀 café ", "inalterado"}, textos, "pré-condição: fixture inclui dois parágrafos e texto Unicode")
	partes := append(partesBaseDocx(fonte),
		entradaZip{nome: "word/styles.xml", conteudo: []byte(`<w:styles xmlns:w="` + espacoNomesW + `"><w:style w:type="paragraph" w:styleId="Corpo"/></w:styles>`), metodo: 0},
		entradaZip{nome: "word/media/imagem.bin", conteudo: []byte{0, 255, 17}, metodo: 0},
	)
	original := montarZip(t, partes)
	doc := abrirParaSubstituicao(t, original)
	require.NoError(t, doc.AplicarEspacamentoAntes([]int{0}, 1.25))
	require.NoError(t, doc.AplicarEspacamentoDepois([]int{0}, 2.75))
	saida := salvarSubstituicao(t, doc)
	xmlSaida := lerParte(t, saida, "word/document.xml")
	saidaTexto := string(xmlSaida)
	esperadoXML := bytes.Replace(bytes.Clone(fonte), []byte(`w:before="12"`), []byte(`w:before="25"`), 1)
	esperadoXML = bytes.Replace(esperadoXML, []byte(`w:after="24"`), []byte(`w:after="55"`), 1)
	assert.Equal(t, esperadoXML, xmlSaida, "somente before e after do espaçamento atual devem mudar no XML")
	assert.Equal(t, []rune(strings.Join(textos, "")), []rune(strings.Join(extrairTextosWT(t, xmlSaida), "")), "sequência de runes preservada")
	for _, esperado := range []string{
		`w:before="25"`, `w:after="55"`, `w:beforeLines="2"`, `w:afterLines="3"`,
		`w:beforeAutospacing="1"`, `w:afterAutospacing="1"`, `w:line="480"`, `w:lineRule="exact"`, `<w:contextualSpacing w:val="1"/>`,
		`<w:pPrChange w:id="7"><w:pPr><w:spacing w:before="33" w:after="44" w:beforeLines="4" w:afterLines="5" w:beforeAutospacing="0" w:afterAutospacing="0" w:line="777" w:lineRule="atLeast"/><w:contextualSpacing w:val="0"/></w:pPr></w:pPrChange>`,
		`>inalterado<`,
	} {
		assert.Contains(t, saidaTexto, esperado)
	}
	for _, parte := range listarPartes(t, original) {
		if parte != "word/document.xml" {
			assert.Equal(t, lerParte(t, original, parte), lerParte(t, saida, parte), "parte não-alvo %s", parte)
		}
	}
}
