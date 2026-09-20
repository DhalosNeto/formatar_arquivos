package consulta

import (
	"context"

	"github.com/google/uuid"

	documentoservice "github.com/daniel-halos/formatador/internal/domain/documento/service"
	"github.com/daniel-halos/formatador/internal/domain/job/entity"
	"github.com/daniel-halos/formatador/internal/domain/job/repository"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

type Servico struct {
	jobs       repository.ConsultaJobRepo
	documentos *documentoservice.Servico
}

func NovoServico(jobs repository.ConsultaJobRepo, documentos *documentoservice.Servico) (*Servico, error) {
	if jobs == nil {
		return nil, errors.NovoErroArgumentoNulo("jobs")
	}
	if documentos == nil {
		return nil, errors.NovoErroArgumentoNulo("documentos")
	}
	return &Servico{jobs: jobs, documentos: documentos}, nil
}

func (s *Servico) Obter(ctx context.Context, solicitante vo.Dono, id uuid.UUID) (entity.Job, error) {
	if err := validarConsulta(solicitante, id); err != nil {
		return entity.Job{}, err
	}
	job, err := s.jobs.ObterPorID(ctx, solicitante, id)
	if err != nil {
		return entity.Job{}, normalizarErro(err)
	}
	if job.ID != id || job.DocumentoID == uuid.Nil {
		return entity.Job{}, errors.NovoErroNaoEncontrado("job")
	}
	if _, err := s.documentos.Obter(ctx, solicitante, job.DocumentoID); err != nil {
		return entity.Job{}, normalizarErro(err)
	}
	return job, nil
}

func (s *Servico) ListarDoDocumento(ctx context.Context, solicitante vo.Dono, documentoID uuid.UUID) ([]entity.Job, error) {
	if err := validarConsulta(solicitante, documentoID); err != nil {
		return nil, err
	}
	if _, err := s.documentos.Obter(ctx, solicitante, documentoID); err != nil {
		return nil, normalizarErro(err)
	}
	jobs, err := s.jobs.ListarPorDocumento(ctx, solicitante, documentoID)
	if err != nil {
		return nil, normalizarErro(err)
	}
	for _, job := range jobs {
		if job.ID == uuid.Nil || job.DocumentoID != documentoID {
			return nil, errors.NovoErroNaoEncontrado("job")
		}
	}
	return jobs, nil
}

func validarConsulta(solicitante vo.Dono, id uuid.UUID) error {
	if solicitante.Vazio() {
		return errors.NovoErroValidacao("dono", "solicitante é obrigatório")
	}
	if id == uuid.Nil {
		return errors.NovoErroValidacao("id", "identificador é obrigatório")
	}
	return nil
}

func normalizarErro(err error) error {
	var ausente *errors.ErroNaoEncontrado
	if errors.Como(err, &ausente) {
		return errors.NovoErroNaoEncontrado("job")
	}
	return errors.Envolver(err, "consultar job")
}
