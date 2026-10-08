package ruleset

import (
	"math"
	"strings"
	"testing"

	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidarFonteCorpo(t *testing.T) {
	t.Run("aceita nomes UTF-8 e caracteres escapáveis em XML", func(t *testing.T) {
		for _, fonte := range []string{"Times New Roman", "A&B <C> \"D\" 'E'", "café 😀 東京"} {
			require.NoError(t, ValidarFonteCorpo(fonte), "%q", fonte)
		}
	})
	cases := []struct {
		name  string
		value string
	}{
		{"vazia", ""}, {"somente espaços", " \t\n"}, {"controle", "Fonte\x01"},
		{"UTF-8 inválido", string([]byte{0xff})}, {"U+FFFE", "Fonte\ufffe"},
		{"U+FFFF", "Fonte\uffff"}, {"101 runes", strings.Repeat("á", 101)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidarFonteCorpo(tc.value)
			var validacao *errors.ErroValidacao
			require.ErrorAs(t, err, &validacao)
		})
	}
}

func TestValidarTamanhoCorpoPT(t *testing.T) {
	cases := []struct {
		name  string
		value float64
		valid bool
	}{
		{"doze pontos", 12, true}, {"fracionário representável", 10.25, true},
		{"zero", 0, false}, {"negativo", -0.5, false}, {"arredonda para zero", 0.1, false},
		{"NaN", math.NaN(), false}, {"infinito positivo", math.Inf(1), false},
		{"infinito negativo", math.Inf(-1), false}, {"overflow", float64(math.MaxInt32), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidarTamanhoCorpoPT(tc.value)
			if tc.valid {
				assert.NoError(t, err)
				return
			}
			var validacao *errors.ErroValidacao
			require.ErrorAs(t, err, &validacao)
		})
	}
}
