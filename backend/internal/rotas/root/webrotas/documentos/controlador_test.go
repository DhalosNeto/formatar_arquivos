package documentos

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/daniel-halos/formatador/internal/application/web/webmodel"
	"github.com/daniel-halos/formatador/internal/application/web/webservices"
	"github.com/daniel-halos/formatador/internal/domain/documento/entity"
	documentoservice "github.com/daniel-halos/formatador/internal/domain/documento/service"
	"github.com/daniel-halos/formatador/internal/domain/job/consulta"
	"github.com/daniel-halos/formatador/internal/domain/job/criacao"
	jobentity "github.com/daniel-halos/formatador/internal/domain/job/entity"
	"github.com/daniel-halos/formatador/internal/domain/job/repository"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/daniel-halos/formatador/internal/rotas"
	"github.com/daniel-halos/formatador/internal/rotas/sessao"
)

// docxValidoDocumentos monta um pacote DOCX mínimo, mas real, o bastante para
// passar por DetectarFormato e por ConferirPacoteDocx.
func docxValidoDocumentos(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	escritor := zip.NewWriter(&buf)
	partes := map[string]string{
		vo.ParteContentTypes:       `<?xml version="1.0"?><Types/>`,
		vo.ParteDocumentoPrincipal: `<?xml version="1.0"?><w:document/>`,
	}
	for nome, conteudo := range partes {
		parte, err := escritor.Create(nome)
		exigirSemErroDocumentos(t, err)
		_, err = parte.Write([]byte(conteudo))
		exigirSemErroDocumentos(t, err)
	}
	exigirSemErroDocumentos(t, escritor.Close())
	return buf.Bytes()
}

func TestTratarCriacaoSemCookieCriaSessaoEDefineCookie(t *testing.T) {
	t.Parallel()
	controlador, _ := controladorDeTeste(t)
	requisicao := &requisicaoFake{
		metodo:  http.MethodPost,
		caminho: CaminhoColecao,
		arquivo: arquivoFormularioDeTeste(t, CampoArquivo, "artigo.docx", docxValidoDocumentos(t)),
	}
	resposta := &respostaFake{}

	exigirSemErroDocumentos(t, controlador.TratarCriacao(context.Background(), requisicao, resposta))

	if resposta.status != http.StatusCreated {
		t.Fatalf("esperava 201, obteve %d (codigo=%q descricao=%q)", resposta.status, resposta.codigo, resposta.descricao)
	}
	if len(resposta.cookies) != 1 {
		t.Fatalf("esperava 1 cookie definido, obteve %d", len(resposta.cookies))
	}
	cookie := resposta.cookies[0]
	if cookie.Name != sessao.NomeCookie {
		t.Fatalf("nome do cookie divergente: %q", cookie.Name)
	}
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" {
		t.Fatalf("atributos de segurança do cookie incorretos: %+v", cookie)
	}
	if _, err := uuid.Parse(cookie.Value); err != nil {
		t.Fatalf("cookie não carrega um uuid válido: %v", err)
	}
}

func TestTratarCriacaoComCookieValidoReusaSemRegravar(t *testing.T) {
	t.Parallel()
	controlador, _ := controladorDeTeste(t)
	requisicao := &requisicaoFake{
		metodo:  http.MethodPost,
		caminho: CaminhoColecao,
		cookie:  uuid.New().String(),
		arquivo: arquivoFormularioDeTeste(t, CampoArquivo, "artigo.docx", docxValidoDocumentos(t)),
	}
	resposta := &respostaFake{}

	exigirSemErroDocumentos(t, controlador.TratarCriacao(context.Background(), requisicao, resposta))

	if resposta.status != http.StatusCreated {
		t.Fatalf("esperava 201, obteve %d", resposta.status)
	}
	if len(resposta.cookies) != 0 {
		t.Fatalf("não deveria regravar cookie já válido, obteve %d gravações", len(resposta.cookies))
	}
}

func TestTratarCriacaoComCookieDeLixoTrataComoAusente(t *testing.T) {
	t.Parallel()
	for _, caso := range []struct{ nome, cookie string }{
		{"não é uuid", "lixo-nao-uuid"},
		{"uuid nil", uuid.Nil.String()},
		{"vazio", ""},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			controlador, _ := controladorDeTeste(t)
			requisicao := &requisicaoFake{
				metodo:  http.MethodPost,
				caminho: CaminhoColecao,
				cookie:  caso.cookie,
				arquivo: arquivoFormularioDeTeste(t, CampoArquivo, "artigo.docx", docxValidoDocumentos(t)),
			}
			resposta := &respostaFake{}
			exigirSemErroDocumentos(t, controlador.TratarCriacao(context.Background(), requisicao, resposta))
			if resposta.status != http.StatusCreated {
				t.Fatalf("esperava 201, obteve %d", resposta.status)
			}
			if len(resposta.cookies) != 1 {
				t.Fatalf("esperava novo cookie gravado para cookie de lixo, obteve %d", len(resposta.cookies))
			}
		})
	}
}

