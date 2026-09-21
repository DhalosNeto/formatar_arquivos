// Este arquivo testa a camada 2 do CDM: AplicarHeuristica, que reclassifica
// blocos já passados pela camada 1 (internal/infra/ooxml.ClassificarPorEstiloDocx)
// usando evidência TEXTUAL e POSICIONAL — numeração de seção no texto, rótulos
// de região ("RESUMO"/"ABSTRACT"/"REFERÊNCIAS"), prefixos de palavras-chave e
// de legenda. docs/plano-backend.md, seção F2, "Heurística de classificação".
//
// FASE VERMELHA: heuristica.go ainda não existe. Este arquivo prova que o
// pacote não compila por AplicarHeuristica estar indefinida — não por erro de
// sintaxe ou de import.
//
// Precedência travada pelo investigador: Palavras-chave > Legenda > Seção
// numerada > Região. Decisão de desenho tomada aqui, não coberta pelo
// contrato: as fronteiras Palavras-chave↔Legenda e Legenda↔Seção-numerada não
// têm teste de CONFLITO real porque os prefixos das três regras são
// mutuamente exclusivos por construção — "Palavras-chave"/"Keywords",
// "Tabela N"/"Figura N"/"Quadro N"/"Fonte:" e um dígito inicial nunca
// coincidem no mesmo texto. A única fronteira que o precedência de fato
// resolve, e que este arquivo prova em cada uma das três regras, é "regra
// específica vence região" (rule 3/4/5 > rule 1/2) — é isso que o fixture real
// exige (palavras-chave e legendas aparecem dentro de regiões de resumo e de
// resultados).
package cdm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Fixtures e helpers
// ---------------------------------------------------------------------------

// confiancaEntradaBaixa e confiancaEntradaAlta simulam o que a camada 1
// (ooxml.ClassificarPorEstiloDocx) entregaria: baixa para o fallback de
// estilo desconhecido/ausente, alta para estilo nomeado reconhecido (Title,
// Normal, HeadingN). O pacote cdm não importa ooxml (regra de arquitetura),
// então os valores são hardcoded e comentados aqui, não reaproveitados por
// import.
const (
	confiancaEntradaBaixa = 0.2
	confiancaEntradaAlta  = 0.95
)

// confiancaEstiloNomeadoRef é o mesmo valor de confiancaEstiloConhecida em
// ooxml.papelPeloEstilo (0.95): o teto que a confiança produzida pela camada 2
// nunca pode alcançar, porque evidência textual é sempre mais fraca que
// estilo nomeado — é a regra de "Confiança" da ficha do investigador.
const confiancaEstiloNomeadoRef = 0.95

// confiancaHeuristicaMinima é o piso de teste para a faixa que a camada 2 pode
// produzir. Faixa, não valor exato, como pede o contrato.
const confiancaHeuristicaMinima = 0.3

// deveNovoBloco constrói um Bloco válido para popular fixtures de teste; entra
// em pânico se os valores não passarem em NovoBloco. É erro de quem escreveu
// o teste, não algo que precise de tratamento em runtime — mesmo espírito de
// regexp.MustCompile.
func deveNovoBloco(papel Papel, texto string, confianca float64, origem Origem, refXML int) Bloco {
	bloco, err := NovoBloco(papel, texto, confianca, origem, refXML)
	if err != nil {
		panic(err)
	}
	return bloco
}

// aplicarHeuristicaUnico roda AplicarHeuristica sobre uma fatia de um único
// bloco — usado pelos casos que não dependem de região/posição.
func aplicarHeuristicaUnico(t *testing.T, entrada Bloco) Bloco {
	t.Helper()

	saida, err := AplicarHeuristica([]Bloco{entrada})
	require.NoError(t, err)
	require.Len(t, saida, 1)
	return saida[0]
}

// blocoEsperado descreve o resultado esperado para um bloco depois de
// AplicarHeuristica.
type blocoEsperado struct {
	papel          Papel
	reclassificado bool // false: bloco tem que sair EXATAMENTE igual à entrada
}

