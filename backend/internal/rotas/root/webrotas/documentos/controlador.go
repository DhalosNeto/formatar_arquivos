package documentos

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"strconv"

	"github.com/google/uuid"

	"github.com/daniel-halos/formatador/internal/application/web/webmodel"
	"github.com/daniel-halos/formatador/internal/application/web/webservices"
	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/daniel-halos/formatador/internal/rotas"
	"github.com/daniel-halos/formatador/internal/rotas/rotasutil"
	"github.com/daniel-halos/formatador/internal/rotas/sessao"
)

// Caminhos atendidos por este roteador.
const (
	CaminhoColecao = "/documentos"
	CaminhoItem    = "/documentos/:id"
	CaminhoPreview = "/documentos/:id/preview"
	// CaminhoAnalise dispara a análise estrutural; CaminhoEstrutura devolve o
	// CDM resultante.
	CaminhoAnalise   = "/documentos/:id/analisar"
	CaminhoEstrutura = "/documentos/:id/estrutura"
	// CaminhoFormatacao dispara a formatação com um perfil; CaminhoFormatado
	// devolve a URL assinada do DOCX já formatado por aquele perfil.
	CaminhoFormatacao = "/documentos/:id/formatar"
	CaminhoFormatado  = "/documentos/:id/formatado"
)

// CampoArquivo é o nome do campo multipart que carrega o arquivo enviado.
const CampoArquivo = "arquivo"

// Mensagens fixas: nunca ecoam nome de arquivo, id nem conteúdo do usuário
// (CLAUDE.md, regra 7).
const (
	mensagemArquivoObrigatorio = "o arquivo é obrigatório"
)

const campoID = "id"

// campoRulesetID nomeia o perfil tanto no corpo do POST quanto na query do GET.
const campoRulesetID = "ruleset_id"

// tamanhoMaximoFormatacao é o teto do corpo do POST: um objeto com um único
// uuid cabe folgado em 256 bytes.
const tamanhoMaximoFormatacao = 256

// Controlador atende os endpoints HTTP de documentos.
type Controlador struct {
	servico    *webservices.ServicoDocumento
	analise    *webservices.ServicoAnalise
	estrutura  *webservices.ServicoEstrutura
	formatacao *webservices.ServicoFormatacao
}

