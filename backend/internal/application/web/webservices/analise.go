package webservices

import (
	"context"

	"github.com/google/uuid"

	"github.com/daniel-halos/formatador/internal/application/web/webmodel"
	"github.com/daniel-halos/formatador/internal/domain/cdm"
	documentoservice "github.com/daniel-halos/formatador/internal/domain/documento/service"
	"github.com/daniel-halos/formatador/internal/domain/job/criacao"
	jobentity "github.com/daniel-halos/formatador/internal/domain/job/entity"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// mensagemSemEstrutura é fixa: nunca cita id nem conteúdo do documento.
const mensagemSemEstrutura = "o documento ainda não foi analisado"

// espacoIdempotenciaAnalise nomeia o espaço de nomes UUIDv5 das chaves de
// idempotência de análise. Qualquer UUID constante serve; o que importa é ser
// estável entre execuções — é ele que faz "analisar o mesmo documento duas
// vezes" produzir a mesma chave e, por consequência, o mesmo job.
var espacoIdempotenciaAnalise = uuid.MustParse("6f9b4a2e-1c3d-4f5a-8b7e-2d9c0a1b3e4f")

// ServicoAnalise orquestra o disparo da análise estrutural e a leitura do CDM.
//
// É um tipo separado de ServicoDocumento de propósito: análise depende da
// criação de jobs, e ServicoDocumento não tem por que ganhar essa dependência
// para atender upload, preview e listagem.
type ServicoAnalise struct {
	documentos *documentoservice.Servico
	jobs       *criacao.Servico
}

// NovoServicoAnalise monta o serviço de análise.
func NovoServicoAnalise(documentos *documentoservice.Servico, jobs *criacao.Servico) (*ServicoAnalise, error) {
	if documentos == nil {
		return nil, errors.NovoErroArgumentoNulo("documentos")
	}
	if jobs == nil {
		return nil, errors.NovoErroArgumentoNulo("jobs")
	}
	return &ServicoAnalise{documentos: documentos, jobs: jobs}, nil
}

// Analisar enfileira a análise estrutural do documento e devolve o job.
//
// A chave de idempotência é DERIVADA do id do documento, não recebida do
// cliente: sem isso, um duplo clique no frontend viraria dois jobs
// concorrentes analisando o mesmo documento e disputando a mesma linha.
// criacao.Servico já autoriza pelo dono e devolve o job existente quando a
// chave se repete.
func (s *ServicoAnalise) Analisar(ctx context.Context, dono vo.Dono, documentoID uuid.UUID) (webmodel.JobResposta, error) {
	job, err := s.jobs.Criar(ctx, dono, criacao.DadosNovoJob{
		DocumentoID:       documentoID,
		Tipo:              jobentity.TipoAnalisar,
		ChaveIdempotencia: chaveIdempotenciaAnalise(documentoID),
	})
	if err != nil {
		return webmodel.JobResposta{}, err
	}
	return paraJobResposta(job), nil
}

// chaveIdempotenciaAnalise deriva uma chave estável por documento. UUIDv5 é
// determinístico por definição: o mesmo documento produz sempre a mesma chave.
func chaveIdempotenciaAnalise(documentoID uuid.UUID) uuid.UUID {
	return uuid.NewSHA1(espacoIdempotenciaAnalise, []byte("analisar:"+documentoID.String()))
}

// ObterEstrutura devolve o CDM do documento, escopado ao dono.
//
// Documento ainda sem CDM é CONFLITO, não ausência: o recurso existe, só não
// está pronto — mesma postura de URLPreview para documento sem preview.
func (s *ServicoAnalise) ObterEstrutura(ctx context.Context, dono vo.Dono, documentoID uuid.UUID) (webmodel.EstruturaResposta, error) {
	documento, err := s.documentos.Obter(ctx, dono, documentoID)
	if err != nil {
		return webmodel.EstruturaResposta{}, err
	}
	if len(documento.CDM) == 0 {
		return webmodel.EstruturaResposta{}, errors.NovoErroConflito(mensagemSemEstrutura)
	}

	indice, err := cdm.Desserializar(documento.CDM)
	if err != nil {
		return webmodel.EstruturaResposta{}, erroCDMCorrompido(err)
	}

	return paraEstruturaResposta(indice), nil
}

// erroCDMCorrompido RECLASSIFICA a falha de desserialização.
//
// cdm.Desserializar devolve *errors.ErroValidacao — correto para quem envia o
// JSON, errado para quem apenas fez um GET. errors.Envolver NÃO reclassifica:
// o ErroValidacao sobreviveria na cadeia e TratarErro responderia 400,
// culpando o cliente por uma linha corrompida no banco do servidor. Mesmo
// tratamento de erroLinhaCorrompida em internal/data/postgres/documento.go.
//
// A mensagem de origem entra no texto porque cdm.Desserializar usa mensagens
// FIXAS, que não ecoam o conteúdo recebido (regra 7).
func erroCDMCorrompido(err error) error {
	return errors.NovoErroAplicacao("cdm do documento está corrompido: " + err.Error())
}

func paraJobResposta(job jobentity.Job) webmodel.JobResposta {
	return webmodel.JobResposta{
		ID:           job.ID,
		DocumentoID:  job.DocumentoID,
		Tipo:         job.Tipo.String(),
		Status:       job.Status.String(),
		Progresso:    job.Progresso,
		Erro:         job.Erro,
		CriadoEm:     job.CriadoEm,
		IniciadoEm:   job.IniciadoEm,
		FinalizadoEm: job.FinalizadoEm,
	}
}

func paraEstruturaResposta(indice cdm.Indice) webmodel.EstruturaResposta {
	blocos := make([]webmodel.BlocoResposta, 0, len(indice.Blocos))
	for _, bloco := range indice.Blocos {
		blocos = append(blocos, webmodel.BlocoResposta{
			Papel:       bloco.Papel.String(),
			Nivel:       bloco.Papel.Nivel(),
			TextoResumo: bloco.TextoResumo,
			Confianca:   bloco.Confianca,
			Origem:      bloco.Origem.String(),
			RefXML:      bloco.RefXML,
		})
	}
	return webmodel.EstruturaResposta{Versao: indice.Versao, Blocos: blocos}
}
