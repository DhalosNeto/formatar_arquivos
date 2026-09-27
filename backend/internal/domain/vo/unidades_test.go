package vo

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-halos/formatador/internal/infra/errors"
)

func TestConversoesUnidades(t *testing.T) {
	t.Parallel()
	conversoes := []struct {
		nome      string
		converter func(float64) (int, error)
		fator     float64
		campo     string
		entrada   float64
		esperado  int
	}{
		{"centimetros_para_twips", CentimetrosParaTwips, 1440.0 / 2.54, "centimetros", 2.54, 1440},
		{"pontos_para_twips", PontosParaTwips, 20, "pontos", 12, 240},
		{"pontos_para_meios_pontos", PontosParaMeiosPontos, 2, "pontos", 12, 24},
		{"entrelinha_para_unidades", EntrelinhaParaUnidades, 240, "entrelinha", 1.5, 360},
	}
	for _, conversao := range conversoes {
		t.Run(conversao.nome, func(t *testing.T) {
			t.Parallel()
			fronteira := (float64(math.MaxInt32) + 0.5) / conversao.fator
			casos := []struct {
				nome     string
				entrada  float64
				esperado int
				erro     bool
			}{
				{"fator_conhecido", conversao.entrada, conversao.esperado, false},
				{"unidade", 1, int(math.Round(conversao.fator)), false},
				{"empate_afasta_de_zero", 0.5 / conversao.fator, 1, false},
				{"abaixo_do_empate", 0.49 / conversao.fator, 0, false},
				{"acima_do_empate", 0.51 / conversao.fator, 1, false},
				{"zero", 0, 0, false},
				{"zero_negativo", math.Copysign(0, -1), 0, false},
				{"positivo_minusculo", math.SmallestNonzeroFloat64, 0, false},
				{"negativo_minusculo", -math.SmallestNonzeroFloat64, 0, true},
				{"negativo", -1, 0, true},
				{"nao_numero", math.NaN(), 0, true},
				{"infinito_positivo", math.Inf(1), 0, true},
				{"infinito_negativo", math.Inf(-1), 0, true},
				{"overflow_multiplicacao", math.MaxFloat64, 0, true},
				{"teto_inclusivo", float64(math.MaxInt32) / conversao.fator, math.MaxInt32, false},
				{"acima_do_teto_arredonda_para_teto", (float64(math.MaxInt32) + 0.25) / conversao.fator, math.MaxInt32, false},
				{"anterior_a_fronteira", math.Nextafter(fronteira, 0), math.MaxInt32, false},
				{"empate_na_fronteira", fronteira, 0, true},
				{"posterior_a_fronteira", math.Nextafter(fronteira, math.Inf(1)), 0, true},
				{"inteiro_acima_do_teto", (float64(math.MaxInt32) + 1) / conversao.fator, 0, true},
			}
			for _, caso := range casos {
				t.Run(caso.nome, func(t *testing.T) {
					t.Parallel()
					resultado, err := conversao.converter(caso.entrada)
					assert.Equal(t, caso.esperado, resultado)
					if !caso.erro {
						require.NoError(t, err)
						return
					}
					var validacao *errors.ErroValidacao
					require.ErrorAs(t, err, &validacao)
					assert.Equal(t, "requisição inválida", validacao.Mensagem)
					assert.Equal(t, []errors.CampoInvalido{{Campo: conversao.campo, Mensagem: "medida fora do intervalo técnico permitido"}}, validacao.Campos)
					assert.Equal(t, "requisição inválida ("+conversao.campo+": medida fora do intervalo técnico permitido)", err.Error())
				})
			}
		})
	}
}

func TestConversoesUnidadesValoresConhecidos(t *testing.T) {
	t.Parallel()
	casos := []struct {
		nome      string
		converter func(float64) (int, error)
		entrada   float64
		esperado  int
	}{
		{"tres_centimetros", CentimetrosParaTwips, 3, 1701},
		{"um_centimetro", CentimetrosParaTwips, 1, 567},
		{"pontos_fracionarios", PontosParaTwips, 10.5, 210},
		{"meios_pontos_fracionarios", PontosParaMeiosPontos, 10.5, 21},
		{"entrelinha_simples", EntrelinhaParaUnidades, 1, 240},
		{"entrelinha_dupla", EntrelinhaParaUnidades, 2, 480},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()
			resultado, err := caso.converter(caso.entrada)
			require.NoError(t, err)
			assert.Equal(t, caso.esperado, resultado)
		})
	}
}
