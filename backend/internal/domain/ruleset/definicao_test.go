package ruleset

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	erros "github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// apontar devolve o endereço de um literal. É o que permite escrever, no
// mesmo struct, "a norma manda X" (ponteiro) e "a norma é silente"  (nil) —
// a distinção que os cinco campos opcionais de Corpo carregam (C1).
func apontar[T any](valor T) *T { return &valor }

func definicaoSintetica() Definicao {
	return Definicao{Slug: "perfil-sintetico", Versao: 1, Nome: "Perfil sintético de teste", Fonte: "https://example.org/perfil",
		Pagina: Pagina{LarguraCM: 20, AlturaCM: 28, Margens: Margens{SuperiorCM: 2, InferiorCM: 2, EsquerdaCM: 2, DireitaCM: 2}},
		Corpo:  Corpo{Fonte: apontar("Fonte Sintética"), TamanhoPT: 11, Entrelinha: 1.25, Alinhamento: apontar("justificado")}}
}

// corpoSilente é o Corpo de um perfil que só declara o que a ABNT NBR
// 14724:2024 define: tamanho (§5.1) e entrelinha (§5.2). Os cinco demais ficam
// nil — a norma não os diz.
func corpoSilente() Corpo {
	return Corpo{TamanhoPT: 12, Entrelinha: 1.5}
}

func exigirCampoInvalido(t *testing.T, err error, campo string) {
	t.Helper()
	var alvo *erros.ErroValidacao
	require.Error(t, err)
	require.ErrorAs(t, err, &alvo)
	require.NotEmpty(t, alvo.Campos)
	assert.Equal(t, campo, alvo.Campos[0].Campo)
}

// A1 — os cinco campos silentes: nil é válido, presente e válido é válido,
// presente e INVÁLIDO é recusado no campo correspondente. Um termo por caso:
// remover a validação de um deles quebra exatamente um subteste, e tratar nil
// como valor quebra exatamente o subteste "cinco nil".
func TestDefinicaoCamposSilentesAceitamNilEValidamQuandoPresentes(t *testing.T) {
	base := definicaoSintetica()
	base.Corpo = corpoSilente()
	require.NoError(t, base.Validar(), "A1: perfil com os cinco campos silentes nil é válido")

	presentes := []struct {
		nome    string
		definir func(*Corpo)
	}{
		{"fonte", func(c *Corpo) { c.Fonte = apontar("Fonte Sintética") }},
		{"alinhamento", func(c *Corpo) { c.Alinhamento = apontar("justificado") }},
		{"recuo_cm", func(c *Corpo) { c.RecuoCM = apontar(1.25) }},
		{"espaco_antes_pt", func(c *Corpo) { c.EspacoAntesPT = apontar(6.0) }},
		{"espaco_depois_pt", func(c *Corpo) { c.EspacoDepoisPT = apontar(6.0) }},
	}
	t.Run("cada um presente e válido", func(t *testing.T) {
		for _, caso := range presentes {
			t.Run(caso.nome, func(t *testing.T) {
				d := definicaoSintetica()
				d.Corpo = corpoSilente()
				caso.definir(&d.Corpo)
				assert.NoError(t, d.Validar())
			})
		}
	})
	t.Run("todos presentes e válidos", func(t *testing.T) {
		d := definicaoSintetica()
		d.Corpo = corpoSilente()
		for _, caso := range presentes {
			caso.definir(&d.Corpo)
		}
		assert.NoError(t, d.Validar())
	})

	// Cada entrada abaixo é um TERMO da decisão composta de Corpo.validar:
	// o campo está PRESENTE e o valor é inválido.
	invalidos := []struct {
		nome    string
		campo   string
		definir func(*Corpo)
	}{
		{"fonte vazia", "corpo.fonte", func(c *Corpo) { c.Fonte = apontar("") }},
		{"fonte com controle", "corpo.fonte", func(c *Corpo) { c.Fonte = apontar("F\x01") }},
		{"alinhamento desconhecido", "corpo.alinhamento", func(c *Corpo) { c.Alinhamento = apontar("centro") }},
		{"alinhamento vazio", "corpo.alinhamento", func(c *Corpo) { c.Alinhamento = apontar("") }},
		{"recuo negativo", "corpo.recuo", func(c *Corpo) { c.RecuoCM = apontar(-0.001) }},
		{"recuo que não cabe em twips", "corpo.recuo", func(c *Corpo) {
			c.RecuoCM = apontar((float64(math.MaxInt32) + 1) * 2.54 / 1440)
		}},
		{"recuo nan", "corpo.recuo", func(c *Corpo) { c.RecuoCM = apontar(math.NaN()) }},
		{"espaco antes negativo", "corpo", func(c *Corpo) { c.EspacoAntesPT = apontar(-0.001) }},
		{"espaco antes infinito", "corpo", func(c *Corpo) { c.EspacoAntesPT = apontar(math.Inf(1)) }},
		{"espaco depois negativo", "corpo", func(c *Corpo) { c.EspacoDepoisPT = apontar(-0.001) }},
		{"espaco depois overflow", "corpo", func(c *Corpo) { c.EspacoDepoisPT = apontar((float64(math.MaxInt32) + 1) / 20) }},
	}
	for _, caso := range invalidos {
		t.Run("presente e inválido/"+caso.nome, func(t *testing.T) {
			d := definicaoSintetica()
			d.Corpo = corpoSilente()
			caso.definir(&d.Corpo)
			exigirCampoInvalido(t, d.Validar(), caso.campo)
		})
	}
}

