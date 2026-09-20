// Package repository declara a porta de acesso a dados de job. A interface
// mora no domínio; a implementação vive em internal/data.
package repository

import (
	"context"

	"github.com/google/uuid"

	"github.com/daniel-halos/formatador/internal/domain/job/entity"
)

// ExecucaoJobRepo é a porta exclusiva do worker: sem dono, com compare-and-set.
type ExecucaoJobRepo interface {
	ObterPorIDInterno(ctx context.Context, id uuid.UUID) (entity.Job, error)

	// Salvar grava status, tentativas, progresso, erro, resultado, iniciado_em e
	// finalizado_em apenas se o status no banco ainda for statusAtual. Zero linhas
	// afetadas é conflito, nunca sucesso silencioso.
	Salvar(ctx context.Context, job entity.Job, statusAtual entity.StatusJob) error
}
