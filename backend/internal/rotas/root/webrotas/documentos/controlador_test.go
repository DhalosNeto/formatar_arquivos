package documentos

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/daniel-halos/formatador/internal/application/web/webmodel"
	"github.com/daniel-halos/formatador/internal/domain/documento/entity"
	"github.com/daniel-halos/formatador/internal/domain/vo"
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
	if cookie.Name != NomeCookieSessao {
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