// A2 — ponteiro para 0 é valor legítimo nos três numéricos.
//
// Este critério prova SÓ isso. A distinção entre ponteiro-para-0 e nil NÃO é
// observável aqui: Validar() devolve nil nos dois casos, inclusive numa
// implementação que colapsasse nil em 0 na entrada. Quem prova a distinção é
// A13, no XML (ooxml/formatar_test.go).
func TestDefinicaoAceitaPonteiroParaZeroNosTresNumericos(t *testing.T) {
	d := definicaoSintetica()
	d.Corpo = corpoSilente()
	d.Corpo.RecuoCM = apontar(0.0)
	d.Corpo.EspacoAntesPT = apontar(0.0)
	d.Corpo.EspacoDepoisPT = apontar(0.0)

	require.NotNil(t, d.Corpo.RecuoCM, "pré-condição: o campo precisa estar PRESENTE, não nil")
	require.NotNil(t, d.Corpo.EspacoAntesPT)
	require.NotNil(t, d.Corpo.EspacoDepoisPT)
	assert.NoError(t, d.Validar())
}

// A3 — tamanho e entrelinha NÃO são opcionais: a norma os define (§5.1, §5.2).
func TestDefinicaoExigeTamanhoEEntrelinha(t *testing.T) {
	for _, caso := range []struct {
		nome    string
		campo   string
		definir func(*Corpo)
	}{
		{"tamanho ausente", "corpo.tamanho", func(c *Corpo) { c.TamanhoPT = 0 }},
		{"tamanho negativo", "corpo.tamanho", func(c *Corpo) { c.TamanhoPT = -1 }},
		{"tamanho arredonda a zero", "corpo.tamanho", func(c *Corpo) { c.TamanhoPT = 0.1 }},
		{"entrelinha ausente", "corpo", func(c *Corpo) { c.Entrelinha = 0 }},
		{"entrelinha negativa", "corpo", func(c *Corpo) { c.Entrelinha = -1 }},
		{"entrelinha arredonda a zero", "corpo", func(c *Corpo) { c.Entrelinha = 0.001 }},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			d := definicaoSintetica()
			d.Corpo = corpoSilente()
			caso.definir(&d.Corpo)
			exigirCampoInvalido(t, d.Validar(), caso.campo)
		})
	}
	t.Run("corpo sem nenhum dos dois", func(t *testing.T) {
		d := definicaoSintetica()
		d.Corpo = Corpo{}
		assert.Error(t, d.Validar(), "A3: Corpo zerado não pode valer como 'tudo silente'")
	})
}