func TestTratarCriacaoSemCampoArquivoRecusa(t *testing.T) {
	t.Parallel()
	controlador, _ := controladorDeTeste(t)
	requisicao := &requisicaoFake{
		metodo:      http.MethodPost,
		caminho:     CaminhoColecao,
		erroArquivo: http.ErrMissingFile,
	}
	resposta := &respostaFake{}

	exigirSemErroDocumentos(t, controlador.TratarCriacao(context.Background(), requisicao, resposta))

	if resposta.status != http.StatusBadRequest {
		t.Fatalf("esperava 400, obteve %d", resposta.status)
	}
}

func TestTratarCriacaoRespostaNaoVazaCamposInternos(t *testing.T) {
	t.Parallel()
	controlador, _ := controladorDeTeste(t)
	requisicao := &requisicaoFake{
		metodo:  http.MethodPost,
		caminho: CaminhoColecao,
		arquivo: arquivoFormularioDeTeste(t, CampoArquivo, "artigo.docx", docxValidoDocumentos(t)),
	}
	resposta := &respostaFake{}
	exigirSemErroDocumentos(t, controlador.TratarCriacao(context.Background(), requisicao, resposta))
	if resposta.status != http.StatusCreated {
		t.Fatalf("pré-condição falhou: criação não retornou 201 (status=%d)", resposta.status)
	}

	bruto, err := json.Marshal(resposta.corpo)
	exigirSemErroDocumentos(t, err)
	corpo := string(bruto)
	for _, termoProibido := range []string{"chave_storage", "chave_storage_pdf", "sessao", "dono"} {
		if strings.Contains(corpo, termoProibido) {
			t.Fatalf("resposta vazou campo interno %q: %s", termoProibido, corpo)
		}
	}
	if len(resposta.cookies) == 1 && strings.Contains(corpo, resposta.cookies[0].Value) {
		t.Fatalf("resposta vazou o uuid de sessão no corpo: %s", corpo)
	}
}

func TestTratarObtencaoIDInvalidoRecusa(t *testing.T) {
	t.Parallel()
	controlador, _ := controladorDeTeste(t)
	requisicao := &requisicaoFake{
		metodo:     http.MethodGet,
		caminho:    CaminhoItem,
		cookie:     uuid.New().String(),
		parametros: map[string]string{"id": "isto-nao-e-um-uuid"},
	}
	resposta := &respostaFake{}
	exigirSemErroDocumentos(t, controlador.TratarObtencao(context.Background(), requisicao, resposta))
	if resposta.status != http.StatusBadRequest {
		t.Fatalf("esperava 400, obteve %d", resposta.status)
	}
}

func TestTratarObtencaoSemCookieDevolve404(t *testing.T) {
	t.Parallel()
	controlador, _ := controladorDeTeste(t)
	requisicao := &requisicaoFake{
		metodo:     http.MethodGet,
		caminho:    CaminhoItem,
		parametros: map[string]string{"id": uuid.New().String()},
	}
	resposta := &respostaFake{}
	exigirSemErroDocumentos(t, controlador.TratarObtencao(context.Background(), requisicao, resposta))
	if resposta.status != http.StatusNotFound {
		t.Fatalf("esperava 404 (não 401, para não virar oráculo de existência), obteve %d", resposta.status)
	}
}