// assertResultadoBloco confere um bloco de saída contra a entrada e a
// expectativa. Quando não reclassificado, exige igualdade TOTAL com a
// entrada (proteção contra qualquer regra tocar um bloco por engano). Quando
// reclassificado, exige Origem == OrigemHeuristica e confiança numa faixa
// mais fraca que a de um estilo nomeado — nunca um valor exato.
func assertResultadoBloco(t *testing.T, indice int, entrada, saida Bloco, esperado blocoEsperado) {
	t.Helper()

	assert.Equalf(t, esperado.papel, saida.Papel, "bloco %d: papel inesperado", indice)
	assert.Equalf(t, entrada.TextoResumo, saida.TextoResumo, "bloco %d: a camada 2 nunca muda o texto", indice)
	assert.Equalf(t, entrada.RefXML, saida.RefXML, "bloco %d: a camada 2 nunca muda a posição", indice)

	if esperado.reclassificado {
		assert.Equalf(t, OrigemHeuristica, saida.Origem, "bloco %d: bloco reclassificado pela camada 2 tem que sair com OrigemHeuristica", indice)
		assert.GreaterOrEqualf(t, saida.Confianca, confiancaHeuristicaMinima, "bloco %d", indice)
		assert.Lessf(t, saida.Confianca, confiancaEstiloNomeadoRef, "bloco %d: heurística é evidência mais fraca que estilo nomeado", indice)
		return
	}

	assert.Equalf(t, entrada, saida, "bloco %d: não bate nenhuma regra da camada 2, precisa sair EXATAMENTE igual à entrada", indice)
}

// ---------------------------------------------------------------------------
// Cenários multi-bloco: região de resumo/referências, precedência e limites
// ---------------------------------------------------------------------------

