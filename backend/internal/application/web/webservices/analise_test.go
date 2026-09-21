// RED (TDD): webservices.ServicoAnalise ainda não existe. Espera-se falha de
// compilação ("undefined: webservices.ServicoAnalise",
// "undefined: webservices.NovoServicoAnalise") até o codador entregar
// internal/application/web/webservices/analise.go.
//
// Contrato travado pela ficha do investigador (recorte "análise ponta a
// ponta"):
//
//	type ServicoAnalise struct { ... }
//	func NovoServicoAnalise(documentos *documentoservice.Servico, jobs *criacao.Servico) (*ServicoAnalise, error)
//	func (s *ServicoAnalise) Analisar(ctx context.Context, dono vo.Dono, documentoID uuid.UUID) (webmodel.JobResposta, error)
//	func (s *ServicoAnalise) ObterEstrutura(ctx context.Context, dono vo.Dono, documentoID uuid.UUID) (webmodel.EstruturaResposta, error)
//
// Decisão de desenho do testador (não coberta pela ficha, ver relato):
// ServicoAnalise é um tipo NOVO, separado de ServicoDocumento, para não mudar
// a assinatura de NovoServicoDocumento (documento_test.go e cmd/api/main.go
// continuam compilando sem alteração). Controlador ganha um segundo campo
// (analise) e NovoControlador ganha um segundo parâmetro — essa mudança É
// inevitável (o controlador precisa da nova dependência) e está isolada em
// pipeline_test.go/listagem_test.go/analise_test.go do pacote documentos.
//
// Este arquivo reusa os fakes e helpers de documento_test.go (mesmo pacote
// webservices_test): repositorioFake, donoDeTeste, documentoDeTeste,
// exigirSemErro, exigirValidacao, exigirNaoEncontrado, exigirConflito.
package webservices_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-halos/formatador/internal/application/web/webservices"
	"github.com/daniel-halos/formatador/internal/domain/cdm"
	"github.com/daniel-halos/formatador/internal/domain/documento/entity"
	documentoservice "github.com/daniel-halos/formatador/internal/domain/documento/service"
	"github.com/daniel-halos/formatador/internal/domain/job/criacao"
	jobentity "github.com/daniel-halos/formatador/internal/domain/job/entity"
	"github.com/daniel-halos/formatador/internal/domain/job/repository"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// jobsRepoMemoria implementa repository.CriacaoJobRepo com idempotência real
// em memória: (documento_id, chave) repetidos devolvem o mesmo job; payload
// diferente sob a mesma chave é conflito. É o suficiente para provar que a
// idempotência de criacao.Servico chega intacta até ServicoAnalise — a
// exaustão de cenários de conflito já está coberta em
// internal/domain/job/criacao/servico_test.go.
type jobsRepoMemoria struct {
	porChave  map[string]jobentity.Job
	chamadas  int
	documento uuid.UUID
}

func novoJobsRepoMemoria() *jobsRepoMemoria {
	return &jobsRepoMemoria{porChave: map[string]jobentity.Job{}}
}

func (r *jobsRepoMemoria) InserirOuObter(_ context.Context, _ vo.Dono, job jobentity.Job, chave uuid.UUID) (jobentity.Job, error) {
	r.chamadas++
	r.documento = job.DocumentoID
	chaveMapa := job.DocumentoID.String() + "|" + chave.String()
	if existente, ok := r.porChave[chaveMapa]; ok {
		if existente.Tipo != job.Tipo {
			return jobentity.Job{}, errors.NovoErroConflito("chave de idempotência já utilizada com dados diferentes")
		}
		return existente, nil
	}
	r.porChave[chaveMapa] = job
	return job, nil
}

var _ repository.CriacaoJobRepo = (*jobsRepoMemoria)(nil)

func novoServicoAnaliseDeTeste(t *testing.T) (*webservices.ServicoAnalise, *repositorioFake, *jobsRepoMemoria) {
	t.Helper()
	repo := &repositorioFake{registrador: &registrador{}}
	docservico, err := documentoservice.NovoServico(repo, 0)
	exigirSemErro(t, err)
	jobsRepo := novoJobsRepoMemoria()
	jobsCriacao, err := criacao.NovoServico(jobsRepo, docservico)
	exigirSemErro(t, err)
	servico, err := webservices.NovoServicoAnalise(docservico, jobsCriacao)
	exigirSemErro(t, err)
	return servico, repo, jobsRepo
}