func TestTratarObtencaoDevolveDocumentoDoProprioDono(t *testing.T) {
	t.Parallel()
	controlador, _ := controladorDeTeste(t)
	requisicaoCriacao := &requisicaoFake{
		metodo:  http.MethodPost,
		caminho: CaminhoColecao,
		arquivo: arquivoFormularioDeTeste(t, CampoArquivo, "artigo.docx", docxValidoDocumentos(t)),
	}
	respostaCriacao := &respostaFake{}
	exigirSemErroDocumentos(t, controlador.TratarCriacao(context.Background(), requisicaoCriacao, respostaCriacao))
	if respostaCriacao.status != http.StatusCreated {
		t.Fatalf("pré-condição falhou: criação não retornou 201 (status=%d)", respostaCriacao.status)
	}
	corpoCriado, ok := respostaCriacao.corpo.(webmodel.DocumentoResposta)
	if !ok {
		t.Fatalf("corpo de criação inesperado: %T", respostaCriacao.corpo)
	}
	if len(respostaCriacao.cookies) != 1 {
		t.Fatalf("pré-condição falhou: esperava cookie de sessão gravado")
	}
	sessaoID := respostaCriacao.cookies[0].Value

	requisicaoObtencao := &requisicaoFake{
		metodo:     http.MethodGet,
		caminho:    CaminhoItem,
		cookie:     sessaoID,
		parametros: map[string]string{"id": corpoCriado.ID.String()},
	}
	respostaObtencao := &respostaFake{}
	exigirSemErroDocumentos(t, controlador.TratarObtencao(context.Background(), requisicaoObtencao, respostaObtencao))
	if respostaObtencao.status != http.StatusOK {
		t.Fatalf("esperava 200, obteve %d", respostaObtencao.status)
	}
}

func TestTratarPreviewIDInvalidoRecusa(t *testing.T) {
	t.Parallel()
	controlador, _ := controladorDeTeste(t)
	requisicao := &requisicaoFake{
		metodo:     http.MethodGet,
		caminho:    CaminhoPreview,
		cookie:     uuid.New().String(),
		parametros: map[string]string{"id": "###"},
	}
	resposta := &respostaFake{}
	exigirSemErroDocumentos(t, controlador.TratarPreview(context.Background(), requisicao, resposta))
	if resposta.status != http.StatusBadRequest {
		t.Fatalf("esperava 400, obteve %d", resposta.status)
	}
}

func TestTratarPreviewSemCookieDevolve404(t *testing.T) {
	t.Parallel()
	controlador, _ := controladorDeTeste(t)
	requisicao := &requisicaoFake{
		metodo:     http.MethodGet,
		caminho:    CaminhoPreview,
		parametros: map[string]string{"id": uuid.New().String()},
	}
	resposta := &respostaFake{}
	exigirSemErroDocumentos(t, controlador.TratarPreview(context.Background(), requisicao, resposta))
	if resposta.status != http.StatusNotFound {
		t.Fatalf("esperava 404, obteve %d", resposta.status)
	}
}

func TestTratarPreviewConflitoQuandoDocumentoNaoTemPreviewDevolve409(t *testing.T) {
	t.Parallel()
	controlador, repo := controladorDeTeste(t)

	sessaoID := uuid.New()
	dono, err := vo.NovoDonoSessao(sessaoID)
	exigirSemErroDocumentos(t, err)
	documento, err := entity.NovoDocumento(dono, "artigo.docx", vo.FormatoDocx, 4096)
	exigirSemErroDocumentos(t, err)
	exigirSemErroDocumentos(t, repo.Inserir(context.Background(), documento))

	requisicao := &requisicaoFake{
		metodo:     http.MethodGet,
		caminho:    CaminhoPreview,
		cookie:     sessaoID.String(),
		parametros: map[string]string{"id": documento.ID.String()},
	}
	resposta := &respostaFake{}
	exigirSemErroDocumentos(t, controlador.TratarPreview(context.Background(), requisicao, resposta))
	if resposta.status != http.StatusConflict {
		t.Fatalf("esperava 409, obteve %d", resposta.status)
	}
}

// ---------------------------------------------------------------------------
// RED (TDD) do recorte f3-endpoint-e-download-de-formatacao — C5 (A10-A14).
//
// Nada do que vem abaixo existe: espera-se falha de COMPILAÇÃO
// ("unknown field formatacao in struct literal",
// "controlador.TratarFormatacao undefined",
// "controlador.TratarFormatado undefined") até o codador entregar os dois
// manipuladores em controlador.go, o campo novo em Controlador e o registro
// em rotas.go.
//
// DECISÃO DE DESENHO (testador): o controlador destes testes é montado por
// LITERAL DE STRUCT com campos nomeados, e não por NovoControlador. Acrescentar
// um quarto parâmetro a NovoControlador quebraria a compilação de cinco
// arquivos fora do recorte deste arquivo (pipeline_test.go:94,
// listagem_test.go:81, analise_test.go:91, estrutura_test.go:83 e
// data/postgres/f2_http_integration_test.go:54). O literal mantém o RED
// confinado ao que o recorte prova e deixa a assinatura de NovoControlador
// como decisão do codador. O PREÇO: o nome do campo novo (`formatacao`) passa
// a ser contrato destes testes, e a fiação por NovoControlador fica coberta
// apenas por compilação, não por teste.
//
// CAMINHOS, literais de propósito: estes testes não dependem do NOME das
// constantes de caminho que o codador vai declarar, só dos caminhos que o
// contrato promete.
// ---------------------------------------------------------------------------

