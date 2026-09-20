// Package service reúne os serviços de domínio do agregado de documento:
// orquestram entidade, objetos de valor e a porta de persistência, sem
// conhecer HTTP, storage nem banco.
package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/daniel-halos/formatador/internal/domain/documento/entity"
	"github.com/daniel-halos/formatador/internal/domain/documento/repository"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// TamanhoMaximoPadraoBytes é o teto de upload usado quando o chamador não
// informa um limite próprio.
const TamanhoMaximoPadraoBytes int64 = 25 << 20

// Limites de paginação aplicados antes de chegar ao repositório.
const (
	limitePadraoListagem = 20
	limiteMaximoListagem = 100
)

// Mensagens fixas: nunca ecoam nome, prefixo ou qualquer conteúdo enviado pelo
// usuário (CLAUDE.md, regra 7).
const (
	mensagemIngestaoInvalida = "não foi possível aceitar o arquivo enviado"
	mensagemNomeObrigatorio  = "o nome do arquivo é obrigatório"
	mensagemTamanhoInvalido  = "o tamanho do arquivo precisa ser maior que zero"
	mensagemTamanhoExcedido  = "o arquivo excede o tamanho máximo permitido"
	mensagemFormatoNaoAceito = "somente arquivos DOCX são aceitos nesta fase"
	mensagemIDObrigatorio    = "identificador do documento é obrigatório"
	mensagemDonoObrigatorio  = "o dono do documento é obrigatório"
	mensagemArquivoNaoAceito = "não foi possível identificar o formato do arquivo"
	campoArquivo             = "arquivo"
	campoTamanhoBytes        = "tamanho_bytes"
	campoNomeOriginal        = "nome_original"
	campoID                  = "id"
	campoChaveStoragePDF     = "chave_storage_pdf"
)

// DadosIngestao são os dados brutos de um upload, antes de virarem documento.
// Prefixo carrega apenas os primeiros bytes do arquivo, o suficiente para a
// detecção de formato por assinatura binária.
type DadosIngestao struct {
	Dono         vo.Dono
	NomeOriginal string
	TamanhoBytes int64
	Prefixo      []byte
}

// Servico concentra as regras de ciclo de vida do documento.
type Servico struct {
	repositorio        repository.DocumentoRepo
	tamanhoMaximoBytes int64
}

// NovoServico cria o serviço de documento. tamanhoMaximoBytes menor ou igual a
// zero cai em TamanhoMaximoPadraoBytes.
func NovoServico(repositorio repository.DocumentoRepo, tamanhoMaximoBytes int64) (*Servico, error) {
	if repositorio == nil {
		return nil, errors.NovoErroArgumentoNulo("repositorio")
	}
	if tamanhoMaximoBytes <= 0 {
		tamanhoMaximoBytes = TamanhoMaximoPadraoBytes
	}
	return &Servico{repositorio: repositorio, tamanhoMaximoBytes: tamanhoMaximoBytes}, nil
}

// ValidarIngestao confere nome, tamanho e formato do upload e devolve o formato
// identificado. O formato sai SEMPRE da assinatura binária do arquivo: nome e
// extensão são entrada do usuário e não decidem nada aqui.
func (s *Servico) ValidarIngestao(dados DadosIngestao) (vo.FormatoArquivo, error) {
	invalidos := &errors.ErroValidacao{Mensagem: mensagemIngestaoInvalida}

	if dados.Dono.Vazio() {
		invalidos.Acrescentar("dono", mensagemDonoObrigatorio)
	}
	if entity.HigienizarNomeArquivo(dados.NomeOriginal) == "" {
		invalidos.Acrescentar(campoNomeOriginal, mensagemNomeObrigatorio)
	}
	switch {
	case dados.TamanhoBytes <= 0:
		invalidos.Acrescentar(campoTamanhoBytes, mensagemTamanhoInvalido)
	case dados.TamanhoBytes > s.tamanhoMaximoBytes:
		invalidos.Acrescentar(campoTamanhoBytes, mensagemTamanhoExcedido)
	}

	formato, err := vo.DetectarFormato(dados.Prefixo)
	switch {
	case err != nil:
		invalidos.Acrescentar(campoArquivo, mensagemArquivoNaoAceito)
	case formato != vo.FormatoDocx:
		invalidos.Acrescentar(campoArquivo, mensagemFormatoNaoAceito)
	}

	if invalidos.TemCampos() {
		return "", invalidos
	}
	return formato, nil
}

