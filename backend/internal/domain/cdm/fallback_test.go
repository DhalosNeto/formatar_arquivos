package cdm

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

type classificadorEstruturaFake struct {
	chamadas     int
	entrada      []Bloco
	resposta     []JulgamentoEstrutura
	err          error
	mutarEntrada bool
	classificar  func([]Bloco) []JulgamentoEstrutura
}

func (f *classificadorEstruturaFake) Classificar(_ context.Context, blocos []Bloco) ([]JulgamentoEstrutura, error) {
	f.chamadas++
	f.entrada = append([]Bloco(nil), blocos...)
	if f.mutarEntrada && len(blocos) > 0 {
		blocos[0].Papel = Titulo
	}
	if f.classificar != nil {
		return f.classificar(blocos), f.err
	}
	return f.resposta, f.err
}

func TestNovoFallbackValidaPolitica(t *testing.T) {
	for _, politica := range []PoliticaConfianca{
		{LimiteConsulta: math.NaN(), LimiteConfirmacao: .5, LimiteAutomatico: .9},
		{LimiteConsulta: -.1, LimiteConfirmacao: .5, LimiteAutomatico: .9},
		{LimiteConsulta: .2, LimiteConfirmacao: .9, LimiteAutomatico: .9},
		{LimiteConsulta: .95, LimiteConfirmacao: .5, LimiteAutomatico: .9},
	} {
		_, err := NovoFallback(&classificadorEstruturaFake{}, politica)
		require.Error(t, err)
	}
}

func TestFallbackAplicaJulgamentosEProtegeEntrada(t *testing.T) {
	blocos := []Bloco{
		{Papel: Paragrafo, TextoResumo: "alto", Confianca: .2, Origem: OrigemHeuristica, RefXML: 1},
		{Papel: Paragrafo, TextoResumo: "medio", Confianca: .2, Origem: OrigemEstiloDocx, RefXML: 2},
		{Papel: Paragrafo, TextoResumo: "baixo", Confianca: .2, Origem: OrigemHeuristica, RefXML: 3},
		{Papel: Paragrafo, TextoResumo: "usuario", Confianca: .2, Origem: OrigemUsuario, RefXML: 4},
		{Papel: Paragrafo, TextoResumo: "forte", Confianca: .8, Origem: OrigemHeuristica, RefXML: 5},
	}
	antes := append([]Bloco(nil), blocos...)
	fake := &classificadorEstruturaFake{mutarEntrada: true, resposta: []JulgamentoEstrutura{
		{RefXML: 1, Papel: Titulo, Confianca: .95},
		{RefXML: 2, Papel: Resumo, Confianca: .7},
		{RefXML: 3, Papel: Resumo, Confianca: .2},
	}}
	f, err := NovoFallback(fake, PoliticaConfianca{LimiteConsulta: .5, LimiteConfirmacao: .6, LimiteAutomatico: .9})
	require.NoError(t, err)
	indice, err := f.Aplicar(context.Background(), blocos)
	require.NoError(t, err)
	require.Equal(t, 1, fake.chamadas)
	require.Equal(t, antes, blocos)
	require.Equal(t, Titulo, indice.Blocos[0].Papel)
	require.Equal(t, OrigemLLM, indice.Blocos[0].Origem)
	require.Len(t, indice.Revisoes, 2)
}

func TestFallbackNoMatchERespostaMalformadaMantemClassificacoes(t *testing.T) {
	bloco := Bloco{Papel: Paragrafo, TextoResumo: "x", Confianca: .2, Origem: OrigemHeuristica, RefXML: 1}
	for _, caso := range []struct {
		resposta []JulgamentoEstrutura
		motivo   string
	}{
		{[]JulgamentoEstrutura{{RefXML: 1, SemCorrespondencia: true, Confianca: .4}}, ""},
		{[]JulgamentoEstrutura{{RefXML: 8, Papel: Titulo, Confianca: .95}}, "resposta_classificador_invalida"},
		{[]JulgamentoEstrutura{{RefXML: 1, Papel: Papel{}, Confianca: .95}}, "resposta_classificador_invalida"},
	} {
		fake := &classificadorEstruturaFake{resposta: caso.resposta}
		f, err := NovoFallback(fake, PoliticaConfianca{LimiteConsulta: .5, LimiteConfirmacao: .6, LimiteAutomatico: .9})
		require.NoError(t, err)
		indice, err := f.Aplicar(context.Background(), []Bloco{bloco})
		require.NoError(t, err)
		require.Equal(t, bloco, indice.Blocos[0])
		require.NotEmpty(t, indice.Revisoes)
		require.Equal(t, caso.motivo, indice.Revisoes[0].Motivo)
	}
}

