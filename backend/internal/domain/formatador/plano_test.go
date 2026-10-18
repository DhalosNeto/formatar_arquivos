// Este arquivo é o RED do contrato C2 do recorte f3-integracao-plano-formatacao:
// o pacote domain/formatador, com Plano e Planejar.
//
// FASE VERMELHA: plano.go ainda não existe. O pacote não compila porque Plano
// e Planejar estão indefinidos — não por erro de sintaxe ou de import.
//
// Critérios cobertos aqui: A5 (seleção e cópia dos campos da definição),
// A6 (filtro por papel + ordenação), A7 (propagação sem reclassificação do
// erro do índice), A8 (propagação do erro da definição) e a parte de A16 que
// pertence a Planejar.
//
// A18 não é escrito aqui: os testes de arquitetura que o provam já existem em
// internal/arquitetura/ e passam a cobrir este pacote pelo simples fato de ele
// existir.
package formatador

import (
	"fmt"
	"testing"

	"github.com/daniel-halos/formatador/internal/domain/cdm"
	"github.com/daniel-halos/formatador/internal/domain/ruleset"
	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// definicaoSintetica é um perfil que passa em ruleset.Definicao.Validar sem
// afirmar nada normativo (ABNT, APA e Geousp estão fora do recorte). Slug em
// kebab-case, Versao >= 1, Nome preenchido e Fonte com URL http/https: sem
// isso, Validar reprovaria antes do comportamento em teste.
// apontar devolve o endereço de um literal: os cinco campos silentes de
// ruleset.Corpo são ponteiro.
func apontar[T any](valor T) *T { return &valor }

func definicaoSintetica() ruleset.Definicao {
	return ruleset.Definicao{
		Slug:   "revista-sintetica",
		Versao: 1,
		Nome:   "Revista Sintética de Teste",
		Fonte:  "https://exemplo.test/diretrizes",
		Pagina: ruleset.Pagina{
			LarguraCM: 21,
			AlturaCM:  29.7,
			Margens:   ruleset.Margens{SuperiorCM: 2, InferiorCM: 2, EsquerdaCM: 3, DireitaCM: 2},
		},
		Corpo: ruleset.Corpo{
			Fonte:          apontar("Times New Roman"),
			TamanhoPT:      12,
			Entrelinha:     1.5,
			RecuoCM:        apontar(1.25),
			EspacoAntesPT:  apontar(0.0),
			EspacoDepoisPT: apontar(6.0),
			Alinhamento:    apontar("justificado"),
		},
	}
}

// bloco monta uma entrada do índice por literal: Planejar recebe índices que
// podem não ter passado por cdm.NovoBloco.
func bloco(papel cdm.Papel, refXML int) cdm.Bloco {
	return cdm.Bloco{
		Papel:       papel,
		TextoResumo: "texto qualquer",
		Confianca:   0.9,
		Origem:      cdm.OrigemEstiloDocx,
		RefXML:      refXML,
	}
}

func indiceValido(blocos ...cdm.Bloco) cdm.Indice {
	return cdm.Indice{Versao: cdm.VersaoFormatoCDM, Blocos: blocos}
}

// ---------------------------------------------------------------------------
// A5 — seleção dos parágrafos e cópia fiel dos campos da definição
// ---------------------------------------------------------------------------

func TestPlanejarSelecionaParagrafosECopiaCamposDaDefinicao(t *testing.T) {
	definicao := definicaoSintetica()
	require.NoError(t, definicao.Validar(), "pré-condição: a definição sintética precisa ser válida")

	indice := indiceValido(
		bloco(cdm.Titulo, 0),
		bloco(cdm.Paragrafo, 1),
		bloco(cdm.Secao(1), 2),
		bloco(cdm.Paragrafo, 3),
	)
	require.NoError(t, indice.Validar(), "pré-condição: o índice do caso precisa ser válido")

	plano, err := Planejar(indice, definicao)
	require.NoError(t, err)

	assert.Equal(t, []int{1, 3}, plano.ReferenciasCorpo, "A5: só os parágrafos genéricos entram no corpo")
	assert.Equal(t, definicao.Pagina, plano.Pagina, "A5: a página do plano é a da definição")
	assert.Equal(t, definicao.Corpo, plano.Corpo, "A5: o corpo do plano é o da definição")
}

func TestPlanejarIndiceSemBlocosProduzCorpoVazio(t *testing.T) {
	plano, err := Planejar(indiceValido(), definicaoSintetica())
	require.NoError(t, err, "C2: índice sem blocos é válido")
	assert.Len(t, plano.ReferenciasCorpo, 0, "C2: nenhum parágrafo a formatar é comprimento 0, não erro")
}

// ---------------------------------------------------------------------------
// A6 — filtro por papel e ordem crescente
// ---------------------------------------------------------------------------

func TestPlanejarFiltraPorPapelEOrdenaCrescente(t *testing.T) {
	definicao := definicaoSintetica()
	require.NoError(t, definicao.Validar(), "pré-condição: a definição sintética precisa ser válida")

	// Parágrafos fora de ordem (5, 2, 9) intercalados com todos os papéis que
	// o recorte não formata: remover o filtro OU a ordenação quebra o caso.
	indice := indiceValido(
		bloco(cdm.Tabela, 0),
		bloco(cdm.Figura, 1),
		bloco(cdm.Paragrafo, 5),
		bloco(cdm.Legenda, 3),
		bloco(cdm.Citacao, 4),
		bloco(cdm.Paragrafo, 2),
		bloco(cdm.Referencia, 6),
		bloco(cdm.NotaRodape, 7),
		bloco(cdm.Paragrafo, 9),
	)
	require.NoError(t, indice.Validar(), "pré-condição: o índice do caso precisa ser válido")

	plano, err := Planejar(indice, definicao)
	require.NoError(t, err)
	assert.Equal(t, []int{2, 5, 9}, plano.ReferenciasCorpo)
}

// ---------------------------------------------------------------------------
// A7 — o erro do índice sobe sem reclassificação
// ---------------------------------------------------------------------------

func TestPlanejarPropagaErroDoIndiceSemReclassificar(t *testing.T) {
	casos := []struct {
		nome        string
		indice      cdm.Indice
		validacao   bool
		duplicidade bool
	}{
		{
			nome:      "versão incompatível",
			indice:    cdm.Indice{Versao: cdm.VersaoFormatoCDM + 1, Blocos: []cdm.Bloco{bloco(cdm.Paragrafo, 0)}},
			validacao: true,
		},
		{
			nome: "bloco inválido",
			indice: cdm.Indice{
				Versao: cdm.VersaoFormatoCDM,
				Blocos: []cdm.Bloco{{Papel: cdm.Paragrafo, Confianca: 0.9, Origem: cdm.Origem(""), RefXML: 0}},
			},
			validacao: true,
		},
		{
			nome: "ref_xml duplicado",
			indice: cdm.Indice{
				Versao: cdm.VersaoFormatoCDM,
				Blocos: []cdm.Bloco{bloco(cdm.Paragrafo, 2), bloco(cdm.Titulo, 2)},
			},
			duplicidade: true,
		},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			require.Error(t, caso.indice.Validar(), "pré-condição: o índice do caso precisa ser recusado pelo próprio cdm")

			plano, err := Planejar(caso.indice, definicaoSintetica())
			require.Error(t, err, "A7: índice inválido não pode produzir plano")
			assert.Equal(t, Plano{}, plano, "A7: em erro, o plano devolvido é o zero")

			var validacao *errors.ErroValidacao
			assert.Equal(t, caso.validacao, errors.Como(err, &validacao),
				"A7: a classificação de entrada inválida precisa sobreviver à propagação; erro foi %T", err)
			assert.Equal(t, caso.duplicidade, errors.E(err, cdm.ErroRefXMLDuplicado),
				"A7: a sentinela de corrupção precisa continuar identificável por errors.E; erro foi %v", err)
		})
	}
}

