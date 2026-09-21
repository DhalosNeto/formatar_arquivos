// Este arquivo fecha o critério de pronto da F2 (docs/plano-backend.md,
// "F2 — Parser e CDM"): "o sistema identifica título, resumo, palavras-chave,
// seções e referências num artigo real — e o round-trip não perde nada".
//
// Não pode viver em internal/domain/cdm: o domínio não importa infra, e este
// teste precisa do pacote ooxml para extrair os blocos e classificá-los pela
// camada 1 (estilos nomeados) antes de passar por cdm.AplicarHeuristica (a
// camada 2, heurística estrutural).
//
// Reusa lerFixture, Abrir e ExtrairBlocos de pacote_test.go/blocos.go —
// mesmo pacote.
package ooxml

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-halos/formatador/internal/domain/cdm"
)

// classificarFixtureReal roda o pipeline completo (extrair -> camada 1 ->
// camada 2) sobre o artigo real e devolve o CDM classificado.
func classificarFixtureReal(t *testing.T) []cdm.Bloco {
	t.Helper()

	dados := lerFixture(t, "artigo-real-libreoffice.docx")
	doc, err := Abrir(bytes.NewReader(dados), int64(len(dados)))
	require.NoError(t, err)

	brutos, err := doc.ExtrairBlocos()
	require.NoError(t, err)
	require.Len(t, brutos, 40)

	classificados := make([]cdm.Bloco, len(brutos))
	for i, bruto := range brutos {
		classificados[i] = ClassificarPorEstiloDocx(bruto)
	}

	saida, err := cdm.AplicarHeuristica(classificados)
	require.NoError(t, err)
	require.Len(t, saida, 40)

	return saida
}

// TestAplicarHeuristicaFixtureRealIdentificaEstruturaDoArtigo é o teste de
// ponta a ponta da F2: papel esperado por índice, derivado das cinco regras
// da camada 2 e da classificação por estilo da camada 1 (ficha do
// investigador). Índices 1..5 (título em inglês e autores/afiliação) não
// batem com NENHUMA regra deste recorte — nenhuma evidência textual confiável
// separa "Maria Eduarda Nogueira Prado" de um parágrafo comum. Ficam como a
// camada 1 deixou (Paragrafo, veio de estilo "Normal"); identificar
// ListaAutores fica para a camada de LLM (F5) ou correção manual do usuário —
// não é uma regra inventada aqui.
func TestAplicarHeuristicaFixtureRealIdentificaEstruturaDoArtigo(t *testing.T) {
	t.Parallel()

	saida := classificarFixtureReal(t)

	esperados := map[int]cdm.Papel{
		0:  cdm.Titulo,
		1:  cdm.Paragrafo, // título em inglês — sem regra desta camada, ver comentário do teste
		2:  cdm.Paragrafo, // autor — idem
		3:  cdm.Paragrafo, // afiliação — idem
		4:  cdm.Paragrafo, // autor — idem
		5:  cdm.Paragrafo, // afiliação — idem
		6:  cdm.Secao(1),  // rótulo "RESUMO": permanece como está (regra 1)
		7:  cdm.Resumo,
		8:  cdm.PalavrasChave, // "Palavras-chave:" vence a região do resumo (regra 3 > regra 1)
		9:  cdm.Secao(1),      // rótulo "ABSTRACT": permanece como está (regra 1)
		10: cdm.Resumo,
		11: cdm.PalavrasChave, // "Keywords:" vence a região do resumo
		12: cdm.Secao(1),      // "1 INTRODUÇÃO" — regra 5, nível 1; encerra a região do abstract
		13: cdm.Paragrafo,
		14: cdm.Paragrafo,
		15: cdm.Paragrafo,
		16: cdm.Secao(1), // "2 METODOLOGIA"
		17: cdm.Paragrafo,
		18: cdm.Secao(2), // "2.1 Instrumento de coleta" — regra 5, nível 2
		19: cdm.Paragrafo,
		20: cdm.Secao(2), // "2.2 Participantes"
		21: cdm.Paragrafo,
		22: cdm.Legenda,  // "Tabela 1 — ..." — regra 4
		23: cdm.Tabela,   // w:tbl — forma do nó, camada 1; texto não bate com nenhuma regra da camada 2
		24: cdm.Legenda,  // "Fonte: ..." — regra 4
		25: cdm.Secao(1), // "3 RESULTADOS E DISCUSSÃO"
		26: cdm.Paragrafo,
		27: cdm.Paragrafo,
		28: cdm.Paragrafo,
		29: cdm.Legenda, // "Figura 1 — ..." — regra 4
		30: cdm.Paragrafo,
		31: cdm.Legenda,  // "Fonte: ..." — regra 4
		32: cdm.Secao(1), // "4 CONSIDERAÇÕES FINAIS"
		33: cdm.Paragrafo,
		34: cdm.Paragrafo,
		35: cdm.Secao(1), // rótulo "REFERÊNCIAS": permanece como está (regra 2)
		36: cdm.Referencia,
		37: cdm.Referencia,
		38: cdm.Referencia,
		39: cdm.Referencia, // região de referências vai até o fim do documento, sem próximo cabeçalho
	}
	require.Len(t, esperados, 40, "ficha de expectativa incompleta: precisa cobrir os 40 blocos do fixture")

	for indice := 0; indice < 40; indice++ {
		indice := indice
		t.Run(fmt.Sprintf("bloco de índice %d", indice), func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, esperados[indice], saida[indice].Papel,
				"bloco %d (%q)", indice, saida[indice].TextoResumo)
		})
	}
}

// TestAplicarHeuristicaFixtureRealProduzBlocosValidosParaCDM é o teste de
// fronteira entre os dois pacotes para o pipeline inteiro: nenhum cdm.Bloco
// devolvido pela camada 2, rodando sobre o documento real, pode ser algo que
// cdm.NovoBloco recusaria.
func TestAplicarHeuristicaFixtureRealProduzBlocosValidosParaCDM(t *testing.T) {
	t.Parallel()

	saida := classificarFixtureReal(t)

	for i, bloco := range saida {
		_, err := cdm.NovoBloco(bloco.Papel, bloco.TextoResumo, bloco.Confianca, bloco.Origem, bloco.RefXML)
		assert.NoErrorf(t, err, "bloco %d devolvido pela camada 2 não passaria em cdm.NovoBloco", i)
	}
}