// PrepararIngestao monta o documento a ser gravado no storage. Não toca no
// repositório de propósito: o registro só acontece depois que o arquivo subiu,
// para que nenhuma linha aponte para um objeto inexistente.
func (s *Servico) PrepararIngestao(dados DadosIngestao) (entity.Documento, error) {
	formato, err := s.ValidarIngestao(dados)
	if err != nil {
		return entity.Documento{}, err
	}

	documento, err := entity.NovoDocumento(dados.Dono, dados.NomeOriginal, formato, dados.TamanhoBytes)
	if err != nil {
		return entity.Documento{}, errors.Envolver(err, "montar documento recebido")
	}
	return documento, nil
}

// Registrar persiste um documento já preparado e armazenado.
func (s *Servico) Registrar(ctx context.Context, solicitante vo.Dono, documento entity.Documento) error {
	if err := validarDono(solicitante); err != nil {
		return err
	}
	if err := documento.Validar(); err != nil {
		return err
	}
	if !solicitante.PodeAcessar(documento.Dono) {
		return errors.NovoErroNaoEncontrado("documento")
	}
	if documento.TamanhoBytes > s.tamanhoMaximoBytes {
		return errors.NovoErroValidacao(campoTamanhoBytes, mensagemTamanhoExcedido)
	}
	if documento.Formato != vo.FormatoDocx {
		return errors.NovoErroValidacao("formato", mensagemFormatoNaoAceito)
	}
	if documento.ChaveStoragePDF != nil {
		return errors.NovoErroValidacao(campoChaveStoragePDF, "documento novo não pode ter preview")
	}
	if documento.CDM != nil {
		return errors.NovoErroValidacao("cdm", "documento novo não pode ter cdm")
	}
	if err := s.repositorio.Inserir(ctx, documento); err != nil {
		return errors.Envolver(err, "inserir documento")
	}
	return nil
}

// Obter devolve um documento pelo identificador.
func (s *Servico) Obter(ctx context.Context, solicitante vo.Dono, id uuid.UUID) (entity.Documento, error) {
	if err := validarDono(solicitante); err != nil {
		return entity.Documento{}, err
	}
	if id == uuid.Nil {
		return entity.Documento{}, errors.NovoErroValidacao(campoID, mensagemIDObrigatorio)
	}

	documento, err := s.repositorio.ObterPorID(ctx, solicitante, id)
	if err != nil {
		var ausente *errors.ErroNaoEncontrado
		if errors.Como(err, &ausente) {
			return entity.Documento{}, errors.NovoErroNaoEncontrado("documento")
		}
		return entity.Documento{}, errors.Envolver(err, "obter documento")
	}
	if documento.ID != id || !solicitante.PodeAcessar(documento.Dono) {
		return entity.Documento{}, errors.NovoErroNaoEncontrado("documento")
	}
	return documento, nil
}

// ListarDoDono normaliza a paginação e confere o dono de todos os resultados.
func (s *Servico) ListarDoDono(
	ctx context.Context,
	solicitante vo.Dono,
	limite, deslocamento int,
) ([]entity.Documento, error) {
	if err := validarDono(solicitante); err != nil {
		return nil, err
	}
	limite, deslocamento = normalizarPaginacao(limite, deslocamento)

	documentos, err := s.repositorio.ListarPorDono(ctx, solicitante, limite, deslocamento)
	if err != nil {
		return nil, errors.Envolver(err, "listar documentos do dono")
	}
	for _, documento := range documentos {
		if !solicitante.PodeAcessar(documento.Dono) {
			return nil, errors.NovoErroNaoEncontrado("documento")
		}
	}
	return documentos, nil
}

func normalizarPaginacao(limite, deslocamento int) (int, int) {
	if limite <= 0 {
		limite = limitePadraoListagem
	}
	if limite > limiteMaximoListagem {
		limite = limiteMaximoListagem
	}
	if deslocamento < 0 {
		deslocamento = 0
	}
	return limite, deslocamento
}

func validarDono(solicitante vo.Dono) error {
	if solicitante.Vazio() {
		return errors.NovoErroValidacao("dono", mensagemDonoObrigatorio)
	}
	return nil
}

// RegistrarPreviewPDF associa o PDF de pré-visualização ao documento. Gerar
// preview não é etapa do ciclo de vida, então o status não muda.
func (s *Servico) RegistrarPreviewPDF(ctx context.Context, solicitante vo.Dono, id uuid.UUID) error {
	documento, err := s.Obter(ctx, solicitante, id)
	if err != nil {
		return err
	}
	chave, err := vo.NovaChavePreviewPDF(documento.ID)
	if err != nil {
		return errors.Envolver(err, "montar chave do preview")
	}

	if err := s.repositorio.DefinirChavePreviewPDF(ctx, solicitante, documento.ID, chave); err != nil {
		return errors.Envolver(err, "registrar preview do documento")
	}
	return nil
}