const (
	caminhoFormatarEsperado  = "/documentos/:id/formatar"
	caminhoFormatadoEsperado = "/documentos/:id/formatado"
)

// mensagemGateDePerfil é a mensagem que o gate real produz
// (entity.TipoJob.ValidarRulesetParaNovoJob, job/entity/tipo.go:52), chamada
// pelo adaptador em data/postgres/job.go:148-167. Está aqui para A12 poder
// exigir que o controlador NÃO reescreva o texto que sobe de baixo.
const mensagemGateDePerfil = "perfil de formatação indisponível"

// jobsRepoComGate implementa repository.CriacaoJobRepo com unicidade real de
// (documento_id, chave) E com o gate de perfil, delegando a decisão à REGRA DE
// DOMÍNIO (ValidarRulesetParaNovoJob) em vez de inventar um erro próprio —
// senão A12 estaria conferindo a mensagem do fake contra ela mesma.
type jobsRepoComGate struct {
	porChave map[string]jobentity.Job
	// ativos lista os perfis existentes e ativos. Perfil fora do mapa é
	// AUSENTE; perfil com valor false é INATIVO.
	ativos map[uuid.UUID]bool
}

func novoJobsRepoComGate() *jobsRepoComGate {
	return &jobsRepoComGate{porChave: map[string]jobentity.Job{}, ativos: map[uuid.UUID]bool{}}
}

func (r *jobsRepoComGate) InserirOuObter(_ context.Context, _ vo.Dono, job jobentity.Job, chave uuid.UUID) (jobentity.Job, error) {
	chaveMapa := job.DocumentoID.String() + "|" + chave.String()
	if existente, ok := r.porChave[chaveMapa]; ok {
		// Repetição de chave vence o gate, como o adaptador real faz.
		if existente.Tipo != job.Tipo {
			return jobentity.Job{}, errors.NovoErroConflito("chave de idempotência já utilizada com dados diferentes")
		}
		return existente, nil
	}
	if job.Tipo.ExigeRuleset() {
		var ativo *bool
		if job.RulesetID != nil {
			if lido, existe := r.ativos[*job.RulesetID]; existe {
				ativo = &lido
			}
		}
		if err := job.Tipo.ValidarRulesetParaNovoJob(ativo); err != nil {
			return jobentity.Job{}, err
		}
	}
	r.porChave[chaveMapa] = job
	return job, nil
}

var _ repository.CriacaoJobRepo = (*jobsRepoComGate)(nil)

// consultaJobsHTTP implementa repository.ConsultaJobRepo com a lista de jobs
// de um documento, na ordem em que o SQL as devolve.
type consultaJobsHTTP struct {
	porDocumento map[uuid.UUID][]jobentity.Job
}

func (*consultaJobsHTTP) ObterPorID(context.Context, vo.Dono, uuid.UUID) (jobentity.Job, error) {
	panic("os manipuladores de formatação não consultam job por id")
}

func (r *consultaJobsHTTP) ListarPorDocumento(_ context.Context, _ vo.Dono, documentoID uuid.UUID) ([]jobentity.Job, error) {
	return r.porDocumento[documentoID], nil
}

var _ repository.ConsultaJobRepo = (*consultaJobsHTTP)(nil)

// pilhaFormatacaoHTTP é a pilha montada para os testes de C5: domínio e
// aplicação REAIS, só as bordas (storage, repositórios) em memória.
type pilhaFormatacaoHTTP struct {
	servidor  *echo.Echo
	rotas     rotas.Roteador
	documento entity.Documento
	sessaoID  uuid.UUID
	jobs      *jobsRepoComGate
	consultas *consultaJobsHTTP
	perfilOK  uuid.UUID
}