// ---------------------------------------------------------------------------
// A8 — o erro da definição sobe sem virar mensagem genérica
// ---------------------------------------------------------------------------

func TestPlanejarPropagaErroDaDefinicao(t *testing.T) {
	casos := []struct {
		nome    string
		ajustar func(*ruleset.Definicao)
	}{
		{"entrelinha zero", func(d *ruleset.Definicao) { d.Corpo.Entrelinha = 0 }},
		{"slug vazio", func(d *ruleset.Definicao) { d.Slug = "" }},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			definicao := definicaoSintetica()
			caso.ajustar(&definicao)

			esperado := definicao.Validar()
			var esperadoValidacao *errors.ErroValidacao
			require.True(t, errors.Como(esperado, &esperadoValidacao),
				"pré-condição: o caso precisa alcançar a guarda de ruleset.Definicao.Validar")

			indice := indiceValido(bloco(cdm.Paragrafo, 0))
			require.NoError(t, indice.Validar(), "pré-condição: só a definição pode reprovar neste caso")

			_, err := Planejar(indice, definicao)
			require.Error(t, err, "A8: definição inválida não pode produzir plano")

			var obtido *errors.ErroValidacao
			require.True(t, errors.Como(err, &obtido), "A8: o *ErroValidacao da definição precisa sobreviver; erro foi %T", err)
			assert.Equal(t, esperadoValidacao.Campos, obtido.Campos,
				"A8: os campos reprovados são os de definicao.Validar(), não uma mensagem genérica")
		})
	}
}

