package postgres

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/daniel-halos/formatador/internal/domain/ruleset"
	"github.com/daniel-halos/formatador/internal/infra/errors"
	rulesetinfra "github.com/daniel-halos/formatador/internal/infra/ruleset"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSemearValidaLoteAntesDeAcessarPool(t *testing.T) {
	valida := ruleset.Definicao{Slug: "perfil", Versao: 1, Nome: "Sintético", Fonte: "https://example.org", Pagina: ruleset.Pagina{LarguraCM: 20, AlturaCM: 28}, Corpo: ruleset.Corpo{Fonte: "Sintética", TamanhoPT: 11, Entrelinha: 1.25, Alinhamento: "justificado"}}
	require.NoError(t, valida.Validar())
	casos := []struct {
		nome    string
		alterar func(*ruleset.Definicao)
	}{
		{"entidade_vazia", func(d *ruleset.Definicao) { *d = ruleset.Definicao{} }},
		{"slug_vazio", func(d *ruleset.Definicao) { d.Slug = "" }},
		{"versao_zero", func(d *ruleset.Definicao) { d.Versao = 0 }},
		{"nome_vazio", func(d *ruleset.Definicao) { d.Nome = "" }},
		{"fonte_vazia", func(d *ruleset.Definicao) { d.Fonte = "" }},
		{"pagina_vazia", func(d *ruleset.Definicao) { d.Pagina = ruleset.Pagina{} }},
		{"corpo_vazio", func(d *ruleset.Definicao) { d.Corpo = ruleset.Corpo{} }},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			invalida := valida
			caso.alterar(&invalida)
			require.Error(t, invalida.Validar())
			repo := &RepositorioRuleset{pool: nil}
			var err error
			require.NotPanics(t, func() { err = repo.Semear(context.Background(), []ruleset.Definicao{valida, invalida}) })
			var validacao *errors.ErroValidacao
			assert.ErrorAs(t, err, &validacao)
		})
	}
	t.Run("acima_de_4096", func(t *testing.T) {
		lote := make([]ruleset.Definicao, 4097)
		for i := range lote {
			lote[i] = valida
		}
		var err error
		require.NotPanics(t, func() { err = NovoRepositorioRuleset(nil).Semear(context.Background(), lote) })
		var validacao *errors.ErroValidacao
		assert.ErrorAs(t, err, &validacao)
	})
}

// ---------------------------------------------------------------------------
// Recorte f3-leitura-ruleset-por-id — C2 (decodificarRuleset), critérios A1-A6.
// ---------------------------------------------------------------------------

// definicaoSintetica é um perfil válido com TODOS os campos preenchidos com
// valor não zero: Equal contra um fixture cheio de zeros passaria mesmo se a
// decodificação perdesse campos aninhados.
func definicaoSintetica() ruleset.Definicao {
	return ruleset.Definicao{
		Slug:   "revista-sintetica",
		Versao: 3,
		Nome:   "Revista Sintética",
		Fonte:  "https://example.org/diretrizes",
		Pagina: ruleset.Pagina{
			LarguraCM: 21,
			AlturaCM:  29.7,
			Margens:   ruleset.Margens{SuperiorCM: 3, InferiorCM: 2, EsquerdaCM: 3, DireitaCM: 2},
		},
		Corpo: ruleset.Corpo{
			Fonte:          "Fonte Sintética",
			TamanhoPT:      12,
			Entrelinha:     1.5,
			RecuoCM:        1.25,
			EspacoAntesPT:  6,
			EspacoDepoisPT: 12,
			Alinhamento:    "justificado",
		},
	}
}

// vocabularioInvalido é o conjunto FECHADO de nomes de campo que invalido()
// (internal/domain/ruleset/definicao.go:95) pode produzir. A ordem importa:
// "corpo", "pagina" e "fonte" são substring de nomes mais longos, então o nome
// longo tem de ser consumido primeiro para a extração não contar duas vezes.
var vocabularioInvalido = []string{
	"pagina.margens", "corpo.alinhamento", "corpo.tamanho", "corpo.recuo", "corpo.fonte",
	"versao", "pagina", "corpo", "fonte", "slug", "nome",
}

// camposMencionados extrai de uma mensagem quais nomes do vocabulário fechado
// ela cita. A asserção dos testes é de PERTENCIMENTO a esse conjunto, não de
// substring solta: comparar o conjunto extraído com o esperado pega tanto a
// omissão do nome do campo quanto a citação de um campo a mais.
func camposMencionados(mensagem string) []string {
	encontrados := []string{}
	restante := mensagem
	for _, campo := range vocabularioInvalido {
		if strings.Contains(restante, campo) {
			encontrados = append(encontrados, campo)
			restante = strings.ReplaceAll(restante, campo, "\x00")
		}
	}
	sort.Strings(encontrados)
	return encontrados
}

