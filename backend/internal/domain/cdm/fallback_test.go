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

// ---------------------------------------------------------------------------
// Recorte f3-cdm-classifica-citacao — C4, critérios A6 e A16
//
// C4 fixa a confiança do sinal de recuo (C3) em 0,5: ESTRITAMENTE abaixo de
// LimiteConsulta, cujo default de produção é 0,7 (infra/config/jev.go:26), e
// acima de confiancaSemEstilo (0,2). Valor fixo, não faixa — a faixa
// "abaixo de 0,95" admitiria 0,8, e com 0,8 o bloco SAIRIA do conjunto de
// candidatos (Aplicar exige Confianca < LimiteConsulta), isto é, o desempate
// que o recorte promete poderia ser eliminado em silêncio.
// ---------------------------------------------------------------------------

// politicaDefaultDeProducao é a política que carregarJev monta sem nenhuma
// variável de ambiente definida.
func politicaDefaultDeProducao() PoliticaConfianca {
	return PoliticaConfianca{LimiteConsulta: 0.7, LimiteConfirmacao: 0.7, LimiteAutomatico: 0.95}
}

// candidatosConsultados devolve os blocos que Fallback.Aplicar de fato
// mandou para o classificador. É a única forma observável do conjunto de
// candidatos, e é exatamente a fronteira que o TextoResumo atravessa.
func candidatosConsultados(t *testing.T, politica PoliticaConfianca, blocos []Bloco) []Bloco {
	t.Helper()

	fake := &classificadorEstruturaFake{classificar: func(entrada []Bloco) []JulgamentoEstrutura {
		julgamentos := make([]JulgamentoEstrutura, 0, len(entrada))
		for _, bloco := range entrada {
			julgamentos = append(julgamentos, JulgamentoEstrutura{RefXML: bloco.RefXML, SemCorrespondencia: true})
		}
		return julgamentos
	}}

	f, err := NovoFallback(fake, politica)
	require.NoError(t, err)

	_, err = f.Aplicar(context.Background(), blocos)
	require.NoError(t, err)
	return fake.entrada
}

// A6 — a confiança de C3 mantém o bloco candidato ao desempate; a de C2
// (estilo nomeado, evidência declarada pelo documento) não precisa de
// desempate e fica fora.
//
// RECORTE f3-mutadores-por-papel, A13: a construção do bloco de recuo passa a
// ser OrigemHeuristica, que é o que C1 emite. A afirmação sobre o FILTRO não
// muda — fallback.go:87 exclui só OrigemUsuario e OrigemLLM —, e é justamente
// isso que o critério A13 reivindica: renomear a origem do recuo não pode
// tirá-lo do desempate do LLM. Mata o mutante que acrescenta OrigemHeuristica
// às exclusões de candidato, reflexo plausível de quem implementa C1.
func TestFallbackCitacaoPorRecuoEhCandidataECitacaoPorEstiloNao(t *testing.T) {
	const (
		refRecuo  = 1
		refEstilo = 2
	)

	blocos := []Bloco{
		{Papel: Citacao, TextoResumo: "citação detectada pelo recuo", Confianca: 0.5, Origem: OrigemHeuristica, RefXML: refRecuo},
		{Papel: Citacao, TextoResumo: "citação declarada pelo estilo", Confianca: 0.95, Origem: OrigemEstiloDocx, RefXML: refEstilo},
	}

	politica := politicaDefaultDeProducao()
	require.Less(t, blocos[0].Confianca, politica.LimiteConsulta,
		"pré-condição: a confiança do recuo precisa ficar ABAIXO de LimiteConsulta, senão o caso não alcança o filtro de candidatos")

	consultados := candidatosConsultados(t, politica, blocos)

	require.Len(t, consultados, 1,
		"com LimiteConsulta 0,7, a citação de recuo (0,5) entra no desempate e a de estilo (0,95) não")
	require.Equal(t, refRecuo, consultados[0].RefXML)
	require.Equal(t, OrigemHeuristica, consultados[0].Origem)
	require.Equal(t, 0.5, consultados[0].Confianca)
}

