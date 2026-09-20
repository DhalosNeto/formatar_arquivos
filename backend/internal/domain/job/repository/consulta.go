package repository

import (
	"context"

	"github.com/google/uuid"

	"github.com/daniel-halos/formatador/internal/domain/job/entity"
	"github.com/daniel-halos/formatador/internal/domain/vo"
)

// ConsultaJobRepo exige consultas restritas ao dono do documento via JOIN/EXISTS.
type ConsultaJobRepo interface {
	ObterPorID(ctx context.Context, solicitante vo.Dono, id uuid.UUID) (entity.Job, error)
	ListarPorDocumento(ctx context.Context, solicitante vo.Dono, documentoID uuid.UUID) ([]entity.Job, error)
}