// NovoControlador cria o controlador de documentos.
func NovoControlador(
	servico *webservices.ServicoDocumento,
	analise *webservices.ServicoAnalise,
	estrutura *webservices.ServicoEstrutura,
	formatacao *webservices.ServicoFormatacao,
) *Controlador {
	return &Controlador{servico: servico, analise: analise, estrutura: estrutura, formatacao: formatacao}
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

	dono, err := sessao.Garantir(requisicao, resposta)
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
	id, err := rotasutil.IDDaRota(requisicao, campoID)
	if err != nil {
		return rotasutil.TratarErro(ctx, resposta, err)
	}

	dono, err := sessao.Existente(requisicao, recursoDocumento)
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
// lista vazia em vez do 404 que sessao.Existente usa para obter/preview.
func (c *Controlador) TratarListagem(ctx context.Context, requisicao rotas.Requisicao, resposta rotas.Resposta) error {
	dono, ok := sessao.Opcional(requisicao)
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
	id, err := rotasutil.IDDaRota(requisicao, campoID)
	if err != nil {
		return rotasutil.TratarErro(ctx, resposta, err)
	}

	dono, err := sessao.Existente(requisicao, recursoDocumento)
	if err != nil {
		return rotasutil.TratarErro(ctx, resposta, err)
	}

	preview, err := c.servico.URLPreview(ctx, dono, id)
	if err != nil {
		return rotasutil.TratarErro(ctx, resposta, err)
	}
	return resposta.Ok(preview)
}

// TratarAnalise enfileira a análise estrutural do documento.
//
// Responde 202, não 200: a análise é assíncrona e o corpo devolvido é o job,
// não o resultado. Chamar duas vezes devolve o MESMO job — a idempotência
// vem da chave derivada em ServicoAnalise, não de um controle aqui.
func (c *Controlador) TratarAnalise(ctx context.Context, requisicao rotas.Requisicao, resposta rotas.Resposta) error {
	id, err := rotasutil.IDDaRota(requisicao, campoID)
	if err != nil {
		return rotasutil.TratarErro(ctx, resposta, err)
	}

	dono, err := sessao.Existente(requisicao, recursoDocumento)
	if err != nil {
		return rotasutil.TratarErro(ctx, resposta, err)
	}

	job, err := c.analise.Analisar(ctx, dono, id)
	if err != nil {
		return rotasutil.TratarErro(ctx, resposta, err)
	}
	return resposta.Aceito(job)
}

// TratarEstrutura devolve o CDM do documento. Documento ainda sem análise é
// conflito, não ausência — mesma postura de TratarPreview.
func (c *Controlador) TratarEstrutura(ctx context.Context, requisicao rotas.Requisicao, resposta rotas.Resposta) error {
	id, err := rotasutil.IDDaRota(requisicao, campoID)
	if err != nil {
		return rotasutil.TratarErro(ctx, resposta, err)
	}

	dono, err := sessao.Existente(requisicao, recursoDocumento)
	if err != nil {
		return rotasutil.TratarErro(ctx, resposta, err)
	}

	estrutura, err := c.analise.ObterEstrutura(ctx, dono, id)
	if err != nil {
		return rotasutil.TratarErro(ctx, resposta, err)
	}
	return resposta.Ok(estrutura)
}

// erroFormatacaoInvalida é a ÚNICA razão devolvida na leitura do corpo, e é
// FIXA: derivá-la do que o cliente mandou devolveria conteúdo arbitrário na
// resposta (regra 7). Mesmo cuidado que rotasutil.MensagemIDInvalido tem para
// o id de rota.
func erroFormatacaoInvalida() error {
	return errors.NovoErroValidacao(campoRulesetID,
		"informe ruleset_id como um uuid válido em um único objeto JSON de até 256 bytes")
}

// TratarFormatacao enfileira a formatação do documento com o perfil pedido.
//
// Responde 202, não 200: a formatação é assíncrona e o corpo devolvido é o
// job. Chamar duas vezes com o mesmo perfil devolve o MESMO job, inclusive se
// ele já terminou — a idempotência vem da chave derivada em
// ServicoFormatacao, e a perda permanente do par está declarada no contrato
// da API.
func (c *Controlador) TratarFormatacao(ctx context.Context, requisicao rotas.Requisicao, resposta rotas.Resposta) error {
	id, err := rotasutil.IDDaRota(requisicao, campoID)
	if err != nil {
		return rotasutil.TratarErro(ctx, resposta, err)
	}

	dono, err := sessao.Existente(requisicao, recursoDocumento)
	if err != nil {
		return rotasutil.TratarErro(ctx, resposta, err)
	}

	tipo, _, err := mime.ParseMediaType(requisicao.Cabecalho("Content-Type"))
	if err != nil || tipo != "application/json" {
		return rotasutil.TratarErro(ctx, resposta, erroFormatacaoInvalida())
	}

	rulesetID, err := lerPedidoFormatacao(requisicao.Corpo())
	if err != nil {
		return rotasutil.TratarErro(ctx, resposta, err)
	}

	job, err := c.formatacao.Formatar(ctx, dono, id, rulesetID)
	if err != nil {
		// Repassado intacto: o gate de perfil já responde no campo ruleset_id,
		// e reescrever a mensagem aqui esconderia a razão real do cliente.
		return rotasutil.TratarErro(ctx, resposta, err)
	}
	return resposta.Aceito(job)
}

// lerPedidoFormatacao lê o único campo do corpo pelo idioma de lerCorrecao: a
// leitura por tokens recusa chave duplicada, que json.Unmarshal aceitaria, e
// campo desconhecido.
func lerPedidoFormatacao(corpo io.Reader) (uuid.UUID, error) {
	dados, err := io.ReadAll(io.LimitReader(corpo, tamanhoMaximoFormatacao+1))
	if err != nil || len(dados) > tamanhoMaximoFormatacao {
		return uuid.Nil, erroFormatacaoInvalida()
	}
	leitor := json.NewDecoder(bytes.NewReader(dados))
	token, err := leitor.Token()
	if err != nil || token != json.Delim('{') {
		return uuid.Nil, erroFormatacaoInvalida()
	}
	var rulesetID uuid.UUID
	visto := false
	for leitor.More() {
		token, err = leitor.Token()
		if err != nil {
			return uuid.Nil, erroFormatacaoInvalida()
		}
		nome, ok := token.(string)
		if !ok || nome != campoRulesetID || visto {
			return uuid.Nil, erroFormatacaoInvalida()
		}
		visto = true
		var bruto string
		if err = leitor.Decode(&bruto); err != nil {
			return uuid.Nil, erroFormatacaoInvalida()
		}
		if rulesetID, err = uuid.Parse(bruto); err != nil {
			return uuid.Nil, erroFormatacaoInvalida()
		}
	}
	if _, err = leitor.Token(); err != nil {
		return uuid.Nil, erroFormatacaoInvalida()
	}
	if err = leitor.Decode(new(any)); !errors.E(err, io.EOF) || !visto || rulesetID == uuid.Nil {
		return uuid.Nil, erroFormatacaoInvalida()
	}
	return rulesetID, nil
}

// TratarFormatado devolve a URL pré-assinada do DOCX formatado. O ruleset_id
// é obrigatório na query: sem ele a leitura cairia no "último concluído", que
// entregaria o artefato de outro perfil com 200 e sem o cliente detectar.
func (c *Controlador) TratarFormatado(ctx context.Context, requisicao rotas.Requisicao, resposta rotas.Resposta) error {
	id, err := rotasutil.IDDaRota(requisicao, campoID)
	if err != nil {
		return rotasutil.TratarErro(ctx, resposta, err)
	}

	dono, err := sessao.Existente(requisicao, recursoDocumento)
	if err != nil {
		return rotasutil.TratarErro(ctx, resposta, err)
	}

	// Mensagem fixa pelo mesmo motivo de erroFormatacaoInvalida: o valor
	// recebido nunca volta na resposta.
	rulesetID, err := uuid.Parse(requisicao.ParametroConsulta(campoRulesetID))
	if err != nil || rulesetID == uuid.Nil {
		return rotasutil.TratarErro(ctx, resposta,
			errors.NovoErroValidacao(campoRulesetID, "informe ruleset_id como um uuid válido"))
	}

	formatado, err := c.formatacao.URLFormatado(ctx, dono, id, rulesetID)
	if err != nil {
		return rotasutil.TratarErro(ctx, resposta, err)
	}
	return resposta.Ok(formatado)
}
