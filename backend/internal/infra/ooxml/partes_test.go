package ooxml

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"hash/crc32"
	"io"
	"strings"
	"testing"

	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func abrirParaSubstituicao(t *testing.T, dados []byte) *Documento {
	t.Helper()
	doc, err := Abrir(bytes.NewReader(dados), int64(len(dados)))
	require.NoError(t, err)
	return doc
}

func salvarSubstituicao(t *testing.T, doc *Documento) []byte {
	t.Helper()
	var saida bytes.Buffer
	require.NoError(t, doc.Salvar(&saida))
	return saida.Bytes()
}

func TestSubstituirParteBytesOpacosECopiaDefensiva(t *testing.T) {
	casos := []struct {
		nome     string
		conteudo []byte
	}{
		{"nulo substitui sem remover", nil},
		{"vazio substitui sem remover", []byte{}},
		{"XML malformado aceito", []byte("<xml incompleto")},
		{"binário opaco", []byte{0, 255, 128, 1}},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			original := docxSintetico(t, documentoXMLMinimo("original"))
			doc := abrirParaSubstituicao(t, original)
			entrada := bytes.Clone(caso.conteudo)
			esperado := bytes.Clone(entrada)
			require.NoError(t, doc.SubstituirParte("word/document.xml", entrada))
			for i := range entrada {
				entrada[i] ^= 255
			}
			saida := salvarSubstituicao(t, doc)
			assert.True(t, bytes.Equal(esperado, lerParte(t, saida, "word/document.xml")))
			assert.Equal(t, listarPartes(t, original), listarPartes(t, saida))
			assert.Equal(t, saida, salvarSubstituicao(t, doc))
		})
	}
}

func TestSubstituirPartePreservaPartesIntactasETexto(t *testing.T) {
	for _, metodo := range []uint16{zip.Store, zip.Deflate} {
		nome := "sem compressão"
		if metodo == zip.Deflate {
			nome = "com compressão"
		}
		t.Run(nome, func(t *testing.T) {
			xml := documentoXMLMinimo("cafe\u0301 😀 𠀀")
			entradas := append(partesBaseDocx(xml), entradaZip{nome: "word/media/imagem.bin", conteudo: []byte{0, 255, 1}, metodo: zip.Store})
			entradas[2].metodo = metodo
			original := montarZip(t, entradas)
			copiaOriginal := bytes.Clone(original)
			doc := abrirParaSubstituicao(t, original)
			novo := bytes.Replace(xml, []byte("<w:p>"), []byte("<w:p><w:pPr><w:keepNext/></w:pPr>"), 1)
			require.NoError(t, doc.SubstituirParte("word/document.xml", novo))
			saida := salvarSubstituicao(t, doc)
			antes, err := zip.NewReader(bytes.NewReader(original), int64(len(original)))
			require.NoError(t, err)
			depois, err := zip.NewReader(bytes.NewReader(saida), int64(len(saida)))
			require.NoError(t, err)
			require.Len(t, depois.File, len(antes.File))
			for i, parte := range antes.File {
				atual := depois.File[i]
				assert.Equal(t, parte.Name, atual.Name)
				if parte.Name == "word/document.xml" {
					assert.Equal(t, metodo, atual.Method)
					assert.Equal(t, uint64(len(novo)), atual.UncompressedSize64)
					assert.Equal(t, crc32.ChecksumIEEE(novo), atual.CRC32)
					continue
				}
				assert.Equal(t, parte.FileHeader, atual.FileHeader)
				rawAntes, err := parte.OpenRaw()
				require.NoError(t, err)
				rawDepois, err := atual.OpenRaw()
				require.NoError(t, err)
				bytesAntes, err := io.ReadAll(rawAntes)
				require.NoError(t, err)
				bytesDepois, err := io.ReadAll(rawDepois)
				require.NoError(t, err)
				assert.Equal(t, bytesAntes, bytesDepois)
				assert.Equal(t, sha256.Sum256(lerParte(t, original, parte.Name)), sha256.Sum256(lerParte(t, saida, parte.Name)))
			}
			assert.Equal(t, novo, lerParte(t, saida, "word/document.xml"))
			assert.Equal(t, []rune(strings.Join(extrairTextosWT(t, xml), "")), []rune(strings.Join(extrairTextosWT(t, lerParte(t, saida, "word/document.xml")), "")))
			assert.Equal(t, copiaOriginal, original)
			assert.Equal(t, saida, salvarSubstituicao(t, doc))
		})
	}
}

