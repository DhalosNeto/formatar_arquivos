package ooxml

import (
	"bytes"
	"runtime"
	"testing"

	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Deliberadamente sequencial: um único buffer de entrada e no máximo um
// orçamento de deltas retidos. Não paralelizar este teste de fronteira real.
func TestSubstituirParteLimitesIndividuaisESomaRetida(t *testing.T) {
	limite := int(vo.TamanhoDescomprimidoMaximoBytes)
	entrada := make([]byte, limite+1)
	doc := abrirParaSubstituicao(t, docxSintetico(t, documentoXMLMinimo("original")))
	antes := salvarSubstituicao(t, doc)
	require.Error(t, doc.SubstituirParte("word/document.xml", entrada))
	assert.Equal(t, antes, salvarSubstituicao(t, doc), "excesso individual não muda estado")

	// A soma considera apenas deltas, não os bytes das partes originais.
	require.NoError(t, doc.SubstituirParte("word/document.xml", entrada[:limite]))
	require.Error(t, doc.SubstituirParte("_rels/.rels", entrada[:1]))
	// Descontar o delta antigo antes de avaliar a substituição.
	require.NoError(t, doc.SubstituirParte("word/document.xml", entrada[:limite-1]))
	runtime.GC()
	require.NoError(t, doc.SubstituirParte("_rels/.rels", entrada[:1]))
	noLimite := salvarSubstituicao(t, doc)
	require.Error(t, doc.SubstituirParte("_rels/.rels", entrada[:2]))
	assert.Equal(t, noLimite, salvarSubstituicao(t, doc), "excesso agregado preserva ambos os deltas")

	// Reduzir libera o orçamento; nil também deve liberar o delta anterior.
	require.NoError(t, doc.SubstituirParte("word/document.xml", nil))
	runtime.GC()
	require.NoError(t, doc.SubstituirParte("_rels/.rels", entrada[:limite]))
	require.Error(t, doc.SubstituirParte("word/document.xml", entrada[:1]))
	require.NoError(t, doc.SubstituirParte("_rels/.rels", []byte{}))
	runtime.GC()
	require.NoError(t, doc.SubstituirParte("word/document.xml", entrada[:limite]))
}

func TestSubstituirParteExtracaoRejeitaDeltaInvalido(t *testing.T) {
	casos := []struct {
		nome     string
		conteudo []byte
	}{
		{"nulo", nil},
		{"vazio", []byte{}},
		{"XML truncado", []byte("<w:document")},
		{"entidade externa não resolvida", []byte(`<!DOCTYPE w:document [<!ENTITY externa SYSTEM "file:///arquivo-que-nao-deve-ser-lido">]><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>&externa;</w:t></w:r></w:p></w:body></w:document>`)},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			doc := abrirParaSubstituicao(t, docxSintetico(t, documentoXMLMinimo("original válido")))
			require.NoError(t, doc.SubstituirParte("word/document.xml", caso.conteudo))
			_, err := doc.ExtrairBlocos()
			require.Error(t, err, "extração não pode reutilizar a parte original")
			saida := salvarSubstituicao(t, doc)
			assert.True(t, bytes.Equal(caso.conteudo, lerParte(t, saida, "word/document.xml")))
			_, errReaberto := abrirParaSubstituicao(t, saida).ExtrairBlocos()
			require.Error(t, errReaberto)
			assert.Equal(t, err.Error(), errReaberto.Error())
		})
	}
}