func TestAplicarHeuristicaCenarios(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome     string
		entrada  []Bloco
		esperado []blocoEsperado
	}{
		{
			nome: "resumo: o rótulo permanece e os blocos seguintes viram Resumo até o próximo cabeçalho",
			entrada: []Bloco{
				deveNovoBloco(Paragrafo, "RESUMO", confiancaEntradaBaixa, OrigemEstiloDocx, 0),
				deveNovoBloco(Paragrafo, "Este é o texto do resumo.", confiancaEntradaBaixa, OrigemEstiloDocx, 1),
				deveNovoBloco(Paragrafo, "Continuação do resumo.", confiancaEntradaBaixa, OrigemEstiloDocx, 2),
				deveNovoBloco(Secao(1), "Contextualização", confiancaEntradaAlta, OrigemEstiloDocx, 3),
				deveNovoBloco(Paragrafo, "Texto após o cabeçalho, fora do resumo.", confiancaEntradaBaixa, OrigemEstiloDocx, 4),
			},
			esperado: []blocoEsperado{
				{papel: Paragrafo}, // o rótulo em si não é tocado
				{papel: Resumo, reclassificado: true},
				{papel: Resumo, reclassificado: true},
				{papel: Secao(1)},  // cabeçalho: encerra a região, texto não bate com regra 5
				{papel: Paragrafo}, // fora da região
			},
		},
		{
			nome: "abstract: mesma mecânica do resumo, rótulo em inglês",
			entrada: []Bloco{
				deveNovoBloco(Paragrafo, "ABSTRACT", confiancaEntradaBaixa, OrigemEstiloDocx, 0),
				deveNovoBloco(Paragrafo, "This study investigates undergraduate perception.", confiancaEntradaBaixa, OrigemEstiloDocx, 1),
			},
			esperado: []blocoEsperado{
				{papel: Paragrafo},
				{papel: Resumo, reclassificado: true},
			},
		},
		{
			nome: "referências: rótulo 'REFERÊNCIAS' com acento — a região roda até o fim do documento",
			entrada: []Bloco{
				deveNovoBloco(Secao(1), "REFERÊNCIAS", confiancaEntradaAlta, OrigemEstiloDocx, 0),
				deveNovoBloco(Paragrafo, "SEVERINO, Antônio Joaquim. Metodologia do trabalho científico.", confiancaEntradaBaixa, OrigemEstiloDocx, 1),
				deveNovoBloco(Paragrafo, "MARCONI, Marina de Andrade; LAKATOS, Eva Maria. Fundamentos.", confiancaEntradaBaixa, OrigemEstiloDocx, 2),
			},
			esperado: []blocoEsperado{
				{papel: Secao(1)}, // rótulo permanece; texto não bate com regra 5
				{papel: Referencia, reclassificado: true},
				{papel: Referencia, reclassificado: true},
			},
		},
		{
			nome: "referências: rótulo 'REFERENCIAS' sem acento também dispara a região",
			entrada: []Bloco{
				deveNovoBloco(Paragrafo, "REFERENCIAS", confiancaEntradaBaixa, OrigemEstiloDocx, 0),
				deveNovoBloco(Paragrafo, "Uma referência qualquer.", confiancaEntradaBaixa, OrigemEstiloDocx, 1),
			},
			esperado: []blocoEsperado{
				{papel: Paragrafo},
				{papel: Referencia, reclassificado: true},
			},
		},
		{
			nome: "referências: rótulo 'REFERENCES' em inglês também dispara a região",
			entrada: []Bloco{
				deveNovoBloco(Paragrafo, "REFERENCES", confiancaEntradaBaixa, OrigemEstiloDocx, 0),
				deveNovoBloco(Paragrafo, "Some reference here.", confiancaEntradaBaixa, OrigemEstiloDocx, 1),
			},
			esperado: []blocoEsperado{
				{papel: Paragrafo},
				{papel: Referencia, reclassificado: true},
			},
		},
		{
			nome: "palavras-chave vence a região do resumo (regra 3 > regra 1)",
			entrada: []Bloco{
				deveNovoBloco(Paragrafo, "RESUMO", confiancaEntradaBaixa, OrigemEstiloDocx, 0),
				deveNovoBloco(Paragrafo, "Texto do resumo.", confiancaEntradaBaixa, OrigemEstiloDocx, 1),
				deveNovoBloco(Paragrafo, "Palavras-chave: educação, ensino, tecnologia.", confiancaEntradaBaixa, OrigemEstiloDocx, 2),
			},
			esperado: []blocoEsperado{
				{papel: Paragrafo},
				{papel: Resumo, reclassificado: true},
				{papel: PalavrasChave, reclassificado: true}, // não Resumo
			},
		},
		{
			nome: "legenda vence a região de referências (regra 4 > regra 1/2), e a região continua depois da legenda",
			entrada: []Bloco{
				deveNovoBloco(Secao(1), "REFERÊNCIAS", confiancaEntradaAlta, OrigemEstiloDocx, 0),
				deveNovoBloco(Paragrafo, "Tabela 1 — Distribuição dos participantes por área de conhecimento", confiancaEntradaBaixa, OrigemEstiloDocx, 1),
				deveNovoBloco(Paragrafo, "Figura 2 — Outra legenda qualquer", confiancaEntradaBaixa, OrigemEstiloDocx, 2),
				deveNovoBloco(Paragrafo, "Quadro 3 — Mais uma legenda", confiancaEntradaBaixa, OrigemEstiloDocx, 3),
				deveNovoBloco(Paragrafo, "Fonte: elaborado pelos autores (2025).", confiancaEntradaBaixa, OrigemEstiloDocx, 4),
				deveNovoBloco(Paragrafo, "SEVERINO, Antônio Joaquim. Metodologia do trabalho científico.", confiancaEntradaBaixa, OrigemEstiloDocx, 5),
			},
			esperado: []blocoEsperado{
				{papel: Secao(1)},
				{papel: Legenda, reclassificado: true},
				{papel: Legenda, reclassificado: true},
				{papel: Legenda, reclassificado: true},
				{papel: Legenda, reclassificado: true},
				{papel: Referencia, reclassificado: true}, // legenda não é cabeçalho: não encerra a região
			},
		},
		{
			nome: "seção numerada dentro do resumo vence a região e a encerra para os blocos seguintes (regra 5 > regra 1)",
			entrada: []Bloco{
				deveNovoBloco(Paragrafo, "RESUMO", confiancaEntradaBaixa, OrigemEstiloDocx, 0),
				deveNovoBloco(Paragrafo, "Texto do resumo.", confiancaEntradaBaixa, OrigemEstiloDocx, 1),
				deveNovoBloco(Paragrafo, "3.1 Alguma seção numerada dentro do resumo", confiancaEntradaBaixa, OrigemEstiloDocx, 2),
				deveNovoBloco(Paragrafo, "Texto após a seção numerada, não é mais resumo.", confiancaEntradaBaixa, OrigemEstiloDocx, 3),
			},
			esperado: []blocoEsperado{
				{papel: Paragrafo},
				{papel: Resumo, reclassificado: true},
				{papel: Secao(2), reclassificado: true}, // "3.1" tem dois níveis
				{papel: Paragrafo},                      // a seção numerada encerrou a região
			},
		},
		{
			nome: "região de resumo termina ao encontrar outro rótulo de região (ABSTRACT), mesmo sem papel Secao",
			entrada: []Bloco{
				deveNovoBloco(Paragrafo, "RESUMO", confiancaEntradaBaixa, OrigemEstiloDocx, 0),
				deveNovoBloco(Paragrafo, "Texto do resumo.", confiancaEntradaBaixa, OrigemEstiloDocx, 1),
				deveNovoBloco(Paragrafo, "ABSTRACT", confiancaEntradaBaixa, OrigemEstiloDocx, 2),
				deveNovoBloco(Paragrafo, "This is the abstract text.", confiancaEntradaBaixa, OrigemEstiloDocx, 3),
			},
			esperado: []blocoEsperado{
				{papel: Paragrafo},
				{papel: Resumo, reclassificado: true},
				{papel: Paragrafo},                    // o segundo rótulo também não é tocado
				{papel: Resumo, reclassificado: true}, // nova região, aberta pelo ABSTRACT
			},
		},
	}

	for _, caso := range casos {
		caso := caso
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, len(caso.entrada), len(caso.esperado), "tabela de teste malformada: entrada e esperado precisam ter o mesmo tamanho")

			saida, err := AplicarHeuristica(caso.entrada)

			require.NoError(t, err)
			require.Len(t, saida, len(caso.entrada), "AplicarHeuristica precisa devolver a MESMA quantidade de blocos da entrada, na mesma ordem")

			for i := range caso.entrada {
				assertResultadoBloco(t, i, caso.entrada[i], saida[i], caso.esperado[i])

				// Fronteira: nenhum bloco devolvido pode ser algo que o
				// domínio recusaria — mesmo espírito de
				// TestClassificarPorEstiloDocxProduzBlocoValidoParaCDM.
				_, err := NovoBloco(saida[i].Papel, saida[i].TextoResumo, saida[i].Confianca, saida[i].Origem, saida[i].RefXML)
				assert.NoErrorf(t, err, "bloco %d devolvido por AplicarHeuristica não passaria em NovoBloco", i)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Regra 3 — Palavras-chave: prefixo, sem diferenciar maiúscula/minúscula
// ---------------------------------------------------------------------------

func TestAplicarHeuristicaPalavrasChaveReconhecidaSemDiferenciarCaixaEComOuSemHifen(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome  string
		texto string
	}{
		{nome: "hífen, caixa padrão", texto: "Palavras-chave: educação, ensino, tecnologia."},
		{nome: "hífen, minúsculo", texto: "palavras-chave: educação, ensino, tecnologia."},
		{nome: "hífen, maiúsculo", texto: "PALAVRAS-CHAVE: educação, ensino, tecnologia."},
		{nome: "sem hífen", texto: "Palavras chave: educação, ensino, tecnologia."},
		{nome: "keywords em inglês", texto: "Keywords: documentary standardization, academic writing."},
		{nome: "KEYWORDS maiúsculo", texto: "KEYWORDS: documentary standardization."},
	}

	for _, caso := range casos {
		caso := caso
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			entrada := deveNovoBloco(Paragrafo, caso.texto, confiancaEntradaBaixa, OrigemEstiloDocx, 0)
			saida := aplicarHeuristicaUnico(t, entrada)

			assertResultadoBloco(t, 0, entrada, saida, blocoEsperado{papel: PalavrasChave, reclassificado: true})
		})
	}
}

