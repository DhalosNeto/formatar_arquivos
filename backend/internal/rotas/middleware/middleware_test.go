package middleware_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/daniel-halos/formatador/internal/infra/log"
	"github.com/daniel-halos/formatador/internal/infra/telemetry"
	"github.com/daniel-halos/formatador/internal/rotas"
	"github.com/daniel-halos/formatador/internal/rotas/middleware"
)

func TestIdentificarRequisicaoGeraIDQuandoClienteNaoEnvia(t *testing.T) {
	var idVisto string
	requisicao := &requisicaoFalsa{caminho: "/v1/saude", metodo: http.MethodGet}
	resposta := &respostaFalsa{}

	manipulador := middleware.IdentificarRequisicao()(func(ctx context.Context, _ rotas.Requisicao, _ rotas.Resposta) error {
		idVisto = log.IDRequisicao(ctx)
		return nil
	})

	if err := manipulador(context.Background(), requisicao, resposta); err != nil {
		t.Fatalf("não esperava erro, obteve %v", err)
	}

	if idVisto == "" {
		t.Fatal("esperava um identificador gerado no contexto")
	}
	if resposta.cabecalhos[middleware.CabecalhoIDRequisicao] != idVisto {
		t.Fatalf("esperava o mesmo id no cabeçalho, obteve %q", resposta.cabecalhos[middleware.CabecalhoIDRequisicao])
	}
}

func TestIdentificarRequisicaoReaproveitaIDDoCliente(t *testing.T) {
	var idVisto string
	requisicao := &requisicaoFalsa{
		metodo:     http.MethodGet,
		cabecalhos: map[string]string{middleware.CabecalhoIDRequisicao: "req-do-cliente"},
	}

	manipulador := middleware.IdentificarRequisicao()(func(ctx context.Context, _ rotas.Requisicao, _ rotas.Resposta) error {
		idVisto = log.IDRequisicao(ctx)
		return nil
	})

	if err := manipulador(context.Background(), requisicao, &respostaFalsa{}); err != nil {
		t.Fatalf("não esperava erro, obteve %v", err)
	}

	if idVisto != "req-do-cliente" {
		t.Fatalf("esperava reaproveitar o id do cliente, obteve %q", idVisto)
	}
}

func TestRecuperarDePanicoDevolveErroInternoSemVazarDetalhe(t *testing.T) {
	var saida bytes.Buffer
	slog.SetDefault(log.Novo("info", &saida))

	resposta := &respostaFalsa{}
	manipulador := middleware.RecuperarDePanico()(func(context.Context, rotas.Requisicao, rotas.Resposta) error {
		panic("segredo=abc123 estourou aqui")
	})

	err := manipulador(context.Background(), &requisicaoFalsa{caminho: "/v1/boom", metodo: http.MethodPost}, resposta)
	if err != nil {
		t.Fatalf("esperava o panic convertido em resposta, obteve erro %v", err)
	}

	if resposta.status != http.StatusInternalServerError {
		t.Fatalf("esperava status 500, obteve %d", resposta.status)
	}
	if strings.Contains(resposta.descricao, "abc123") {
		t.Fatalf("descrição vazou o conteúdo do panic: %q", resposta.descricao)
	}
}

func TestRegistrarAcessoNaoLogaCorpoDaRequisicao(t *testing.T) {
	var saida bytes.Buffer
	slog.SetDefault(log.Novo("info", &saida))

	conteudoSigiloso := "resumo confidencial do artigo do usuário"
	requisicao := &requisicaoFalsa{caminho: "/v1/documentos", metodo: http.MethodPost, corpo: conteudoSigiloso}

	manipulador := middleware.RegistrarAcesso()(func(context.Context, rotas.Requisicao, rotas.Resposta) error { return nil })
	if err := manipulador(context.Background(), requisicao, &respostaFalsa{status: http.StatusOK}); err != nil {
		t.Fatalf("não esperava erro, obteve %v", err)
	}

	if strings.Contains(saida.String(), "confidencial") {
		t.Fatalf("o log vazou conteúdo do usuário: %s", saida.String())
	}

	var registro map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(saida.Bytes()), &registro); err != nil {
		t.Fatalf("log não é JSON válido: %v", err)
	}
	if registro["rota"] != "/v1/documentos" || registro["metodo"] != http.MethodPost {
		t.Fatalf("esperava rota e método no log, obteve %v", registro)
	}
}