func TestNovoServicoAnaliseRecusaDependenciaNula(t *testing.T) {
	t.Parallel()
	repo := &repositorioFake{}
	docservico, err := documentoservice.NovoServico(repo, 0)
	exigirSemErro(t, err)
	jobsCriacao, err := criacao.NovoServico(novoJobsRepoMemoria(), docservico)
	exigirSemErro(t, err)

	casos := []struct {
		nome       string
		documentos *documentoservice.Servico
		jobs       *criacao.Servico
	}{
		{"documentos nulo", nil, jobsCriacao},
		{"jobs nulo", docservico, nil},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			_, err := webservices.NovoServicoAnalise(caso.documentos, caso.jobs)
			var nulo *errors.ErroArgumentoNulo
			require.ErrorAsf(t, err, &nulo, "esperava ErroArgumentoNulo, obteve %T: %v", err, err)
		})
	}
}

// ---------------------------------------------------------------------------
// Analisar: cria (ou reusa) o job de análise, escopado ao dono.
// ---------------------------------------------------------------------------

func TestAnalisarDocumentoInexistenteNaoEncontrado(t *testing.T) {
	t.Parallel()
	servico, _, _ := novoServicoAnaliseDeTeste(t)
	_, err := servico.Analisar(context.Background(), donoDeTeste(t), uuid.New())
	exigirNaoEncontrado(t, err)
}

func TestAnalisarDonoErradoNaoEncontrado(t *testing.T) {
	t.Parallel()
	servico, repo, _ := novoServicoAnaliseDeTeste(t)
	dono := donoDeTeste(t)
	outroDono := donoDeTeste(t)
	documento := documentoDeTeste(t, dono)
	repo.documentos = map[uuid.UUID]entity.Documento{documento.ID: documento}

	_, err := servico.Analisar(context.Background(), outroDono, documento.ID)
	exigirNaoEncontrado(t, err)
}

func TestAnalisarCriaJobPendenteDoTipoAnalisar(t *testing.T) {
	t.Parallel()
	servico, repo, jobsRepo := novoServicoAnaliseDeTeste(t)
	dono := donoDeTeste(t)
	documento := documentoDeTeste(t, dono)
	repo.documentos = map[uuid.UUID]entity.Documento{documento.ID: documento}

	resposta, err := servico.Analisar(context.Background(), dono, documento.ID)
	exigirSemErro(t, err)

	assert.NotEqual(t, uuid.Nil, resposta.ID)
	assert.Equal(t, documento.ID, resposta.DocumentoID)
	assert.Equal(t, jobentity.TipoAnalisar.String(), resposta.Tipo)
	assert.Equal(t, jobentity.StatusPendente.String(), resposta.Status)
	assert.Equal(t, 1, jobsRepo.chamadas)
}

func TestAnalisarChamadoDuasVezesDevolveMesmoJob(t *testing.T) {
	t.Parallel()
	servico, repo, jobsRepo := novoServicoAnaliseDeTeste(t)
	dono := donoDeTeste(t)
	documento := documentoDeTeste(t, dono)
	repo.documentos = map[uuid.UUID]entity.Documento{documento.ID: documento}

	primeiro, err := servico.Analisar(context.Background(), dono, documento.ID)
	exigirSemErro(t, err)
	segundo, err := servico.Analisar(context.Background(), dono, documento.ID)
	exigirSemErro(t, err)

	assert.Equal(t, primeiro.ID, segundo.ID, "chamar Analisar duas vezes para o mesmo documento tem que devolver o MESMO job")
	assert.Len(t, jobsRepo.porChave, 1, "só um job deveria ter sido inserido de fato")
}

// ---------------------------------------------------------------------------
// ObterEstrutura: devolve o CDM público de um documento já analisado.
// ---------------------------------------------------------------------------

func TestObterEstruturaDocumentoInexistenteNaoEncontrado(t *testing.T) {
	t.Parallel()
	servico, _, _ := novoServicoAnaliseDeTeste(t)
	_, err := servico.ObterEstrutura(context.Background(), donoDeTeste(t), uuid.New())
	exigirNaoEncontrado(t, err)
}

func TestObterEstruturaDonoErradoNaoEncontrado(t *testing.T) {
	t.Parallel()
	servico, repo, _ := novoServicoAnaliseDeTeste(t)
	dono := donoDeTeste(t)
	outroDono := donoDeTeste(t)
	documento := documentoComCDMDeTeste(t, dono)
	repo.documentos = map[uuid.UUID]entity.Documento{documento.ID: documento}

	_, err := servico.ObterEstrutura(context.Background(), outroDono, documento.ID)
	exigirNaoEncontrado(t, err)
}

