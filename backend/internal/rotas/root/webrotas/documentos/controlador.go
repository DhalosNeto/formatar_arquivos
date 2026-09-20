package documentos

import (
	"context"
	"io"
	"strconv"

	"github.com/google/uuid"

	"github.com/daniel-halos/formatador/internal/application/web/webmodel"
	"github.com/daniel-halos/formatador/internal/application/web/webservices"
	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/daniel-halos/formatador/internal/rotas"
	"github.com/daniel-halos/formatador/internal/rotas/rotasutil"
)

// Caminhos atendidos por este roteador.
const (
	CaminhoColecao = "/documentos"
	CaminhoItem    = "/documentos/:id"
	CaminhoPreview = "/documentos/:id/preview"
)

// CampoArquivo é o nome do campo multipart que carrega o arquivo enviado.
const CampoArquivo = "arquivo"

// Mensagens fixas: nunca ecoam nome de arquivo, id nem conteúdo do usuário
// (CLAUDE.md, regra 7).
const (
	mensagemArquivoObrigatorio = "o arquivo é obrigatório"
	mensagemIDInvalido         = "identificador inválido"
)

const campoID = "id"

// Controlador atende os endpoints HTTP de documentos.
type Controlador struct {
	servico *webservices.ServicoDocumento
}

// NovoControlador cria o controlador de documentos.
func NovoControlador(servico *webservices.ServicoDocumento) *Controlador {
	return &Controlador{servico: servico}
}

// TratarCriacao recebe o upload multipart, garante a sessão do solicitante e
// ingere o documento.
func (c *Controlador) TratarCriacao(ctx context.Context, requisicao rotas.Requisicao, resposta rotas.Resposta) error {
	cabecalho, err := requisicao.ArquivoFormulario(CampoArquivo)
	if err != nil {
		return rotasutil.TratarErro(ctx, resposta, errors.NovoErroValidacao(CampoArquivo, mensagemArquivoObrigatorio))
	}

	arquivo, err := cabecalho.Open()
	if err != nil {
		return rotasutil.TratarErro(ctx, resposta, errors.NovoErroValidacao(CampoArquivo, mensagemArquivoObrigatorio))
	}
	// Leitura apenas: o Close não tem erro a reportar e falhar a requisição por
	// causa dele esconderia o resultado real.
	defer func() { _ = arquivo.Close() }()

	// Tamanho medido pela leitura real, nunca pelo cabeçalho declarado pelo
	// cliente (CLAUDE.md, regra A2). O teto de bytes já foi imposto no
	// transporte por rotas.LimitarCorpo.
	conteudo, err := io.ReadAll(arquivo)
	if err != nil {
		return rotasutil.TratarErro(ctx, resposta, errors.NovoErroAplicacao("falha ao ler o arquivo enviado"))
	}

	dono, err := garantirSessao(requisicao, resposta)
	if err != nil {
		return rotasutil.TratarErro(ctx, resposta, err)
	}

	documento, err := c.servico.Ingerir(ctx, dono, cabecalho.Filename, conteudo)
	if err != nil {
		return rotasutil.TratarErro(ctx, resposta, err)
	}
	return resposta.Criado(documento)
}

// TratarObtencao devolve o retrato público de um documento do próprio dono.
func (c *Controlador) TratarObtencao(ctx context.Context, requisicao rotas.Requisicao, resposta rotas.Resposta) error {
	id, err := uuid.Parse(requisicao.Parametro(campoID))
	if err != nil {
		return rotasutil.TratarErro(ctx, resposta, errors.NovoErroValidacao(campoID, mensagemIDInvalido))
	}

	dono, err := sessaoExistente(requisicao)
	if err != nil {
		return rotasutil.TratarErro(ctx, resposta, err)
	}

	documento, err := c.servico.Obter(ctx, dono, id)
	if err != nil {
		return rotasutil.TratarErro(ctx, resposta, err)
	}
	return resposta.Ok(documento)
}

// TratarListagem devolve os documentos da sessão do solicitante, paginados.
// Sem cookie de sessão é um visitante novo, não uma falha: responde 200 com
// lista vazia em vez do 404 que sessaoExistente usa para obter/preview.
func (c *Controlador) TratarListagem(ctx context.Context, requisicao rotas.Requisicao, resposta rotas.Resposta) error {
	dono, ok := lerDonoDoCookie(requisicao)
	if !ok {
		return resposta.Ok([]webmodel.DocumentoResposta{})
	}

	limite := parametroInteiroOuPadrao(requisicao, "limite")
	deslocamento := parametroInteiroOuPadrao(requisicao, "deslocamento")

	documentos, err := c.servico.Listar(ctx, dono, limite, deslocamento)
	if err != nil {
		return rotasutil.TratarErro(ctx, resposta, err)
	}
	return resposta.Ok(documentos)
}

// parametroInteiroOuPadrao lê um parâmetro de consulta numérico. Ausente ou
// não numérico cai silenciosamente em zero: o domínio já normaliza limite e
// deslocamento fora da faixa, então não é erro do usuário.
func parametroInteiroOuPadrao(requisicao rotas.Requisicao, nome string) int {
	valor, err := strconv.Atoi(requisicao.ParametroConsulta(nome))
	if err != nil {
		return 0
	}
	return valor
}

// TratarPreview devolve a URL pré-assinada do preview em PDF de um documento
// do próprio dono.
func (c *Controlador) TratarPreview(ctx context.Context, requisicao rotas.Requisicao, resposta rotas.Resposta) error {
	id, err := uuid.Parse(requisicao.Parametro(campoID))
	if err != nil {
		return rotasutil.TratarErro(ctx, resposta, errors.NovoErroValidacao(campoID, mensagemIDInvalido))
	}

	dono, err := sessaoExistente(requisicao)
	if err != nil {
		return rotasutil.TratarErro(ctx, resposta, err)
	}

	preview, err := c.servico.URLPreview(ctx, dono, id)
	if err != nil {
		return rotasutil.TratarErro(ctx, resposta, err)
	}
	return resposta.Ok(preview)
}
