// Package rotas define o contrato HTTP do projeto, independente de framework.
// Nenhum manipulador conhece Echo: o adaptador em echo.go faz a ponte.
package rotas

import (
	"context"
	"io"
	"mime/multipart"
	"net/http"
)

// Requisicao expõe o que um manipulador pode ler da requisição HTTP.
type Requisicao interface {
	Vincular(destino any) error
	Corpo() io.ReadCloser
	Metodo() string
	Caminho() string
	Parametro(nome string) string
	ParametroConsulta(nome string) string
	ParametrosConsulta(nome string) []string
	Cabecalho(nome string) string
	ValorFormulario(nome string) string
	ArquivoFormulario(nome string) (*multipart.FileHeader, error)
	IPCliente() string
	Contexto() context.Context
}

// Resposta expõe o que um manipulador pode escrever na resposta HTTP.
type Resposta interface {
	SemConteudo() error
	Ok(corpo any) error
	Criado(corpo any) error
	Aceito(corpo any) error
	Erro(status int, codigo, descricao string, razoes []string) error
	DefinirCabecalho(nome, valor string)
	Status() int
	Escritor() http.ResponseWriter
}

// Manipulador é a assinatura de todo handler do projeto.
type Manipulador func(context.Context, Requisicao, Resposta) error

// Middleware envolve um manipulador. É propositalmente agnóstico de framework.
type Middleware func(Manipulador) Manipulador

// Metodo é um verbo HTTP.
type Metodo string

// Verbos HTTP suportados pelo roteador.
const (
	Get     Metodo = http.MethodGet
	Post    Metodo = http.MethodPost
	Put     Metodo = http.MethodPut
	Patch   Metodo = http.MethodPatch
	Delete  Metodo = http.MethodDelete
	Options Metodo = http.MethodOptions
)

// Roteador agrupa rotas e sub-roteadores sob um prefixo.
type Roteador interface {
	Adicionar(metodo Metodo, caminho string, manipulador Manipulador, middlewares ...Middleware)
	Registrar(filho Roteador, prefixo string, middlewares ...Middleware)
	Rotas(prefixo string, middlewares ...Middleware) []Rota
}

// Rota é uma rota já resolvida, com caminho completo e middlewares acumulados.
type Rota struct {
	Metodo      Metodo
	Caminho     string
	Manipulador Manipulador
	Middlewares []Middleware
}

// Aplicar devolve o manipulador da rota com todos os middlewares aplicados.
// O primeiro middleware da lista é o mais externo.
func (r Rota) Aplicar() Manipulador {
	manipulador := r.Manipulador
	for i := len(r.Middlewares) - 1; i >= 0; i-- {
		manipulador = r.Middlewares[i](manipulador)
	}
	return manipulador
}
