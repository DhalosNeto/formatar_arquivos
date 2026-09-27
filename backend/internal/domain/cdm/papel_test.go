package cdm_test

import (
	"github.com/daniel-halos/formatador/internal/domain/cdm"
	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestParaPapel(t *testing.T) {
	for _, caso := range []struct {
		nome   string
		nivel  int
		valido bool
	}{
		{"titulo", 0, true}, {"paragrafo", 0, true}, {"secao", 1, true}, {"secao", 6, true},
		{"secao", 0, false}, {"secao", 7, false}, {"titulo", 1, false}, {"privado", 0, false}, {"", 0, false},
	} {
		t.Run(caso.nome+string(rune('0'+caso.nivel)), func(t *testing.T) {
			papel, err := cdm.ParaPapel(caso.nome, caso.nivel)
			if caso.valido {
				require.NoError(t, err)
				require.True(t, papel.Valido())
				require.Equal(t, caso.nome, papel.String())
				require.Equal(t, caso.nivel, papel.Nivel())
			} else {
				var validacao *errors.ErroValidacao
				require.ErrorAs(t, err, &validacao)
				require.False(t, papel.Valido())
				require.NotContains(t, err.Error(), "privado")
			}
		})
	}
}