// A16 — o delta de candidatos, medido e NOMEADO. É o efeito colateral que o
// recorte torna visível: hoje um parágrafo com estilo "Normal" fica em 0,95 e
// NUNCA é candidato, logo seu TextoResumo nunca atravessa a fronteira para o
// Jev; com C3, todo "Normal" RECUADO cai para 0,5 e passa a atravessar.
// Mitigado por Jev.Habilitado ser false por padrão, e o teto de 32 blocos de
// Aplicar não muda — o que muda é QUAIS blocos saem.
func TestFallbackRecuoAumentaOConjuntoDeCandidatos(t *testing.T) {
	const (
		candidatosSemRecuo = 0
		candidatosComRecuo = 2
	)

	// Documento de cinco parágrafos com estilo "Normal": a camada 1 devolve
	// Paragrafo a 0,95 em todos.
	semRecuo := []Bloco{
		{Papel: Paragrafo, TextoResumo: "parágrafo um", Confianca: 0.95, Origem: OrigemEstiloDocx, RefXML: 0},
		{Papel: Paragrafo, TextoResumo: "parágrafo dois", Confianca: 0.95, Origem: OrigemEstiloDocx, RefXML: 1},
		{Papel: Paragrafo, TextoResumo: "parágrafo três", Confianca: 0.95, Origem: OrigemEstiloDocx, RefXML: 2},
		{Papel: Paragrafo, TextoResumo: "parágrafo quatro", Confianca: 0.95, Origem: OrigemEstiloDocx, RefXML: 3},
		{Papel: Paragrafo, TextoResumo: "parágrafo cinco", Confianca: 0.95, Origem: OrigemEstiloDocx, RefXML: 4},
	}

	// O MESMO documento com dois desses parágrafos recuados acima do limiar.
	comRecuo := append([]Bloco(nil), semRecuo...)
	// Origem do recuo é OrigemHeuristica desde C1 (f3-mutadores-por-papel):
	// palpite geométrico, não declaração do documento. O delta medido abaixo
	// não depende disso — o filtro não exclui OrigemHeuristica —, mas o estado
	// construído aqui precisa ser um que a camada 1 realmente produza.
	comRecuo[1] = Bloco{Papel: Citacao, TextoResumo: "parágrafo dois, recuado", Confianca: 0.5, Origem: OrigemHeuristica, RefXML: 1}
	comRecuo[3] = Bloco{Papel: Citacao, TextoResumo: "parágrafo quatro, recuado", Confianca: 0.5, Origem: OrigemHeuristica, RefXML: 3}

	politica := politicaDefaultDeProducao()

	semRecuoConsultados := candidatosConsultados(t, politica, semRecuo)
	require.Len(t, semRecuoConsultados, candidatosSemRecuo,
		"sem recuo, nenhum dos cinco parágrafos atravessa a fronteira: 0,95 nunca é menor que LimiteConsulta 0,7")

	consultados := candidatosConsultados(t, politica, comRecuo)
	require.Len(t, consultados, candidatosComRecuo,
		"com recuo, os dois blocos a 0,5 passam a atravessar a fronteira para o Jev")

	refs := []int{consultados[0].RefXML, consultados[1].RefXML}
	require.ElementsMatch(t, []int{1, 3}, refs, "são exatamente os dois blocos recuados que passam a sair")

	// O delta é comparado sobre os RESULTADOS, não sobre as duas constantes:
	// comparar candidatosComRecuo com candidatosSemRecuo seria 2 > 0, verdade
	// aritmética que passa sem o código ser executado.
	require.Greater(t, len(consultados), len(semRecuoConsultados),
		"o critério é o delta MEDIDO: o documento com recuo tem de produzir mais candidatos que o mesmo documento sem recuo")
}