func TestDefinicaoNumeros(t *testing.T) {
	// Os três últimos campos são PONTEIRO (C1): o caso os preenche com
	// ponteiro para o valor, porque é o estado "presente" que a validação
	// numérica precisa exercitar. Nil é A1.
	campos := []struct {
		nome     string
		definir  func(*Definicao, float64)
		positivo bool
		fator    float64
	}{
		{"largura", func(d *Definicao, v float64) { d.Pagina.LarguraCM = v }, true, 1440.0 / 2.54},
		{"altura", func(d *Definicao, v float64) { d.Pagina.AlturaCM = v }, true, 1440.0 / 2.54},
		{"superior", func(d *Definicao, v float64) { d.Pagina.Margens.SuperiorCM = v }, false, 1440.0 / 2.54},
		{"inferior", func(d *Definicao, v float64) { d.Pagina.Margens.InferiorCM = v }, false, 1440.0 / 2.54},
		{"esquerda", func(d *Definicao, v float64) { d.Pagina.Margens.EsquerdaCM = v }, false, 1440.0 / 2.54},
		{"direita", func(d *Definicao, v float64) { d.Pagina.Margens.DireitaCM = v }, false, 1440.0 / 2.54},
		{"tamanho", func(d *Definicao, v float64) { d.Corpo.TamanhoPT = v }, true, 2},
		{"entrelinha", func(d *Definicao, v float64) { d.Corpo.Entrelinha = v }, true, 240},
		{"recuo", func(d *Definicao, v float64) { d.Corpo.RecuoCM = &v }, false, 1440.0 / 2.54},
		{"antes", func(d *Definicao, v float64) { d.Corpo.EspacoAntesPT = &v }, false, 20},
		{"depois", func(d *Definicao, v float64) { d.Corpo.EspacoDepoisPT = &v }, false, 20},
	}
	for _, campo := range campos {
		t.Run(campo.nome, func(t *testing.T) {
			casos := []struct {
				nome  string
				valor float64
				erro  bool
			}{
				{"nan", math.NaN(), true}, {"infinito positivo", math.Inf(1), true}, {"infinito negativo", math.Inf(-1), true}, {"negativo", -0.001, true},
				{"overflow", (float64(math.MaxInt32) + 1) / campo.fator, true}, {"zero", 0, campo.positivo}, {"arredonda zero", 0.1 / campo.fator, campo.positivo},
				{"um na unidade final", 1 / campo.fator, false},
			}
			for _, caso := range casos {
				t.Run(caso.nome, func(t *testing.T) {
					d := definicaoSintetica()
					d.Pagina.Margens = Margens{}
					campo.definir(&d, caso.valor)
					err := d.Validar()
					if caso.erro {
						require.Error(t, err)
						var alvo *erros.ErroValidacao
						assert.ErrorAs(t, err, &alvo)
					} else {
						assert.NoError(t, err)
					}
				})
			}
		})
	}
}

func TestDefinicaoTextoEVersao(t *testing.T) {
	campos := []struct {
		nome    string
		definir func(*Definicao, string)
		limite  int
	}{
		{"slug", func(d *Definicao, v string) { d.Slug = v }, 80},
		{"nome", func(d *Definicao, v string) { d.Nome = v }, 200},
		// Corpo.Fonte é ponteiro (C1): aqui ele está PRESENTE e o valor é que
		// está sob teste. O caso nil é A1.
		{"fonte corpo", func(d *Definicao, v string) { d.Corpo.Fonte = &v }, 100},
	}
	for _, campo := range campos {
		for _, caso := range []struct {
			nome, valor string
			erro        bool
		}{
			{"vazio", "", true}, {"brancos", " \t\n", true}, {"controle", "abc\x00def", true}, {"controle interno", "abc\ndef", true},
			{"limite", strings.Repeat("a", campo.limite), false}, {"acima limite", strings.Repeat("a", campo.limite+1), true},
		} {
			t.Run(campo.nome+"/"+caso.nome, func(t *testing.T) {
				d := definicaoSintetica()
				campo.definir(&d, caso.valor)
				if caso.erro {
					assert.Error(t, d.Validar())
				} else {
					assert.NoError(t, d.Validar())
				}
			})
		}
	}
	for _, slug := range []string{"Maiusculo", "com_underscore", "-inicio", "fim-", "dois--hifens", "com espaço", "acentuação"} {
		t.Run(slug, func(t *testing.T) { d := definicaoSintetica(); d.Slug = slug; assert.Error(t, d.Validar()) })
	}
	for _, versao := range []int{0, -1, 1, math.MaxInt32, math.MaxInt32 + 1} {
		d := definicaoSintetica()
		d.Versao = versao
		if versao < 1 || versao > math.MaxInt32 {
			assert.Error(t, d.Validar())
		} else {
			assert.NoError(t, d.Validar())
		}
	}
	for _, alinhamento := range []string{"esquerda", "direita", "centralizado", "justificado", "", "centro", "Justificado", " justificado"} {
		t.Run("alinhamento/"+alinhamento, func(t *testing.T) {
			d := definicaoSintetica()
			d.Corpo.Alinhamento = &alinhamento
			if alinhamento == "esquerda" || alinhamento == "direita" || alinhamento == "centralizado" || alinhamento == "justificado" {
				assert.NoError(t, d.Validar())
			} else {
				assert.Error(t, d.Validar())
			}
		})
	}
	for _, caso := range []struct {
		nome, valor string
		erro        bool
	}{
		{"https", "https://example.org/perfil", false}, {"http", "http://example.org/perfil", false}, {"vazia", "", true}, {"relativa", "/perfil", true}, {"sem host", "https:///perfil", true}, {"outro esquema", "ftp://example.org", true}, {"usuario", "https://usuario@example.org", true}, {"senha", "https://usuario:senha@example.org", true}, {"controle", "https://example.org/\n", true}, {"branco", "https://example.org/a b", true},
		{"limite", "https://example.org/" + strings.Repeat("a", 2048-len("https://example.org/")), false},
		{"acima limite", "https://example.org/" + strings.Repeat("a", 2049-len("https://example.org/")), true},
	} {
		t.Run("fonte/"+caso.nome, func(t *testing.T) {
			d := definicaoSintetica()
			d.Fonte = caso.valor
			if caso.erro {
				assert.Error(t, d.Validar())
			} else {
				assert.NoError(t, d.Validar())
			}
		})
	}
	d := definicaoSintetica()
	d.Nome = "Pesquisa 漢字 😀 cafe\u0301"
	d.Corpo.Fonte = apontar("Sintética 漢字")
	assert.NoError(t, d.Validar())
	assert.Error(t, (Definicao{}).Validar())
}