func TestMedirContabilizaRequisicao(t *testing.T) {
	registrador := prometheus.NewRegistry()
	metricas := telemetry.NovasMetricas(registrador)

	requisicao := &requisicaoFalsa{caminho: "/v1/documentos/:id", metodo: http.MethodGet}
	manipulador := middleware.Medir(metricas)(func(context.Context, rotas.Requisicao, rotas.Resposta) error { return nil })

	if err := manipulador(context.Background(), requisicao, &respostaFalsa{status: http.StatusOK}); err != nil {
		t.Fatalf("não esperava erro, obteve %v", err)
	}

	esperado := `
# HELP http_requisicoes_total Total de requisições HTTP atendidas.
# TYPE http_requisicoes_total counter
http_requisicoes_total{metodo="GET",rota="/v1/documentos/:id",status="200"} 1
`
	if err := testutil.CollectAndCompare(metricas.RequisicoesHTTP, strings.NewReader(esperado), "http_requisicoes_total"); err != nil {
		t.Fatalf("métrica inesperada: %v", err)
	}
}

// requisicaoFalsa implementa rotas.Requisicao para os testes de middleware.
type requisicaoFalsa struct {
	metodo     string
	caminho    string
	corpo      string
	cabecalhos map[string]string
}

func (r *requisicaoFalsa) Vincular(any) error                 { return nil }
func (r *requisicaoFalsa) Corpo() io.ReadCloser               { return io.NopCloser(strings.NewReader(r.corpo)) }
func (r *requisicaoFalsa) Metodo() string                     { return r.metodo }
func (r *requisicaoFalsa) Caminho() string                    { return r.caminho }
func (r *requisicaoFalsa) Parametro(string) string            { return "" }
func (r *requisicaoFalsa) ParametroConsulta(string) string    { return "" }
func (r *requisicaoFalsa) ParametrosConsulta(string) []string { return nil }
func (r *requisicaoFalsa) Cabecalho(nome string) string       { return r.cabecalhos[nome] }
func (r *requisicaoFalsa) ValorFormulario(string) string      { return "" }
func (r *requisicaoFalsa) ArquivoFormulario(string) (*multipart.FileHeader, error) {
	return nil, nil
}
func (r *requisicaoFalsa) IPCliente() string         { return "127.0.0.1" }
func (r *requisicaoFalsa) Contexto() context.Context { return context.Background() }
func (r *requisicaoFalsa) Cookie(string) string      { return "" }

// respostaFalsa implementa rotas.Resposta para os testes de middleware.
type respostaFalsa struct {
	status     int
	codigo     string
	descricao  string
	razoes     []string
	corpo      any
	cabecalhos map[string]string
}

func (r *respostaFalsa) SemConteudo() error { r.status = http.StatusNoContent; return nil }
func (r *respostaFalsa) Ok(corpo any) error { r.status, r.corpo = http.StatusOK, corpo; return nil }
func (r *respostaFalsa) Criado(corpo any) error {
	r.status, r.corpo = http.StatusCreated, corpo
	return nil
}
func (r *respostaFalsa) Aceito(corpo any) error {
	r.status, r.corpo = http.StatusAccepted, corpo
	return nil
}
func (r *respostaFalsa) Erro(status int, codigo, descricao string, razoes []string) error {
	r.status, r.codigo, r.descricao, r.razoes = status, codigo, descricao, razoes
	return nil
}
func (r *respostaFalsa) DefinirCabecalho(nome, valor string) {
	if r.cabecalhos == nil {
		r.cabecalhos = map[string]string{}
	}
	r.cabecalhos[nome] = valor
}
func (r *respostaFalsa) Status() int                   { return r.status }
func (r *respostaFalsa) Escritor() http.ResponseWriter { return nil }
func (r *respostaFalsa) DefinirCookie(*http.Cookie)    {}
