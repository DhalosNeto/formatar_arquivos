package webservices

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/daniel-halos/formatador/internal/application/web/webmodel"
	"github.com/daniel-halos/formatador/internal/domain/documento/entity"
	documentoservice "github.com/daniel-halos/formatador/internal/domain/documento/service"
	"github.com/daniel-halos/formatador/internal/domain/job/consulta"
	"github.com/daniel-halos/formatador/internal/domain/job/criacao"
	jobentity "github.com/daniel-halos/formatador/internal/domain/job/entity"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// Mensagens fixas: nunca citam id, nome nem conteúdo do documento (regra 7).
const (
	mensagemFormatoNaoFormatavel = "apenas documentos docx podem ser formatados"
	mensagemStatusNaoFormatavel  = "o documento ainda não está pronto para formatação"
	mensagemSemFormatado         = "o documento ainda não tem formatação concluída para este perfil"
)

// espacoIdempotenciaFormatacao é o espaço de nomes UUIDv5 das chaves de
// idempotência de FORMATAÇÃO, deliberadamente distinto do de análise: dois
// tipos de job sobre o mesmo documento não podem colidir na mesma chave.
var espacoIdempotenciaFormatacao = uuid.MustParse("c1a7f4d6-9b52-4e38-a0d7-5f6b8c2e9a14")

// ServicoFormatacao orquestra o disparo da formatação e o download do
// artefato formatado.
//
// Depende de SERVIÇOS DE DOMÍNIO, nunca de porta de dados: é o idioma de
// ServicoAnalise e de ServicoJob. O serviço de documento existe porque a
// pré-condição de C1 e a leitura escopada ao dono de C2 precisam ler o
// documento, e criacao.Servico guarda o seu em campo privado.
type ServicoFormatacao struct {
	documentos  *documentoservice.Servico
	jobs        *criacao.Servico
	consultas   *consulta.Servico
	armazenador ArmazenadorObjetos
}

// NovoServicoFormatacao monta o serviço de formatação.
func NovoServicoFormatacao(
	documentos *documentoservice.Servico,
	jobs *criacao.Servico,
	consultas *consulta.Servico,
	armazenador ArmazenadorObjetos,
) (*ServicoFormatacao, error) {
	if documentos == nil {
		return nil, errors.NovoErroArgumentoNulo("documentos")
	}
	if jobs == nil {
		return nil, errors.NovoErroArgumentoNulo("jobs")
	}
	if consultas == nil {
		return nil, errors.NovoErroArgumentoNulo("consultas")
	}
	if armazenador == nil {
		return nil, errors.NovoErroArgumentoNulo("armazenador")
	}
	return &ServicoFormatacao{documentos: documentos, jobs: jobs, consultas: consultas, armazenador: armazenador}, nil
}

// Formatar enfileira a formatação do documento com o perfil pedido.
//
// A pré-condição do documento é conferida ANTES de criar o job, e é defesa em
// profundidade: o worker continua autoritativo, porque lê o estado commitado
// no momento da execução. Sem esta guarda, um POST antes de a análise
// concluir cria um job que o worker recusa — e como a chave de idempotência
// devolve o job terminal para sempre, o dono nunca mais formataria aquele
// documento com aquele perfil.
func (s *ServicoFormatacao) Formatar(
	ctx context.Context,
	dono vo.Dono,
	documentoID, rulesetID uuid.UUID,
) (webmodel.JobResposta, error) {
	documento, err := s.documentos.Obter(ctx, dono, documentoID)
	if err != nil {
		return webmodel.JobResposta{}, err
	}
	if err := recusarPreCondicaoDeFormatacao(documento); err != nil {
		return webmodel.JobResposta{}, err
	}

	perfil := rulesetID
	job, err := s.jobs.Criar(ctx, dono, criacao.DadosNovoJob{
		DocumentoID:       documentoID,
		Tipo:              jobentity.TipoFormatar,
		RulesetID:         &perfil,
		ChaveIdempotencia: chaveIdempotenciaFormatacao(documentoID, rulesetID),
	})
	if err != nil {
		return webmodel.JobResposta{}, err
	}
	return paraJobResposta(job), nil
}

