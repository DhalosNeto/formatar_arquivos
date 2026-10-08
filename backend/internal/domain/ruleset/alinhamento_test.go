package ruleset

import (
	"testing"

	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidarAlinhamentoAceitaValoresContratuais(t *testing.T) {
	for _, alinhamento := range []string{"esquerda", "direita", "centralizado", "justificado"} {
		t.Run(alinhamento, func(t *testing.T) {
			require.NoError(t, ValidarAlinhamento(alinhamento))
		})
	}
}

func TestValidarAlinhamentoRejeitaValoresNaoContratuais(t *testing.T) {
	for _, alinhamento := range []string{"centro", "Justificado", " justificado", "esquerda ", "ESQUERDA", "\tesquerda", "alinhado", " direita"} {
		t.Run("inválido/"+alinhamento, func(t *testing.T) {
			err := ValidarAlinhamento(alinhamento)
			require.Error(t, err)
			var validacao *errors.ErroValidacao
			require.ErrorAs(t, err, &validacao)
			if alinhamento != "" {
				assert.NotContains(t, err.Error(), alinhamento)
			}
		})
	}
	t.Run("string vazia", func(t *testing.T) {
		err := ValidarAlinhamento("")
		require.Error(t, err)
		var validacao *errors.ErroValidacao
		require.ErrorAs(t, err, &validacao)
	})
}
