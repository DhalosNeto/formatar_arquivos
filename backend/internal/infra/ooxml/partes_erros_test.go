package ooxml

import (
	"archive/zip"
	"bytes"
	"testing"

	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubstituirParteRejeitaNomeInseguroMesmoExistente(t *testing.T) {
	casos := []struct{ nome, alvo string }{
		{"vazio", ""},
		{"absoluto", "/word/document.xml"},
		{"barra invertida", `word\document.xml`},
		{"NUL", "word/\x00document.xml"},
		{"drive Windows absoluto", "C:/document.xml"},
		{"drive Windows relativo", "C:document.xml"},
		{"segmento vazio", "word//document.xml"},
		{"segmento ponto", "word/./document.xml"},
		{"segmento pai", "word/../document.xml"},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			entradas := append(partesBaseDocx(documentoXMLMinimo("original")), entradaZip{nome: caso.alvo, conteudo: []byte("parte insegura existente"), metodo: zip.Store})
			doc := abrirParaSubstituicao(t, montarZip(t, entradas))
			require.NotNil(t, doc.parte(caso.alvo), "o nome inseguro deve existir exatamente no ZIP")
			require.NoError(t, doc.SubstituirParte("word/document.xml", documentoXMLMinimo("delta anterior")))
			antes := salvarSubstituicao(t, doc)
			err := doc.SubstituirParte(caso.alvo, []byte("novo"))
			require.Error(t, err)
			var validacao *errors.ErroValidacao
			assert.ErrorAs(t, err, &validacao)
			assert.Equal(t, antes, salvarSubstituicao(t, doc))
		})
	}
}

type destinoDeltaComFalha struct {
	limite  int
	escrito int
	erro    error
}

func (destino *destinoDeltaComFalha) Write(conteudo []byte) (int, error) {
	quantidade := min(len(conteudo), destino.limite-destino.escrito)
	destino.escrito += quantidade
	if quantidade < len(conteudo) {
		return quantidade, destino.erro
	}
	return quantidade, nil
}

func TestSubstituirParteFalhasEscritaECloseSemVazamento(t *testing.T) {
	casos := []struct {
		nome    string
		tamanho int
		limite  int
	}{
		// O pacote pequeno cabe no bufio do ZIP: a falha surge no Close.
		{"flush final do pacote", 20, 0},
		// Store evita que a compressão reduza a escrita abaixo do buffer.
		{"escrita parcial do delta", 32 << 10, 8192},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			entradas := partesBaseDocx(documentoXMLMinimo("original"))
			entradas[2].metodo = zip.Store
			doc := abrirParaSubstituicao(t, montarZip(t, entradas))
			novo := bytes.Repeat([]byte("x"), caso.tamanho)
			require.NoError(t, doc.SubstituirParte("word/document.xml", novo))
			esperado := salvarSubstituicao(t, doc)
			var mensagem string
			for _, segredo := range []string{"storage segredo A", "storage segredo B"} {
				destino := &destinoDeltaComFalha{limite: caso.limite, erro: errors.Novo(segredo)}
				err := doc.Salvar(destino)
				require.Error(t, err)
				var aplicacao *errors.ErroAplicacao
				assert.ErrorAs(t, err, &aplicacao)
				assert.NotContains(t, err.Error(), segredo)
				assert.NotContains(t, err.Error(), "word/document.xml")
				assert.NotContains(t, err.Error(), string(novo))
				if mensagem != "" {
					assert.Equal(t, mensagem, err.Error(), "mensagem fixa independente do erro externo")
				}
				mensagem = err.Error()
				assert.Equal(t, caso.limite, destino.escrito)
				assert.Equal(t, esperado, salvarSubstituicao(t, doc))
			}
		})
	}
}
