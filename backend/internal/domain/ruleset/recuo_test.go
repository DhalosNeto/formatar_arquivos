package ruleset

import (
	"math"
	"testing"

	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/stretchr/testify/require"
)

func TestValidarRecuoCMAceitaValoresValidos(t *testing.T) {
	for _, caso := range []struct {
		nome string
		cm   float64
	}{{"zero", 0}, {"medida positiva", 1.25}, {"positivo arredondado para zero", 0.0001}} {
		t.Run(caso.nome, func(t *testing.T) { require.NoError(t, ValidarRecuoCM(caso.cm)) })
	}
}

func TestValidarRecuCMRejeitaMedidasInvalidas(t *testing.T) {
	for _, caso := range []struct {
		nome string
		cm   float64
	}{{"negativo", -0.01}, {"NaN", math.NaN()}, {"infinito positivo", math.Inf(1)}, {"infinito negativo", math.Inf(-1)}, {"overflow pós-conversão", 1e100}} {
		t.Run(caso.nome, func(t *testing.T) {
			err := ValidarRecuoCM(caso.cm)
			require.Error(t, err)
			var validacao *errors.ErroValidacao
			require.ErrorAs(t, err, &validacao)
		})
	}
}
