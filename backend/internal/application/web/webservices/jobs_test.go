package webservices_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-halos/formatador/internal/application/web/webservices"
	documentoentity "github.com/daniel-halos/formatador/internal/domain/documento/entity"
	documentoservice "github.com/daniel-halos/formatador/internal/domain/documento/service"
	"github.com/daniel-halos/formatador/internal/domain/job/consulta"
	jobentity "github.com/daniel-halos/formatador/internal/domain/job/entity"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

type consultaJobsFake struct {
	job  jobentity.Job
	erro error
	ctx  context.Context
	dono vo.Dono
	id   uuid.UUID
}

func (r *consultaJobsFake) ObterPorID(ctx context.Context, dono vo.Dono, id uuid.UUID) (jobentity.Job, error) {
	r.ctx, r.dono, r.id = ctx, dono, id
	return r.job, r.erro
}

func (*consultaJobsFake) ListarPorDocumento(context.Context, vo.Dono, uuid.UUID) ([]jobentity.Job, error) {
	panic("consulta individual não deve listar jobs")
}

func TestNovoServicoJobsRecusaDependenciaNula(t *testing.T) {
	t.Parallel()
	servico, err := webservices.NovoServicoJob(nil)
	var nulo *errors.ErroArgumentoNulo
	require.ErrorAs(t, err, &nulo)
	assert.Nil(t, servico)
}

func TestServicoJobsObter(t *testing.T) {
	t.Parallel()
	dono := donoDeTeste(t)
	documento := documentoDeTeste(t, dono)
	agora := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	job := jobentity.Job{ID: uuid.New(), DocumentoID: documento.ID, Tipo: jobentity.TipoAnalisar,
		Status: jobentity.StatusFalhou, Progresso: 42, Erro: "falha no processamento",
		CriadoEm: agora, IniciadoEm: &agora, FinalizadoEm: &agora,
		Resultado: json.RawMessage(`{"chave_storage":"segredo-interno"}`)}
	falha := errors.NovoErroAplicacao("indisponibilidade do repositório")
	casos := []struct {
		nome   string
		dono   vo.Dono
		id     uuid.UUID
		erro   error
		codigo string
	}{
		{"proprio dono", dono, job.ID, nil, ""},
		{"outro dono", donoDeTeste(t), job.ID, nil, "ausente"},
		{"job ausente", dono, job.ID, errors.NovoErroNaoEncontrado("job"), "ausente"},
		{"falha de consulta", dono, job.ID, falha, "interno"},
		{"identificador nulo", dono, uuid.Nil, nil, "validacao"},
		{"dono vazio", vo.Dono{}, job.ID, nil, "validacao"},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			repo := &repositorioFake{registrador: &registrador{}, documentos: map[uuid.UUID]documentoentity.Documento{documento.ID: documento}}
			documentos, err := documentoservice.NovoServico(repo, 0)
			require.NoError(t, err)
			jobs := &consultaJobsFake{job: job, erro: caso.erro}
			consultas, err := consulta.NovoServico(jobs, documentos)
			require.NoError(t, err)
			servico, err := webservices.NovoServicoJob(consultas)
			require.NoError(t, err)
			ctx, cancelar := context.WithCancel(context.Background())
			defer cancelar()
			resposta, err := servico.Obter(ctx, caso.dono, caso.id)
			switch caso.codigo {
			case "ausente":
				exigirNaoEncontrado(t, err)
				assert.Equal(t, "job não encontrado", err.Error())
			case "interno":
				assert.ErrorIs(t, err, falha)
			case "validacao":
				var invalido *errors.ErroValidacao
				assert.ErrorAs(t, err, &invalido)
				assert.Nil(t, jobs.ctx)
			default:
				require.NoError(t, err)
				assert.Equal(t, job.ID, resposta.ID)
				assert.Equal(t, documento.ID, resposta.DocumentoID)
				assert.Equal(t, "analisar", resposta.Tipo)
				assert.Equal(t, "falhou", resposta.Status)
				assert.Equal(t, 42, resposta.Progresso)
				assert.Equal(t, job.Erro, resposta.Erro)
				assert.Equal(t, agora, resposta.CriadoEm)
				assert.Equal(t, &agora, resposta.IniciadoEm)
				assert.Equal(t, &agora, resposta.FinalizadoEm)
				assert.Same(t, ctx, jobs.ctx)
				assert.True(t, jobs.dono.PodeAcessar(dono))
				assert.Equal(t, job.ID, jobs.id)
				corpo, err := json.Marshal(resposta)
				require.NoError(t, err)
				assert.NotContains(t, string(corpo), "resultado")
				assert.NotContains(t, string(corpo), "segredo-interno")
			}
		})
	}
}
