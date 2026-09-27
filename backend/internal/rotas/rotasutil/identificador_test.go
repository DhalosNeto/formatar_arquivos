package rotasutil_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/daniel-halos/formatador/internal/rotas"
	"github.com/daniel-halos/formatador/internal/rotas/rotasutil"
)

// requisicaoParametro embute a interface nula e implementa só o método usado.
type requisicaoParametro struct {
	rotas.Requisicao
	valor string
}

func (r *requisicaoParametro) Parametro(string) string { return r.valor }

func TestIDDaRotaAceitaUUIDValido(t *testing.T) {
	t.Parallel()

	esperado := uuid.New()

	id, err := rotasutil.IDDaRota(&requisicaoParametro{valor: esperado.String()}, "id")

	require.NoError(t, err)
	assert.Equal(t, esperado, id)
}

// TestIDDaRotaRecusaValorInutilizavel cobre o UUID nulo junto com as formas
// malformadas de propósito.
//
// A auditoria independente de 27/09 achou a assimetria que motivou este
// helper: o controlador de jobs recusava o nulo e os cinco handlers de
// documento não. Nenhum dos dois era explorável, mas duas validações
// diferentes para a mesma coisa deixam a resposta certa dependendo do arquivo.
func TestIDDaRotaRecusaValorInutilizavel(t *testing.T) {
	t.Parallel()

	casos := []struct{ nome, valor string }{
		{"vazio", ""},
		{"não é uuid", "isto-nao-e-um-uuid"},
		{"uuid nulo", uuid.Nil.String()},
		{"uuid truncado", "6d734a86-702d-44d3-b622"},
		{"caminho no lugar do id", "../../etc/passwd"},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			id, err := rotasutil.IDDaRota(&requisicaoParametro{valor: caso.valor}, "id")

			require.Error(t, err)
			assert.Equal(t, uuid.Nil, id)

			var validacao *errors.ErroValidacao
			require.True(t, errors.Como(err, &validacao), "esperava ErroValidacao, obteve %T", err)
			assert.Equal(t, "id", validacao.Campos[0].Campo)
		})
	}
}

// TestIDDaRotaNaoEcoaOValorRecebido trava a regra 7 num ponto fácil de
// escorregar: o id vem do cliente e é tentador devolvê-lo para "ajudar a
// depurar", o que transforma a mensagem de erro num refletor de conteúdo
// arbitrário.
func TestIDDaRotaNaoEcoaOValorRecebido(t *testing.T) {
	t.Parallel()

	const suspeito = "<script>alert(1)</script>CONTEUDO-DO-CLIENTE"

	_, err := rotasutil.IDDaRota(&requisicaoParametro{valor: suspeito}, "id")

	require.Error(t, err)
	assert.NotContains(t, err.Error(), suspeito)
	assert.NotContains(t, err.Error(), "script")
}
