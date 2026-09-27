package webservices

import (
	"context"

	"github.com/google/uuid"

	"github.com/daniel-halos/formatador/internal/application/web/webmodel"
	"github.com/daniel-halos/formatador/internal/domain/job/consulta"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// ServicoJob expõe a consulta autorizada como DTO público.
type ServicoJob struct {
	consulta *consulta.Servico
}

// NovoServicoJob monta o serviço de consulta de job.
func NovoServicoJob(consultas *consulta.Servico) (*ServicoJob, error) {
	if consultas == nil {
		return nil, errors.NovoErroArgumentoNulo("consultas")
	}
	return &ServicoJob{consulta: consultas}, nil
}

// Obter devolve o job do solicitante como DTO público.
//
// A autorização por dono acontece no domínio, não aqui: job inexistente e job
// de terceiro devolvem o mesmo erro, e a conversão para DTO é o que impede
// entity.Job.Resultado — que carrega chave de storage — de atravessar a
// fronteira HTTP.
func (s *ServicoJob) Obter(ctx context.Context, dono vo.Dono, id uuid.UUID) (webmodel.JobResposta, error) {
	job, err := s.consulta.Obter(ctx, dono, id)
	if err != nil {
		return webmodel.JobResposta{}, err
	}
	return paraJobResposta(job), nil
}