// recusarPreCondicaoDeFormatacao espelha, classe por classe e campo por
// campo, o que fila.recusarPreCondicoesDeFormatacao devolve (executor.go:327):
// formato não docx é validação no campo "formato", status não formatável é
// conflito. Divergir faria as duas camadas recusarem o mesmo pedido com
// respostas diferentes.
//
// O termo do CDM NÃO é duplicado aqui: `analisado` só é alcançável com CDM
// não vazio (ConcluirAnalise valida o CDM) e `formatado` herda a invariante.
func recusarPreCondicaoDeFormatacao(documento entity.Documento) error {
	if documento.Formato != vo.FormatoDocx {
		return errors.NovoErroValidacao("formato", mensagemFormatoNaoFormatavel)
	}
	if documento.Status != entity.StatusAnalisado && documento.Status != entity.StatusFormatado {
		return errors.NovoErroConflito(mensagemStatusNaoFormatavel)
	}
	return nil
}

// chaveIdempotenciaFormatacao deriva uma chave estável por PAR (documento,
// perfil). O perfil entra na chave porque criacao.Servico recusa chave
// repetida com dados diferentes: uma chave só por documento prenderia o dono
// ao primeiro perfil pedido, com 409 em qualquer outro.
func chaveIdempotenciaFormatacao(documentoID, rulesetID uuid.UUID) uuid.UUID {
	return uuid.NewSHA1(espacoIdempotenciaFormatacao,
		[]byte("formatar:"+documentoID.String()+":"+rulesetID.String()))
}

// URLFormatado devolve a URL pré-assinada do DOCX formatado do par
// (documento, perfil). Par sem formatação concluída é CONFLITO, não ausência
// — mesma postura de URLPreview.
//
// A chave é DERIVADA de (documentoID, rulesetID), nunca lida do Resultado do
// job: o esquema é determinístico e parsear metadado interno na camada de
// aplicação é classe de bug que não precisa existir. TETO: se o esquema de
// chave mudar, artefatos antigos ficam inalcançáveis, porque o Resultado
// deixou de ser consultado.
func (s *ServicoFormatacao) URLFormatado(
	ctx context.Context,
	dono vo.Dono,
	documentoID, rulesetID uuid.UUID,
) (webmodel.FormatadoResposta, error) {
	// ListarDoDocumento é a leitura ESCOPADA AO DONO, e precede qualquer
	// assinatura: ela confere o documento do solicitante antes de listar, e o
	// filtro de dono dos jobs continua no WHERE do adaptador.
	jobs, err := s.consultas.ListarDoDocumento(ctx, dono, documentoID)
	if err != nil {
		return webmodel.FormatadoResposta{}, err
	}
	if !temFormatacaoConcluida(jobs, rulesetID) {
		return webmodel.FormatadoResposta{}, errors.NovoErroConflito(mensagemSemFormatado)
	}

	chave, err := vo.NovaChaveFormatado(documentoID, rulesetID)
	if err != nil {
		return webmodel.FormatadoResposta{}, err
	}

	url, err := s.armazenador.URLPreAssinada(ctx, chave, validadePadraoPreviewURL)
	if err != nil {
		return webmodel.FormatadoResposta{}, errors.Envolver(err, "gerar url do formatado")
	}
	return webmodel.FormatadoResposta{URL: url, ExpiraEm: time.Now().Add(validadePadraoPreviewURL)}, nil
}

// temFormatacaoConcluida seleciona em Go por TIPO, STATUS e PERFIL sobre a
// lista já escopada ao dono pelo SQL. Nenhum dos três é fronteira de
// autorização; comparar DONO aqui seria, e é proibido.
func temFormatacaoConcluida(jobs []jobentity.Job, rulesetID uuid.UUID) bool {
	for _, job := range jobs {
		if job.Tipo != jobentity.TipoFormatar || job.Status != jobentity.StatusConcluido {
			continue
		}
		if job.RulesetID != nil && *job.RulesetID == rulesetID {
			return true
		}
	}
	return false
}
