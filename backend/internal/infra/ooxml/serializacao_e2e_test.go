// Este arquivo é o teste de ponta a ponta do CDM sobre o artigo real: as
// três camadas de classificação (estilo-docx, heurística) mais a
// serialização, num único fluxo — ExtrairBlocos -> ClassificarPorEstiloDocx
// -> cdm.AplicarHeuristica -> cdm.NovoIndice -> Serializar -> Desserializar.
// Reusa lerFixture de pacote_test.go (mesmo pacote).
//
// FASE VERMELHA: depende de cdm.NovoIndice, Indice.Serializar e
// cdm.Desserializar, que ainda não existem.
package ooxml

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-halos/formatador/internal/domain/cdm"
	"github.com/daniel-halos/formatador/internal/domain/documento/entity"
)

// limiteConfortavelCDMBytes é a evidência de que o CDM é ÍNDICE, não cópia
// do documento (docs/adr/0001-docx-in-place.md): 40 blocos com resumo
// truncado a TamanhoMaximoTextoResumo runas cada precisam produzir um
// envelope folgadamente abaixo de entity.TamanhoMaximoCDMBytes (5 MiB), não
// só tecnicamente abaixo dele.
const limiteConfortavelCDMBytes = 100 * 1024 // 100 KiB

func TestPipelineCompletoCDMSerializaEDesserializaFixtureReal(t *testing.T) {
	t.Parallel()

	dados := lerFixture(t, "artigo-real-libreoffice.docx")
	doc, err := Abrir(bytes.NewReader(dados), int64(len(dados)))
	require.NoError(t, err)
	require.NotNil(t, doc)

	brutos, err := doc.ExtrairBlocos()
	require.NoError(t, err)
	require.Len(t, brutos, 40, "39 parágrafos de nível superior + 1 tabela, ver TestExtrairBlocosFixtureReal")

	camada1 := make([]cdm.Bloco, len(brutos))
	for i, bruto := range brutos {
		camada1[i] = ClassificarPorEstiloDocx(bruto)
	}

	blocosFinais, err := cdm.AplicarHeuristica(camada1)
	require.NoError(t, err)
	require.Len(t, blocosFinais, 40)

	indice := cdm.NovoIndice(blocosFinais)
	serializado, err := indice.Serializar()
	require.NoError(t, err)

	assert.Lessf(t, len(serializado), limiteConfortavelCDMBytes,
		"40 blocos de resumo truncado precisam produzir um índice folgadamente abaixo de %d bytes, obteve %d",
		limiteConfortavelCDMBytes, len(serializado))
	assert.Less(t, len(serializado), entity.TamanhoMaximoCDMBytes,
		"o CDM é índice, não cópia do documento: precisa ficar bem abaixo do teto do agregado")

	desserializado, err := cdm.Desserializar(serializado)
	require.NoError(t, err)
	require.Len(t, desserializado.Blocos, 40)

	for i := range blocosFinais {
		assert.Equalf(t, blocosFinais[i], desserializado.Blocos[i],
			"bloco %d (RefXML %d) não sobreviveu ao round-trip serializar/desserializar", i, blocosFinais[i].RefXML)
	}
}
