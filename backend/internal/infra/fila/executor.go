package fila

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	"github.com/daniel-halos/formatador/internal/domain/documento/processamento"
	"github.com/daniel-halos/formatador/internal/domain/job/entity"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// ArmazenadorObjetos é a porta de storage usada pelo executor de documento.
// Implementada em produção por infra/storage.ClienteS3.
type ArmazenadorObjetos interface {
	Obter(ctx context.Context, chave vo.ChaveStorage) (io.ReadCloser, error)
	Salvar(ctx context.Context, chave vo.ChaveStorage, conteudo io.Reader, tamanho int64, contentType string) error
}

// ConversorPDF é a porta de conversão DOCX->PDF usada pelo executor de
// documento. Implementada em produção por infra/pdfconv.Cliente.
type ConversorPDF interface {
	ConverterParaPDF(ctx context.Context, docx []byte) ([]byte, error)
}

// resultadoRenderizarPreview é o único dado persistido no job para o tipo
// renderizar_preview: a chave do preview gerado, nunca o conteúdo do
// documento (CLAUDE.md, regra 7).
type resultadoRenderizarPreview struct {
	ChavePreviewPDF string `json:"chave_preview_pdf"`
}

// ExecutorDocumento roda o trabalho de fato dos jobs de documento para o
// laço da fila. Hoje só sabe renderizar_preview (DOCX -> PDF); qualquer
// outro tipo falha com erro claro, nunca é ignorado silenciosamente.
type ExecutorDocumento struct {
	documentos  *processamento.ServicoInterno
	armazenador ArmazenadorObjetos
	conversor   ConversorPDF
}

// NovoExecutorDocumento monta o executor de jobs de documento.
func NovoExecutorDocumento(
	documentos *processamento.ServicoInterno,
	armazenador ArmazenadorObjetos,
	conversor ConversorPDF,
) (*ExecutorDocumento, error) {
	if documentos == nil {
		return nil, errors.NovoErroArgumentoNulo("documentos")
	}
	if armazenador == nil {
		return nil, errors.NovoErroArgumentoNulo("armazenador")
	}
	if conversor == nil {
		return nil, errors.NovoErroArgumentoNulo("conversor")
	}
	return &ExecutorDocumento{documentos: documentos, armazenador: armazenador, conversor: conversor}, nil
}

// Executar despacha o job pelo tipo. Tipo desconhecido ou ainda não
// suportado pelo worker devolve erro, nunca sucesso silencioso.
func (e *ExecutorDocumento) Executar(ctx context.Context, job entity.Job) (json.RawMessage, error) {
	if job.Tipo != entity.TipoRenderizarPreview {
		return nil, errors.NovoErroAplicacao("tipo de job não suportado pelo worker: " + job.Tipo.String())
	}
	return e.renderizarPreview(ctx, job)
}

// renderizarPreview baixa o original do storage, converte para PDF e grava o
// preview de volta no storage. Só a chave do resultado vai para o job — a
// gravação da chave no agregado de documento fica para um passo seguinte,
// fora do escopo desta entrega (ver relato do codador).
func (e *ExecutorDocumento) renderizarPreview(ctx context.Context, job entity.Job) (json.RawMessage, error) {
	documento, err := e.documentos.ObterPorIDInterno(ctx, job.DocumentoID)
	if err != nil {
		return nil, errors.Envolver(err, "obter documento para renderizar preview")
	}

	original, err := e.armazenador.Obter(ctx, documento.ChaveStorage)
	if err != nil {
		return nil, errors.Envolver(err, "obter arquivo original do documento")
	}
	defer func() { _ = original.Close() }()

	conteudo, err := io.ReadAll(original)
	if err != nil {
		return nil, errors.Envolver(err, "ler arquivo original do documento")
	}

	pdf, err := e.conversor.ConverterParaPDF(ctx, conteudo)
	if err != nil {
		return nil, errors.Envolver(err, "converter documento em pdf")
	}

	chave, err := vo.NovaChavePreviewPDF(documento.ID)
	if err != nil {
		return nil, errors.Envolver(err, "montar chave do preview")
	}

	if err := e.armazenador.Salvar(ctx, chave, bytes.NewReader(pdf), int64(len(pdf)), vo.MIMEPDF); err != nil {
		return nil, errors.Envolver(err, "salvar preview em pdf")
	}

	resultado, err := json.Marshal(resultadoRenderizarPreview{ChavePreviewPDF: chave.String()})
	if err != nil {
		return nil, errors.Envolver(err, "montar resultado do job de preview")
	}
	return resultado, nil
}

var _ Executor = (*ExecutorDocumento)(nil)