// novaPilhaFormatacaoHTTP planta um documento analisado, com CDM, do dono da
// sessão devolvida, e um perfil ATIVO em perfilOK.
func novaPilhaFormatacaoHTTP(t *testing.T) pilhaFormatacaoHTTP {
	t.Helper()
	sessaoID := uuid.New()
	dono, err := vo.NovoDonoSessao(sessaoID)
	exigirSemErroDocumentos(t, err)
	documento, err := entity.NovoDocumento(dono, "artigo.docx", vo.FormatoDocx, 4096)
	exigirSemErroDocumentos(t, err)
	documento.Status = entity.StatusAnalisado
	documento.CDM = cdmValidoDeTeste(t)

	repo := novoRepositorioMemoria()
	exigirSemErroDocumentos(t, repo.Inserir(context.Background(), documento))
	docservico, err := documentoservice.NovoServico(repo, 0)
	exigirSemErroDocumentos(t, err)

	jobsRepo := novoJobsRepoComGate()
	perfilOK := uuid.New()
	jobsRepo.ativos[perfilOK] = true
	jobsCriacao, err := criacao.NovoServico(jobsRepo, docservico)
	exigirSemErroDocumentos(t, err)

	consultaRepo := &consultaJobsHTTP{porDocumento: map[uuid.UUID][]jobentity.Job{}}
	consultas, err := consulta.NovoServico(consultaRepo, docservico)
	exigirSemErroDocumentos(t, err)

	formatacao, err := webservices.NovoServicoFormatacao(docservico, jobsCriacao, consultas, armazenadorMemoria{})
	exigirSemErroDocumentos(t, err)

	servicoDocumento, err := webservices.NovoServicoDocumento(docservico, armazenadorMemoria{}, conversorMemoria{})
	exigirSemErroDocumentos(t, err)

	controlador := &Controlador{
		servico:    servicoDocumento,
		analise:    analiseDeTeste(t, docservico),
		formatacao: formatacao,
	}
	roteador := Roteador(controlador, 1<<20)
	servidor := echo.New()
	rotas.AplicarEmEcho(servidor, roteador, "/v1")

	return pilhaFormatacaoHTTP{
		servidor: servidor, rotas: roteador, documento: documento, sessaoID: sessaoID,
		jobs: jobsRepo, consultas: consultaRepo, perfilOK: perfilOK,
	}
}

// pedir executa uma requisição HTTP de verdade contra o servidor montado.
// contentType vazio significa SEM cabeçalho Content-Type, e cookie vazio
// significa SEM cookie de sessão — as duas ausências são casos de teste, não
// descuido.
func (p pilhaFormatacaoHTTP) pedir(t *testing.T, metodo, caminho, corpo, contentType string, cookie string) *httptest.ResponseRecorder {
	t.Helper()
	requisicao := httptest.NewRequestWithContext(t.Context(), metodo, caminho, strings.NewReader(corpo))
	if contentType != "" {
		requisicao.Header.Set("Content-Type", contentType)
	}
	if cookie != "" {
		requisicao.AddCookie(&http.Cookie{Name: sessao.NomeCookie, Value: cookie})
	}
	resposta := httptest.NewRecorder()
	p.servidor.ServeHTTP(resposta, requisicao)
	return resposta
}

func (p pilhaFormatacaoHTTP) urlFormatar() string {
	return "/v1/documentos/" + p.documento.ID.String() + "/formatar"
}

func (p pilhaFormatacaoHTTP) urlFormatado(rulesetID string) string {
	destino := "/v1/documentos/" + p.documento.ID.String() + "/formatado"
	if rulesetID != "" {
		destino += "?ruleset_id=" + rulesetID
	}
	return destino
}

// TestFormatacaoHTTPCaminhoFeliz é o CONTROLE POSITIVO dos testes de recusa
// abaixo: sem ele, um manipulador que respondesse 400 a tudo passaria em A10,
// A11 e A12, e um "nenhum job criado" seria verdade por vacuidade.
func TestFormatacaoHTTPCaminhoFeliz(t *testing.T) {
	t.Parallel()
	pilha := novaPilhaFormatacaoHTTP(t)

	resposta := pilha.pedir(t, http.MethodPost, pilha.urlFormatar(),
		`{"ruleset_id":"`+pilha.perfilOK.String()+`"}`, "application/json", pilha.sessaoID.String())

	require.Equal(t, http.StatusAccepted, resposta.Code, resposta.Body.String())
	require.Len(t, pilha.jobs.porChave, 1, "o caminho feliz tem de criar exatamente um job")
	corpo := resposta.Body.String()
	require.Contains(t, corpo, `"tipo":"formatar"`)
	require.Contains(t, corpo, `"status":"pendente"`)
	require.NotContains(t, corpo, "ruleset", "a JobResposta não carrega ruleset_id")
	require.NotContains(t, corpo, "resultado", "a JobResposta não carrega o Resultado interno")
}

