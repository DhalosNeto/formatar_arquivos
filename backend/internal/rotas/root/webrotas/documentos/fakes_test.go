package documentos

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/daniel-halos/formatador/internal/rotas"
)

// requisicaoFake implementa rotas.Requisicao para os testes deste pacote.
// Cookie ausente é sempre string vazia, como o contrato exige.
type requisicaoFake struct {
	metodo      string
	caminho     string
	parametros  map[string]string
	consulta    map[string]string
	cookie      string
	arquivo     *multipart.FileHeader
	erroArquivo error
}

var _ rotas.Requisicao = (*requisicaoFake)(nil)

func (r *requisicaoFake) Vincular(any) error   { return nil }
func (r *requisicaoFake) Corpo() io.ReadCloser { return io.NopCloser(strings.NewReader("")) }
func (r *requisicaoFake) Metodo() string       { return r.metodo }
func (r *requisicaoFake) Caminho() string      { return r.caminho }
func (r *requisicaoFake) Parametro(nome string) string {
	return r.parametros[nome]
}
func (r *requisicaoFake) ParametroConsulta(nome string) string { return r.consulta[nome] }
func (r *requisicaoFake) ParametrosConsulta(string) []string   { return nil }
func (r *requisicaoFake) Cabecalho(string) string              { return "" }
func (r *requisicaoFake) ValorFormulario(string) string        { return "" }
func (r *requisicaoFake) ArquivoFormulario(string) (*multipart.FileHeader, error) {
	if r.erroArquivo != nil {
		return nil, r.erroArquivo
	}
	return r.arquivo, nil
}
func (r *requisicaoFake) IPCliente() string         { return "127.0.0.1" }
func (r *requisicaoFake) Contexto() context.Context { return context.Background() }
func (r *requisicaoFake) Cookie(string) string      { return r.cookie }

// respostaFake implementa rotas.Resposta para os testes deste pacote,
// registrando todo cookie definido para o teste inspecionar depois.
type respostaFake struct {
	status    int
	corpo     any
	codigo    string
	descricao string
	razoes    []string
	cookies   []*http.Cookie
}

var _ rotas.Resposta = (*respostaFake)(nil)

func (r *respostaFake) SemConteudo() error { r.status = http.StatusNoContent; return nil }
func (r *respostaFake) Ok(corpo any) error { r.status, r.corpo = http.StatusOK, corpo; return nil }
func (r *respostaFake) Criado(corpo any) error {
	r.status, r.corpo = http.StatusCreated, corpo
	return nil
}
func (r *respostaFake) Aceito(corpo any) error {
	r.status, r.corpo = http.StatusAccepted, corpo
	return nil
}
func (r *respostaFake) Erro(status int, codigo, descricao string, razoes []string) error {
	r.status, r.codigo, r.descricao, r.razoes = status, codigo, descricao, razoes
	return nil
}
func (r *respostaFake) DefinirCabecalho(string, string)   {}
func (r *respostaFake) Status() int                       { return r.status }
func (r *respostaFake) Escritor() http.ResponseWriter     { return nil }
func (r *respostaFake) DefinirCookie(cookie *http.Cookie) { r.cookies = append(r.cookies, cookie) }

func exigirSemErroDocumentos(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
}

// arquivoFormularioDeTeste monta um *multipart.FileHeader real, com conteúdo
// legível por Open(), a partir de um upload multipart genuíno codificado e
// decodificado de verdade — só assim o fake exercita o mesmo caminho que o
// adaptador Echo usa em produção, em vez de um struct forjado à mão que
// quebraria no primeiro Open().
func arquivoFormularioDeTeste(t *testing.T, campo, nomeArquivo string, conteudo []byte) *multipart.FileHeader {
	t.Helper()
	var buf bytes.Buffer
	escritor := multipart.NewWriter(&buf)
	parte, err := escritor.CreateFormFile(campo, nomeArquivo)
	exigirSemErroDocumentos(t, err)
	_, err = parte.Write(conteudo)
	exigirSemErroDocumentos(t, err)
	exigirSemErroDocumentos(t, escritor.Close())

	leitor := multipart.NewReader(&buf, escritor.Boundary())
	formulario, err := leitor.ReadForm(int64(len(conteudo)) + 1<<20)
	exigirSemErroDocumentos(t, err)
	t.Cleanup(func() { _ = formulario.RemoveAll() })

	arquivos := formulario.File[campo]
	if len(arquivos) != 1 {
		t.Fatalf("esperava 1 arquivo no campo %q, obteve %d", campo, len(arquivos))
	}
	return arquivos[0]
}
