package postgres

import (
	"context"
	"testing"

	"github.com/daniel-halos/formatador/internal/domain/ruleset"
	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSemearValidaLoteAntesDeAcessarPool(t *testing.T) {
	valida := ruleset.Definicao{Slug: "perfil", Versao: 1, Nome: "Sintético", Fonte: "https://example.org", Pagina: ruleset.Pagina{LarguraCM: 20, AlturaCM: 28}, Corpo: ruleset.Corpo{Fonte: "Sintética", TamanhoPT: 11, Entrelinha: 1.25, Alinhamento: "justificado"}}
	require.NoError(t, valida.Validar())
	casos := []struct {
		nome    string
		alterar func(*ruleset.Definicao)
	}{
		{"entidade_vazia", func(d *ruleset.Definicao) { *d = ruleset.Definicao{} }},
		{"slug_vazio", func(d *ruleset.Definicao) { d.Slug = "" }},
		{"versao_zero", func(d *ruleset.Definicao) { d.Versao = 0 }},
		{"nome_vazio", func(d *ruleset.Definicao) { d.Nome = "" }},
		{"fonte_vazia", func(d *ruleset.Definicao) { d.Fonte = "" }},
		{"pagina_vazia", func(d *ruleset.Definicao) { d.Pagina = ruleset.Pagina{} }},
		{"corpo_vazio", func(d *ruleset.Definicao) { d.Corpo = ruleset.Corpo{} }},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			invalida := valida
			caso.alterar(&invalida)
			require.Error(t, invalida.Validar())
			repo := &RepositorioRuleset{pool: nil}
			var err error
			require.NotPanics(t, func() { err = repo.Semear(context.Background(), []ruleset.Definicao{valida, invalida}) })
			var validacao *errors.ErroValidacao
			assert.ErrorAs(t, err, &validacao)
		})
	}
	t.Run("acima_de_4096", func(t *testing.T) {
		lote := make([]ruleset.Definicao, 4097)
		for i := range lote {
			lote[i] = valida
		}
		var err error
		require.NotPanics(t, func() { err = NovoRepositorioRuleset(nil).Semear(context.Background(), lote) })
		var validacao *errors.ErroValidacao
		assert.ErrorAs(t, err, &validacao)
	})
}
