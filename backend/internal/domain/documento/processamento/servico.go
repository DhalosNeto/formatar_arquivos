// Package processamento reúne as operações exclusivas dos workers de documento.
package processamento

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/daniel-halos/formatador/internal/domain/documento/entity"
	"github.com/daniel-halos/formatador/internal/domain/documento/repository"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// ServicoInterno persiste as transições com comparação do status anterior.
type ServicoInterno struct {
	repositorio repository.DocumentoInternoRepo
}

func NovoServicoInterno(repositorio repository.DocumentoInternoRepo) (*ServicoInterno, error) {
	if repositorio == nil {
		return nil, errors.NovoErroArgumentoNulo("repositorio")
	}
	return &ServicoInterno{repositorio: repositorio}, nil
}

func (s *ServicoInterno) obter(ctx context.Context, id uuid.UUID) (entity.Documento, error) {
	if id == uuid.Nil {
		return entity.Documento{}, errors.NovoErroValidacao("id", "identificador do documento é obrigatório")
	}
	documento, err := s.repositorio.ObterPorIDInterno(ctx, id)
	if err != nil {
		return entity.Documento{}, errors.Envolver(err, "obter documento para processamento")
	}
	if documento.ID != id {
		return entity.Documento{}, errors.NovoErroNaoEncontrado("documento")
	}
	return documento, nil
}

// ObterPorIDInterno lê o documento sem restrição de dono e sem mudar seu
// status — porta exclusiva de workers que precisam dos metadados (chave de
// storage, formato) para trabalhar sobre o arquivo, como o executor de
// renderização de preview.
func (s *ServicoInterno) ObterPorIDInterno(ctx context.Context, id uuid.UUID) (entity.Documento, error) {
	return s.obter(ctx, id)
}

func (s *ServicoInterno) IniciarAnalise(ctx context.Context, id uuid.UUID) (entity.Documento, error) {
	return s.transitarStatus(ctx, id, (*entity.Documento).IniciarAnalise)
}

func (s *ServicoInterno) MarcarFalha(ctx context.Context, id uuid.UUID) (entity.Documento, error) {
	return s.transitarStatus(ctx, id, (*entity.Documento).MarcarFalha)
}

func (s *ServicoInterno) ConcluirAnalise(ctx context.Context, id uuid.UUID, cdm json.RawMessage) (entity.Documento, error) {
	documento, err := s.obter(ctx, id)
	if err != nil {
		return entity.Documento{}, err
	}
	statusAnterior := documento.Status
	if err := documento.ConcluirAnalise(cdm); err != nil {
		return entity.Documento{}, err
	}
	if err := s.repositorio.DefinirCDM(ctx, id, documento.CDM, statusAnterior, documento.Status); err != nil {
		return entity.Documento{}, errors.Envolver(err, "gravar CDM do documento")
	}
	return documento, nil
}

func (s *ServicoInterno) transitarStatus(
	ctx context.Context,
	id uuid.UUID,
	aplicar func(*entity.Documento) error,
) (entity.Documento, error) {
	documento, err := s.obter(ctx, id)
	if err != nil {
		return entity.Documento{}, err
	}
	statusAnterior := documento.Status
	if err := aplicar(&documento); err != nil {
		return entity.Documento{}, err
	}
	if err := s.repositorio.AtualizarStatus(ctx, id, statusAnterior, documento.Status); err != nil {
		return entity.Documento{}, errors.Envolver(err, "atualizar status do documento")
	}
	return documento, nil
}
