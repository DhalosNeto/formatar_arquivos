// Package execucao reúne as operações exclusivas dos workers de job.
package execucao

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/daniel-halos/formatador/internal/domain/job/entity"
	"github.com/daniel-halos/formatador/internal/domain/job/repository"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// ServicoInterno persiste as transições de job com comparação do status anterior.
type ServicoInterno struct {
	repositorio repository.ExecucaoJobRepo
}

func NovoServicoInterno(repositorio repository.ExecucaoJobRepo) (*ServicoInterno, error) {
	if repositorio == nil {
		return nil, errors.NovoErroArgumentoNulo("repositorio")
	}
	return &ServicoInterno{repositorio: repositorio}, nil
}

func (s *ServicoInterno) obter(ctx context.Context, id uuid.UUID) (entity.Job, error) {
	if id == uuid.Nil {
		return entity.Job{}, errors.NovoErroValidacao("id", "identificador do job é obrigatório")
	}
	job, err := s.repositorio.ObterPorIDInterno(ctx, id)
	if err != nil {
		return entity.Job{}, errors.Envolver(err, "obter execução do job")
	}
	if job.ID != id {
		return entity.Job{}, errors.NovoErroNaoEncontrado("job")
	}
	return job, nil
}

// Iniciar move o job para "executando". Se a leitura encontrar o job em
// "falhou" (retentativa vinda da fila), Reenfileirar (falhou->pendente) é
// aplicado antes de Iniciar (pendente->executando): StatusJob.PodeTransitarPara
// não permite executando diretamente a partir de falhou, e a única saída de
// "executando" é terminal, então o próprio Job.Iniciar não teria como fazer
// esse salto sozinho. O CAS final ainda compara contra o status realmente
// lido (falhou), não contra o pendente intermediário, porque statusAnterior é
// capturado antes de qualquer mutação.
func (s *ServicoInterno) Iniciar(ctx context.Context, id uuid.UUID) (entity.Job, error) {
	job, err := s.obter(ctx, id)
	if err != nil {
		return entity.Job{}, err
	}
	statusAnterior := job.Status
	if job.Status == entity.StatusFalhou {
		if err := job.Reenfileirar(); err != nil {
			return entity.Job{}, err
		}
	}
	if err := job.Iniciar(); err != nil {
		return entity.Job{}, err
	}
	if err := s.repositorio.Salvar(ctx, job, statusAnterior); err != nil {
		return entity.Job{}, errors.Envolver(err, "gravar execução do job")
	}
	return job, nil
}

func (s *ServicoInterno) Concluir(ctx context.Context, id uuid.UUID, resultado json.RawMessage) (entity.Job, error) {
	job, err := s.obter(ctx, id)
	if err != nil {
		return entity.Job{}, err
	}
	statusAnterior := job.Status
	if err := job.Concluir(resultado); err != nil {
		return entity.Job{}, err
	}
	if err := s.repositorio.Salvar(ctx, job, statusAnterior); err != nil {
		return entity.Job{}, errors.Envolver(err, "gravar execução do job")
	}
	return job, nil
}

func (s *ServicoInterno) Falhar(ctx context.Context, id uuid.UUID, motivo string) (entity.Job, error) {
	job, err := s.obter(ctx, id)
	if err != nil {
		return entity.Job{}, err
	}
	statusAnterior := job.Status
	if err := job.Falhar(motivo); err != nil {
		return entity.Job{}, err
	}
	if err := s.repositorio.Salvar(ctx, job, statusAnterior); err != nil {
		return entity.Job{}, errors.Envolver(err, "gravar execução do job")
	}
	return job, nil
}