func TestAplicarHeuristicaPalavrasChaveExigePrefixoNaoBastaConterNoMeioDaFrase(t *testing.T) {
	t.Parallel()

	entrada := deveNovoBloco(Paragrafo, "O uso de palavras-chave é comum em artigos científicos.", confiancaEntradaBaixa, OrigemEstiloDocx, 0)
	saida := aplicarHeuristicaUnico(t, entrada)

	assertResultadoBloco(t, 0, entrada, saida, blocoEsperado{papel: Paragrafo})
}

// ---------------------------------------------------------------------------
// Regra 4 — Legenda: Tabela N / Figura N / Quadro N / Fonte:
// ---------------------------------------------------------------------------

func TestAplicarHeuristicaLegendaReconhecidaPorTipoEPorFonte(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome  string
		texto string
	}{
		{nome: "Tabela com travessão", texto: "Tabela 1 — Distribuição dos participantes por área de conhecimento"},
		{nome: "Figura com travessão", texto: "Figura 2 — Alguma legenda de figura"},
		{nome: "Quadro com travessão", texto: "Quadro 3 — Alguma legenda de quadro"},
		{nome: "Fonte", texto: "Fonte: elaborado pelos autores (2025)."},
	}

	for _, caso := range casos {
		caso := caso
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			entrada := deveNovoBloco(Paragrafo, caso.texto, confiancaEntradaBaixa, OrigemEstiloDocx, 0)
			saida := aplicarHeuristicaUnico(t, entrada)

			assertResultadoBloco(t, 0, entrada, saida, blocoEsperado{papel: Legenda, reclassificado: true})
		})
	}
}

