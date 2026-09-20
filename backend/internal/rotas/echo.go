package rotas

import (
	"context"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/labstack/echo/v4"
)

// CorpoErro é o formato único de erro devolvido pela API.
type CorpoErro struct {
	Codigo    string   `json:"codigo"`
	Descricao string   `json:"descricao"`
	Razoes    []string `json:"razoes,omitempty"`
}

// AplicarEmEcho registra no Echo todas as rotas resolvidas a partir do roteador
// raiz, sob o prefixo informado, com os middlewares globais aplicados a todas
// elas (o primeiro da lista é o mais externo).
func AplicarEmEcho(servidor *echo.Echo, raiz Roteador, prefixo string, middlewares ...Middleware) {
	for _, rota := range raiz.Rotas(prefixo, middlewares...) {
		servidor.Add(string(rota.Metodo), rota.Caminho, adaptar(rota.Aplicar()))
	}
}

func adaptar(manipulador Manipulador) echo.HandlerFunc {
	return func(contextoEcho echo.Context) error {
		requisicao := &requisicaoEcho{contexto: contextoEcho}
		resposta := &respostaEcho{contexto: contextoEcho}
		return manipulador(requisicao.Contexto(), requisicao, resposta)
	}
}

type requisicaoEcho struct {
	contexto echo.Context
}

func (r *requisicaoEcho) Vincular(destino any) error { return r.contexto.Bind(destino) }

func (r *requisicaoEcho) Corpo() io.ReadCloser { return r.contexto.Request().Body }

func (r *requisicaoEcho) Metodo() string { return r.contexto.Request().Method }

func (r *requisicaoEcho) Caminho() string { return r.contexto.Path() }

func (r *requisicaoEcho) Parametro(nome string) string { return r.contexto.Param(nome) }

func (r *requisicaoEcho) ParametroConsulta(nome string) string { return r.contexto.QueryParam(nome) }

func (r *requisicaoEcho) ParametrosConsulta(nome string) []string {
	return r.contexto.QueryParams()[nome]
}

func (r *requisicaoEcho) Cabecalho(nome string) string {
	return r.contexto.Request().Header.Get(nome)
}

func (r *requisicaoEcho) ValorFormulario(nome string) string { return r.contexto.FormValue(nome) }

func (r *requisicaoEcho) ArquivoFormulario(nome string) (*multipart.FileHeader, error) {
	return r.contexto.FormFile(nome)
}

func (r *requisicaoEcho) IPCliente() string { return r.contexto.RealIP() }

func (r *requisicaoEcho) Contexto() context.Context { return r.contexto.Request().Context() }

type respostaEcho struct {
	contexto echo.Context
}

func (r *respostaEcho) SemConteudo() error { return r.contexto.NoContent(http.StatusNoContent) }

func (r *respostaEcho) Ok(corpo any) error { return r.contexto.JSON(http.StatusOK, corpo) }

func (r *respostaEcho) Criado(corpo any) error { return r.contexto.JSON(http.StatusCreated, corpo) }

func (r *respostaEcho) Aceito(corpo any) error { return r.contexto.JSON(http.StatusAccepted, corpo) }

func (r *respostaEcho) Erro(status int, codigo, descricao string, razoes []string) error {
	return r.contexto.JSON(status, CorpoErro{Codigo: codigo, Descricao: descricao, Razoes: razoes})
}

func (r *respostaEcho) DefinirCabecalho(nome, valor string) {
	r.contexto.Response().Header().Set(nome, valor)
}

func (r *respostaEcho) Status() int { return r.contexto.Response().Status }

func (r *respostaEcho) Escritor() http.ResponseWriter { return r.contexto.Response().Writer }