// ---------------------------------------------------------------------------
// A16 (parte de Planejar) — conteúdo do usuário nunca entra no erro
// ---------------------------------------------------------------------------

const marcaVazamentoPlano = "ZZMARCA-TEXTO-DO-USUARIO-7f3a9b-PLANO"

// conferirSemVazamentoPlano varre mensagem, cada CampoInvalido isoladamente
// (é CampoInvalido.String() que rotasutil.go:36 copia para `razoes`), toda a
// cadeia de Unwrap e os três verbos de fmt. %#v não passa por Stringer, e
// cdm.Bloco não tem String()/GoString().
func conferirSemVazamentoPlano(t *testing.T, err error, marca string) {
	t.Helper()
	require.Error(t, err, "o caso de vazamento precisa realmente falhar")

	assert.NotContains(t, err.Error(), marca, "regra 7: Error() não cita conteúdo do documento")
	for _, formato := range []string{"%v", "%+v", "%#v"} {
		assert.NotContains(t, fmt.Sprintf(formato, err), marca,
			"regra 7: %s sobre o erro não pode expor conteúdo do documento", formato)
	}

	pendentes := []error{err}
	for len(pendentes) > 0 {
		atual := pendentes[len(pendentes)-1]
		pendentes = pendentes[:len(pendentes)-1]
		if atual == nil {
			continue
		}
		assert.NotContains(t, atual.Error(), marca, "regra 7: elo da cadeia de Unwrap cita conteúdo")

		if validacao, ok := atual.(*errors.ErroValidacao); ok { //nolint:errorlint // atual já é um elo desembrulhado da cadeia; errors.As pararia no primeiro match e deixaria elos sem conferir, que é justamente o que A16 precisa varrer.
			assert.NotContains(t, validacao.Mensagem, marca, "regra 7: ErroValidacao.Mensagem cita conteúdo")
			for _, campo := range validacao.Campos {
				assert.NotContains(t, campo.Campo, marca, "regra 7: CampoInvalido.Campo cita conteúdo")
				assert.NotContains(t, campo.Mensagem, marca, "regra 7: CampoInvalido.Mensagem cita conteúdo")
				assert.NotContains(t, campo.String(), marca,
					"regra 7: CampoInvalido.String() é o que vai para razoes da resposta HTTP")
			}
		}
		switch desembrulhavel := atual.(type) { //nolint:errorlint // o type switch É o mecanismo de desembrulho desta varredura, não uma checagem de erro específico.
		case interface{ Unwrap() error }:
			pendentes = append(pendentes, desembrulhavel.Unwrap())
		case interface{ Unwrap() []error }:
			pendentes = append(pendentes, desembrulhavel.Unwrap()...)
		}
	}
}

func TestPlanejarNaoVazaTextoDoUsuarioNoErro(t *testing.T) {
	comMarca := func(blocos ...cdm.Bloco) []cdm.Bloco {
		for i := range blocos {
			blocos[i].TextoResumo = marcaVazamentoPlano + " parágrafo confidencial"
		}
		return blocos
	}
	definicaoInvalida := definicaoSintetica()
	definicaoInvalida.Corpo.Entrelinha = 0

	casos := []struct {
		nome      string
		indice    cdm.Indice
		definicao ruleset.Definicao
	}{
		{
			nome:      "versão incompatível",
			indice:    cdm.Indice{Versao: cdm.VersaoFormatoCDM + 1, Blocos: comMarca(bloco(cdm.Paragrafo, 0))},
			definicao: definicaoSintetica(),
		},
		{
			nome: "bloco inválido",
			indice: cdm.Indice{
				Versao: cdm.VersaoFormatoCDM,
				Blocos: comMarca(cdm.Bloco{Papel: cdm.Papel{}, Origem: cdm.OrigemEstiloDocx, RefXML: 0}),
			},
			definicao: definicaoSintetica(),
		},
		{
			nome: "ref_xml duplicado",
			indice: cdm.Indice{
				Versao: cdm.VersaoFormatoCDM,
				Blocos: comMarca(bloco(cdm.Paragrafo, 2), bloco(cdm.Paragrafo, 2)),
			},
			definicao: definicaoSintetica(),
		},
		{
			nome:      "definição inválida",
			indice:    cdm.Indice{Versao: cdm.VersaoFormatoCDM, Blocos: comMarca(bloco(cdm.Paragrafo, 0))},
			definicao: definicaoInvalida,
		},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			for _, b := range caso.indice.Blocos {
				require.Contains(t, b.TextoResumo, marcaVazamentoPlano, "pré-condição: todo bloco do caso carrega a marca")
			}
			_, err := Planejar(caso.indice, caso.definicao)
			conferirSemVazamentoPlano(t, err, marcaVazamentoPlano)
		})
	}
}

