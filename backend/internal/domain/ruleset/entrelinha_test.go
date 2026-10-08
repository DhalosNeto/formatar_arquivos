package ruleset

import (
	"math"
	"testing"

	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidarEntrelinhaAceitaMultiplosRepresentaveis(t *testing.T) {
	for _, caso := range []struct {
		nome  string
		valor float64
	}{{"simples", 1}, {"uma e meia", 1.5}, {"dupla", 2}, {"arredonda para uma unidade", 1.0 / 240}} {
		t.Run(caso.nome, func(t *testing.T) { require.NoError(t, ValidarEntrelinha(caso.valor)) })
	}
}

func TestValidarEntrelinhaRejeitaMedidasInvalidasOuQueArredondamParaZero(t *testing.T) {
	for _, caso := range []struct {
		nome  string
		valor float64
	}{{"zero", 0}, {"negativa", -1}, {"NaN", math.NaN()}, {"infinito positivo", math.Inf(1)}, {"infinito negativo", math.Inf(-1)}, {"subnormal", math.SmallestNonzeroFloat64}, {"arredonda para zero", 0.49 / 240}, {"teto após conversão", float64(math.MaxInt32)/240 + 1}} {
		t.Run(caso.nome, func(t *testing.T) {
			err := ValidarEntrelinha(caso.valor)
			require.Error(t, err)
			var validacao *errors.ErroValidacao
			require.ErrorAs(t, err, &validacao)
			require.Len(t, validacao.Campos, 1)
			assert.Equal(t, "corpo", validacao.Campos[0].Campo)
		})
	}
}

func TestCorpoValidarPreservaErroAnteriorAoValidarEntrelinha(t *testing.T) {
	corpo := Corpo{Fonte: "", TamanhoPT: 12, Entrelinha: 0, RecuoCM: 0, EspacoAntesPT: 0, EspacoDepoisPT: 0, Alinhamento: "esquerda"}
	err := corpo.validar()
	require.Error(t, err)
	var validacao *errors.ErroValidacao
	require.ErrorAs(t, err, &validacao)
	require.Len(t, validacao.Campos, 1)
	assert.Equal(t, "corpo.fonte", validacao.Campos[0].Campo)
}