func TestDefinicaoSomaMargens(t *testing.T) {
	for _, eixo := range []string{"horizontal", "vertical"} {
		for _, caso := range []struct {
			nome           string
			dimensao, a, b float64
			erro           bool
		}{
			{"menor", 20, 2, 2, false}, {"igual", 20, 10, 10, true}, {"maior", 20, 11, 10, true},
			{"igual apenas apos arredondar", 20.4 * 2.54 / 1440, 10.2 * 2.54 / 1440, 10.1 * 2.54 / 1440, true},
		} {
			t.Run(eixo+"/"+caso.nome, func(t *testing.T) {
				d := definicaoSintetica()
				if eixo == "horizontal" {
					d.Pagina.LarguraCM = caso.dimensao
					d.Pagina.Margens.EsquerdaCM = caso.a
					d.Pagina.Margens.DireitaCM = caso.b
				} else {
					d.Pagina.AlturaCM = caso.dimensao
					d.Pagina.Margens.SuperiorCM = caso.a
					d.Pagina.Margens.InferiorCM = caso.b
				}
				if caso.erro {
					assert.Error(t, d.Validar())
				} else {
					assert.NoError(t, d.Validar())
				}
			})
		}
	}
}

func TestDefinicaoSerializacaoSnakeCase(t *testing.T) {
	const esperado = `{"slug":"perfil-sintetico","versao":1,"nome":"Perfil sintético de teste","fonte":"https://example.org/perfil","pagina":{"largura_cm":20,"altura_cm":28,"margens":{"superior_cm":2,"inferior_cm":2,"esquerda_cm":2,"direita_cm":2}},"corpo":{"fonte":"Fonte Sintética","tamanho_pt":11,"entrelinha":1.25,"recuo_cm":0,"espaco_antes_pt":0,"espaco_depois_pt":0,"alinhamento":"justificado"}}`
	d := definicaoSintetica()
	d.Corpo.RecuoCM = apontar(0.0)
	d.Corpo.EspacoAntesPT = apontar(0.0)
	d.Corpo.EspacoDepoisPT = apontar(0.0)
	dados, err := json.Marshal(d)
	require.NoError(t, err)
	assert.JSONEq(t, esperado, string(dados))
	dados, err = yaml.Marshal(d)
	require.NoError(t, err)
	var objeto map[string]any
	require.NoError(t, yaml.Unmarshal(dados, &objeto))
	dados, err = json.Marshal(objeto)
	require.NoError(t, err)
	assert.JSONEq(t, esperado, string(dados))
}