// mensagensDaCadeia devolve o texto de cada elo alcançável por Unwrap, mais a
// formatação verbosa do topo. ErroEnvolvido.Error() já interpola o original com
// %v, então um vazamento em elo profundo aparece também no topo.
func mensagensDaCadeia(err error) []string {
	var mensagens []string
	for atual := err; atual != nil; atual = stderrors.Unwrap(atual) {
		mensagens = append(mensagens, atual.Error())
	}
	return append(mensagens, fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err))
}

func erroAplicacaoDe(t *testing.T, err error) *errors.ErroAplicacao {
	t.Helper()
	require.Error(t, err)
	var aplicacao *errors.ErroAplicacao
	require.True(t, errors.Como(err, &aplicacao), "esperado *errors.ErroAplicacao (500), obtido %T", err)
	var validacao *errors.ErroValidacao
	require.False(t, errors.Como(err, &validacao),
		"*ErroValidacao na cadeia faz rotasutil.Classificar responder 400 com os nomes de campo em razoes")
	return aplicacao
}

// A1: bytes de json.Marshal de um perfil válido — exatamente o que
// prepararRulesets grava na coluna — voltam campo por campo.
func TestDecodificarRulesetDevolveDefinicaoCompleta(t *testing.T) {
	esperada := definicaoSintetica()
	require.NoError(t, esperada.Validar())
	for nome, valor := range map[string]float64{
		"margem superior": esperada.Pagina.Margens.SuperiorCM,
		"margem inferior": esperada.Pagina.Margens.InferiorCM,
		"margem esquerda": esperada.Pagina.Margens.EsquerdaCM,
		"margem direita":  esperada.Pagina.Margens.DireitaCM,
		"tamanho":         esperada.Corpo.TamanhoPT,
		"entrelinha":      esperada.Corpo.Entrelinha,
		"recuo":           esperada.Corpo.RecuoCM,
		"espaço antes":    esperada.Corpo.EspacoAntesPT,
		"espaço depois":   esperada.Corpo.EspacoDepoisPT,
	} {
		require.NotZero(t, valor, "fixture com %s zerada tornaria o Equal trivialmente verdadeiro", nome)
	}
	dados, err := json.Marshal(esperada)
	require.NoError(t, err)

	decodificada, err := decodificarRuleset(dados)

	require.NoError(t, err)
	assert.Equal(t, esperada, decodificada)
	assert.Equal(t, esperada.Pagina.Margens, decodificada.Pagina.Margens)
	assert.Equal(t, esperada.Corpo, decodificada.Corpo)
}

// A2: bytes que não produzem objeto utilizável. Os casos se dividem pelas DUAS
// origens de falha de C2: `null` e `{}` NÃO falham no json.Unmarshal (devolvem
// Definicao zero) e só são recusados por Validar; os demais falham no unmarshal
// ou no teto. `null` é alcançável na prática: definicao jsonb NOT NULL aceita
// 'null'::jsonb.
func TestDecodificarRulesetRecusaBytesInutilizaveis(t *testing.T) {
	casos := []struct {
		nome   string
		dados  []byte
		origem string
		campos []string
	}{
		{"fatia_nil", nil, "teto", nil},
		{"fatia_vazia", []byte{}, "teto", nil},
		{"string_json", []byte(`"texto"`), "unmarshal", nil},
		{"array_json", []byte(`[]`), "unmarshal", nil},
		{"objeto_truncado", []byte(`{`), "unmarshal", nil},
		{"literal_null", []byte(`null`), "validar", []string{"slug"}},
		{"objeto_vazio", []byte(`{}`), "validar", []string{"slug"}},
	}
	mensagemPorOrigem := map[string]map[string][]string{}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			definicao, err := decodificarRuleset(caso.dados)

			aplicacao := erroAplicacaoDe(t, err)
			assert.Zero(t, definicao, "não devolver definição aproveitável junto do erro")
			assert.ElementsMatch(t, caso.campos, camposMencionados(aplicacao.Mensagem),
				"mensagem %q cita campos fora do esperado para a origem %q", aplicacao.Mensagem, caso.origem)
			if mensagemPorOrigem[caso.origem] == nil {
				mensagemPorOrigem[caso.origem] = map[string][]string{}
			}
			mensagemPorOrigem[caso.origem][aplicacao.Mensagem] = append(mensagemPorOrigem[caso.origem][aplicacao.Mensagem], caso.nome)
		})
	}
	// C2 exige mensagem FIXA para falha de unmarshal: json.SyntaxError cita um
	// caractere do dado e json.UnmarshalTypeError cita Definicao.<campo>.
	assert.Len(t, mensagemPorOrigem["unmarshal"], 1,
		"falha de unmarshal tem de usar uma mensagem fixa, não derivada do dado: %v", mensagemPorOrigem["unmarshal"])
}

