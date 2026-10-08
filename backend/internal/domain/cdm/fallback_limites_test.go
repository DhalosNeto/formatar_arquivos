package cdm

import (
	"context"
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFallbackLimiteConsultaExatoNaoChamaClassificador(t *testing.T) {
	fake := &classificadorEstruturaFake{}
	fallback, err := NovoFallback(fake, PoliticaConfianca{LimiteConsulta: .5, LimiteConfirmacao: .6, LimiteAutomatico: .9})
	require.NoError(t, err)
	bloco := Bloco{Papel: Paragrafo, TextoResumo: "resumo intacto", Confianca: .5, Origem: OrigemHeuristica, RefXML: 41}

	indice, err := fallback.Aplicar(context.Background(), []Bloco{bloco})
	require.NoError(t, err)
	require.Zero(t, fake.chamadas)
	require.Equal(t, []Bloco{bloco}, indice.Blocos)
	require.Empty(t, indice.Revisoes)
}

func TestFallbackLimitesDeDecisaoExatos(t *testing.T) {
	for _, caso := range []struct {
		nome      string
		confianca float64
		revisao   bool
		papel     Papel
		origem    Origem
	}{
		{nome: "confirmacao exata sugere", confianca: .6, revisao: true, papel: Paragrafo, origem: OrigemHeuristica},
		{nome: "automatico exato aplica", confianca: .9, papel: Titulo, origem: OrigemLLM},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			bloco := Bloco{Papel: Paragrafo, TextoResumo: "conteudo mantido", Confianca: .2, Origem: OrigemHeuristica, RefXML: 52}
			fake := &classificadorEstruturaFake{resposta: []JulgamentoEstrutura{{RefXML: bloco.RefXML, Papel: Titulo, Confianca: caso.confianca}}}
			fallback, err := NovoFallback(fake, PoliticaConfianca{LimiteConsulta: .5, LimiteConfirmacao: .6, LimiteAutomatico: .9})
			require.NoError(t, err)
			indice, err := fallback.Aplicar(context.Background(), []Bloco{bloco})
			require.NoError(t, err)
			require.Equal(t, 1, fake.chamadas)
			require.Len(t, indice.Blocos, 1)
			require.Equal(t, caso.papel, indice.Blocos[0].Papel)
			require.Equal(t, caso.origem, indice.Blocos[0].Origem)
			require.Equal(t, bloco.RefXML, indice.Blocos[0].RefXML)
			require.Equal(t, bloco.TextoResumo, indice.Blocos[0].TextoResumo)
			require.Equal(t, caso.revisao, len(indice.Revisoes) == 1)
			if caso.revisao {
				require.Equal(t, bloco.RefXML, indice.Revisoes[0].RefXML)
				require.Equal(t, acaoConfirmar, indice.Revisoes[0].Acao)
				require.NotNil(t, indice.Revisoes[0].PapelSugerido)
				require.Equal(t, Titulo, *indice.Revisoes[0].PapelSugerido)
				require.Equal(t, caso.confianca, indice.Revisoes[0].Confianca)
			}
		})
	}
}

func TestFallbackIgnoraOrigemUsuarioELLMBaixa(t *testing.T) {
	blocos := []Bloco{
		{Papel: Titulo, TextoResumo: "decisao humana", Confianca: .1, Origem: OrigemUsuario, RefXML: 60},
		{Papel: Resumo, TextoResumo: "decisao previa", Confianca: .1, Origem: OrigemLLM, RefXML: 61},
	}
	fake := &classificadorEstruturaFake{}
	fallback, err := NovoFallback(fake, PoliticaConfianca{LimiteConsulta: .5, LimiteConfirmacao: .6, LimiteAutomatico: .9})
	require.NoError(t, err)
	indice, err := fallback.Aplicar(context.Background(), blocos)
	require.NoError(t, err)
	require.Zero(t, fake.chamadas)
	require.Equal(t, blocos, indice.Blocos)
	require.Empty(t, indice.Revisoes)
}

func TestFallbackVazioSemChamadaECanceladoComVazioFalha(t *testing.T) {
	fake := &classificadorEstruturaFake{}
	fallback, err := NovoFallback(fake, PoliticaConfianca{LimiteConsulta: .5, LimiteConfirmacao: .6, LimiteAutomatico: .9})
	require.NoError(t, err)
	indice, err := fallback.Aplicar(context.Background(), nil)
	require.NoError(t, err)
	require.Empty(t, indice.Blocos)
	require.Empty(t, indice.Revisoes)
	require.Zero(t, fake.chamadas)
	ctx, cancelar := context.WithCancel(context.Background())
	cancelar()
	_, err = fallback.Aplicar(ctx, nil)
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, fake.chamadas)
}

