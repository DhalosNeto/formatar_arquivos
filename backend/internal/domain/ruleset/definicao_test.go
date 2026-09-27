package ruleset

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	erros "github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func definicaoSintetica() Definicao {
	return Definicao{Slug: "perfil-sintetico", Versao: 1, Nome: "Perfil sintético de teste", Fonte: "https://example.org/perfil",
		Pagina: Pagina{LarguraCM: 20, AlturaCM: 28, Margens: Margens{SuperiorCM: 2, InferiorCM: 2, EsquerdaCM: 2, DireitaCM: 2}},
		Corpo:  Corpo{Fonte: "Fonte Sintética", TamanhoPT: 11, Entrelinha: 1.25, Alinhamento: "justificado"}}
}

func TestDefinicaoNumeros(t *testing.T) {
	campos := []struct {
		nome     string
		obter    func(*Definicao) *float64
		positivo bool
		fator    float64
	}{
		{"largura", func(d *Definicao) *float64 { return &d.Pagina.LarguraCM }, true, 1440.0 / 2.54},
		{"altura", func(d *Definicao) *float64 { return &d.Pagina.AlturaCM }, true, 1440.0 / 2.54},
		{"superior", func(d *Definicao) *float64 { return &d.Pagina.Margens.SuperiorCM }, false, 1440.0 / 2.54},
		{"inferior", func(d *Definicao) *float64 { return &d.Pagina.Margens.InferiorCM }, false, 1440.0 / 2.54},
		{"esquerda", func(d *Definicao) *float64 { return &d.Pagina.Margens.EsquerdaCM }, false, 1440.0 / 2.54},
		{"direita", func(d *Definicao) *float64 { return &d.Pagina.Margens.DireitaCM }, false, 1440.0 / 2.54},
		{"tamanho", func(d *Definicao) *float64 { return &d.Corpo.TamanhoPT }, true, 2},
		{"entrelinha", func(d *Definicao) *float64 { return &d.Corpo.Entrelinha }, true, 240},
		{"recuo", func(d *Definicao) *float64 { return &d.Corpo.RecuoCM }, false, 1440.0 / 2.54},
		{"antes", func(d *Definicao) *float64 { return &d.Corpo.EspacoAntesPT }, false, 20},
		{"depois", func(d *Definicao) *float64 { return &d.Corpo.EspacoDepoisPT }, false, 20},
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
					*campo.obter(&d) = caso.valor
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
		nome   string
		obter  func(*Definicao) *string
		limite int
	}{
		{"slug", func(d *Definicao) *string { return &d.Slug }, 80},
		{"nome", func(d *Definicao) *string { return &d.Nome }, 200},
		{"fonte corpo", func(d *Definicao) *string { return &d.Corpo.Fonte }, 100},
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
				*campo.obter(&d) = caso.valor
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
			d.Corpo.Alinhamento = alinhamento
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
	d.Corpo.Fonte = "Sintética 漢字"
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