func TestSubstituirParteExtrairBlocosLeUltimoDelta(t *testing.T) {
	doc := abrirParaSubstituicao(t, docxSintetico(t, documentoXMLMinimo("original")))
	for _, texto := range []string{"primeiro", "último 😀 cafe\u0301"} {
		require.NoError(t, doc.SubstituirParte("word/document.xml", documentoXMLMinimo(texto)))
		blocos, err := doc.ExtrairBlocos()
		require.NoError(t, err)
		require.Len(t, blocos, 1)
		assert.Equal(t, texto, blocos[0].Texto)
		reaberto := abrirParaSubstituicao(t, salvarSubstituicao(t, doc))
		persistidos, err := reaberto.ExtrairBlocos()
		require.NoError(t, err)
		assert.Equal(t, blocos, persistidos)
	}
	require.NoError(t, doc.SubstituirParte("word/document.xml", nil))
	_, err := doc.ExtrairBlocos()
	require.Error(t, err, "delta vazio não pode ler XML original")
	_, err = abrirParaSubstituicao(t, salvarSubstituicao(t, doc)).ExtrairBlocos()
	require.Error(t, err)
}

func TestSubstituirParteRejeitaAlvoSemAlterarDelta(t *testing.T) {
	for _, nome := range []string{"", "/word/document.xml", `word\document.xml`, "word/\x00document.xml", "C:/document.xml", "word//document.xml", "word/./document.xml", "word/../document.xml", "Word/document.xml", "ausente.xml", "word/"} {
		t.Run("alvo rejeitado "+nome, func(t *testing.T) {
			entradas := append(partesBaseDocx(documentoXMLMinimo("original")), entradaZip{nome: "word/", metodo: zip.Store})
			doc := abrirParaSubstituicao(t, montarZip(t, entradas))
			require.NoError(t, doc.SubstituirParte("word/document.xml", documentoXMLMinimo("delta")))
			antes := salvarSubstituicao(t, doc)
			require.Error(t, doc.SubstituirParte(nome, []byte("inválido")))
			assert.Equal(t, antes, salvarSubstituicao(t, doc))
		})
	}
}

func TestSubstituirParteRejeitaDuplicataENil(t *testing.T) {
	entradas := append(partesBaseDocx(documentoXMLMinimo("original")), entradaZip{nome: "word/document.xml", conteudo: documentoXMLMinimo("duplicado"), metodo: zip.Store})
	doc := abrirParaSubstituicao(t, montarZip(t, entradas))
	antes := salvarSubstituicao(t, doc)
	require.Error(t, doc.SubstituirParte("word/document.xml", []byte("novo")))
	assert.Equal(t, antes, salvarSubstituicao(t, doc))
	var nulo *Documento
	err := nulo.SubstituirParte("word/document.xml", nil)
	require.Error(t, err)
	var argumento *errors.ErroArgumentoNulo
	assert.ErrorAs(t, err, &argumento)
	require.Error(t, (&Documento{}).SubstituirParte("word/document.xml", nil))
}

func TestSubstituirPartePreservaDeltaAposFalhaSalvar(t *testing.T) {
	doc := abrirParaSubstituicao(t, docxSintetico(t, documentoXMLMinimo("original")))
	novo := documentoXMLMinimo("delta preservado")
	require.NoError(t, doc.SubstituirParte("word/document.xml", novo))
	esperado := salvarSubstituicao(t, doc)
	err := doc.Salvar(&escritorComFalha{falhaApos: 50})
	require.Error(t, err)
	var aplicacao *errors.ErroAplicacao
	assert.ErrorAs(t, err, &aplicacao)
	assert.NotContains(t, err.Error(), errFalhaEscritaSimulada.Error())
	assert.NotContains(t, err.Error(), "word/document.xml")
	assert.NotContains(t, err.Error(), "delta preservado")
	assert.Equal(t, esperado, salvarSubstituicao(t, doc))
}