func TestObterEstruturaDocumentoAindaNaoAnalisadoConflito(t *testing.T) {
	t.Parallel()
	servico, repo, _ := novoServicoAnaliseDeTeste(t)
	dono := donoDeTeste(t)
	documento := documentoDeTeste(t, dono) // status recebido, sem CDM
	repo.documentos = map[uuid.UUID]entity.Documento{documento.ID: documento}

	_, err := servico.ObterEstrutura(context.Background(), dono, documento.ID)
	exigirConflito(t, err)
}

func TestObterEstruturaFeliz(t *testing.T) {
	t.Parallel()
	servico, repo, _ := novoServicoAnaliseDeTeste(t)
	dono := donoDeTeste(t)
	documento := documentoComCDMDeTeste(t, dono)
	repo.documentos = map[uuid.UUID]entity.Documento{documento.ID: documento}

	resposta, err := servico.ObterEstrutura(context.Background(), dono, documento.ID)
	exigirSemErro(t, err)

	require.Equal(t, cdm.VersaoFormatoCDM, resposta.Versao)
	require.Len(t, resposta.Blocos, 2)
	assert.Equal(t, "titulo", resposta.Blocos[0].Papel)
	assert.Equal(t, 0, resposta.Blocos[0].Nivel)
	assert.Equal(t, "Título do artigo de teste", resposta.Blocos[0].TextoResumo)
	assert.Equal(t, "secao", resposta.Blocos[1].Papel)
	assert.Equal(t, 1, resposta.Blocos[1].Nivel)
}

// TestObterEstruturaCDMCorrompidoDevolveErroAplicacaoNuncaErroValidacao é o
// teste que trava o bug já ocorrido neste projeto (ver erroLinhaCorrompida em
// internal/data/postgres/documento.go): cdm.Desserializar devolve
// *errors.ErroValidacao para dados inválidos, e errors.Envolver NÃO
// reclassifica — sem reclassificação explícita em ObterEstrutura, uma linha
// corrompida no banco vira "requisição inválida" (400) e culpa quem só fez um
// GET. CDM corrompido é falha do SERVIDOR (dado gravado antes, não enviado
// agora pelo cliente): tem que ser 500, nunca 400.
func TestObterEstruturaCDMCorrompidoDevolveErroAplicacaoNuncaErroValidacao(t *testing.T) {
	t.Parallel()
	servico, repo, _ := novoServicoAnaliseDeTeste(t)
	dono := donoDeTeste(t)
	documento := documentoDeTeste(t, dono)
	documento.Status = entity.StatusAnalisado
	documento.CDM = json.RawMessage(`{"versao":1,"blocos":[{"papel":`) // JSON corrompido, não vem do cliente agora
	repo.documentos = map[uuid.UUID]entity.Documento{documento.ID: documento}

	_, err := servico.ObterEstrutura(context.Background(), dono, documento.ID)
	require.Error(t, err)

	var aplicacao *errors.ErroAplicacao
	assert.Truef(t, errors.Como(err, &aplicacao),
		"cdm corrompido no banco é falha do servidor (HTTP 500), esperava *errors.ErroAplicacao, obteve %T: %v", err, err)

	var validacao *errors.ErroValidacao
	assert.Falsef(t, errors.Como(err, &validacao),
		"cdm corrompido no banco NUNCA pode virar *errors.ErroValidacao (culparia o cliente por um GET, HTTP 400): %v", err)
}

// documentoComCDMDeTeste monta um documento já analisado, com um CDM válido
// de dois blocos (um título e uma seção), pronto para ObterEstrutura.
func documentoComCDMDeTeste(t *testing.T, dono vo.Dono) entity.Documento {
	t.Helper()
	documento := documentoDeTeste(t, dono)
	documento.Status = entity.StatusAnalisado

	titulo, err := cdm.NovoBloco(cdm.Titulo, "Título do artigo de teste", 0.95, cdm.OrigemEstiloDocx, 0)
	exigirSemErro(t, err)
	secao, err := cdm.NovoBloco(cdm.Secao(1), "1 INTRODUÇÃO", 0.95, cdm.OrigemEstiloDocx, 1)
	exigirSemErro(t, err)

	serializado, err := cdm.NovoIndice([]cdm.Bloco{titulo, secao}).Serializar()
	exigirSemErro(t, err)
	documento.CDM = serializado
	return documento
}