// ---------------------------------------------------------------------------
// RECORTE f3-mutadores-por-papel — C3, critérios A5 a A10
//
// O PISO DE AUTORIZAÇÃO mora aqui, em Planejar, que é a porta de validação do
// fluxo e domínio puro. A regra é uma disjunção de dois termos:
//
//	autorizado := bloco.Origem == cdm.OrigemEstiloDocx || bloco.Origem == cdm.OrigemUsuario
//
// Origem declarada pelo documento, ou confirmada pelo humano. Detecção por
// recuo, por região, por regra textual e palpite de LLM NUNCA autorizam —
// ainda que a confiança seja alta. Por isso o piso é por ORIGEM e não por
// limiar de confiança: A8 é o caso que separa as duas leituras.
//
// Bloco com papel declarado e origem não autorizada fica fora do plano
// INTEIRO: como ReferenciasCorpo segue exigindo Papel == cdm.Paragrafo,
// nenhum mutador o alcança e o parágrafo sai como o autor escreveu.
// ---------------------------------------------------------------------------

// blocoOrigem monta a entrada do índice nomeando confiança e origem, que são
// justamente os dois eixos que C3 separa.
func blocoOrigem(papel cdm.Papel, refXML int, confianca float64, origem cdm.Origem) cdm.Bloco {
	return cdm.Bloco{
		Papel:       papel,
		TextoResumo: "texto qualquer",
		Confianca:   confianca,
		Origem:      origem,
		RefXML:      refXML,
	}
}

func definicaoComEntrelinhaPorPapel(medidas map[string]float64) ruleset.Definicao {
	definicao := definicaoSintetica()
	definicao.EntrelinhaPorPapel = medidas
	return definicao
}

// conferirConjuntosDisjuntos é o invariante estrutural de C3: os grupos são
// dois a dois disjuntos e disjuntos de ReferenciasCorpo. Um ordinal em dois
// conjuntos produziria duas AplicarEntrelinha sobre o mesmo w:p, a segunda
// sobrescrevendo a primeira em silêncio.
func conferirConjuntosDisjuntos(t *testing.T, plano Plano) {
	t.Helper()

	visto := map[int]string{}
	for _, referencia := range plano.ReferenciasCorpo {
		visto[referencia] = "ReferenciasCorpo"
	}
	for _, grupo := range plano.EntrelinhaPorPapel {
		assert.NotEmpty(t, grupo.Referencias, "C3: grupo sem bloco autorizado não pode ser emitido")
		assert.IsIncreasing(t, append([]int(nil), grupo.Referencias...),
			"C3: as referências de cada grupo saem em ordem crescente")
		for _, referencia := range grupo.Referencias {
			dono, repetido := visto[referencia]
			assert.False(t, repetido, "C3: o ordinal %d está em %s e também no grupo %s", referencia, dono, grupo.Papel)
			visto[referencia] = "grupo " + grupo.Papel.String()
		}
	}
}

// ---------------------------------------------------------------------------
// A5 — TERMO 1 do piso: OrigemEstiloDocx
// ---------------------------------------------------------------------------

func TestPlanejarAutorizaOrigemDeclaradaPeloDocumento(t *testing.T) {
	definicao := definicaoComEntrelinhaPorPapel(map[string]float64{"citacao": 1.0})
	require.NoError(t, definicao.Validar(), "pré-condição: a definição do caso precisa ser válida")

	indice := indiceValido(
		blocoOrigem(cdm.Paragrafo, 0, 0.95, cdm.OrigemEstiloDocx),
		blocoOrigem(cdm.Paragrafo, 1, 0.95, cdm.OrigemEstiloDocx),
		blocoOrigem(cdm.Citacao, 3, 0.95, cdm.OrigemEstiloDocx),
	)
	require.NoError(t, indice.Validar(), "pré-condição: o índice do caso precisa ser válido")

	plano, err := Planejar(indice, definicao)
	require.NoError(t, err)

	assert.Equal(t, []GrupoEntrelinha{{Papel: cdm.Citacao, Entrelinha: 1.0, Referencias: []int{3}}},
		plano.EntrelinhaPorPapel,
		"A5: remover o termo OrigemEstiloDocx da disjunção derruba este caso")
	assert.Equal(t, []int{0, 1}, plano.ReferenciasCorpo, "A5: a citação sai de ReferenciasCorpo e vira grupo próprio")
	conferirConjuntosDisjuntos(t, plano)
}

