// Package errors centraliza a criação e o encadeamento de erros do sistema.
// Toda camada devolve erros daqui: a camada de rotas usa o tipo concreto para
// decidir o status HTTP, sem precisar inspecionar mensagens.
package errors

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
)

// ErroEnvolvido carrega um erro original acrescido do caminho onde foi
// envolvido e de mensagens de contexto.
type ErroEnvolvido struct {
	original  error
	caminho   string
	mensagens []string
}

func (e *ErroEnvolvido) Error() string {
	if len(e.mensagens) == 0 {
		return fmt.Sprintf("%s => %v", e.caminho, e.original)
	}
	return fmt.Sprintf("%s: %s => %v", e.caminho, strings.Join(e.mensagens, "; "), e.original)
}

// Unwrap permite que errors.Is e errors.As atravessem o envelope.
func (e *ErroEnvolvido) Unwrap() error { return e.original }

// Mensagens devolve as mensagens de contexto acumuladas.
func (e *ErroEnvolvido) Mensagens() []string { return e.mensagens }

// Caminho devolve a função onde o erro foi envolvido.
func (e *ErroEnvolvido) Caminho() string { return e.caminho }

// Envolver acrescenta contexto a um erro, registrando quem o envolveu.
// Devolve nil quando err é nil, para permitir `return errors.Envolver(err)` direto.
func Envolver(err error, mensagens ...string) error {
	if err == nil {
		return nil
	}
	return &ErroEnvolvido{original: err, caminho: caminhoDoChamador(2), mensagens: mensagens}
}

func caminhoDoChamador(pulos int) string {
	ponteiros := make([]uintptr, 1)
	if runtime.Callers(pulos+1, ponteiros) == 0 {
		return "desconhecido"
	}
	funcao := runtime.FuncForPC(ponteiros[0] - 1)
	if funcao == nil {
		return "desconhecido"
	}
	partes := strings.Split(funcao.Name(), "/")
	return partes[len(partes)-1]
}

// CampoInvalido descreve um único campo reprovado na validação.
type CampoInvalido struct {
	Campo    string `json:"campo"`
	Mensagem string `json:"mensagem"`
}

func (c CampoInvalido) String() string { return fmt.Sprintf("%s: %s", c.Campo, c.Mensagem) }

// ErroValidacao agrupa campos reprovados. Vira HTTP 400.
type ErroValidacao struct {
	Mensagem string
	Campos   []CampoInvalido
}

func (e *ErroValidacao) Error() string {
	if len(e.Campos) == 0 {
		return e.Mensagem
	}
	partes := make([]string, 0, len(e.Campos))
	for _, campo := range e.Campos {
		partes = append(partes, campo.String())
	}
	return fmt.Sprintf("%s (%s)", e.Mensagem, strings.Join(partes, "; "))
}

// NovoErroValidacao cria um erro de validação para um único campo.
func NovoErroValidacao(campo, mensagem string) *ErroValidacao {
	return &ErroValidacao{
		Mensagem: "requisição inválida",
		Campos:   []CampoInvalido{{Campo: campo, Mensagem: mensagem}},
	}
}

// NovoErroValidacaoCampos cria um erro de validação com vários campos.
func NovoErroValidacaoCampos(mensagem string, campos ...CampoInvalido) *ErroValidacao {
	return &ErroValidacao{Mensagem: mensagem, Campos: campos}
}

// Acrescentar adiciona um campo reprovado ao erro de validação.
func (e *ErroValidacao) Acrescentar(campo, mensagem string) {
	e.Campos = append(e.Campos, CampoInvalido{Campo: campo, Mensagem: mensagem})
}

// TemCampos informa se algum campo foi reprovado.
func (e *ErroValidacao) TemCampos() bool { return len(e.Campos) > 0 }

// ErroNaoEncontrado indica recurso inexistente. Vira HTTP 404.
type ErroNaoEncontrado struct {
	Recurso string
}

func (e *ErroNaoEncontrado) Error() string {
	if e.Recurso == "" {
		return "recurso não encontrado"
	}
	return fmt.Sprintf("%s não encontrado", e.Recurso)
}

// NovoErroNaoEncontrado cria um erro de recurso inexistente.
func NovoErroNaoEncontrado(recurso string) *ErroNaoEncontrado {
	return &ErroNaoEncontrado{Recurso: recurso}
}

// ErroConflito indica violação de invariante de unicidade ou de estado. Vira HTTP 409.
type ErroConflito struct {
	Mensagem string
}

func (e *ErroConflito) Error() string { return e.Mensagem }

// NovoErroConflito cria um erro de conflito.
func NovoErroConflito(mensagem string) *ErroConflito { return &ErroConflito{Mensagem: mensagem} }

// ErroNaoAutorizado indica ausência ou invalidez de credencial. Vira HTTP 401.
type ErroNaoAutorizado struct {
	Mensagem string
}

func (e *ErroNaoAutorizado) Error() string { return e.Mensagem }

// NovoErroNaoAutorizado cria um erro de credencial ausente ou inválida.
func NovoErroNaoAutorizado(mensagem string) *ErroNaoAutorizado {
	return &ErroNaoAutorizado{Mensagem: mensagem}
}

// ErroProibido indica credencial válida sem permissão para a operação. Vira HTTP 403.
type ErroProibido struct {
	Mensagem string
}

func (e *ErroProibido) Error() string { return e.Mensagem }

// NovoErroProibido cria um erro de permissão insuficiente.
func NovoErroProibido(mensagem string) *ErroProibido { return &ErroProibido{Mensagem: mensagem} }

// ErroArgumentoNulo indica parâmetro obrigatório ausente numa chamada interna.
type ErroArgumentoNulo struct {
	Argumento string
}

func (e *ErroArgumentoNulo) Error() string {
	return fmt.Sprintf("argumento obrigatório não informado: %s", e.Argumento)
}

// NovoErroArgumentoNulo cria um erro de argumento obrigatório ausente.
func NovoErroArgumentoNulo(argumento string) *ErroArgumentoNulo {
	return &ErroArgumentoNulo{Argumento: argumento}
}

// ErroAplicacao indica falha interna inesperada. Vira HTTP 500.
type ErroAplicacao struct {
	Mensagem string
}

func (e *ErroAplicacao) Error() string { return e.Mensagem }

// NovoErroAplicacao cria um erro interno da aplicação.
func NovoErroAplicacao(mensagem string) *ErroAplicacao { return &ErroAplicacao{Mensagem: mensagem} }

// Como reexporta errors.As, para que as demais camadas importem só este pacote.
func Como(err error, alvo any) bool { return errors.As(err, alvo) }

// E reexporta errors.Is, para que as demais camadas importem só este pacote.
func E(err error, alvo error) bool { return errors.Is(err, alvo) }

// Novo reexporta errors.New.
func Novo(texto string) error { return errors.New(texto) }
