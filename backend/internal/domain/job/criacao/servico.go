package criacao

import (
	"context"

	"github.com/google/uuid"

	documentoservice "github.com/daniel-halos/formatador/internal/domain/documento/service"
	"github.com/daniel-halos/formatador/internal/domain/job/entity"
	"github.com/daniel-halos/formatador/internal/domain/job/repository"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

type DadosNovoJob struct {
	DocumentoID       uuid.UUID
	Tipo              entity.TipoJob
	RulesetID         *uuid.UUID
	ChaveIdempotencia uuid.UUID
}

type Servico struct {
	jobs       repository.CriacaoJobRepo
	documentos *documentoservice.Servico
}

func NovoServico(jobs repository.CriacaoJobRepo, documentos *documentoservice.Servico) (*Servico, error) {
	if jobs == nil {
		return nil, errors.NovoErroArgumentoNulo("jobs")
	}
	if documentos == nil {
		return nil, errors.NovoErroArgumentoNulo("documentos")
	}
	return &Servico{jobs: jobs, documentos: documentos}, nil
}

func (s *Servico) Criar(ctx context.Context, solicitante vo.Dono, dados DadosNovoJob) (entity.Job, error) {
	if solicitante.Vazio() {
		return entity.Job{}, errors.NovoErroValidacao("dono", "solicitante é obrigatório")
	}
	if dados.ChaveIdempotencia == uuid.Nil {
		return entity.Job{}, errors.NovoErroValidacao("chave_idempotencia", "chave de idempotência é obrigatória")
	}
	candidato, err := entity.NovoJob(dados.DocumentoID, dados.Tipo, dados.RulesetID)
	if err != nil {
		return entity.Job{}, normalizarErro(err)
	}
	if _, err := s.documentos.Obter(ctx, solicitante, candidato.DocumentoID); err != nil {
		return entity.Job{}, normalizarErro(err)
	}
	job, err := s.jobs.InserirOuObter(ctx, solicitante, candidato, dados.ChaveIdempotencia)
	if err != nil {
		return entity.Job{}, normalizarErro(err)
	}
	if job.ID == uuid.Nil || job.DocumentoID != dados.DocumentoID {
		return entity.Job{}, errors.NovoErroNaoEncontrado("job")
	}
	if job.Tipo != dados.Tipo || !rulesetsIguais(job.RulesetID, dados.RulesetID) {
		return entity.Job{}, errors.NovoErroConflito("chave de idempotência já utilizada com dados diferentes")
	}
	return job, nil
}

func rulesetsIguais(primeiro, segundo *uuid.UUID) bool {
	if primeiro == nil || segundo == nil {
		return primeiro == segundo
	}
	return *primeiro == *segundo
}

func normalizarErro(err error) error {
	var ausente *errors.ErroNaoEncontrado
	if errors.Como(err, &ausente) {
		return errors.NovoErroNaoEncontrado("job")
	}
	return errors.Envolver(err, "criar job")
}