// ---------------------------------------------------------------------------
// A6 — TERMO 2 do piso: OrigemUsuario, o estado que o PATCH grava
// ---------------------------------------------------------------------------

func TestPlanejarAutorizaOrigemConfirmadaPeloHumano(t *testing.T) {
	definicao := definicaoComEntrelinhaPorPapel(map[string]float64{"legenda": 1.0})
	require.NoError(t, definicao.Validar(), "pré-condição: a definição do caso precisa ser válida")

	// Confiança 1 e OrigemUsuario é exatamente o que
	// ServicoEstrutura.Corrigir grava: é o único caminho pelo qual Legenda,
	// Referencia e NotaRodape chegam hoje ao motor.
	indice := indiceValido(blocoOrigem(cdm.Legenda, 5, 1, cdm.OrigemUsuario))
	require.NoError(t, indice.Validar(), "pré-condição: o índice do caso precisa ser válido")

	plano, err := Planejar(indice, definicao)
	require.NoError(t, err)

	assert.Equal(t, []GrupoEntrelinha{{Papel: cdm.Legenda, Entrelinha: 1.0, Referencias: []int{5}}},
		plano.EntrelinhaPorPapel,
		"A6: remover o termo OrigemUsuario derruba este caso, e nenhum dos dois termos sozinho satisfaz A5 e A6 ao mesmo tempo")
	assert.Empty(t, plano.ReferenciasCorpo)
}

// ---------------------------------------------------------------------------
// A7 — recuo não autoriza, e o ordinal fica fora do plano INTEIRO
// ---------------------------------------------------------------------------

func TestPlanejarNaoAutorizaCitacaoDetectadaSoPeloRecuo(t *testing.T) {
	definicao := definicaoComEntrelinhaPorPapel(map[string]float64{"citacao": 1.0})
	require.NoError(t, definicao.Validar(), "pré-condição: a definição do caso precisa ser válida")

	const ordinalDoRecuo = 2

	// Citacao/0,5/OrigemHeuristica é o que ClassificarPorEstiloDocx passa a
	// emitir para parágrafo recuado sem estilo (C1).
	indice := indiceValido(
		blocoOrigem(cdm.Paragrafo, 1, 0.95, cdm.OrigemEstiloDocx),
		blocoOrigem(cdm.Citacao, ordinalDoRecuo, 0.5, cdm.OrigemHeuristica),
		blocoOrigem(cdm.Paragrafo, 3, 0.95, cdm.OrigemEstiloDocx),
	)
	require.NoError(t, indice.Validar(), "pré-condição: o índice do caso precisa ser válido")

	plano, err := Planejar(indice, definicao)
	require.NoError(t, err)

	assert.Empty(t, plano.EntrelinhaPorPapel,
		"A7: mata o mutante que amplia o piso para OrigemHeuristica")
	assert.Equal(t, []int{1, 3}, plano.ReferenciasCorpo)
	assert.NotContains(t, plano.ReferenciasCorpo, ordinalDoRecuo,
		"A7: o ordinal não aparece em NENHUM conjunto do plano, logo nenhum mutador o alcança e o parágrafo do autor fica como ele escreveu")
	conferirConjuntosDisjuntos(t, plano)
}

// ---------------------------------------------------------------------------
// A8 — dois mutantes que só este critério mata
// ---------------------------------------------------------------------------

