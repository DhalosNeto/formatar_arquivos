// RED (TDD): CaminhoAnalise, CaminhoEstrutura, Controlador.TratarAnalise e
// Controlador.TratarEstrutura ainda não existem em controlador.go, e
// NovoControlador ainda só aceita um parâmetro. Espera-se falha de
// compilação até o codador:
//   - acrescentar analise *webservices.ServicoAnalise a Controlador;
//   - mudar NovoControlador para NovoControlador(servico, analise);
//   - declarar CaminhoAnalise = "/documentos/:id/analisar" e
//     CaminhoEstrutura = "/documentos/:id/estrutura";
//   - implementar TratarAnalise (202, idempotente) e TratarEstrutura
//     (200/409/404), seguindo exatamente o padrão de TratarPreview.
//
// analiseDeTeste, abaixo, é usado também por pipeline_test.go e
// listagem_test.go (mesmo pacote) para montar o segundo argumento do novo
// NovoControlador — nenhum dos dois arquivos testa análise/estrutura, só
// precisam continuar compilando com a nova dependência.
package documentos

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/daniel-halos/formatador/internal/application/web/webmodel"
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

// jobsRepoMemoriaDocumentos implementa repository.CriacaoJobRepo com
// idempotência real em memória — mesmo espírito do jobsRepoMemoria em
// internal/application/web/webservices/analise_test.go, duplicado aqui de
// propósito: pacotes diferentes, dublês locais (mesma convenção já usada
// neste pacote para repositorioMemoria vs. repositorioFake).
type jobsRepoMemoriaDocumentos struct {
	porChave map[string]jobentity.Job
}

func novoJobsRepoMemoriaDocumentos() *jobsRepoMemoriaDocumentos {
	return &jobsRepoMemoriaDocumentos{porChave: map[string]jobentity.Job{}}
}

