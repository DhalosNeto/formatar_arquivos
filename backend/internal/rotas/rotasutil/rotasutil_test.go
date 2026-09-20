package rotasutil_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/daniel-halos/formatador/internal/rotas/rotasutil"
)

func TestClassificarMapeiaCadaTipoDeErro(t *testing.T) {
	casos := []struct {
		nome           string
		erro           error
		statusEsperado int
		codigoEsperado string
	}{
		{"validação", errors.NovoErroValidacao("email", "obrigatório"), http.StatusBadRequest, rotasutil.CodigoRequisicaoInvalida},
		{"não encontrado", errors.NovoErroNaoEncontrado("documento"), http.StatusNotFound, rotasutil.CodigoNaoEncontrado},
		{"conflito", errors.NovoErroConflito("já existe"), http.StatusConflict, rotasutil.CodigoConflito},
		{"não autorizado", errors.NovoErroNaoAutorizado("token ausente"), http.StatusUnauthorized, rotasutil.CodigoNaoAutorizado},
		{"proibido", errors.NovoErroProibido("sem permissão"), http.StatusForbidden, rotasutil.CodigoAcessoRestrito},
		{"desconhecido", errors.Novo("qualquer falha"), http.StatusInternalServerError, rotasutil.CodigoErroInterno},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			status, codigo, _, _ := rotasutil.Classificar(caso.erro)

			if status != caso.statusEsperado {
				t.Fatalf("esperava status %d, obteve %d", caso.statusEsperado, status)
			}
			if codigo != caso.codigoEsperado {
				t.Fatalf("esperava código %q, obteve %q", caso.codigoEsperado, codigo)
			}
		})
	}
}

func TestClassificarAtravessaErroEnvolvido(t *testing.T) {
	envolvido := errors.Envolver(errors.Envolver(errors.NovoErroNaoEncontrado("job"), "serviço"), "manipulador")

	status, codigo, descricao, _ := rotasutil.Classificar(envolvido)

	if status != http.StatusNotFound {
		t.Fatalf("esperava 404, obteve %d", status)
	}
	if codigo != rotasutil.CodigoNaoEncontrado {
		t.Fatalf("esperava %q, obteve %q", rotasutil.CodigoNaoEncontrado, codigo)
	}
	if descricao != "job não encontrado" {
		t.Fatalf("descrição inesperada: %q", descricao)
	}
}

func TestClassificarListaOsCamposInvalidos(t *testing.T) {
	erro := errors.NovoErroValidacao("email", "obrigatório")
	erro.Acrescentar("senha", "curta demais")

	_, _, _, razoes := rotasutil.Classificar(erro)

	if len(razoes) != 2 {
		t.Fatalf("esperava 2 razões, obteve %v", razoes)
	}
	if razoes[0] != "email: obrigatório" {
		t.Fatalf("razão inesperada: %q", razoes[0])
	}
}

func TestErroInternoNaoVazaDetalheParaOCliente(t *testing.T) {
	segredo := "dial tcp 10.0.0.5:5432: senha=supersecreta"

	_, _, descricao, razoes := rotasutil.Classificar(errors.Envolver(errors.Novo(segredo), "ao conectar"))

	if strings.Contains(descricao, "supersecreta") || strings.Contains(descricao, "10.0.0.5") {
		t.Fatalf("descrição vazou detalhe interno: %q", descricao)
	}
	if razoes != nil {
		t.Fatalf("não esperava razões em erro interno, obteve %v", razoes)
	}
}