func TestAplicarHeuristicaLegendaExigeNumeroInteiroLogoAposAPalavra(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome  string
		texto string
	}{
		{nome: "Tabela sem número imediatamente após", texto: "Tabela mostra os resultados agregados."},
		{nome: "Tabelas no plural não bate com o prefixo singular", texto: "Tabelas 1 e 2 mostram os resultados."},
		{nome: "Fonte sem dois-pontos não é reconhecida", texto: "Fonte dos dados: pesquisa de campo."},
	}

	for _, caso := range casos {
		caso := caso
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			entrada := deveNovoBloco(Paragrafo, caso.texto, confiancaEntradaBaixa, OrigemEstiloDocx, 0)
			saida := aplicarHeuristicaUnico(t, entrada)

			assertResultadoBloco(t, 0, entrada, saida, blocoEsperado{papel: Paragrafo})
		})
	}
}

// ---------------------------------------------------------------------------
// Regra 5 — Seção numerada sem estilo
// ---------------------------------------------------------------------------

func TestAplicarHeuristicaSecaoNumeradaContaNiveisPelaNumeracao(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome          string
		texto         string
		nivelEsperado int
	}{
		{nome: "nível único", texto: "1 INTRODUÇÃO", nivelEsperado: 1},
		{nome: "dois níveis", texto: "2.1 Instrumento de coleta", nivelEsperado: 2},
		{nome: "três níveis", texto: "3.2.4 Algo", nivelEsperado: 3},
		{nome: "número de dois dígitos em nível único", texto: "10 Numeração de dois dígitos", nivelEsperado: 1},
		{nome: "componente de dois dígitos num nível interno", texto: "1.10 Algo", nivelEsperado: 2},
	}

	for _, caso := range casos {
		caso := caso
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			entrada := deveNovoBloco(Paragrafo, caso.texto, confiancaEntradaBaixa, OrigemEstiloDocx, 0)
			saida := aplicarHeuristicaUnico(t, entrada)

			assertResultadoBloco(t, 0, entrada, saida, blocoEsperado{papel: Secao(caso.nivelEsperado), reclassificado: true})
		})
	}
}

func TestAplicarHeuristicaSecaoNumeradaExigeEspacoAposANumeracao(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome  string
		texto string
	}{
		{nome: "ponto sem espaço antes do texto", texto: "1.Introdução sem espaço"},
		{nome: "sem separador nenhum", texto: "1Introdução colada"},
		{nome: "número não está no início", texto: "Capítulo 1 Introdução"},
	}

	for _, caso := range casos {
		caso := caso
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			entrada := deveNovoBloco(Paragrafo, caso.texto, confiancaEntradaBaixa, OrigemEstiloDocx, 0)
			saida := aplicarHeuristicaUnico(t, entrada)

			assertResultadoBloco(t, 0, entrada, saida, blocoEsperado{papel: Paragrafo})
		})
	}
}