func (r *jobsRepoMemoriaDocumentos) InserirOuObter(_ context.Context, _ vo.Dono, job jobentity.Job, chave uuid.UUID) (jobentity.Job, error) {
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

var _ repository.CriacaoJobRepo = (*jobsRepoMemoriaDocumentos)(nil)

// analiseDeTeste monta um *webservices.ServicoAnalise sobre o MESMO
// documentoservice.Servico já usado pelo resto da pilha de teste, com um
// repositório de jobs em memória próprio — reusado por controladorDeTeste
// (pipeline_test.go) e controladorComEspiaDeTeste (listagem_test.go).
func analiseDeTeste(t *testing.T, docservico *documentoservice.Servico) *webservices.ServicoAnalise {
	t.Helper()
	jobsCriacao, err := criacao.NovoServico(novoJobsRepoMemoriaDocumentos(), docservico)
	exigirSemErroDocumentos(t, err)
	analise, err := webservices.NovoServicoAnalise(docservico, jobsCriacao)
	exigirSemErroDocumentos(t, err)
	return analise
}

// controladorDeAnaliseDeTeste monta a mesma pilha de controladorDeTeste, mas
// devolve também o repositório de documentos para os testes deste arquivo
// poderem plantar um documento já em estado "analisado" com CDM — o mesmo
// truque que TestTratarPreviewConflitoQuandoDocumentoNaoTemPreviewDevolve409
// já usa para o preview.
func controladorDeAnaliseDeTeste(t *testing.T) (*Controlador, *repositorioMemoria) {
	t.Helper()
	repo := novoRepositorioMemoria()
	docservico, err := documentoservice.NovoServico(repo, 0)
	exigirSemErroDocumentos(t, err)
	servico, err := webservices.NovoServicoDocumento(docservico, armazenadorMemoria{}, conversorMemoria{})
	exigirSemErroDocumentos(t, err)
	analise := analiseDeTeste(t, docservico)
	return NovoControlador(servico, analise, nil), repo
}

func inserirDocumentoDeTeste(t *testing.T, repo *repositorioMemoria, dono vo.Dono, mutar func(*entity.Documento)) entity.Documento {
	t.Helper()
	documento, err := entity.NovoDocumento(dono, "artigo.docx", vo.FormatoDocx, 4096)
	exigirSemErroDocumentos(t, err)
	if mutar != nil {
		mutar(&documento)
	}
	exigirSemErroDocumentos(t, repo.Inserir(context.Background(), documento))
	return documento
}

// cdmValidoDeTeste serializa um CDM de dois blocos, pronto para ser plantado
// em entity.Documento.CDM.
func cdmValidoDeTeste(t *testing.T) json.RawMessage {
	t.Helper()
	titulo, err := cdm.NovoBloco(cdm.Titulo, "Título do artigo de teste", 0.95, cdm.OrigemEstiloDocx, 0)
	exigirSemErroDocumentos(t, err)
	secao, err := cdm.NovoBloco(cdm.Secao(1), "1 INTRODUÇÃO", 0.95, cdm.OrigemEstiloDocx, 1)
	exigirSemErroDocumentos(t, err)
	serializado, err := cdm.NovoIndice([]cdm.Bloco{titulo, secao}).Serializar()
	exigirSemErroDocumentos(t, err)
	return serializado
}

// ---------------------------------------------------------------------------
// POST /documentos/:id/analisar
// ---------------------------------------------------------------------------

func TestTratarAnaliseIDInvalidoRecusa(t *testing.T) {
	t.Parallel()
	controlador, _ := controladorDeAnaliseDeTeste(t)
	requisicao := &requisicaoFake{
		metodo: http.MethodPost, caminho: CaminhoAnalise,
		cookie: uuid.New().String(), parametros: map[string]string{"id": "###"},
	}
	resposta := &respostaFake{}
	exigirSemErroDocumentos(t, controlador.TratarAnalise(context.Background(), requisicao, resposta))
	if resposta.status != http.StatusBadRequest {
		t.Fatalf("esperava 400, obteve %d", resposta.status)
	}
}

func TestTratarAnaliseSemCookieDevolve404(t *testing.T) {
	t.Parallel()
	controlador, _ := controladorDeAnaliseDeTeste(t)
	requisicao := &requisicaoFake{
		metodo: http.MethodPost, caminho: CaminhoAnalise,
		parametros: map[string]string{"id": uuid.New().String()},
	}
	resposta := &respostaFake{}
	exigirSemErroDocumentos(t, controlador.TratarAnalise(context.Background(), requisicao, resposta))
	if resposta.status != http.StatusNotFound {
		t.Fatalf("esperava 404 (mesmo tratamento das demais rotas para sessão ausente), obteve %d", resposta.status)
	}
}

func TestTratarAnaliseDocumentoInexistenteOuDeOutroDonoDevolve404(t *testing.T) {
	t.Parallel()
	controlador, repo := controladorDeAnaliseDeTeste(t)
	donoDono, err := vo.NovoDonoSessao(uuid.New())
	exigirSemErroDocumentos(t, err)
	documento := inserirDocumentoDeTeste(t, repo, donoDono, nil)

	requisicao := &requisicaoFake{
		metodo: http.MethodPost, caminho: CaminhoAnalise,
		cookie:     uuid.New().String(), // outro dono
		parametros: map[string]string{"id": documento.ID.String()},
	}
	resposta := &respostaFake{}
	exigirSemErroDocumentos(t, controlador.TratarAnalise(context.Background(), requisicao, resposta))
	if resposta.status != http.StatusNotFound {
		t.Fatalf("esperava 404 para documento de outro dono, obteve %d", resposta.status)
	}
}

func TestTratarAnaliseFelizDevolve202ComJob(t *testing.T) {
	t.Parallel()
	controlador, repo := controladorDeAnaliseDeTeste(t)
	sessaoID := uuid.New()
	dono, err := vo.NovoDonoSessao(sessaoID)
	exigirSemErroDocumentos(t, err)
	documento := inserirDocumentoDeTeste(t, repo, dono, nil)

	requisicao := &requisicaoFake{
		metodo: http.MethodPost, caminho: CaminhoAnalise,
		cookie:     sessaoID.String(),
		parametros: map[string]string{"id": documento.ID.String()},
	}
	resposta := &respostaFake{}
	exigirSemErroDocumentos(t, controlador.TratarAnalise(context.Background(), requisicao, resposta))
	if resposta.status != http.StatusAccepted {
		t.Fatalf("esperava 202, obteve %d (codigo=%q descricao=%q)", resposta.status, resposta.codigo, resposta.descricao)
	}
	job, ok := resposta.corpo.(webmodel.JobResposta)
	if !ok {
		t.Fatalf("corpo inesperado: %T", resposta.corpo)
	}
	if job.DocumentoID != documento.ID || job.Tipo != jobentity.TipoAnalisar.String() || job.Status != jobentity.StatusPendente.String() {
		t.Fatalf("job devolvido inesperado: %+v", job)
	}
}

func TestTratarAnaliseChamadaDuasVezesDevolveOMesmoJob(t *testing.T) {
	t.Parallel()
	controlador, repo := controladorDeAnaliseDeTeste(t)
	sessaoID := uuid.New()
	dono, err := vo.NovoDonoSessao(sessaoID)
	exigirSemErroDocumentos(t, err)
	documento := inserirDocumentoDeTeste(t, repo, dono, nil)

	requisicao := &requisicaoFake{
		metodo: http.MethodPost, caminho: CaminhoAnalise,
		cookie:     sessaoID.String(),
		parametros: map[string]string{"id": documento.ID.String()},
	}

	primeira := &respostaFake{}
	exigirSemErroDocumentos(t, controlador.TratarAnalise(context.Background(), requisicao, primeira))
	segunda := &respostaFake{}
	exigirSemErroDocumentos(t, controlador.TratarAnalise(context.Background(), requisicao, segunda))

	jobUm, ok := primeira.corpo.(webmodel.JobResposta)
	if !ok {
		t.Fatalf("corpo inesperado na primeira chamada: %T", primeira.corpo)
	}
	jobDois, ok := segunda.corpo.(webmodel.JobResposta)
	if !ok {
		t.Fatalf("corpo inesperado na segunda chamada: %T", segunda.corpo)
	}
	if jobUm.ID != jobDois.ID {
		t.Fatalf("chamar POST /analisar duas vezes criou dois jobs: %s != %s", jobUm.ID, jobDois.ID)
	}
}

// ---------------------------------------------------------------------------
// GET /documentos/:id/estrutura
// ---------------------------------------------------------------------------

func TestTratarEstruturaIDInvalidoRecusa(t *testing.T) {
	t.Parallel()
	controlador, _ := controladorDeAnaliseDeTeste(t)
	requisicao := &requisicaoFake{
		metodo: http.MethodGet, caminho: CaminhoEstrutura,
		cookie: uuid.New().String(), parametros: map[string]string{"id": "###"},
	}
	resposta := &respostaFake{}
	exigirSemErroDocumentos(t, controlador.TratarEstrutura(context.Background(), requisicao, resposta))
	if resposta.status != http.StatusBadRequest {
		t.Fatalf("esperava 400, obteve %d", resposta.status)
	}
}

func TestTratarEstruturaSemCookieDevolve404(t *testing.T) {
	t.Parallel()
	controlador, _ := controladorDeAnaliseDeTeste(t)
	requisicao := &requisicaoFake{
		metodo: http.MethodGet, caminho: CaminhoEstrutura,
		parametros: map[string]string{"id": uuid.New().String()},
	}
	resposta := &respostaFake{}
	exigirSemErroDocumentos(t, controlador.TratarEstrutura(context.Background(), requisicao, resposta))
	if resposta.status != http.StatusNotFound {
		t.Fatalf("esperava 404, obteve %d", resposta.status)
	}
}

func TestTratarEstruturaDocumentoDeOutroDonoDevolve404(t *testing.T) {
	t.Parallel()
	controlador, repo := controladorDeAnaliseDeTeste(t)
	dono, err := vo.NovoDonoSessao(uuid.New())
	exigirSemErroDocumentos(t, err)
	documento := inserirDocumentoDeTeste(t, repo, dono, func(d *entity.Documento) {
		d.Status = entity.StatusAnalisado
		d.CDM = cdmValidoDeTeste(t)
	})

	requisicao := &requisicaoFake{
		metodo: http.MethodGet, caminho: CaminhoEstrutura,
		cookie:     uuid.New().String(), // outro dono
		parametros: map[string]string{"id": documento.ID.String()},
	}
	resposta := &respostaFake{}
	exigirSemErroDocumentos(t, controlador.TratarEstrutura(context.Background(), requisicao, resposta))
	if resposta.status != http.StatusNotFound {
		t.Fatalf("esperava 404 para documento de outro dono, obteve %d", resposta.status)
	}
}

func TestTratarEstruturaDocumentoAindaNaoAnalisadoDevolve409(t *testing.T) {
	t.Parallel()
	controlador, repo := controladorDeAnaliseDeTeste(t)
	sessaoID := uuid.New()
	dono, err := vo.NovoDonoSessao(sessaoID)
	exigirSemErroDocumentos(t, err)
	documento := inserirDocumentoDeTeste(t, repo, dono, nil) // status recebido, sem CDM

	requisicao := &requisicaoFake{
		metodo: http.MethodGet, caminho: CaminhoEstrutura,
		cookie:     sessaoID.String(),
		parametros: map[string]string{"id": documento.ID.String()},
	}
	resposta := &respostaFake{}
	exigirSemErroDocumentos(t, controlador.TratarEstrutura(context.Background(), requisicao, resposta))
	if resposta.status != http.StatusConflict {
		t.Fatalf("esperava 409, obteve %d", resposta.status)
	}
}

func TestTratarEstruturaFelizDevolve200ComEnvelope(t *testing.T) {
	t.Parallel()
	controlador, repo := controladorDeAnaliseDeTeste(t)
	sessaoID := uuid.New()
	dono, err := vo.NovoDonoSessao(sessaoID)
	exigirSemErroDocumentos(t, err)
	documento := inserirDocumentoDeTeste(t, repo, dono, func(d *entity.Documento) {
		d.Status = entity.StatusAnalisado
		d.CDM = cdmValidoDeTeste(t)
	})

	requisicao := &requisicaoFake{
		metodo: http.MethodGet, caminho: CaminhoEstrutura,
		cookie:     sessaoID.String(),
		parametros: map[string]string{"id": documento.ID.String()},
	}
	resposta := &respostaFake{}
	exigirSemErroDocumentos(t, controlador.TratarEstrutura(context.Background(), requisicao, resposta))
	if resposta.status != http.StatusOK {
		t.Fatalf("esperava 200, obteve %d (codigo=%q descricao=%q)", resposta.status, resposta.codigo, resposta.descricao)
	}
	estrutura, ok := resposta.corpo.(webmodel.EstruturaResposta)
	if !ok {
		t.Fatalf("corpo inesperado: %T", resposta.corpo)
	}
	if estrutura.Versao != cdm.VersaoFormatoCDM || len(estrutura.Blocos) != 2 {
		t.Fatalf("envelope inesperado: %+v", estrutura)
	}
}

// TestTratarEstruturaCDMCorrompidoDevolve500NuncaBadRequest é a mesma
// asserção de TestObterEstruturaCDMCorrompidoDevolveErroAplicacaoNuncaErroValidacao
// (internal/application/web/webservices/analise_test.go), mas verificada
// ponta a ponta a partir do controlador HTTP: o bug que erroLinhaCorrompida
// (internal/data/postgres/documento.go) existe para evitar não pode
// reaparecer nesta rota nova.
func TestTratarEstruturaCDMCorrompidoDevolve500NuncaBadRequest(t *testing.T) {
	t.Parallel()
	controlador, repo := controladorDeAnaliseDeTeste(t)
	sessaoID := uuid.New()
	dono, err := vo.NovoDonoSessao(sessaoID)
	exigirSemErroDocumentos(t, err)
	documento := inserirDocumentoDeTeste(t, repo, dono, func(d *entity.Documento) {
		d.Status = entity.StatusAnalisado
		d.CDM = json.RawMessage(`{"versao":1,"blocos":[{"papel":`) // corrompido no banco, não enviado agora pelo cliente
	})

	requisicao := &requisicaoFake{
		metodo: http.MethodGet, caminho: CaminhoEstrutura,
		cookie:     sessaoID.String(),
		parametros: map[string]string{"id": documento.ID.String()},
	}
	resposta := &respostaFake{}
	exigirSemErroDocumentos(t, controlador.TratarEstrutura(context.Background(), requisicao, resposta))
	if resposta.status != http.StatusInternalServerError {
		t.Fatalf("cdm corrompido no banco é falha do servidor: esperava 500, obteve %d (codigo=%q descricao=%q) — "+
			"se veio 400, ObterEstrutura não reclassificou o *errors.ErroValidacao de cdm.Desserializar (mesmo bug de erroLinhaCorrompida)",
			resposta.status, resposta.codigo, resposta.descricao)
	}
}