func TestPlanejarRecusaOrigensAutomaticasAindaQueComConfiancaAlta(t *testing.T) {
	casos := []struct {
		nome      string
		medidas   map[string]float64
		blocos    []cdm.Bloco
		mutante   string
		sobrevive string
	}{
		{
			nome:    "(a) região (0,6) e regra textual (0,8) são palpite, não declaração",
			medidas: map[string]float64{"referencia": 1.0, "legenda": 1.0, "nota_rodape": 1.0},
			blocos: []cdm.Bloco{
				// 0,6 e 0,8 são os valores REAIS de confiancaRegiao e
				// confiancaRegraTextual da camada 2.
				blocoOrigem(cdm.Referencia, 4, 0.6, cdm.OrigemHeuristica),
				blocoOrigem(cdm.Legenda, 6, 0.8, cdm.OrigemHeuristica),
			},
			mutante:   "trocar o piso por limiar de confiança (Confianca >= 0,6)",
			sobrevive: "A5 (0,95), A6 (1) e A7 (0,5)",
		},
		{
			nome:    "(b) palpite de LLM com confiança alta não autoriza",
			medidas: map[string]float64{"citacao": 1.0},
			// Confianca 0,95 com OrigemLLM é o estado que fallback.go:129
			// grava quando o julgamento passa de LimiteAutomatico.
			blocos:    []cdm.Bloco{blocoOrigem(cdm.Citacao, 2, 0.95, cdm.OrigemLLM)},
			mutante:   "acrescentar || bloco.Origem == cdm.OrigemLLM",
			sobrevive: "todos os outros critérios, e é imune a qualquer piso por confiança",
		},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			definicao := definicaoComEntrelinhaPorPapel(caso.medidas)
			require.NoError(t, definicao.Validar(), "pré-condição: a definição do caso precisa ser válida")

			indice := indiceValido(caso.blocos...)
			require.NoError(t, indice.Validar(), "pré-condição: o índice do caso precisa ser válido")
			for _, bloco := range caso.blocos {
				require.Contains(t, caso.medidas, bloco.Papel.String(),
					"pré-condição: o papel do bloco PRECISA estar declarado na definição, senão o caso não alcança o piso de origem")
			}

			plano, err := Planejar(indice, definicao)
			require.NoError(t, err)

			assert.Empty(t, plano.EntrelinhaPorPapel,
				"A8: mata o mutante %q, que sobrevive a %s", caso.mutante, caso.sobrevive)
			assert.Empty(t, plano.ReferenciasCorpo, "A8: nenhum dos blocos do caso é Paragrafo")
		})
	}
}

// ---------------------------------------------------------------------------
// A9 — regressão (chave ausente) e grupo vazio não emitido
// ---------------------------------------------------------------------------

func TestPlanejarSemChaveNovaEComPapelSemBlocoAutorizado(t *testing.T) {
	indice := indiceValido(
		blocoOrigem(cdm.Paragrafo, 0, 0.95, cdm.OrigemEstiloDocx),
		blocoOrigem(cdm.Citacao, 1, 0.95, cdm.OrigemEstiloDocx),
	)
	require.NoError(t, indice.Validar(), "pré-condição: o índice do caso precisa ser válido")

	t.Run("(a) definição sem a chave nova preserva o comportamento atual", func(t *testing.T) {
		definicao := definicaoSintetica()
		require.Nil(t, definicao.EntrelinhaPorPapel, "pré-condição: o perfil do caso é silente no campo novo")

		plano, err := Planejar(indice, definicao)
		require.NoError(t, err)

		assert.Empty(t, plano.EntrelinhaPorPapel, "A9: norma silente preserva o documento")
		assert.Equal(t, []int{0}, plano.ReferenciasCorpo, "A9: ReferenciasCorpo é idêntico ao comportamento atual")
	})

	t.Run("(b) papel declarado sem nenhum bloco autorizado não emite grupo", func(t *testing.T) {
		definicao := definicaoComEntrelinhaPorPapel(map[string]float64{"nota_rodape": 1.0})
		require.NoError(t, definicao.Validar())

		plano, err := Planejar(indice, definicao)
		require.NoError(t, err)

		assert.Empty(t, plano.EntrelinhaPorPapel,
			"A9: falsifica emitir grupo por papel DECLARADO em vez de por bloco AUTORIZADO")
	})
}

// ---------------------------------------------------------------------------
// A10 — determinismo da saída e propagação dos erros das duas fontes
// ---------------------------------------------------------------------------