// TestAplicarHeuristicaSecaoAcimaDoNivelMaximoNaoReclassificaBlocoFicaComoEstava
// trava a regra explícita da ficha do investigador: nível acima de
// NivelSecaoMaximo não pode virar Secao — produziria um bloco que NovoBloco
// recusaria. O bloco tem que sair EXATAMENTE igual à entrada, sem erro.
func TestAplicarHeuristicaSecaoAcimaDoNivelMaximoNaoReclassificaBlocoFicaComoEstava(t *testing.T) {
	t.Parallel()

	require.Greater(t, 7, NivelSecaoMaximo, "pré-condição do teste: 7 níveis precisa estar acima do máximo suportado")

	entrada := deveNovoBloco(Paragrafo, "1.1.1.1.1.1.1 Sete níveis, acima do máximo suportado", confiancaEntradaBaixa, OrigemEstiloDocx, 0)
	saida := aplicarHeuristicaUnico(t, entrada)

	assert.Equal(t, entrada, saida, "nível acima de NivelSecaoMaximo não pode virar Secao: o bloco fica exatamente como estava, sem erro")
}

// ---------------------------------------------------------------------------
// Invariantes: fatia nova, nunca muta a entrada, nil/vazia não quebram
// ---------------------------------------------------------------------------

func TestAplicarHeuristicaFatiaNilNaoQuebra(t *testing.T) {
	t.Parallel()

	saida, err := AplicarHeuristica(nil)

	require.NoError(t, err)
	assert.Empty(t, saida)
}

func TestAplicarHeuristicaFatiaVaziaNaoQuebra(t *testing.T) {
	t.Parallel()

	saida, err := AplicarHeuristica([]Bloco{})

	require.NoError(t, err)
	assert.Empty(t, saida)
}

func TestAplicarHeuristicaNaoMutaAFatiaDeEntrada(t *testing.T) {
	t.Parallel()

	entrada := []Bloco{
		deveNovoBloco(Paragrafo, "RESUMO", confiancaEntradaBaixa, OrigemEstiloDocx, 0),
		deveNovoBloco(Paragrafo, "Texto do resumo.", confiancaEntradaBaixa, OrigemEstiloDocx, 1),
	}
	referencia := append([]Bloco(nil), entrada...) // cópia independente para comparar depois

	saida, err := AplicarHeuristica(entrada)

	require.NoError(t, err)
	require.Len(t, saida, 2)
	assert.NotEqual(t, referencia[1].Papel, saida[1].Papel,
		"pré-condição do teste: a camada 2 precisa ter reclassificado o segundo bloco para o teste valer alguma coisa")
	assert.Equal(t, referencia, entrada, "AplicarHeuristica não pode mutar a fatia recebida, mesmo tendo reclassificado o que devolveu")
}

// ---------------------------------------------------------------------------
// Proteção central: bloco de OrigemUsuario nunca é tocado, qualquer que seja
// a regra que casaria nele.
// ---------------------------------------------------------------------------

func TestAplicarHeuristicaBlocoDeOrigemUsuarioSaiIdenticoQualquerQueSejaARegraQueCasaria(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome  string
		texto string
	}{
		{nome: "texto que bateria com a região de resumo", texto: "RESUMO"},
		{nome: "texto que bateria com a região de referências", texto: "REFERÊNCIAS"},
		{nome: "texto que bateria com palavras-chave", texto: "Palavras-chave: educação, ensino."},
		{nome: "texto que bateria com legenda", texto: "Tabela 1 — Distribuição dos participantes."},
		{nome: "texto que bateria com seção numerada", texto: "1 INTRODUÇÃO"},
	}

	for _, caso := range casos {
		caso := caso
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			corrigidoPeloUsuario := deveNovoBloco(Paragrafo, caso.texto, 1.0, OrigemUsuario, 3)

			saida, err := AplicarHeuristica([]Bloco{corrigidoPeloUsuario})

			require.NoError(t, err)
			require.Len(t, saida, 1)
			assert.Equal(t, corrigidoPeloUsuario, saida[0],
				"bloco de OrigemUsuario precisa sair EXATAMENTE igual, campo por campo, mesmo que o texto bata com uma regra da camada 2")
		})
	}
}

