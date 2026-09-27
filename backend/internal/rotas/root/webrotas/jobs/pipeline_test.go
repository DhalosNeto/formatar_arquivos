package jobs_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-halos/formatador/internal/application/web/webservices"
	documentoentity "github.com/daniel-halos/formatador/internal/domain/documento/entity"
	documentorepo "github.com/daniel-halos/formatador/internal/domain/documento/repository"
	documentoservice "github.com/daniel-halos/formatador/internal/domain/documento/service"
	"github.com/daniel-halos/formatador/internal/domain/job/consulta"
	jobentity "github.com/daniel-halos/formatador/internal/domain/job/entity"
	jobrepo "github.com/daniel-halos/formatador/internal/domain/job/repository"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/daniel-halos/formatador/internal/infra/telemetry"
	"github.com/daniel-halos/formatador/internal/rotas/root"
	"github.com/daniel-halos/formatador/internal/rotas/root/webrotas/jobs"
	"github.com/daniel-halos/formatador/internal/rotas/sessao"
	"github.com/daniel-halos/formatador/internal/servidor"
)

// As portas não usadas ficam embutidas: uma chamada indevida falha o teste.
type documentosFake struct {
	documentorepo.DocumentoRepo
	documento documentoentity.Documento
	erro      error
	ctx       context.Context
}

func (r *documentosFake) ObterPorID(ctx context.Context, dono vo.Dono, id uuid.UUID) (documentoentity.Documento, error) {
	r.ctx = ctx
	if r.erro != nil {
		return documentoentity.Documento{}, r.erro
	}
	if id != r.documento.ID || !dono.PodeAcessar(r.documento.Dono) {
		return documentoentity.Documento{}, errors.NovoErroNaoEncontrado("documento")
	}
	return r.documento, nil
}

type jobsFake struct {
	jobrepo.ConsultaJobRepo
	job      jobentity.Job
	dono     vo.Dono
	erro     error
	ctx      context.Context
	chamadas int
}

func (r *jobsFake) ObterPorID(ctx context.Context, dono vo.Dono, id uuid.UUID) (jobentity.Job, error) {
	r.chamadas++
	r.ctx = ctx
	if r.erro != nil {
		return jobentity.Job{}, r.erro
	}
	if id != r.job.ID || !dono.PodeAcessar(r.dono) {
		return jobentity.Job{}, errors.NovoErroNaoEncontrado("job")
	}
	return r.job, nil
}

func TestGETJobPeloServidor(t *testing.T) {
	t.Parallel()
	sessaoID := uuid.New()
	dono, err := vo.NovoDonoSessao(sessaoID)
	require.NoError(t, err)
	documento := documentoentity.Documento{ID: uuid.New(), Dono: dono}
	job := jobentity.Job{ID: uuid.New(), DocumentoID: documento.ID, Tipo: jobentity.TipoAnalisar,
		Status: jobentity.StatusConcluido, Progresso: 100, CriadoEm: time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC),
		Resultado: json.RawMessage(`{"chave_storage":"segredo-storage"}`)}
	casos := []struct {
		nome, id, cookie       string
		erroJob, erroDocumento error
		status, chamadas       int
	}{
		{"proprio dono", job.ID.String(), sessaoID.String(), nil, nil, 200, 1},
		{"sem cookie", job.ID.String(), "", nil, nil, 404, 0},
		{"cookie malformado", job.ID.String(), "segredo-cookie-invalido", nil, nil, 404, 0},
		{"cookie nulo", job.ID.String(), uuid.Nil.String(), nil, nil, 404, 0},
		{"outro dono", job.ID.String(), uuid.NewString(), nil, nil, 404, 1},
		{"job ausente", uuid.NewString(), sessaoID.String(), nil, nil, 404, 1},
		{"documento ausente", job.ID.String(), sessaoID.String(), nil, errors.NovoErroNaoEncontrado("documento"), 404, 1},
		{"uuid invalido", "id-invalido", sessaoID.String(), nil, nil, 400, 0},
		{"uuid nulo", uuid.Nil.String(), sessaoID.String(), nil, nil, 400, 0},
		{"uuid invalido sem cookie", "id-invalido", "", nil, nil, 400, 0},
		{"uuid nulo sem cookie", uuid.Nil.String(), "", nil, nil, 400, 0},
		{"falha de jobs", job.ID.String(), sessaoID.String(), errors.NovoErroAplicacao("falha do banco"), nil, 500, 1},
		{"falha de documentos", job.ID.String(), sessaoID.String(), nil, errors.NovoErroAplicacao("falha do banco"), 500, 1},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			documentos := &documentosFake{documento: documento, erro: caso.erroDocumento}
			servicoDocumento, err := documentoservice.NovoServico(documentos, 0)
			require.NoError(t, err)
			repoJobs := &jobsFake{job: job, dono: dono, erro: caso.erroJob}
			consultas, err := consulta.NovoServico(repoJobs, servicoDocumento)
			require.NoError(t, err)
			servicoJobs, err := webservices.NovoServicoJob(consultas)
			require.NoError(t, err)
			registro := prometheus.NewRegistry()
			api := servidor.Novo(servidor.Opcoes{Registrador: registro, Metricas: telemetry.NovasMetricas(registro),
				Dependencias: root.Dependencias{Jobs: jobs.NovoControlador(servicoJobs)}})
			requisicao := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/jobs/"+caso.id, nil)
			if caso.cookie != "" {
				requisicao.AddCookie(&http.Cookie{Name: sessao.NomeCookie, Value: caso.cookie})
			}
			resposta := httptest.NewRecorder()
			api.Handler.ServeHTTP(resposta, requisicao)
			require.Equal(t, caso.status, resposta.Code)
			assert.Empty(t, resposta.Header().Values("Set-Cookie"))
			assert.Equal(t, caso.chamadas, repoJobs.chamadas)
			corpo := resposta.Body.String()
			assert.NotContains(t, corpo, "segredo-")
			assert.NotContains(t, corpo, "resultado")
			assert.NotContains(t, corpo, "chave_storage")
			assert.NotContains(t, corpo, sessaoID.String())
			switch caso.status {
			case 200:
				assert.JSONEq(t, `{"id":"`+job.ID.String()+`","documento_id":"`+documento.ID.String()+`","tipo":"analisar","status":"concluido","progresso":100,"criado_em":"2026-09-22T12:00:00Z"}`, corpo)
				assert.Same(t, repoJobs.ctx, documentos.ctx)
			case 404:
				assert.JSONEq(t, `{"codigo":"recurso_nao_encontrado","descricao":"job não encontrado"}`, corpo)
			case 400:
				assert.Contains(t, corpo, `"codigo":"requisicao_invalida"`)
				assert.NotContains(t, corpo, "id-invalido")
			case 500:
				assert.JSONEq(t, `{"codigo":"erro_interno","descricao":"erro interno ao processar a requisição"}`, corpo)
			}
		})
	}
}
