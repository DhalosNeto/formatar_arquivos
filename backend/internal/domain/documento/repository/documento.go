// Package repository declara a porta de acesso a dados de documento. A
// interface mora no domínio; a implementação vive em internal/data.
package repository

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/daniel-halos/formatador/internal/domain/documento/entity"
	"github.com/daniel-halos/formatador/internal/domain/vo"
)

// DocumentoRepo persiste e recupera documentos enviados pelo usuário.
type DocumentoRepo interface {
	// Inserir grava um documento recém-recebido.
	Inserir(ctx context.Context, documento entity.Documento) error

	// ObterPorID restringe a consulta ao solicitante, sem distinguir terceiros de ausentes.
	ObterPorID(ctx context.Context, solicitante vo.Dono, id uuid.UUID) (entity.Documento, error)

	// ListarPorDono devolve apenas os documentos do solicitante, dos mais recentes aos antigos.
	ListarPorDono(ctx context.Context, solicitante vo.Dono, limite, deslocamento int) ([]entity.Documento, error)

	// DefinirChavePreviewPDF restringe a atualização ao solicitante.
	DefinirChavePreviewPDF(ctx context.Context, solicitante vo.Dono, id uuid.UUID, chave vo.ChaveStorage) error
}

// DocumentoInternoRepo é a porta exclusiva do processamento por workers.
type DocumentoInternoRepo interface {
	ObterPorIDInterno(ctx context.Context, id uuid.UUID) (entity.Documento, error)

	// AtualizarStatus muda o status apenas se o documento ainda estiver em
	// statusAtual. A comparação acontece na escrita para que dois workers
	// concorrentes não avancem o mesmo documento duas vezes.
	AtualizarStatus(ctx context.Context, id uuid.UUID, statusAtual, novoStatus entity.Status) error

	// DefinirCDM grava CDM e status atomicamente, somente se statusAtual ainda confere.
	DefinirCDM(ctx context.Context, id uuid.UUID, cdm json.RawMessage, statusAtual, novoStatus entity.Status) error
}
