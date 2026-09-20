// Package webservices orquestra os casos de uso da API web: compõe os
// serviços de domínio com as bordas de I/O (storage, conversão) e converte
// entidade em DTO. Não conhece HTTP nem Echo.
package webservices

import (
	"bytes"
	"context"
	"io"
	"time"

	"github.com/google/uuid"

	"github.com/daniel-halos/formatador/internal/application/web/webmodel"
	"github.com/daniel-halos/formatador/internal/domain/documento/entity"
	documentoservice "github.com/daniel-halos/formatador/internal/domain/documento/service"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// ArmazenadorObjetos é a porta de storage de objetos usada pela ingestão de
// documento. Implementada em produção por infra/storage.ClienteS3.
type ArmazenadorObjetos interface {
	Salvar(ctx context.Context, chave vo.ChaveStorage, conteudo io.Reader, tamanho int64, contentType string) error
	URLPreAssinada(ctx context.Context, chave vo.ChaveStorage, validade time.Duration) (string, error)
}

// ConversorPDF é a porta de conversão DOCX→PDF usada para o preview.
// Implementada em produção por infra/pdfconv.Cliente.
type ConversorPDF interface {
	ConverterParaPDF(ctx context.Context, docx []byte) ([]byte, error)
}

// validadePadraoPreviewURL é a validade da URL assinada devolvida para o download do preview.
const validadePadraoPreviewURL = 15 * time.Minute

// mensagemSemPreview é fixa: nunca ecoa nome nem conteúdo do documento (CLAUDE.md, regra 7).
const mensagemSemPreview = "o documento ainda não tem preview disponível"

// ServicoDocumento orquestra a ingestão de documentos: valida, salva o
// original, registra o agregado e gera o preview em PDF.
type ServicoDocumento struct {
	documentos  *documentoservice.Servico
	armazenador ArmazenadorObjetos
	conversor   ConversorPDF
}

// NovoServicoDocumento monta o serviço de aplicação de documento.
func NovoServicoDocumento(
	documentos *documentoservice.Servico,
	armazenador ArmazenadorObjetos,
	conversor ConversorPDF,
) (*ServicoDocumento, error) {
	if documentos == nil {
		return nil, errors.NovoErroArgumentoNulo("documentos")
	}
	if armazenador == nil {
		return nil, errors.NovoErroArgumentoNulo("armazenador")
	}
	if conversor == nil {
		return nil, errors.NovoErroArgumentoNulo("conversor")
	}
	return &ServicoDocumento{documentos: documentos, armazenador: armazenador, conversor: conversor}, nil
}

// Ingerir valida o upload, salva o arquivo original, registra o documento e
// gera o preview em PDF, nesta ordem: nada é salvo nem registrado antes de
// passar por toda a validação, e o original é gravado e o documento inserido
// antes de a conversão sequer começar.
//
// ponytail: a conversão do preview roda aqui, de forma síncrona, por decisão
// já tomada nesta rodada. Vira job assíncrono quando a fila (River) existir.
func (s *ServicoDocumento) Ingerir(
	ctx context.Context,
	dono vo.Dono,
	nomeOriginal string,
	conteudo []byte,
) (webmodel.DocumentoResposta, error) {
	documento, err := s.documentos.PrepararIngestao(documentoservice.DadosIngestao{
		Dono:         dono,
		NomeOriginal: nomeOriginal,
		TamanhoBytes: int64(len(conteudo)),
		Prefixo:      prefixoDeteccao(conteudo),
	})
	if err != nil {
		return webmodel.DocumentoResposta{}, err
	}

	// DetectarFormato (dentro de PrepararIngestao) só olha a assinatura binária
	// dos primeiros bytes; o formato docx é provisório até o pacote inteiro ser
	// conferido. Falha aqui ainda é validação de entrada, não I/O.
	if documento.Formato == vo.FormatoDocx {
		if err := vo.ConferirPacoteDocx(conteudo); err != nil {
			return webmodel.DocumentoResposta{}, err
		}
	}

	if err := s.armazenador.Salvar(ctx, documento.ChaveStorage, bytes.NewReader(conteudo), int64(len(conteudo)), documento.MIME()); err != nil {
		return webmodel.DocumentoResposta{}, errors.Envolver(err, "salvar arquivo original")
	}

	if err := s.documentos.Registrar(ctx, dono, documento); err != nil {
		return webmodel.DocumentoResposta{}, err
	}

	if err := s.gerarPreview(ctx, dono, &documento, conteudo); err != nil {
		return webmodel.DocumentoResposta{}, err
	}

	return paraDocumentoResposta(documento), nil
}

// gerarPreview converte o conteúdo original em PDF, salva o preview e registra
// a chave no documento já inserido.
func (s *ServicoDocumento) gerarPreview(ctx context.Context, dono vo.Dono, documento *entity.Documento, conteudoOriginal []byte) error {
	pdf, err := s.conversor.ConverterParaPDF(ctx, conteudoOriginal)
	if err != nil {
		return errors.Envolver(err, "converter documento em pdf")
	}

	chave, err := vo.NovaChavePreviewPDF(documento.ID)
	if err != nil {
		return errors.Envolver(err, "montar chave do preview")
	}

	if err := s.armazenador.Salvar(ctx, chave, bytes.NewReader(pdf), int64(len(pdf)), vo.MIMEPDF); err != nil {
		return errors.Envolver(err, "salvar preview em pdf")
	}

	if err := s.documentos.RegistrarPreviewPDF(ctx, dono, documento.ID); err != nil {
		return errors.Envolver(err, "registrar preview do documento")
	}

	documento.ChaveStoragePDF = &chave
	return nil
}

// Obter devolve o retrato público de um documento do dono solicitante.
func (s *ServicoDocumento) Obter(ctx context.Context, dono vo.Dono, id uuid.UUID) (webmodel.DocumentoResposta, error) {
	documento, err := s.documentos.Obter(ctx, dono, id)
	if err != nil {
		return webmodel.DocumentoResposta{}, err
	}
	return paraDocumentoResposta(documento), nil
}

// URLPreview devolve uma URL pré-assinada para o preview em PDF do documento.
// Documento sem preview ainda gerado é conflito, não ausência.
func (s *ServicoDocumento) URLPreview(ctx context.Context, dono vo.Dono, id uuid.UUID) (webmodel.PreviewResposta, error) {
	documento, err := s.documentos.Obter(ctx, dono, id)
	if err != nil {
		return webmodel.PreviewResposta{}, err
	}
	if !documento.TemPreview() {
		return webmodel.PreviewResposta{}, errors.NovoErroConflito(mensagemSemPreview)
	}

	url, err := s.armazenador.URLPreAssinada(ctx, *documento.ChaveStoragePDF, validadePadraoPreviewURL)
	if err != nil {
		return webmodel.PreviewResposta{}, errors.Envolver(err, "gerar url do preview")
	}

	return webmodel.PreviewResposta{URL: url, ExpiraEm: time.Now().Add(validadePadraoPreviewURL)}, nil
}

// Listar devolve o retrato público de todos os documentos do dono, paginado.
// Lista vazia é sempre []webmodel.DocumentoResposta{}, nunca nil.
func (s *ServicoDocumento) Listar(ctx context.Context, dono vo.Dono, limite, deslocamento int) ([]webmodel.DocumentoResposta, error) {
	documentos, err := s.documentos.ListarDoDono(ctx, dono, limite, deslocamento)
	if err != nil {
		return nil, err
	}
	resposta := make([]webmodel.DocumentoResposta, 0, len(documentos))
	for _, documento := range documentos {
		resposta = append(resposta, paraDocumentoResposta(documento))
	}
	return resposta, nil
}

// prefixoDeteccao devolve só os bytes iniciais que DetectarFormato precisa,
// sem copiar o arquivo inteiro por engano.
func prefixoDeteccao(conteudo []byte) []byte {
	if len(conteudo) > vo.TamanhoPrefixoDeteccao {
		return conteudo[:vo.TamanhoPrefixoDeteccao]
	}
	return conteudo
}

func paraDocumentoResposta(documento entity.Documento) webmodel.DocumentoResposta {
	return webmodel.DocumentoResposta{
		ID:           documento.ID,
		NomeOriginal: documento.NomeOriginal,
		Formato:      string(documento.Formato),
		TamanhoBytes: documento.TamanhoBytes,
		Status:       documento.Status.String(),
		TemPreview:   documento.TemPreview(),
	}
}
