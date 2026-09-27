package vo

import (
	"math"

	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// CentimetrosParaTwips converte centímetros em twips com arredondamento math.Round.
func CentimetrosParaTwips(centimetros float64) (int, error) {
	return converterUnidade(centimetros, 1440.0/2.54, "centimetros")
}

// PontosParaTwips converte pontos em twips com arredondamento math.Round.
func PontosParaTwips(pontos float64) (int, error) {
	return converterUnidade(pontos, 20, "pontos")
}

// PontosParaMeiosPontos converte pontos em meios-pontos com arredondamento math.Round.
func PontosParaMeiosPontos(pontos float64) (int, error) {
	return converterUnidade(pontos, 2, "pontos")
}

// EntrelinhaParaUnidades converte múltiplos de linha em unidades de 240 para
// lineRule="auto"; não se aplica a lineRule="exact" ou "atLeast".
func EntrelinhaParaUnidades(entrelinha float64) (int, error) {
	return converterUnidade(entrelinha, 240, "entrelinha")
}

func converterUnidade(valor, fator float64, campo string) (int, error) {
	if math.IsNaN(valor) || math.IsInf(valor, 0) || valor < 0 {
		return 0, errors.NovoErroValidacao(campo, "medida fora do intervalo técnico permitido")
	}
	resultado := math.Round(valor * fator)
	// Teto técnico portátil, não validação das restrições contextuais de OOXML.
	// Também rejeita overflow da multiplicação para +Inf antes da conversão.
	if resultado > math.MaxInt32 {
		return 0, errors.NovoErroValidacao(campo, "medida fora do intervalo técnico permitido")
	}
	return int(resultado), nil
}