// TestFormatacaoHTTPCorpoInvalido cobre A10.
func TestFormatacaoHTTPCorpoInvalido(t *testing.T) {
	t.Parallel()
	casos := []struct {
		nome        string
		corpo       string
		contentType string
		// campoNaRazao vazio significa "não exigir campo nomeado": para
		// Content-Type e teto de corpo, o contrato é o status, não o campo.
		campoNaRazao string
	}{
		{nome: "sem ruleset_id", corpo: `{}`, contentType: "application/json", campoNaRazao: "ruleset_id"},
		{nome: "ruleset_id malformado", corpo: `{"ruleset_id":"nao-e-um-uuid"}`, contentType: "application/json", campoNaRazao: "ruleset_id"},
		{nome: "ruleset_id nulo", corpo: `{"ruleset_id":"` + uuid.Nil.String() + `"}`, contentType: "application/json", campoNaRazao: "ruleset_id"},
		{nome: "sem content-type", corpo: `{"ruleset_id":"` + uuid.New().String() + `"}`, contentType: ""},
		{nome: "content-type errado", corpo: `{"ruleset_id":"` + uuid.New().String() + `"}`, contentType: "text/plain"},
		{nome: "corpo acima do teto", corpo: strings.Repeat(" ", 8192) + `{"ruleset_id":"` + uuid.New().String() + `"}`, contentType: "application/json"},
		{nome: "corpo vazio", corpo: ``, contentType: "application/json", campoNaRazao: "ruleset_id"},
		{nome: "chave duplicada", corpo: `{"ruleset_id":"` + uuid.New().String() + `","ruleset_id":"` + uuid.New().String() + `"}`, contentType: "application/json"},
		{nome: "campo desconhecido", corpo: `{"ruleset_id":"` + uuid.New().String() + `","forcar":true}`, contentType: "application/json"},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()
			pilha := novaPilhaFormatacaoHTTP(t)

			resposta := pilha.pedir(t, http.MethodPost, pilha.urlFormatar(), caso.corpo, caso.contentType, pilha.sessaoID.String())

			require.Equal(t, http.StatusBadRequest, resposta.Code, resposta.Body.String())
			if caso.campoNaRazao != "" {
				require.Contains(t, resposta.Body.String(), caso.campoNaRazao,
					"a razão tem de nomear o campo reprovado")
			}
			require.Empty(t, pilha.jobs.porChave, "nenhum job pode ser criado com corpo inválido")
		})
	}
}

// TestFormatacaoHTTPNaoEcoaOCorpoRecebido cobre A11, a regra 7 na leitura do
// corpo: a mensagem de parse é FIXA, nunca derivada do que o cliente mandou.
//
// É o espelho de TestIDDaRotaNaoEcoaOValorRecebido. Escrito sobre o CORPO, e
// não sobre Vincular, porque C5 manda adotar o idioma de lerCorrecao
// (estrutura.go:58), que não usa Bind — mas continua derrubando quem trocar a
// leitura por requisicao.Vincular, cujo erro do Echo carrega fragmento do
// corpo.
func TestFormatacaoHTTPNaoEcoaOCorpoRecebido(t *testing.T) {
	t.Parallel()
	const marca = "MARCA-UNICA-7Q3X"
	casos := []struct{ nome, corpo string }{
		{"valor com a marca", `{"ruleset_id":"` + marca + `"}`},
		{"json truncado com a marca", `{"ruleset_id":"` + marca + `"`},
		{"chave com a marca", `{"` + marca + `":1}`},
		{"corpo que não é objeto", `"` + marca + `"`},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()
			pilha := novaPilhaFormatacaoHTTP(t)

			resposta := pilha.pedir(t, http.MethodPost, pilha.urlFormatar(), caso.corpo, "application/json", pilha.sessaoID.String())

			require.Equal(t, http.StatusBadRequest, resposta.Code)
			require.NotContains(t, resposta.Body.String(), marca,
				"nenhuma razão, descrição ou código pode ecoar o corpo enviado")
			require.Empty(t, pilha.jobs.porChave)
		})
	}
}

