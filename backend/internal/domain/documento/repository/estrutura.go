package repository

import (
	"context"
	"encoding/json"
	"github.com/daniel-halos/formatador/internal/domain/documento/entity"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/google/uuid"
)

// EstruturaRepo autoriza a leitura e a correção por dono, com escrita CAS.
type EstruturaRepo interface {
	ObterPorID(ctx context.Context, dono vo.Dono, id uuid.UUID) (entity.Documento, error)
	// SalvarEstrutura compara status e JSON anterior atomicamente na escrita.
	SalvarEstrutura(ctx context.Context, dono vo.Dono, id uuid.UUID, anterior, novo json.RawMessage, statusAtual entity.Status) error
}
