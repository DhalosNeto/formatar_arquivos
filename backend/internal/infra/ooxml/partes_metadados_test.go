package ooxml

import (
	"archive/zip"
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pacoteComMetadados(t *testing.T, metodo uint16, extra []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	escritor := zip.NewWriter(&buf)
	for _, entrada := range partesBaseDocx(documentoXMLMinimo("original")) {
		cabecalho := &zip.FileHeader{
			Name: entrada.nome, Method: metodo, Comment: "comentário preservado",
			Modified: time.Date(2020, 3, 4, 5, 6, 8, 0, time.UTC),
			Extra:    bytes.Clone(extra),
		}
		cabecalho.SetMode(0640)
		parte, err := escritor.CreateHeader(cabecalho)
		require.NoError(t, err)
		_, err = parte.Write(entrada.conteudo)
		require.NoError(t, err)
	}
	require.NoError(t, escritor.Close())
	return buf.Bytes()
}

func TestSubstituirParteMetadadosSemCrescimentoOuMutacaoOrigem(t *testing.T) {
	// TLV desconhecido seguido de ZIP64 vazio: os tamanhos reais cabem em 32 bits.
	extra := []byte{0xfe, 0xca, 2, 0, 7, 8, 1, 0, 0, 0}
	for _, metodo := range []uint16{zip.Store, zip.Deflate} {
		nome := "armazenado"
		if metodo == zip.Deflate {
			nome = "comprimido"
		}
		t.Run(nome, func(t *testing.T) {
			original := pacoteComMetadados(t, metodo, extra)
			doc := abrirParaSubstituicao(t, original)
			cabecalhos := make([]zip.FileHeader, len(doc.arquivos))
			for i, arquivo := range doc.arquivos {
				cabecalhos[i] = arquivo.FileHeader
				cabecalhos[i].Extra = bytes.Clone(arquivo.Extra)
			}
			novo := documentoXMLMinimo("alterado e maior 😀")
			require.NoError(t, doc.SubstituirParte("word/document.xml", novo))
			saida := salvarSubstituicao(t, doc)
			assert.Equal(t, saida, salvarSubstituicao(t, doc))
			reaberto := abrirParaSubstituicao(t, saida)
			for i, arquivo := range doc.arquivos {
				assert.Equal(t, cabecalhos[i], arquivo.FileHeader, "Salvar não pode alterar header ou Extra original")
				atual := reaberto.arquivos[i]
				if arquivo.Name != "word/document.xml" {
					assert.Equal(t, cabecalhos[i], atual.FileHeader, "ZIP64 de partes intactas permanece")
					continue
				}
				assert.Equal(t, arquivo.Method, atual.Method)
				assert.Equal(t, arquivo.Comment, atual.Comment)
				assert.Equal(t, arquivo.ExternalAttrs, atual.ExternalAttrs)
				assert.Equal(t, arquivo.ModifiedDate, atual.ModifiedDate)
				assert.Equal(t, arquivo.ModifiedTime, atual.ModifiedTime)
				esperado := append(bytes.Clone(arquivo.Extra[:6]), arquivo.Extra[10:]...)
				assert.Equal(t, esperado, atual.Extra, "só ZIP64 é removido; timestamp não duplica")
			}
			require.NoError(t, reaberto.SubstituirParte("word/document.xml", novo))
			assert.Equal(t, saida, salvarSubstituicao(t, reaberto), "novo ciclo não faz extras crescerem")
		})
	}
}

func TestSubstituirParteRejeitaExtraTruncadoAntesDeMudarEstado(t *testing.T) {
	casos := []struct {
		nome  string
		extra []byte
	}{
		{"identificador incompleto", []byte{1}},
		{"comprimento incompleto", []byte{1, 0, 2}},
		{"payload incompleto", []byte{0xfe, 0xca, 2, 0, 7}},
		{"sufixo truncado após TLV válido", []byte{0xfe, 0xca, 0, 0, 1}},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			doc := abrirParaSubstituicao(t, docxSintetico(t, documentoXMLMinimo("original")))
			// Simula o header lido de uma entrada adversarial, sem depender de
			// normalizações de Extra feitas pelo escritor de fixtures.
			doc.parte("word/document.xml").Extra = bytes.Clone(caso.extra)
			antes := salvarSubstituicao(t, doc)
			require.Error(t, doc.SubstituirParte("word/document.xml", []byte("novo")))
			assert.Equal(t, antes, salvarSubstituicao(t, doc))
			assert.Equal(t, caso.extra, doc.parte("word/document.xml").Extra)
		})
	}
}

func TestSubstituirParteRejeitaMetodoNaoSuportado(t *testing.T) {
	doc := abrirParaSubstituicao(t, docxSintetico(t, documentoXMLMinimo("original")))
	doc.parte("word/document.xml").Method = 99
	antes := salvarSubstituicao(t, doc)
	require.Error(t, doc.SubstituirParte("word/document.xml", []byte("novo")))
	assert.Equal(t, antes, salvarSubstituicao(t, doc))
}