// A1/C1 — tag: nil tem de produzir chave AUSENTE em json E em yaml.
//
// Não é detalhe de serialização: prepararRulesets faz json.Marshal + sha256 e
// inserirRuleset compara definicao=$4::jsonb para impor imutabilidade
// (data/postgres/ruleset.go:42-66, :112-130). {"fonte":null} é jsonb
// DIFERENTE de chave ausente, e o re-seed de abnt-14724/v1 bateria em
// ErroConflito contra uma versão imutável. A15 fecha isso no banco real.
func TestDefinicaoCamposSilentesNilNaoAparecemNoJSONNemNoYAML(t *testing.T) {
	const esperado = `{"slug":"perfil-sintetico","versao":1,"nome":"Perfil sintético de teste","fonte":"https://example.org/perfil","pagina":{"largura_cm":20,"altura_cm":28,"margens":{"superior_cm":2,"inferior_cm":2,"esquerda_cm":2,"direita_cm":2}},"corpo":{"tamanho_pt":12,"entrelinha":1.5}}`
	d := definicaoSintetica()
	d.Corpo = corpoSilente()

	dados, err := json.Marshal(d)
	require.NoError(t, err)
	assert.JSONEq(t, esperado, string(dados))
	for _, chave := range []string{"fonte", "alinhamento", "recuo_cm", "espaco_antes_pt", "espaco_depois_pt"} {
		assert.NotContains(t, string(dados), `"corpo":{"`+chave+`"`, "chave %q não pode aparecer no corpo", chave)
	}

	dados, err = yaml.Marshal(d)
	require.NoError(t, err)
	var objeto map[string]any
	require.NoError(t, yaml.Unmarshal(dados, &objeto))
	corpo, ok := objeto["corpo"].(map[string]any)
	require.True(t, ok, "corpo precisa ser um mapa no YAML")
	assert.Equal(t, []string{"entrelinha", "tamanho_pt"}, chavesOrdenadas(corpo),
		"YAML de corpo silente só pode ter os dois campos normativos")

	dados, err = json.Marshal(objeto)
	require.NoError(t, err)
	assert.JSONEq(t, esperado, string(dados))
}

func chavesOrdenadas(objeto map[string]any) []string {
	chaves := make([]string, 0, len(objeto))
	for chave := range objeto {
		chaves = append(chaves, chave)
	}
	sort.Strings(chaves)
	return chaves
}

// ---------------------------------------------------------------------------
// RECORTE f3-mutadores-por-papel — C2, critérios A3 e A4
//
// EntrelinhaPorPapel é o campo novo, OPCIONAL, irmão de Pagina e Corpo. Duas
// coisas são contrato e não estilo:
//
//  1. `omitempty`: inserirRuleset compara definicao=$4::jsonb e o checksum é
//     sha256 do json.Marshal. A Definicao lida de um perfil JÁ SEMEADO precisa
//     continuar serializando SEM a chave nova, senão um re-seed da mesma
//     versão bate em conflito contra uma versão imutável (A3).
//  2. A lista branca é LITERAL e tem só os quatro papéis de exceção da §5.2.
//     Delegar a cdm.ParaPapel aceitaria "paragrafo" — grupo idêntico a
//     ReferenciasCorpo, duas AplicarEntrelinha sobre os mesmos ordinais, a
//     segunda sobrescrevendo em silêncio — e "tabela", que colocaria todo
//     ordinal w:tbl no plano para o mutador recusar, brickando qualquer
//     documento com tabela sem ação humana nenhuma (A4).
// ---------------------------------------------------------------------------

func TestDefinicaoSemEntrelinhaPorPapelNaoMudaOJSONDoPerfilSemeado(t *testing.T) {
	caminho := filepath.Join("..", "..", "..", "rulesets", "abnt-14724", "v1.yaml")
	conteudo, err := os.ReadFile(caminho)
	require.NoError(t, err, "A3: o perfil semeado %s precisa existir", caminho)
	require.NotContains(t, string(conteudo), "entrelinha_por_papel",
		"pré-condição de A3: o arquivo é IMUTÁVEL e não declara a chave nova; se declarasse, o critério não mediria omitempty")

	var definicao Definicao
	require.NoError(t, yaml.Unmarshal(conteudo, &definicao))
	require.NoError(t, definicao.Validar(), "A3: a ausência da chave é perfil válido")

	assert.Nil(t, definicao.EntrelinhaPorPapel, "A3: chave ausente no YAML produz nil, não mapa vazio")

	serializado, err := json.Marshal(definicao)
	require.NoError(t, err)
	assert.NotContains(t, string(serializado), "entrelinha_por_papel",
		"A3: sem omitempty o jsonb e o checksum da versão já semeada mudam, e o re-seed bate em conflito")

	t.Run("mapa vazio equivale a ausência", func(t *testing.T) {
		vazia := definicao
		vazia.EntrelinhaPorPapel = map[string]float64{}
		require.NoError(t, vazia.Validar())

		serializado, err := json.Marshal(vazia)
		require.NoError(t, err)
		assert.NotContains(t, string(serializado), "entrelinha_por_papel",
			"C2: mapa vazio é 'a norma é silente', e precisa sair do JSON como a ausência sai")
	})
}