// TestFormatacaoHTTPGateDePerfil cobre A12: perfil INATIVO e perfil
// INEXISTENTE pela primeira vez por HTTP.
//
// O que este teste prova: o manipulador NÃO reescreve nem remapeia o erro do
// gate — mesma classe (400 de validação), mesmo campo e MESMA MENSAGEM que
// sobe de baixo, sem texto inventado no controlador.
//
// LIMITE DECLARADO: ele NÃO prova indistinguibilidade entre inativo e
// inexistente. O gate real roda em data/postgres/job.go:148-167; aqui, com
// fake, a igualdade das duas respostas é consequência do fake, não do sistema.
// A indistinguibilidade é provada em job/entity/tipo_test.go e em integração.
func TestFormatacaoHTTPGateDePerfil(t *testing.T) {
	t.Parallel()
	respostas := map[string]string{}
	for _, caso := range []struct {
		nome   string
		ativo  *bool
		perfil uuid.UUID
	}{
		{nome: "perfil inativo", ativo: new(bool), perfil: uuid.New()},
		{nome: "perfil inexistente", perfil: uuid.New()},
	} {
		pilha := novaPilhaFormatacaoHTTP(t)
		if caso.ativo != nil {
			pilha.jobs.ativos[caso.perfil] = *caso.ativo
		}

		resposta := pilha.pedir(t, http.MethodPost, pilha.urlFormatar(),
			`{"ruleset_id":"`+caso.perfil.String()+`"}`, "application/json", pilha.sessaoID.String())

		require.Equal(t, http.StatusBadRequest, resposta.Code, caso.nome+": "+resposta.Body.String())
		require.Contains(t, resposta.Body.String(), "ruleset_id: "+mensagemGateDePerfil,
			caso.nome+": a razão do gate tem de chegar ao cliente intacta, sem texto do controlador")
		require.Empty(t, pilha.jobs.porChave, caso.nome+": nenhum job criado quando o gate reprova")
		respostas[caso.nome] = resposta.Body.String()
	}
	require.Equal(t, respostas["perfil inativo"], respostas["perfil inexistente"],
		"as duas respostas são iguais PORQUE o fake delega à mesma regra de domínio — não é prova de indistinguibilidade do sistema")
}

// TestFormatacaoHTTPIDDeRotaESessao cobre A13, nos DOIS manipuladores.
//
// Valores NOMEADOS: 400 para id de rota inválido e 404 para ausência de
// cookie — 404 é o idioma de sessao.Existente já travado por
// TestTratarPreviewSemCookieDevolve404. Dizer "o status que Classificar
// define" deixaria a implementação se autoconfirmar.
func TestFormatacaoHTTPIDDeRotaESessao(t *testing.T) {
	t.Parallel()
	const corpoValido = `{"ruleset_id":"a2d9a0d0-7f9a-4f7e-9a3e-4b1c2d3e4f50"}`
	casos := []struct {
		nome    string
		metodo  string
		caminho func(pilha pilhaFormatacaoHTTP) string
		corpo   string
		comTipo bool
		cookie  bool
		status  int
	}{
		{
			nome: "formatar com id de rota invalido", metodo: http.MethodPost,
			caminho: func(pilhaFormatacaoHTTP) string { return "/v1/documentos/isto-nao-e-um-uuid/formatar" },
			corpo:   corpoValido, comTipo: true, cookie: true, status: http.StatusBadRequest,
		},
		{
			nome: "formatar com id de rota nulo", metodo: http.MethodPost,
			caminho: func(pilhaFormatacaoHTTP) string { return "/v1/documentos/" + uuid.Nil.String() + "/formatar" },
			corpo:   corpoValido, comTipo: true, cookie: true, status: http.StatusBadRequest,
		},
		{
			nome: "formatar sem cookie de sessao", metodo: http.MethodPost,
			caminho: func(pilha pilhaFormatacaoHTTP) string { return pilha.urlFormatar() },
			corpo:   corpoValido, comTipo: true, cookie: false, status: http.StatusNotFound,
		},
		{
			nome: "formatado com id de rota invalido", metodo: http.MethodGet,
			caminho: func(pilhaFormatacaoHTTP) string {
				return "/v1/documentos/isto-nao-e-um-uuid/formatado?ruleset_id=" + uuid.New().String()
			},
			cookie: true, status: http.StatusBadRequest,
		},
		{
			nome: "formatado com id de rota nulo", metodo: http.MethodGet,
			caminho: func(pilhaFormatacaoHTTP) string {
				return "/v1/documentos/" + uuid.Nil.String() + "/formatado?ruleset_id=" + uuid.New().String()
			},
			cookie: true, status: http.StatusBadRequest,
		},
		{
			nome: "formatado sem cookie de sessao", metodo: http.MethodGet,
			caminho: func(pilha pilhaFormatacaoHTTP) string { return pilha.urlFormatado(uuid.New().String()) },
			cookie:  false, status: http.StatusNotFound,
		},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()
			pilha := novaPilhaFormatacaoHTTP(t)
			contentType := ""
			if caso.comTipo {
				contentType = "application/json"
			}
			cookie := ""
			if caso.cookie {
				cookie = pilha.sessaoID.String()
			}

			resposta := pilha.pedir(t, caso.metodo, caso.caminho(pilha), caso.corpo, contentType, cookie)

			require.Equal(t, caso.status, resposta.Code, resposta.Body.String())
			require.NotContains(t, resposta.Body.String(), "isto-nao-e-um-uuid",
				"o id recebido não pode voltar na resposta (regra 7)")
			require.Empty(t, pilha.jobs.porChave)
		})
	}
}

