package rotas_test

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/daniel-halos/formatador/internal/rotas"
)

// TestLimitarCorpoDeixaPassarCorpoDentroDoLimite garante que LimitarCorpo não
// interfere em requisições que já respeitam o teto configurado.
func TestLimitarCorpoDeixaPassarCorpoDentroDoLimite(t *testing.T) {
	t.Parallel()
	servidor := echo.New()
	raiz := rotas.NovoRoteador()
	var lido []byte
	var erroLeitura error
	raiz.Adicionar(rotas.Post, "/upload", func(_ context.Context, requisicao rotas.Requisicao, resposta rotas.Resposta) error {
		lido, erroLeitura = io.ReadAll(requisicao.Corpo())
		return resposta.SemConteudo()
	}, rotas.LimitarCorpo(16))
	rotas.AplicarEmEcho(servidor, raiz, "")

	requisicaoHTTP := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/upload", strings.NewReader("ate16bytesok!!"))
	gravador := httptest.NewRecorder()
	servidor.ServeHTTP(gravador, requisicaoHTTP)

	if erroLeitura != nil {
		t.Fatalf("não esperava erro de leitura, obteve %v", erroLeitura)
	}
	if string(lido) != "ate16bytesok!!" {
		t.Fatalf("corpo lido divergente: %q", lido)
	}
	if gravador.Code != http.StatusNoContent {
		t.Fatalf("esperava 204, obteve %d", gravador.Code)
	}
}

// TestLimitarCorpoInterrompeLeituraAcimaDoLimite prova que o corte acontece
// ANTES do parser materializar o corpo inteiro: o handler que tenta ler além
// do teto recebe um erro de leitura, em vez de um corpo silenciosamente
// truncado ou de um upload gigante aceito sem limite algum.
func TestLimitarCorpoInterrompeLeituraAcimaDoLimite(t *testing.T) {
	t.Parallel()
	servidor := echo.New()
	raiz := rotas.NovoRoteador()
	var erroLeitura error
	raiz.Adicionar(rotas.Post, "/upload", func(_ context.Context, requisicao rotas.Requisicao, resposta rotas.Resposta) error {
		_, erroLeitura = io.ReadAll(requisicao.Corpo())
		return resposta.SemConteudo()
	}, rotas.LimitarCorpo(8))
	rotas.AplicarEmEcho(servidor, raiz, "")

	grande := bytes.Repeat([]byte("x"), 1<<20) // 1 MiB, bem acima do limite de 8 bytes
	requisicaoHTTP := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/upload", bytes.NewReader(grande))
	gravador := httptest.NewRecorder()
	servidor.ServeHTTP(gravador, requisicaoHTTP)

	if erroLeitura == nil {
		t.Fatal("esperava erro de leitura ao estourar o limite de corpo")
	}
}

// TestLimitarCorpoENoOpForaDoAdaptadorEcho garante que o middleware não tenta
// mutar nada quando a Requisicao concreta não é *requisicaoEcho: como o
// contrato é dele o único jeito de testar isso é chamando o middleware
// diretamente, sem servidor HTTP algum.
func TestLimitarCorpoENoOpForaDoAdaptadorEcho(t *testing.T) {
	t.Parallel()
	chamado := false
	manipulador := rotas.LimitarCorpo(1)(func(context.Context, rotas.Requisicao, rotas.Resposta) error {
		chamado = true
		return nil
	})

	err := manipulador(context.Background(), requisicaoNaoEcho{}, respostaNaoEcho{})
	if err != nil {
		t.Fatalf("não esperava erro, obteve %v", err)
	}
	if !chamado {
		t.Fatal("esperava que o próximo manipulador fosse chamado (no-op fora do adaptador Echo)")
	}
}

// requisicaoNaoEcho e respostaNaoEcho são implementações mínimas de
// rotas.Requisicao/rotas.Resposta que NÃO são o adaptador Echo, para provar o
// comportamento de no-op de LimitarCorpo fora dele.
type requisicaoNaoEcho struct{}

func (requisicaoNaoEcho) Vincular(any) error                 { return nil }
func (requisicaoNaoEcho) Corpo() io.ReadCloser               { return io.NopCloser(strings.NewReader("")) }
func (requisicaoNaoEcho) Metodo() string                     { return http.MethodPost }
func (requisicaoNaoEcho) Caminho() string                    { return "/fake" }
func (requisicaoNaoEcho) Parametro(string) string            { return "" }
func (requisicaoNaoEcho) ParametroConsulta(string) string    { return "" }
func (requisicaoNaoEcho) ParametrosConsulta(string) []string { return nil }
func (requisicaoNaoEcho) Cabecalho(string) string            { return "" }
func (requisicaoNaoEcho) ValorFormulario(string) string      { return "" }
func (requisicaoNaoEcho) ArquivoFormulario(string) (*multipart.FileHeader, error) {
	return nil, nil
}
func (requisicaoNaoEcho) IPCliente() string         { return "127.0.0.1" }
func (requisicaoNaoEcho) Contexto() context.Context { return context.Background() }
func (requisicaoNaoEcho) Cookie(string) string      { return "" }

type respostaNaoEcho struct{}

func (respostaNaoEcho) SemConteudo() error                       { return nil }
func (respostaNaoEcho) Ok(any) error                             { return nil }
func (respostaNaoEcho) Criado(any) error                         { return nil }
func (respostaNaoEcho) Aceito(any) error                         { return nil }
func (respostaNaoEcho) Erro(int, string, string, []string) error { return nil }
func (respostaNaoEcho) DefinirCabecalho(string, string)          {}
func (respostaNaoEcho) Status() int                              { return 0 }
func (respostaNaoEcho) Escritor() http.ResponseWriter            { return nil }
func (respostaNaoEcho) DefinirCookie(*http.Cookie)               {}

var _ rotas.Requisicao = requisicaoNaoEcho{}
var _ rotas.Resposta = respostaNaoEcho{}
