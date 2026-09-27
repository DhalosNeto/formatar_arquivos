package sessao_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/daniel-halos/formatador/internal/rotas"
	"github.com/daniel-halos/formatador/internal/rotas/sessao"
)

type requisicaoCookie struct {
	rotas.Requisicao
	valor string
	nome  string
}

func (r *requisicaoCookie) Cookie(nome string) string {
	r.nome = nome
	return r.valor
}

func TestExistente(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	for _, caso := range []struct {
		nome, valor string
		valido      bool
	}{
		{"valido", id.String(), true},
		{"ausente", "", false},
		{"invalido", "segredo-cookie", false},
		{"nulo", uuid.Nil.String(), false},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			requisicao := &requisicaoCookie{valor: caso.valor}
			dono, err := sessao.Existente(requisicao, "job")
			assert.Equal(t, "sessao_id", requisicao.nome)
			if caso.valido {
				require.NoError(t, err)
				esperado, err := vo.NovoDonoSessao(id)
				require.NoError(t, err)
				assert.True(t, dono.PodeAcessar(esperado))
				return
			}
			var ausente *errors.ErroNaoEncontrado
			require.ErrorAs(t, err, &ausente)
			assert.Equal(t, "job não encontrado", err.Error())
			assert.True(t, dono.Vazio())
		})
	}
}