// TestFormatadoHTTPExigeRulesetIDNaQuery fecha a outra metade de C5: no GET o
// ruleset_id é obrigatório. Sem este caso, um manipulador que ignorasse a
// query cairia no "último concluído" que A6 proíbe.
func TestFormatadoHTTPExigeRulesetIDNaQuery(t *testing.T) {
	t.Parallel()
	for _, caso := range []struct{ nome, valor string }{
		{"ausente", ""},
		{"malformado", "nao-e-um-uuid"},
		{"nulo", uuid.Nil.String()},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()
			pilha := novaPilhaFormatacaoHTTP(t)

			resposta := pilha.pedir(t, http.MethodGet, pilha.urlFormatado(caso.valor), "", "", pilha.sessaoID.String())

			require.Equal(t, http.StatusBadRequest, resposta.Code, resposta.Body.String())
			require.Contains(t, resposta.Body.String(), "ruleset_id")
			require.NotContains(t, resposta.Body.String(), "nao-e-um-uuid",
				"o valor recebido não pode voltar na resposta (regra 7)")
		})
	}
}

// TestFormatadoHTTPCaminhoFeliz é o controle positivo do GET: com job de
// formatar CONCLUÍDO do par pedido, a resposta é 200 com url e expira_em.
func TestFormatadoHTTPCaminhoFeliz(t *testing.T) {
	t.Parallel()
	pilha := novaPilhaFormatacaoHTTP(t)
	ruleset := uuid.New()
	pilha.consultas.porDocumento[pilha.documento.ID] = []jobentity.Job{{
		ID: uuid.New(), DocumentoID: pilha.documento.ID, RulesetID: &ruleset,
		Tipo: jobentity.TipoFormatar, Status: jobentity.StatusConcluido, CriadoEm: time.Now().UTC(),
	}}

	resposta := pilha.pedir(t, http.MethodGet, pilha.urlFormatado(ruleset.String()), "", "", pilha.sessaoID.String())

	require.Equal(t, http.StatusOK, resposta.Code, resposta.Body.String())
	var corpo map[string]any
	require.NoError(t, json.Unmarshal(resposta.Body.Bytes(), &corpo))
	require.Len(t, corpo, 2, "a FormatadoResposta só tem url e expira_em: %v", corpo)
	require.Contains(t, corpo, "url")
	require.Contains(t, corpo, "expira_em")
}

// TestRoteadorRegistraAsRotasDeFormatacao cobre A14.
//
// LIMITE DO CRITÉRIO, declarado na própria spec: "alcançável pelo servidor
// montado" NÃO tem teste. O que este teste cobre é o Roteador — que as duas
// rotas estão registradas com método e caminho certos e que o manipulador
// registrado é de fato alcançado por uma requisição HTTP real. A fiação de
// cmd/api/main.go é coberta apenas por COMPILAÇÃO (V2), por nenhum teste.
func TestRoteadorRegistraAsRotasDeFormatacao(t *testing.T) {
	t.Parallel()
	pilha := novaPilhaFormatacaoHTTP(t)

	esperadas := []struct {
		metodo  rotas.Metodo
		caminho string
	}{
		{rotas.Post, caminhoFormatarEsperado},
		{rotas.Get, caminhoFormatadoEsperado},
	}
	for _, esperada := range esperadas {
		encontrada := false
		for _, rota := range pilha.rotas.Rotas("") {
			if rota.Metodo == esperada.metodo && rota.Caminho == esperada.caminho {
				encontrada = true
			}
		}
		require.Truef(t, encontrada, "%v %s não está registrado no roteador de documentos",
			esperada.metodo, esperada.caminho)
	}

	// E o manipulador registrado é alcançado de verdade pelo servidor: 202 só
	// pode vir de TratarFormatacao.
	resposta := pilha.pedir(t, http.MethodPost, pilha.urlFormatar(),
		`{"ruleset_id":"`+pilha.perfilOK.String()+`"}`, "application/json", pilha.sessaoID.String())
	require.Equal(t, http.StatusAccepted, resposta.Code, resposta.Body.String())
}
