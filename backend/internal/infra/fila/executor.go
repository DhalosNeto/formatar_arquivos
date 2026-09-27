package fila

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"

	"github.com/google/uuid"

	"github.com/daniel-halos/formatador/internal/domain/cdm"
	"github.com/daniel-halos/formatador/internal/domain/documento/processamento"
	"github.com/daniel-halos/formatador/internal/domain/job/entity"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/daniel-halos/formatador/internal/infra/ooxml"
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

// resultadoAnalisar é o que fica em jobs.resultado depois de uma análise:
// quantos blocos foram classificados, e nada além disso. O CDM completo —
// que carrega trecho do documento em TextoResumo — é gravado no DOCUMENTO
// por ConcluirAnalise, onde a regra de acesso por dono se aplica. Repeti-lo
// aqui o espalharia para uma tabela com outra fronteira de acesso.
type resultadoAnalisar struct {
	Blocos int `json:"blocos"`
}

// ExecutorDocumento roda o trabalho de fato dos jobs de documento para o
// laço da fila. Sabe renderizar_preview (DOCX -> PDF) e analisar (DOCX ->
// CDM); qualquer outro tipo falha com erro claro, nunca é ignorado
// silenciosamente.
type ExecutorDocumento struct {
	documentos  *processamento.ServicoInterno
	armazenador ArmazenadorObjetos
	conversor   ConversorPDF
	registrador *slog.Logger
}

// NovoExecutorDocumento monta o executor de jobs de documento.
func NovoExecutorDocumento(
	documentos *processamento.ServicoInterno,
	armazenador ArmazenadorObjetos,
	conversor ConversorPDF,
	registrador *slog.Logger,
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
	if registrador == nil {
		return nil, errors.NovoErroArgumentoNulo("registrador")
	}
	return &ExecutorDocumento{documentos: documentos, armazenador: armazenador, conversor: conversor, registrador: registrador}, nil
}

// Executar despacha o job pelo tipo. Tipo desconhecido ou ainda não
// suportado pelo worker devolve erro, nunca sucesso silencioso.
func (e *ExecutorDocumento) Executar(ctx context.Context, job entity.Job) (json.RawMessage, error) {
	switch job.Tipo {
	case entity.TipoRenderizarPreview:
		return e.renderizarPreview(ctx, job)
	case entity.TipoAnalisar:
		return e.analisar(ctx, job)
	}
	return nil, errors.NovoErroAplicacao("tipo de job não suportado pelo worker: " + job.Tipo.String())
}

// analisar extrai a estrutura do DOCX e grava o CDM no documento.
//
// Após iniciar a análise, tenta persistir o estado de falha quando a extração
// falha. Se a persistência também falhar, o documento pode ficar em analisando;
// o erro retornado preserva ambas as causas.
func (e *ExecutorDocumento) analisar(ctx context.Context, job entity.Job) (json.RawMessage, error) {
	if _, err := e.documentos.IniciarAnalise(ctx, job.DocumentoID); err != nil {
		// Ainda não saiu de `recebido`: não há do que marcar falha, e
		// MarcarFalha aqui mascararia uma disputa entre dois workers.
		return nil, errors.Envolver(err, "iniciar análise do documento")
	}

	resultado, err := e.extrairCDM(ctx, job)
	if err != nil {
		return nil, e.marcarFalha(ctx, job.DocumentoID, err)
	}
	return resultado, nil
}

// extrairCDM é o caminho feliz isolado, para que analisar tenha um único
// ponto de tratamento de falha em vez de repetir marcarFalha em cada etapa.
func (e *ExecutorDocumento) extrairCDM(ctx context.Context, job entity.Job) (json.RawMessage, error) {
	documento, err := e.documentos.ObterPorIDInterno(ctx, job.DocumentoID)
	if err != nil {
		return nil, errors.Envolver(err, "obter documento para analisar")
	}

	conteudo, err := e.baixarOriginal(ctx, documento.ChaveStorage)
	if err != nil {
		return nil, err
	}

	blocos, err := ooxml.AnalisarEstrutura(conteudo)
	if err != nil {
		// Sem Envolver: a mensagem de ooxml é fixa e não cita conteúdo, mas
		// encadear contexto aqui também não acrescentaria nada útil ao log.
		return nil, errors.Envolver(err, "analisar estrutura do documento")
	}

	indice, err := cdm.NovoIndice(blocos).Serializar()
	if err != nil {
		return nil, errors.Envolver(err, "serializar cdm do documento")
	}

	if _, err := e.documentos.ConcluirAnalise(ctx, job.DocumentoID, indice); err != nil {
		return nil, errors.Envolver(err, "gravar cdm do documento")
	}

	resultado, err := json.Marshal(resultadoAnalisar{Blocos: len(blocos)})
	if err != nil {
		return nil, errors.Envolver(err, "montar resultado do job de análise")
	}
	return resultado, nil
}

func (e *ExecutorDocumento) marcarFalha(ctx context.Context, documentoID uuid.UUID, original error) error {
	if _, err := e.documentos.MarcarFalha(ctx, documentoID); err != nil {
		e.registrador.ErrorContext(ctx, "falha ao persistir estado de falha do documento", "documento_id", documentoID.String())
		return errors.NovoErroPersistirFalha(original, err)
	}
	return original
}

// baixarOriginal lê o arquivo original do documento do storage.
func (e *ExecutorDocumento) baixarOriginal(ctx context.Context, chave vo.ChaveStorage) ([]byte, error) {
	original, err := e.armazenador.Obter(ctx, chave)
	if err != nil {
		return nil, errors.Envolver(err, "obter arquivo original do documento")
	}
	defer func() { _ = original.Close() }()

	conteudo, err := io.ReadAll(original)
	if err != nil {
		return nil, errors.Envolver(err, "ler arquivo original do documento")
	}
	return conteudo, nil
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

	conteudo, err := e.baixarOriginal(ctx, documento.ChaveStorage)
	if err != nil {
		return nil, err
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
