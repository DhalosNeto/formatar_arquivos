// RED (TDD): AnalisarEstrutura ainda não existe em internal/infra/ooxml.
// Espera-se falha de compilação ("undefined: AnalisarEstrutura") até o
// codador entregar a função.
//
// Contrato travado pela ficha do investigador (F2, "análise ponta a ponta"):
//
//	func AnalisarEstrutura(docx []byte) ([]cdm.Bloco, error)
//
// Encadeia Abrir -> ExtrairBlocos -> ClassificarPorEstiloDocx (por bloco) ->
// cdm.AplicarHeuristica. Existe para o executor da fila (internal/infra/fila)
// não precisar conhecer a ordem das três camadas — só chamar uma função.
//
// A expectativa do caminho feliz é a MESMA de
// TestAplicarHeuristicaFixtureRealIdentificaEstruturaDoArtigo, em
// heuristica_integrada_test.go: reusa expectativaFixtureReal40Blocos() (mesmo
// pacote) em vez de duplicar a tabela de 40 entradas.
package ooxml

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-halos/formatador/internal/domain/cdm"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// TestAnalisarEstruturaFixtureRealProduzOsMesmosPapeisQueOPipelineManual prova
// que a função de conveniência não muda o resultado do pipeline já testado
// passo a passo: mesmos 40 blocos, mesmos papéis, na mesma ordem.
func TestAnalisarEstruturaFixtureRealProduzOsMesmosPapeisQueOPipelineManual(t *testing.T) {
	t.Parallel()

	dados := lerFixture(t, "artigo-real-libreoffice.docx")

	blocos, err := AnalisarEstrutura(dados)
	require.NoError(t, err)
	require.Len(t, blocos, 40)

	esperados := expectativaFixtureReal40Blocos()
	require.Len(t, esperados, 40, "ficha de expectativa incompleta: precisa cobrir os 40 blocos do fixture")

	for indice := 0; indice < 40; indice++ {
		indice := indice
		t.Run(fmt.Sprintf("bloco de índice %d", indice), func(t *testing.T) {
			t.Parallel()
			assert.Equalf(t, esperados[indice], blocos[indice].Papel,
				"bloco %d (%q)", indice, blocos[indice].TextoResumo)
		})
	}
}

// TestAnalisarEstruturaFixtureRealProduzBlocosValidosParaCDM garante que
// nenhum bloco devolvido pela função de conveniência é algo que
// cdm.NovoBloco recusaria — a mesma fronteira que
// TestAplicarHeuristicaFixtureRealProduzBlocosValidosParaCDM prova para o
// pipeline manual.
func TestAnalisarEstruturaFixtureRealProduzBlocosValidosParaCDM(t *testing.T) {
	t.Parallel()

	dados := lerFixture(t, "artigo-real-libreoffice.docx")

	blocos, err := AnalisarEstrutura(dados)
	require.NoError(t, err)

	for i, bloco := range blocos {
		_, err := cdm.NovoBloco(bloco.Papel, bloco.TextoResumo, bloco.Confianca, bloco.Origem, bloco.RefXML)
		assert.NoErrorf(t, err, "bloco %d devolvido por AnalisarEstrutura não passaria em cdm.NovoBloco", i)
	}
}

// TestAnalisarEstruturaBytesQueNaoSaoDocxDevolveErroValidacao cobre a
// primeira etapa do encadeamento (Abrir): bytes que não formam um pacote DOCX
// válido são culpa de quem enviou o arquivo (HTTP 400), não do servidor —
// mesma classificação que Abrir já garante sozinho.
func TestAnalisarEstruturaBytesQueNaoSaoDocxDevolveErroValidacao(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome  string
		dados []byte
	}{
		{nome: "vazio", dados: []byte{}},
		{nome: "bytes aleatórios, não é zip", dados: []byte("isto não é um zip nem um docx, só texto solto")},
		{nome: "zip válido mas sem partes obrigatórias de um docx",
			dados: montarZip(t, []entradaZip{{nome: "readme.txt", conteudo: []byte("zip qualquer"), metodo: 0}})},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			blocos, err := AnalisarEstrutura(caso.dados)

			require.Error(t, err)
			assert.Nil(t, blocos)

			var validacao *errors.ErroValidacao
			assert.Truef(t, errors.Como(err, &validacao),
				"esperava *errors.ErroValidacao (culpa é do arquivo, HTTP 400), obteve %T (%v)", err, err)
		})
	}
}

// TestAnalisarEstruturaDocumentoSemBlocosNaoQuebra prova que um corpo vazio
// (w:body sem nenhum w:p nem w:tbl) — documento tecnicamente válido, mas sem
// conteúdo — devolve uma fatia vazia sem erro, nunca panic nem erro espúrio.
func TestAnalisarEstruturaDocumentoSemBlocosNaoQuebra(t *testing.T) {
	t.Parallel()

	documentoXML := []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body></w:body></w:document>`)
	dados := docxSintetico(t, documentoXML)

	blocos, err := AnalisarEstrutura(dados)

	require.NoError(t, err)
	assert.Empty(t, blocos)
}