// A3: objeto bem formado e semanticamente inválido. A cobertura termo por termo
// de Definicao.Validar() já existe em domain/ruleset/definicao_test.go:102,126 e
// NÃO é duplicada aqui: a decisão composta de C2 tem UM termo (qualquer erro de
// Validar vira ErroAplicacao), então um caso por família basta.
func TestDecodificarRulesetReclassificaErroDeValidar(t *testing.T) {
	casos := []struct {
		nome    string
		alterar func(*ruleset.Definicao)
		campo   string
		valores []string
	}{
		{"metadados_slug_fora_do_kebab", func(d *ruleset.Definicao) { d.Slug = "Slug_Invalido" }, "slug", []string{"Slug_Invalido"}},
		{"pagina_margens_nao_cabem_na_largura", func(d *ruleset.Definicao) {
			d.Pagina.Margens.EsquerdaCM, d.Pagina.Margens.DireitaCM = 11, 11
		}, "pagina.margens", []string{"11"}},
		{"corpo_alinhamento_desconhecido", func(d *ruleset.Definicao) { d.Corpo.Alinhamento = "diagonal" }, "corpo.alinhamento", []string{"diagonal"}},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			invalida := definicaoSintetica()
			caso.alterar(&invalida)
			require.Error(t, invalida.Validar(), "pré-condição: o fixture tem de reprovar em Validar")
			dados, err := json.Marshal(invalida)
			require.NoError(t, err)

			_, err = decodificarRuleset(dados)

			aplicacao := erroAplicacaoDe(t, err)
			assert.Equal(t, []string{caso.campo}, camposMencionados(aplicacao.Mensagem),
				"a mensagem tem de citar exatamente %q, do conjunto fechado de invalido()", caso.campo)
			for _, valor := range caso.valores {
				assert.NotContains(t, aplicacao.Mensagem, valor, "valor lido do documento não vai para mensagem de erro")
			}
			// C2: interpolar SOMENTE Campos[0].Campo, nunca Campos[0].Mensagem.
			assert.NotContains(t, aplicacao.Mensagem, "definição de ruleset inválida")
			assert.NotContains(t, aplicacao.Mensagem, "requisição inválida")
		})
	}
}

// A4: campo desconhecido é ignorado (sem DisallowUnknownFields) — a leniência é
// deliberada, porque o que quebraria é ler linha NOVA com binário ANTIGO.
func TestDecodificarRulesetIgnoraCampoDesconhecido(t *testing.T) {
	esperada := definicaoSintetica()
	dados, err := json.Marshal(esperada)
	require.NoError(t, err)
	comExtra := strings.Replace(string(dados), `{`, `{"campo_desconhecido":{"a":[1,2]},`, 1)
	require.NotEqual(t, string(dados), comExtra, "pré-condição: o campo extra tem de ter sido injetado")

	decodificada, err := decodificarRuleset([]byte(comExtra))

	require.NoError(t, err)
	assert.Equal(t, esperada, decodificada)

	t.Run("objeto_vazio_recusado_por_validar", func(t *testing.T) {
		_, err := decodificarRuleset([]byte(`{}`))
		aplicacao := erroAplicacaoDe(t, err)
		assert.Equal(t, []string{"slug"}, camposMencionados(aplicacao.Mensagem))
	})
}

