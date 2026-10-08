// RED (TDD): webmodel.EstruturaResposta, webmodel.BlocoResposta e
// webmodel.JobResposta ainda não existem. Espera-se falha de compilação
// ("undefined: EstruturaResposta" etc.) até o codador entregar
// internal/application/web/webmodel/estrutura.go e job.go (ou onde decidir).
//
// Contrato travado pela ficha do investigador: GET .../estrutura devolve
// exatamente {"versao":1,"blocos":[...]}. Este teste trava as chaves JSON —
// não o pacote de onde vêm os tipos — porque é isso que o frontend consome
// (docs/contrato-api.md).
package webmodel_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-halos/formatador/internal/application/web/webmodel"
)

func TestEstruturaRespostaSerializaEnvelopeExigidoPeloContrato(t *testing.T) {
	t.Parallel()

	resposta := webmodel.EstruturaResposta{
		Versao: 1,
		Blocos: []webmodel.BlocoResposta{
			{Papel: "titulo", TextoResumo: "Título do artigo", Confianca: 0.95, Origem: "estilo-docx", RefXML: 0},
			{Papel: "secao", Nivel: 1, TextoResumo: "1 INTRODUÇÃO", Confianca: 0.8, Origem: "heuristica", RefXML: 1},
		},
	}

	bruto, err := json.Marshal(resposta)
	require.NoError(t, err)

	var decodificado map[string]any
	require.NoError(t, json.Unmarshal(bruto, &decodificado))

	assert.Contains(t, decodificado, "versao")
	assert.Contains(t, decodificado, "blocos")
	assert.EqualValues(t, 1, decodificado["versao"])

	blocos, ok := decodificado["blocos"].([]any)
	require.True(t, ok)
	require.Len(t, blocos, 2)

	primeiro, ok := blocos[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "titulo", primeiro["papel"])
	assert.Equal(t, "Título do artigo", primeiro["texto_resumo"])
	assert.EqualValues(t, 0.95, primeiro["confianca"])
	assert.Equal(t, "estilo-docx", primeiro["origem"])
	assert.EqualValues(t, 0, primeiro["ref_xml"])
	assert.NotContains(t, primeiro, "nivel", "papel sem nível (titulo) não pode emitir a chave nivel")

	segundo, ok := blocos[1].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "secao", segundo["papel"])
	assert.EqualValues(t, 1, segundo["nivel"])
}

func TestEstruturaRespostaExpoeRevisoes(t *testing.T) {
	t.Parallel()
	papel := &webmodel.PapelSugeridoResposta{Nome: "secao", Nivel: 2}
	resposta := webmodel.EstruturaResposta{Versao: 1, Blocos: []webmodel.BlocoResposta{}, Revisoes: []webmodel.RevisaoEstruturaResposta{{RefXML: 3, PapelSugerido: papel, Confianca: .7, Acao: "confirmar"}}}
	dados, err := json.Marshal(resposta)
	require.NoError(t, err)
	assert.Contains(t, string(dados), `"revisoes":[{"ref_xml":3,"papel_sugerido":{"nome":"secao","nivel":2},"confianca":0.7,"acao":"confirmar"}]`)
}

func TestJobRespostaSerializaCamposPublicosSemVazarResultado(t *testing.T) {
	t.Parallel()

	resposta := webmodel.JobResposta{
		ID:          uuid.New(),
		DocumentoID: uuid.New(),
		Tipo:        "analisar",
		Status:      "pendente",
		Progresso:   0,
		CriadoEm:    time.Now().UTC(),
	}

	bruto, err := json.Marshal(resposta)
	require.NoError(t, err)
	corpo := string(bruto)

	for _, chave := range []string{"\"id\"", "\"documento_id\"", "\"tipo\"", "\"status\"", "\"progresso\"", "\"criado_em\""} {
		assert.Containsf(t, corpo, chave, "resposta pública do job precisa ter a chave %s: %s", chave, corpo)
	}
	// entity.Job.Resultado é metadado interno (godoc do campo em
	// internal/domain/job/entity/job.go): o DTO não pode expor nenhum campo
	// chamado "resultado".
	assert.NotContains(t, corpo, "\"resultado\"", "JobResposta não pode expor o campo interno Resultado")
}