func TestPlanejarGruposSaoDeterministicosEPropagamErro(t *testing.T) {
	definicao := definicaoComEntrelinhaPorPapel(map[string]float64{"citacao": 1.0, "legenda": 1.0})
	require.NoError(t, definicao.Validar(), "pré-condição: a definição do caso precisa ser válida")

	// Fora de ordem crescente de RefXML, como o índice fica depois de uma
	// correção do usuário.
	indice := indiceValido(
		blocoOrigem(cdm.Citacao, 9, 0.95, cdm.OrigemEstiloDocx),
		blocoOrigem(cdm.Legenda, 7, 1, cdm.OrigemUsuario),
		blocoOrigem(cdm.Citacao, 4, 0.95, cdm.OrigemEstiloDocx),
		blocoOrigem(cdm.Paragrafo, 1, 0.95, cdm.OrigemEstiloDocx),
	)
	require.NoError(t, indice.Validar(), "pré-condição: o índice do caso precisa ser válido")

	plano, err := Planejar(indice, definicao)
	require.NoError(t, err)

	// A ordenação por Papel.String() só é TOTAL porque a lista branca de C2
	// exclui "secao": Secao(1) e Secao(2) têm o mesmo String(). Ampliar a
	// lista branca obriga a chave de ordenação a incluir o nível.
	assert.Equal(t, []GrupoEntrelinha{
		{Papel: cdm.Citacao, Entrelinha: 1.0, Referencias: []int{4, 9}},
		{Papel: cdm.Legenda, Entrelinha: 1.0, Referencias: []int{7}},
	}, plano.EntrelinhaPorPapel, "A10: saída determinística, ordenada por Papel.String() crescente")
	assert.Equal(t, []int{1}, plano.ReferenciasCorpo)
	conferirConjuntosDisjuntos(t, plano)

	t.Run("índice com RefXML repetido devolve a sentinela e Plano zero", func(t *testing.T) {
		duplicado := cdm.Indice{Versao: cdm.VersaoFormatoCDM, Blocos: []cdm.Bloco{
			blocoOrigem(cdm.Citacao, 4, 0.95, cdm.OrigemEstiloDocx),
			blocoOrigem(cdm.Legenda, 4, 1, cdm.OrigemUsuario),
		}}
		require.Error(t, duplicado.Validar(), "pré-condição: o próprio cdm recusa este índice")

		plano, err := Planejar(duplicado, definicao)
		require.Error(t, err)
		assert.True(t, errors.E(err, cdm.ErroRefXMLDuplicado),
			"A10: a sentinela de corrupção segue identificável por errors.E; erro foi %v", err)
		assert.Equal(t, Plano{}, plano, "A10: em erro o plano devolvido é o zero, inclusive sem grupos")
	})

	t.Run("definição inválida devolve *ErroValidacao e Plano zero", func(t *testing.T) {
		invalida := definicao
		// Margens que consomem a página: o erro é da definição, não do índice.
		invalida.Pagina.Margens.EsquerdaCM = invalida.Pagina.LarguraCM
		require.Error(t, invalida.Validar(), "pré-condição: o caso precisa alcançar a guarda de Definicao.Validar")

		plano, err := Planejar(indice, invalida)
		require.Error(t, err)
		var validacao *errors.ErroValidacao
		assert.True(t, errors.Como(err, &validacao), "A10: o *ErroValidacao da definição vira 400; erro foi %T", err)
		assert.Equal(t, Plano{}, plano, "A10: em erro o plano devolvido é o zero, inclusive sem grupos")
	})
}

// ---------------------------------------------------------------------------
// C2/C3 — a lista branca só pode conter PAPEL SEM NÍVEL
//
// Achado da segurança: duas coisas dependiam disso só por comentário
// (ruleset/definicao.go:41-54 e plano.go:110-116) e nenhuma estava travada por
// teste. Quem ampliasse a lista branca quebraria as duas de uma vez:
//
//   - a ordenação dos grupos por Papel.String() deixa de ser TOTAL, porque
//     Secao(1) e Secao(2) colapsam na mesma chave — e `porPapel` é indexado
//     por String(), então os dois níveis cairiam no MESMO grupo;
//   - cdm.ParaPapel(nome, 0) de plano.go:124 passa a FALHAR, porque Secao
//     exige nível 1..6, e Planejar devolve erro em vez de plano.
//
// O enunciado é a propriedade, não "não contém secao": toda chave que a lista
// branca aceita precisa ser um papel SEM NÍVEL cujo String() volta igual à
// chave. Isso mata, de uma vez, ampliar a lista para "secao" e grafar a chave
// de forma que não bata com Papel.String() (por exemplo "nota-rodape").
//
// A propriedade é afirmada AQUI, e não em domain/ruleset, porque é o único
// pacote que vê ruleset E cdm ao mesmo tempo: a lista branca é literal justo
// para o domínio não ganhar a aresta ruleset -> cdm (C2).
// ---------------------------------------------------------------------------