func TestDefinicaoEntrelinhaPorPapelAceitaSoOsQuatroPapeisDeExcecao(t *testing.T) {
	aceitos := []struct {
		nome  string
		valor map[string]float64
	}{
		{"citacao", map[string]float64{"citacao": 1.0}},
		{"referencia", map[string]float64{"referencia": 1.0}},
		{"legenda", map[string]float64{"legenda": 1.0}},
		{"nota_rodape", map[string]float64{"nota_rodape": 1.0}},
		{"os quatro juntos", map[string]float64{"citacao": 1.0, "referencia": 1.0, "legenda": 1.0, "nota_rodape": 1.0}},
	}
	for _, caso := range aceitos {
		t.Run("aceito: "+caso.nome, func(t *testing.T) {
			definicao := definicaoSintetica()
			definicao.EntrelinhaPorPapel = caso.valor
			assert.NoError(t, definicao.Validar(), "A4: os quatro papéis de exceção da §5.2 são os aceitos")
		})
	}

	rejeitados := []struct {
		nome  string
		valor map[string]float64
		campo string
	}{
		{
			// Grupo idêntico a ReferenciasCorpo: falsificaria a disjunção que
			// C3 afirma como teorema e produziria duas AplicarEntrelinha sobre
			// os mesmos ordinais, a segunda sobrescrevendo em silêncio.
			nome:  "paragrafo quebraria a disjunção de C3",
			valor: map[string]float64{"paragrafo": 1.0},
			campo: "entrelinha_por_papel",
		},
		{
			// Todo ordinal w:tbl entraria no plano e o mutador recusaria o
			// alvo: documento com tabela ficaria permanentemente inviável.
			nome:  "tabela brickaria todo documento com tabela",
			valor: map[string]float64{"tabela": 1.0},
			campo: "entrelinha_por_papel",
		},
		{"secao, de que a ordenação total de C3 depende", map[string]float64{"secao": 1.0}, "entrelinha_por_papel"},
		{"titulo", map[string]float64{"titulo": 1.0}, "entrelinha_por_papel"},
		{"citação acentuada não é a chave", map[string]float64{"citação": 1.0}, "entrelinha_por_papel"},
		{"chave vazia", map[string]float64{"": 1.0}, "entrelinha_por_papel"},
		{"medida zero", map[string]float64{"citacao": 0}, "corpo"},
		{"medida negativa", map[string]float64{"citacao": -1}, "corpo"},
		{"medida NaN", map[string]float64{"citacao": math.NaN()}, "corpo"},
		{"medida +Inf", map[string]float64{"citacao": math.Inf(1)}, "corpo"},
		{"medida que arredonda para zero unidade", map[string]float64{"citacao": 0.49 / 240}, "corpo"},
		{
			// Os DOIS defeitos no mesmo mapa. Só é afirmável porque a lista
			// branca varre TODAS as chaves antes de qualquer medida de valor:
			// se a varredura fosse intercalada, o campo devolvido dependeria
			// da ordem de iteração do mapa e a asserção seria instável.
			nome:  "chave fora da lista branca vence a medida inválida, independente da ordem do mapa",
			valor: map[string]float64{"paragrafo": 1.0, "citacao": 0},
			campo: "entrelinha_por_papel",
		},
	}
	for _, caso := range rejeitados {
		t.Run("rejeitado: "+caso.nome, func(t *testing.T) {
			definicao := definicaoSintetica()
			require.NoError(t, definicao.Validar(),
				"pré-condição: só o campo novo pode reprovar neste caso")
			definicao.EntrelinhaPorPapel = caso.valor

			// O campo "corpo" nas medidas é HERDADO de ValidarEntrelinha, de
			// propósito, para não duplicar o validador. É mensagem enganosa
			// para quem consome o 400 no front, e por isso está registrada em
			// docs/contrato-api.md.
			exigirCampoInvalido(t, definicao.Validar(), caso.campo)
		})
	}
}