func TestFallbackCorrelacionaRespostasForaDeOrdemPorReferencia(t *testing.T) {
	blocos := []Bloco{
		{Papel: Paragrafo, TextoResumo: "primeiro", Confianca: .2, Origem: OrigemHeuristica, RefXML: 70},
		{Papel: Paragrafo, TextoResumo: "segundo", Confianca: .2, Origem: OrigemEstiloDocx, RefXML: 71},
	}
	fake := &classificadorEstruturaFake{resposta: []JulgamentoEstrutura{
		{RefXML: 71, Papel: Resumo, Confianca: .9},
		{RefXML: 70, Papel: Titulo, Confianca: .9},
	}}
	fallback, err := NovoFallback(fake, PoliticaConfianca{LimiteConsulta: .5, LimiteConfirmacao: .6, LimiteAutomatico: .9})
	require.NoError(t, err)
	indice, err := fallback.Aplicar(context.Background(), blocos)
	require.NoError(t, err)
	require.Equal(t, 1, fake.chamadas)
	for i, esperado := range []struct {
		papel Papel
		texto string
		ref   int
	}{{Titulo, "primeiro", 70}, {Resumo, "segundo", 71}} {
		require.Equal(t, esperado.papel, indice.Blocos[i].Papel)
		require.Equal(t, esperado.texto, indice.Blocos[i].TextoResumo)
		require.Equal(t, OrigemLLM, indice.Blocos[i].Origem)
		require.Equal(t, esperado.ref, indice.Blocos[i].RefXML)
	}
	require.Empty(t, indice.Revisoes)
}

func TestFallbackSucessoAplica32EDestina33oParaRevisao(t *testing.T) {
	blocos := make([]Bloco, 33)
	for i := range blocos {
		blocos[i] = Bloco{Papel: Paragrafo, TextoResumo: "texto", Confianca: .1, Origem: OrigemHeuristica, RefXML: 100 + i}
	}
	fake := &classificadorEstruturaFake{classificar: func(entrada []Bloco) []JulgamentoEstrutura {
		respostas := make([]JulgamentoEstrutura, len(entrada))
		for i, bloco := range entrada {
			respostas[i] = JulgamentoEstrutura{RefXML: bloco.RefXML, Papel: Titulo, Confianca: .99}
		}
		return respostas
	}}
	fallback, err := NovoFallback(fake, PoliticaConfianca{LimiteConsulta: .5, LimiteConfirmacao: .6, LimiteAutomatico: .9})
	require.NoError(t, err)
	indice, err := fallback.Aplicar(context.Background(), blocos)
	require.NoError(t, err)
	require.Equal(t, 1, fake.chamadas)
	require.Len(t, fake.entrada, 32)
	require.Len(t, indice.Blocos, 33)
	for i := 0; i < 32; i++ {
		require.Equal(t, Titulo, indice.Blocos[i].Papel)
		require.Equal(t, OrigemLLM, indice.Blocos[i].Origem)
		require.Equal(t, blocos[i].TextoResumo, indice.Blocos[i].TextoResumo)
		require.Equal(t, blocos[i].RefXML, indice.Blocos[i].RefXML)
	}
	require.Equal(t, blocos[32], indice.Blocos[32])
	require.Equal(t, []RevisaoEstrutura{{RefXML: blocos[32].RefXML, Acao: acaoRevisar}}, indice.Revisoes)
}

func TestNovoFallbackRejeitaDependenciaELimitesInvalidos(t *testing.T) {
	valida := PoliticaConfianca{LimiteConsulta: .5, LimiteConfirmacao: .6, LimiteAutomatico: .9}
	_, err := NovoFallback(nil, valida)
	require.Error(t, err)
	for _, campo := range []string{"consulta", "confirmacao", "automatico"} {
		for _, valor := range []float64{-0.01, 1.01, math.NaN(), math.Inf(1), math.Inf(-1)} {
			t.Run(fmt.Sprintf("%s=%v", campo, valor), func(t *testing.T) {
				politica := valida
				switch campo {
				case "consulta":
					politica.LimiteConsulta = valor
				case "confirmacao":
					politica.LimiteConfirmacao = valor
				case "automatico":
					politica.LimiteAutomatico = valor
				}
				_, err := NovoFallback(&classificadorEstruturaFake{}, politica)
				require.Error(t, err)
			})
		}
	}
	for _, politica := range []PoliticaConfianca{
		{LimiteConsulta: .5, LimiteConfirmacao: .9, LimiteAutomatico: .9},
		{LimiteConsulta: .95, LimiteConfirmacao: .6, LimiteAutomatico: .9},
	} {
		_, err := NovoFallback(&classificadorEstruturaFake{}, politica)
		require.Error(t, err)
	}
}