func TestListaBrancaDeEntrelinhaSoAceitaPapelSemNivel(t *testing.T) {
	// Todos os nomes de papel que cdm define, mais "secao". A lista é literal
	// porque cdm não exporta o conjunto; se um papel novo aparecer lá e não
	// aqui, este teste não o cobre — e é por isso que a asserção de CONTAGEM
	// abaixo existe, para flagrar a lista branca crescendo sem este teste
	// acompanhar.
	candidatos := []string{
		"titulo", "lista_autores", "resumo", "palavras_chave", "paragrafo",
		"citacao", "item_lista", "tabela", "figura", "legenda", "equacao",
		"referencia", "nota_rodape", "secao",
	}
	require.Contains(t, candidatos, "secao",
		"pré-condição: sem 'secao' entre os candidatos o teste não alcança a guarda que pretende provar")
	// Pré-condição que torna o mutante VISÍVEL: é esta chamada que
	// agruparPorPapel faz, e é ela que quebra se "secao" entrar na lista.
	_, err := cdm.ParaPapel("secao", 0)
	require.Error(t, err, "pré-condição: cdm.Secao exige nível 1..6, logo ParaPapel(\"secao\", 0) é um erro")

	var aceitos []string
	for _, nome := range candidatos {
		definicao := definicaoComEntrelinhaPorPapel(map[string]float64{nome: 1.0})
		if definicao.Validar() != nil {
			continue
		}
		aceitos = append(aceitos, nome)

		papel, err := cdm.ParaPapel(nome, 0)
		require.NoErrorf(t, err, "a lista branca aceita %q, mas cdm.ParaPapel(%q, 0) falha: agruparPorPapel devolveria erro em vez de plano", nome, nome)
		assert.Zerof(t, papel.Nivel(), "a lista branca aceita %q, que carrega nível: a ordenação por Papel.String() deixa de ser total", nome)
		assert.Equalf(t, nome, papel.String(), "a chave %q não é o Papel.String() usado para agrupar: o grupo nasceria vazio em silêncio", nome)
	}

	assert.Equal(t, []string{"citacao", "legenda", "referencia", "nota_rodape"}, aceitos,
		"os quatro papéis de exceção da §5.2, nem um mais: ampliar a lista branca exige revisar a chave de ordenação dos grupos")
}

// ---------------------------------------------------------------------------
// A disjunção é TEOREMA de Planejar, inclusive sob índice adversarial
//
// conferirConjuntosDisjuntos já roda em A5, A7 e A10. Aqui ele roda sobre o
// índice mais hostil que o tipo cdm.Indice admite com RefXML único: os quatro
// papéis declarados, parágrafos intercalados, origens misturadas e dois
// níveis de seção, que é a forma que colapsaria se "secao" entrasse na lista
// branca.
// ---------------------------------------------------------------------------

func TestPlanejarMantemDisjuncaoSobIndiceAdversarial(t *testing.T) {
	definicao := definicaoComEntrelinhaPorPapel(map[string]float64{
		"citacao": 1.0, "referencia": 1.0, "legenda": 1.0, "nota_rodape": 1.0,
	})
	require.NoError(t, definicao.Validar(), "pré-condição: a definição do caso precisa ser válida")

	indice := indiceValido(
		blocoOrigem(cdm.Secao(1), 0, 0.95, cdm.OrigemEstiloDocx),
		blocoOrigem(cdm.Citacao, 8, 0.95, cdm.OrigemEstiloDocx),
		blocoOrigem(cdm.Paragrafo, 1, 0.2, cdm.OrigemEstiloDocx),
		blocoOrigem(cdm.Referencia, 7, 1, cdm.OrigemUsuario),
		blocoOrigem(cdm.Secao(2), 2, 0.95, cdm.OrigemEstiloDocx),
		blocoOrigem(cdm.Legenda, 6, 0.8, cdm.OrigemHeuristica),
		blocoOrigem(cdm.Citacao, 5, 0.5, cdm.OrigemHeuristica),
		blocoOrigem(cdm.Paragrafo, 3, 0.95, cdm.OrigemEstiloDocx),
		blocoOrigem(cdm.NotaRodape, 4, 1, cdm.OrigemUsuario),
	)
	require.NoError(t, indice.Validar(), "pré-condição: o índice do caso precisa ser válido")

	plano, err := Planejar(indice, definicao)
	require.NoError(t, err, "as duas seções não podem fazer Planejar falhar: 'secao' está fora da lista branca")

	conferirConjuntosDisjuntos(t, plano)
	assert.Equal(t, []int{1, 3}, plano.ReferenciasCorpo)
	assert.Equal(t, []GrupoEntrelinha{
		{Papel: cdm.Citacao, Entrelinha: 1.0, Referencias: []int{8}},
		{Papel: cdm.NotaRodape, Entrelinha: 1.0, Referencias: []int{4}},
		{Papel: cdm.Referencia, Entrelinha: 1.0, Referencias: []int{7}},
	}, plano.EntrelinhaPorPapel,
		"os blocos de origem heurística (legenda 0,8 e citação 0,5) ficam fora do plano inteiro, e nenhum grupo de seção é emitido")
}
