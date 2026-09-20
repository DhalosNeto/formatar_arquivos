// Package webmodel reúne os DTOs da API web: o retrato público de cada
// entidade, sem campos internos (chave de storage, dono) que a resposta HTTP
// jamais deve carregar.
package webmodel

import (
	"time"

	"github.com/google/uuid"
)

// DocumentoResposta é o retrato público de um documento.
type DocumentoResposta struct {
	ID           uuid.UUID `json:"id"`
	NomeOriginal string    `json:"nome_original"`
	Formato      string    `json:"formato"`
	TamanhoBytes int64     `json:"tamanho_bytes"`
	Status       string    `json:"status"`
	TemPreview   bool      `json:"tem_preview"`
}

// PreviewResposta é a URL pré-assinada de download do preview em PDF do
// documento, com a validade da assinatura.
type PreviewResposta struct {
	URL      string    `json:"url"`
	ExpiraEm time.Time `json:"expira_em"`
}
