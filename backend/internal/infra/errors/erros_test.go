package errors_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/daniel-halos/formatador/internal/infra/errors"
)

func TestEnvolverDevolveNilParaErroNil(t *testing.T) {
	if envolvido := errors.Envolver(nil, "contexto"); envolvido != nil {
		t.Fatalf("esperava nil, obteve %v", envolvido)
	}
}

func TestEnvolverPreservaErroOriginal(t *testing.T) {
	original := errors.NovoErroNaoEncontrado("documento")

	envolvido := errors.Envolver(original, "ao obter documento")

	var alvo *errors.ErroNaoEncontrado
	if !errors.Como(envolvido, &alvo) {
		t.Fatalf("esperava recuperar *ErroNaoEncontrado de %v", envolvido)
	}
	if alvo.Recurso != "documento" {
		t.Fatalf("esperava recurso %q, obteve %q", "documento", alvo.Recurso)
	}
}

func TestEnvolverAtravessaMultiplasCamadas(t *testing.T) {
	original := errors.NovoErroConflito("slug já existe")

	envolvido := errors.Envolver(errors.Envolver(original, "camada interna"), "camada externa")

	var alvo *errors.ErroConflito
	if !errors.Como(envolvido, &alvo) {
		t.Fatalf("esperava atravessar dois envelopes, obteve %v", envolvido)
	}
}

func TestEnvolverRegistraCaminhoEMensagens(t *testing.T) {
	envolvido := errors.Envolver(errors.Novo("falha"), "ao salvar", "no postgres")

	var alvo *errors.ErroEnvolvido
	if !errors.Como(envolvido, &alvo) {
		t.Fatalf("esperava *ErroEnvolvido, obteve %T", envolvido)
	}
	if !strings.Contains(alvo.Caminho(), "TestEnvolverRegistraCaminhoEMensagens") {
		t.Fatalf("esperava o caminho apontar para o chamador, obteve %q", alvo.Caminho())
	}
	if len(alvo.Mensagens()) != 2 {
		t.Fatalf("esperava 2 mensagens, obteve %v", alvo.Mensagens())
	}
	if !strings.Contains(alvo.Error(), "ao salvar") {
		t.Fatalf("esperava a mensagem no texto do erro, obteve %q", alvo.Error())
	}
}

func TestMensagensDosTiposDeErro(t *testing.T) {
	casos := []struct {
		nome     string
		erro     error
		esperado string
	}{
		{"validacao com um campo", errors.NovoErroValidacao("email", "formato inválido"), "requisição inválida (email: formato inválido)"},
		{"nao encontrado com recurso", errors.NovoErroNaoEncontrado("documento"), "documento não encontrado"},
		{"nao encontrado sem recurso", errors.NovoErroNaoEncontrado(""), "recurso não encontrado"},
		{"conflito", errors.NovoErroConflito("slug já existe"), "slug já existe"},
		{"nao autorizado", errors.NovoErroNaoAutorizado("token ausente"), "token ausente"},
		{"proibido", errors.NovoErroProibido("sem permissão"), "sem permissão"},
		{"argumento nulo", errors.NovoErroArgumentoNulo("ctx"), "argumento obrigatório não informado: ctx"},
		{"aplicacao", errors.NovoErroAplicacao("falha interna"), "falha interna"},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			if obtido := caso.erro.Error(); obtido != caso.esperado {
				t.Fatalf("esperava %q, obteve %q", caso.esperado, obtido)
			}
		})
	}
}

func TestErroValidacaoAcumulaCampos(t *testing.T) {
	erro := errors.NovoErroValidacao("email", "obrigatório")
	erro.Acrescentar("senha", "curta demais")

	if !erro.TemCampos() || len(erro.Campos) != 2 {
		t.Fatalf("esperava 2 campos, obteve %d", len(erro.Campos))
	}
	if !strings.Contains(erro.Error(), "senha: curta demais") {
		t.Fatalf("esperava o segundo campo no texto, obteve %q", erro.Error())
	}
}

func TestEnvolverFuncionaComErroDaBibliotecaPadrao(t *testing.T) {
	original := fmt.Errorf("falha de rede")

	if !errors.E(errors.Envolver(original), original) {
		t.Fatal("esperava que errors.E reconhecesse o erro original")
	}
}