func TestFallbackRejeitaRespostaDuplicadaAusenteEScoreNaoFinitoComoUnidade(t *testing.T) {
	blocos := []Bloco{
		{Papel: Paragrafo, Confianca: .2, Origem: OrigemHeuristica, RefXML: 1},
		{Papel: Paragrafo, Confianca: .2, Origem: OrigemHeuristica, RefXML: 2},
	}
	for _, caso := range []struct {
		nome     string
		resposta []JulgamentoEstrutura
	}{
		{"ref duplicada", []JulgamentoEstrutura{{RefXML: 1, Papel: Titulo, Confianca: .99}, {RefXML: 1, Papel: Resumo, Confianca: .99}}},
		{"ref ausente", []JulgamentoEstrutura{{RefXML: 1, Papel: Titulo, Confianca: .99}}},
		{"score nan", []JulgamentoEstrutura{{RefXML: 1, Papel: Titulo, Confianca: math.NaN()}, {RefXML: 2, Papel: Resumo, Confianca: .99}}},
		{"score infinito", []JulgamentoEstrutura{{RefXML: 1, Papel: Titulo, Confianca: math.Inf(1)}, {RefXML: 2, Papel: Resumo, Confianca: .99}}},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			fake := &classificadorEstruturaFake{resposta: caso.resposta}
			f, err := NovoFallback(fake, PoliticaConfianca{LimiteConsulta: .5, LimiteConfirmacao: .6, LimiteAutomatico: .9})
			require.NoError(t, err)
			indice, err := f.Aplicar(context.Background(), blocos)
			require.NoError(t, err)
			require.Equal(t, blocos, indice.Blocos, "resposta toda inválida não aplica resultado parcial")
			require.Len(t, indice.Revisoes, 2)
			for _, revisao := range indice.Revisoes {
				require.Equal(t, "resposta_classificador_invalida", revisao.Motivo)
			}
		})
	}
}

func TestFallbackContextoCanceladoSemCandidatosRetornaErro(t *testing.T) {
	ctx, cancelar := context.WithCancel(context.Background())
	cancelar()
	f, err := NovoFallback(&classificadorEstruturaFake{}, PoliticaConfianca{LimiteConsulta: .5, LimiteConfirmacao: .6, LimiteAutomatico: .9})
	require.NoError(t, err)
	_, err = f.Aplicar(ctx, []Bloco{{Papel: Titulo, Confianca: .99, Origem: OrigemEstiloDocx, RefXML: 0}})
	require.ErrorIs(t, err, context.Canceled)
}

func TestFallbackLimitaConsultaA32EDegradaErroDeProvedor(t *testing.T) {
	blocos := make([]Bloco, 40)
	for i := range blocos {
		blocos[i] = Bloco{Papel: Paragrafo, TextoResumo: "x", Confianca: .1, Origem: OrigemHeuristica, RefXML: i}
	}
	fake := &classificadorEstruturaFake{err: errors.New("texto de erro secreto"), classificar: func(entrada []Bloco) []JulgamentoEstrutura {
		require.Len(t, entrada, 32)
		return nil
	}}
	f, err := NovoFallback(fake, PoliticaConfianca{LimiteConsulta: .5, LimiteConfirmacao: .6, LimiteAutomatico: .9})
	require.NoError(t, err)
	indice, err := f.Aplicar(context.Background(), blocos)
	require.NoError(t, err)
	require.Len(t, indice.Revisoes, 40)
	for _, revisao := range indice.Revisoes {
		require.Equal(t, "classificador_indisponivel", revisao.Motivo)
	}
}

func TestFallbackCancelaMesmoSeClassificadorRetornaSucesso(t *testing.T) {
	ctx, cancelar := context.WithCancel(context.Background())
	fake := &classificadorEstruturaFake{classificar: func(entrada []Bloco) []JulgamentoEstrutura {
		cancelar()
		return []JulgamentoEstrutura{{RefXML: entrada[0].RefXML, Papel: Titulo, Confianca: .99}}
	}}
	f, err := NovoFallback(fake, PoliticaConfianca{LimiteConsulta: .5, LimiteConfirmacao: .6, LimiteAutomatico: .9})
	require.NoError(t, err)
	_, err = f.Aplicar(ctx, []Bloco{{Papel: Paragrafo, Confianca: .2, Origem: OrigemHeuristica, RefXML: 1}})
	require.ErrorIs(t, err, context.Canceled)
}