// TestAplicarHeuristicaRotuloDeUsuarioAindaDelimitaOsVizinhos é uma decisão de
// desenho que o contrato não cobre explicitamente: Bloco.Reclassificar só
// protege o PRÓPRIO bloco corrigido pelo usuário — não impede que o TEXTO dele
// continue valendo como evidência para as regras de região aplicadas aos
// vizinhos. TextoResumo nunca muda (nem para bloco de usuário), então
// continua uma fonte de evidência válida.
func TestAplicarHeuristicaRotuloDeUsuarioAindaDelimitaOsVizinhos(t *testing.T) {
	t.Parallel()

	entrada := []Bloco{
		deveNovoBloco(Titulo, "RESUMO", 1.0, OrigemUsuario, 0), // usuário reclassificou o rótulo como Titulo
		deveNovoBloco(Paragrafo, "Texto que viria logo após o rótulo resumo.", confiancaEntradaBaixa, OrigemEstiloDocx, 1),
	}

	saida, err := AplicarHeuristica(entrada)

	require.NoError(t, err)
	require.Len(t, saida, 2)
	assert.Equal(t, entrada[0], saida[0], "o bloco corrigido pelo usuário não muda")
	assert.Equal(t, Resumo, saida[1].Papel,
		"o texto \"RESUMO\" do bloco-rótulo ainda vale como evidência para o vizinho, mesmo com o papel do rótulo sobrescrito pelo usuário")
	assert.Equal(t, OrigemHeuristica, saida[1].Origem)
}

// TestAplicarHeuristicaConcordarComACamada1NaoEnfraqueceOBloco trava uma
// decisão de desenho tomada na implementação, contra a leitura literal do
// contrato original ("bloco reclassificado pela camada 2 sai com
// OrigemHeuristica").
//
// Um cabeçalho com estilo nomeado Heading1 chega aqui como Secao(1) com
// confiança alta e OrigemEstiloDocx. O texto dele ("1 INTRODUÇÃO") também
// bate com a regra 5, que diria exatamente a mesma coisa. Reescrever o bloco
// nesse caso trocaria a origem forte pela fraca e derrubaria a confiança:
// duas evidências independentes CONCORDANDO sairiam valendo menos que uma
// sozinha. No artigo real do fixture isso atingiria seis cabeçalhos.
//
// Por isso a camada 2 só reclassifica quando o papel resultante DIFERE do que
// o bloco já tem. A regra continua valendo para delimitar região — ela é o
// que encerra o resumo no bloco seguinte.
func TestAplicarHeuristicaConcordarComACamada1NaoEnfraqueceOBloco(t *testing.T) {
	t.Parallel()

	entrada := []Bloco{
		deveNovoBloco(Paragrafo, "RESUMO", confiancaEntradaBaixa, OrigemEstiloDocx, 0),
		deveNovoBloco(Paragrafo, "Texto do resumo.", confiancaEntradaBaixa, OrigemEstiloDocx, 1),
		// Camada 1 já acertou pelo estilo nomeado; o texto concorda.
		deveNovoBloco(Secao(1), "1 INTRODUÇÃO", confiancaEntradaAlta, OrigemEstiloDocx, 2),
		deveNovoBloco(Paragrafo, "Primeiro parágrafo da introdução.", confiancaEntradaBaixa, OrigemEstiloDocx, 3),
	}

	saida, err := AplicarHeuristica(entrada)

	require.NoError(t, err)
	require.Len(t, saida, 4)

	assert.Equal(t, Resumo, saida[1].Papel, "pré-condição: a região do resumo precisa ter sido aberta")

	assert.Equal(t, entrada[2], saida[2],
		"camada 2 concordando com a camada 1 não pode reescrever o bloco: origem e confiança da evidência mais forte se mantêm")

	assert.Equal(t, Paragrafo, saida[3].Papel,
		"não reescrever o cabeçalho não pode impedi-lo de encerrar a região do resumo")
}
