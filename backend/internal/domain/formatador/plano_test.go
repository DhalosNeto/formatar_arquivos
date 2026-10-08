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
			Fonte:          "Times New Roman",
			TamanhoPT:      12,
			Entrelinha:     1.5,
			RecuoCM:        1.25,
			EspacoAntesPT:  0,
			EspacoDepoisPT: 6,
			Alinhamento:    "justificado",
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