// A5: as três origens de falha, com Nome e Fonte marcados. Nenhum elo da cadeia
// pode vazar a marca nem conter *ErroValidacao, *json.SyntaxError ou
// *json.UnmarshalTypeError — estes últimos citam caractere do dado e
// `Definicao.<campo>`, e assertar a ausência impede que um %w futuro reabra o canal.
func TestDecodificarRulesetNaoVazaConteudoNemReabreCanal(t *testing.T) {
	const marca = "MARCA-UNICA-7f3ab219"
	base := definicaoSintetica()
	base.Nome = "Revista " + marca
	base.Fonte = "https://example.org/" + marca
	require.NoError(t, base.Validar())
	validos, err := json.Marshal(base)
	require.NoError(t, err)
	require.Contains(t, string(validos), marca, "pré-condição: a marca tem de estar no payload")

	gigante := base
	gigante.Nome = marca + strings.Repeat("x", limiteDefinicaoBytes)
	acimaDoTeto, err := json.Marshal(gigante)
	require.NoError(t, err)
	require.Greater(t, len(acimaDoTeto), limiteDefinicaoBytes)

	reprovada := base
	reprovada.Slug = "Slug_" + marca
	reprovados, err := json.Marshal(reprovada)
	require.NoError(t, err)
	require.Error(t, reprovada.Validar())

	tipoErrado := strings.Replace(string(validos), `"versao":3`, `"versao":"tres"`, 1)
	require.NotEqual(t, string(validos), tipoErrado, "pré-condição: o tipo de versao tem de ter sido corrompido")

	casos := []struct {
		nome  string
		dados []byte
	}{
		{"teto", acimaDoTeto},
		{"unmarshal_sintaxe", validos[:len(validos)-1]},
		{"unmarshal_tipo", []byte(tipoErrado)},
		{"validar", reprovados},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			_, err := decodificarRuleset(caso.dados)

			erroAplicacaoDe(t, err)
			for i, mensagem := range mensagensDaCadeia(err) {
				assert.NotContains(t, mensagem, marca, "elo %d da cadeia vaza conteúdo do documento", i)
			}
			var sintaxe *json.SyntaxError
			assert.False(t, errors.Como(err, &sintaxe), "*json.SyntaxError na cadeia cita um caractere do dado")
			var tipo *json.UnmarshalTypeError
			assert.False(t, errors.Como(err, &tipo), "*json.UnmarshalTypeError na cadeia cita Definicao.<campo>")
		})
	}
}

// A6: a SEGUNDA guarda de tamanho, a da função pura. NÃO prova proteção de
// memória do worker: Scan em json.RawMessage já materializou o payload antes de
// decodificarRuleset ver um byte. Quem protege o worker é o CASE/octet_length do
// SQL de C3, coberto por A14.
func TestDecodificarRulesetTetoDeTamanho(t *testing.T) {
	// Amarra o teto da leitura ao teto da ENTRADA, e não a um literal: subir
	// TamanhoMaximoBytes e semear um perfil maior que 65536 faria toda
	// ObterPorID daquela linha devolver 500, e um literal igual a si mesmo não
	// acusaria nada. O import de infra aqui é só de teste.
	require.Equal(t, rulesetinfra.TamanhoMaximoBytes, limiteDefinicaoBytes,
		"o teto da leitura tem de acompanhar o teto da entrada em infra/ruleset")
	esperada := definicaoSintetica()
	base, err := json.Marshal(esperada)
	require.NoError(t, err)
	// Preenchimento em campo desconhecido: A4 garante que ele é ignorado, então
	// o payload continua decodificando para a mesma Definicao.
	montar := func(t *testing.T, total int) []byte {
		t.Helper()
		prefixo := string(base[:len(base)-1]) + `,"preenchimento":"`
		const sufixo = `"}`
		enchimento := total - len(prefixo) - len(sufixo)
		require.Positive(t, enchimento)
		dados := []byte(prefixo + strings.Repeat("a", enchimento) + sufixo)
		require.Len(t, dados, total)
		return dados
	}

	t.Run("zero_bytes_recusado", func(t *testing.T) {
		_, err := decodificarRuleset([]byte{})
		erroAplicacaoDe(t, err)
	})

	t.Run("exatamente_no_teto_decodifica", func(t *testing.T) {
		decodificada, err := decodificarRuleset(montar(t, limiteDefinicaoBytes))
		require.NoError(t, err, "o teto é `acima de`, não `a partir de`")
		assert.Equal(t, esperada, decodificada)
	})

	t.Run("um_byte_acima_do_teto_recusado", func(t *testing.T) {
		_, err := decodificarRuleset(montar(t, limiteDefinicaoBytes+1))
		erroAplicacaoDe(t, err)
	})

	// Ordem observável: um payload acima do teto e um payload acima do teto que
	// nem é JSON têm de produzir a MESMA mensagem; se o unmarshal viesse antes,
	// o segundo receberia a mensagem fixa de falha de unmarshal.
	t.Run("teto_decide_antes_do_unmarshal", func(t *testing.T) {
		lixoGrande := []byte(strings.Repeat("A", limiteDefinicaoBytes+1))
		_, err := decodificarRuleset(lixoGrande)
		grandeInvalido := erroAplicacaoDe(t, err).Mensagem
		_, err = decodificarRuleset(montar(t, limiteDefinicaoBytes+1))
		grandeValido := erroAplicacaoDe(t, err).Mensagem
		_, err = decodificarRuleset([]byte("AAAA"))
		pequenoInvalido := erroAplicacaoDe(t, err).Mensagem

		assert.Equal(t, grandeValido, grandeInvalido, "o teto tem de decidir antes de olhar a sintaxe")
		assert.NotEqual(t, pequenoInvalido, grandeInvalido, "teto e unmarshal não podem ser indistinguíveis")
	})
}
