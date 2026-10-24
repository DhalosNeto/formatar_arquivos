package ruleset

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	domainruleset "github.com/daniel-halos/formatador/internal/domain/ruleset"
	erros "github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// apontar devolve o endereço de um literal: nos cinco campos silentes de
// Corpo, ponteiro é "a norma diz isto" e nil é "a norma não diz" (C1).
func apontar[T any](valor T) *T { return &valor }

// camposSilentesDoCorpo são as cinco chaves que SAEM de required no schema
// (C2). Ausência da chave é o caminho que produz nil em Go.
var camposSilentesDoCorpo = []string{"fonte", "alinhamento", "recuo_cm", "espaco_antes_pt", "espaco_depois_pt"}

func silenteNoCorpo(caminho []string, campo string) bool {
	if len(caminho) != 1 || caminho[0] != "corpo" {
		return false
	}
	for _, silente := range camposSilentesDoCorpo {
		if silente == campo {
			return true
		}
	}
	return false
}

func lerPerfil(t *testing.T) []byte {
	t.Helper()
	dados, err := os.ReadFile("testdata/perfil-sintetico.yaml")
	require.NoError(t, err)
	return dados
}

func exigirRejeicao(t *testing.T, dados []byte) error {
	t.Helper()
	_, err := Carregar(dados)
	require.Error(t, err)
	var alvo *erros.ErroValidacao
	require.ErrorAs(t, err, &alvo)
	assert.NotEmpty(t, err.Error())
	return err
}

func TestCarregarPerfilSintetico(t *testing.T) {
	var carregar = Carregar
	dados := lerPerfil(t)
	copia := append([]byte(nil), dados...)
	d, err := carregar(dados)
	require.NoError(t, err)
	esperado := domainruleset.Definicao{Slug: "perfil-sintetico", Versao: 1, Nome: "Perfil sintético de teste", Fonte: "https://example.org/perfil", Pagina: domainruleset.Pagina{LarguraCM: 20, AlturaCM: 28, Margens: domainruleset.Margens{SuperiorCM: 2, InferiorCM: 2, EsquerdaCM: 2, DireitaCM: 2}}, Corpo: domainruleset.Corpo{Fonte: apontar("Fonte Sintética"), TamanhoPT: 11, Entrelinha: 1.25, RecuoCM: apontar(0.0), EspacoAntesPT: apontar(0.0), EspacoDepoisPT: apontar(0.0), Alinhamento: apontar("justificado")}}
	assert.Equal(t, esperado, d)
	assert.Equal(t, copia, dados)
	assert.NoError(t, d.Validar())
	outra, err := carregar(dados)
	require.NoError(t, err)
	assert.Equal(t, d, outra)
}

func TestCarregarLimiteBytes(t *testing.T) {
	require.EqualValues(t, 65536, TamanhoMaximoBytes)
	base := string(lerPerfil(t))
	limite := base + "#" + strings.Repeat("x", 65536-len(base)-1)
	for _, caso := range []struct {
		nome  string
		dados []byte
		erro  bool
	}{
		{"exatamente 64 KiB", []byte(limite), false}, {"um byte acima", []byte(limite + "x"), true}, {"gigante", []byte(strings.Repeat("x", 1024*1024)), true},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			if caso.erro {
				exigirRejeicao(t, caso.dados)
			} else {
				_, err := Carregar(caso.dados)
				assert.NoError(t, err)
			}
		})
	}
}

func TestCarregarCamposObrigatoriosEObjetosFechados(t *testing.T) {
	base := lerPerfil(t)
	// Usa uma árvore nova em cada caso, incluindo remoção dos zeros obrigatórios.
	caminhos := [][]string{{}, {"pagina"}, {"pagina", "margens"}, {"corpo"}}
	for _, caminho := range caminhos {
		var original map[string]any
		require.NoError(t, yaml.Unmarshal(base, &original))
		objeto := original
		for _, chave := range caminho {
			objeto = objeto[chave].(map[string]any)
		}
		campos := make([]string, 0, len(objeto))
		for chave := range objeto {
			campos = append(campos, chave)
		}
		for _, campo := range campos {
			for _, operacao := range []string{"ausente", "nulo", "tipo incorreto"} {
				t.Run(strings.Join(caminho, "/")+"/"+campo+"/"+operacao, func(t *testing.T) {
					var raiz map[string]any
					require.NoError(t, yaml.Unmarshal(base, &raiz))
					alvo := raiz
					for _, chave := range caminho {
						alvo = alvo[chave].(map[string]any)
					}
					switch operacao {
					case "ausente":
						delete(alvo, campo)
					case "nulo":
						alvo[campo] = nil
					case "tipo incorreto":
						alvo[campo] = []any{"indevido"}
					}
					dados, err := yaml.Marshal(raiz)
					require.NoError(t, err)
					// A4/C2: os cinco campos silentes de corpo saem de
					// required — a AUSÊNCIA da chave é o caminho que produz
					// nil. Chave presente com valor nulo e tipo incorreto
					// continuam recusados.
					if operacao == "ausente" && silenteNoCorpo(caminho, campo) {
						definicao, err := Carregar(dados)
						require.NoError(t, err, "A4: ausência de %q é silêncio da norma, não erro", campo)
						assert.NoError(t, definicao.Validar())
						return
					}
					exigirRejeicao(t, dados)
				})
			}
		}
		t.Run(strings.Join(caminho, "/")+"/desconhecido", func(t *testing.T) {
			objeto["campo_desconhecido"] = "SEGREDO_CAMPO"
			dados, err := yaml.Marshal(original)
			require.NoError(t, err)
			exigirRejeicao(t, dados)
		})
	}
}

func TestCarregarYAMLHostil(t *testing.T) {
	base := string(lerPerfil(t))
	casos := []struct{ nome, entrada string }{
		{"vazio", ""}, {"brancos", " \n\t"}, {"nulo", "null"}, {"sequencia", "[]"}, {"escalar", "segredo"}, {"corrompido", "nome: [SEGREDO"},
		{"anchor sem alias", strings.Replace(base, "nome: Perfil sintético de teste", "nome: &segredo Perfil sintético de teste", 1)},
		{"alias", strings.Replace(strings.Replace(base, "nome: Perfil sintético de teste", "nome: &segredo Perfil sintético de teste", 1), "fonte: Fonte Sintética", "fonte: *segredo", 1)},
		{"merge", base + "<<: {slug: outro}\n"},
		{"tag customizada", strings.Replace(base, "nome: Perfil sintético de teste", "nome: !segredo Perfil sintético de teste", 1)},
		{"duplicata raiz", base + "slug: outro\n"},
		{"duplicata pagina", strings.Replace(base, "  largura_cm: 20", "  largura_cm: 20\n  largura_cm: 21", 1)},
		{"duplicata margens", strings.Replace(base, "    superior_cm: 2", "    superior_cm: 2\n    superior_cm: 3", 1)},
		{"duplicata corpo", base + "  recuo_cm: 1\n"},
		{"multiplos documentos", base + "---\n" + base}, {"segundo documento vazio", base + "---\n"},
		{"profundidade abusiva", "nome: " + strings.Repeat("[", 2000) + "segredo" + strings.Repeat("]", 2000)},
		{"quantidade abusiva de nos", "nome: [" + strings.Repeat("0,", 20000) + "0]"},
		{"validacao dominio", strings.Replace(base, "largura_cm: 20", "largura_cm: -1", 1)},
		{"versao fracionaria", strings.Replace(base, "versao: 1", "versao: 1.5", 1)},
		{"versao overflow", strings.Replace(base, "versao: 1", "versao: 2147483648", 1)},
		{"nan", strings.Replace(base, "tamanho_pt: 11", "tamanho_pt: .nan", 1)},
		{"infinito", strings.Replace(base, "entrelinha: 1.25", "entrelinha: .inf", 1)},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) { exigirRejeicao(t, []byte(caso.entrada)) })
	}
	exigirRejeicao(t, nil)
}

func TestCarregarErrosFixosSemConteudo(t *testing.T) {
	base := string(lerPerfil(t))
	for _, montar := range []struct {
		nome    string
		entrada func(string) string
	}{
		{"sintaxe", func(s string) string { return "nome: [" + s }},
		{"chave desconhecida", func(s string) string { return base + s + ": valor\n" }},
		{"tag", func(s string) string {
			return strings.Replace(base, "nome: Perfil sintético de teste", "nome: !"+s+" valor", 1)
		}},
		{"dominio", func(s string) string { return strings.Replace(base, "slug: perfil-sintetico", "slug: "+s, 1) }},
	} {
		t.Run(montar.nome, func(t *testing.T) {
			var mensagem string
			for _, segredo := range []string{"SEGREDO_USUARIO_A", "SEGREDO_USUARIO_B"} {
				err := exigirRejeicao(t, []byte(montar.entrada(segredo)))
				if mensagem == "" {
					mensagem = err.Error()
				} else {
					assert.Equal(t, mensagem, err.Error())
				}
				assert.NotContains(t, fmt.Sprintf("%+v", err), segredo)
				assert.NotContains(t, fmt.Sprintf("%#v", err), segredo)
				dados, e := json.Marshal(err)
				require.NoError(t, e)
				assert.NotContains(t, string(dados), segredo)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// A4 — YAML sem as cinco chaves silentes, chave desconhecida, sem tamanho_pt
// ---------------------------------------------------------------------------

// perfilSilente é o mesmo perfil sintético com o corpo reduzido ao que uma
// norma silente declara: tamanho e entrelinha.
const perfilSilente = `slug: perfil-silente
versao: 1
nome: Perfil silente de teste
fonte: https://example.org/perfil
pagina:
  largura_cm: 21.0
  altura_cm: 29.7
  margens:
    superior_cm: 3
    inferior_cm: 2
    esquerda_cm: 3
    direita_cm: 2
corpo:
  tamanho_pt: 12
  entrelinha: 1.5
`

func TestCarregarSemChavesSilentesProduzNil(t *testing.T) {
	require.NotContains(t, perfilSilente, "recuo_cm",
		"pré-condição: o YAML do caso não pode declarar nenhuma das cinco chaves silentes")

	definicao, err := Carregar([]byte(perfilSilente))
	require.NoError(t, err, "A4: ausência das cinco chaves é perfil válido")
	require.NoError(t, definicao.Validar())

	assert.Nil(t, definicao.Corpo.Fonte, "A4: corpo.fonte ausente no YAML produz nil")
	assert.Nil(t, definicao.Corpo.Alinhamento, "A4: corpo.alinhamento ausente no YAML produz nil")
	assert.Nil(t, definicao.Corpo.RecuoCM, "A4: corpo.recuo_cm ausente no YAML produz nil")
	assert.Nil(t, definicao.Corpo.EspacoAntesPT, "A4: corpo.espaco_antes_pt ausente no YAML produz nil")
	assert.Nil(t, definicao.Corpo.EspacoDepoisPT, "A4: corpo.espaco_depois_pt ausente no YAML produz nil")
	assert.Equal(t, 12.0, definicao.Corpo.TamanhoPT)
	assert.Equal(t, 1.5, definicao.Corpo.Entrelinha)

	t.Run("chave desconhecida segue recusada", func(t *testing.T) {
		exigirRejeicao(t, []byte(perfilSilente+"  chave_desconhecida: 1\n"))
	})
	t.Run("tamanho_pt segue obrigatorio", func(t *testing.T) {
		exigirRejeicao(t, []byte(strings.Replace(perfilSilente, "  tamanho_pt: 12\n", "", 1)))
	})
	t.Run("entrelinha segue obrigatoria", func(t *testing.T) {
		exigirRejeicao(t, []byte(strings.Replace(perfilSilente, "  entrelinha: 1.5\n", "", 1)))
	})
	t.Run("chave silente presente com valor nulo é recusada", func(t *testing.T) {
		for _, chave := range camposSilentesDoCorpo {
			t.Run(chave, func(t *testing.T) {
				exigirRejeicao(t, []byte(perfilSilente+"  "+chave+": null\n"))
			})
		}
	})
}

// ---------------------------------------------------------------------------
// A11 — o primeiro perfil normativo real: backend/rulesets/abnt-14724/v1.yaml
// ---------------------------------------------------------------------------

func TestCarregarPerfilABNT14724(t *testing.T) {
	caminho := filepath.Join("..", "..", "..", "rulesets", "abnt-14724", "v1.yaml")
	conteudo, err := os.ReadFile(caminho)
	require.NoError(t, err, "A11: o perfil %s precisa existir", caminho)

	definicao, err := Carregar(conteudo)
	require.NoError(t, err, "A11: o perfil normativo precisa carregar e validar")
	require.NoError(t, definicao.Validar())

	assert.Equal(t, "abnt-14724", definicao.Slug)
	assert.Equal(t, 1, definicao.Versao)
	// O identificador citável mora em `nome`: é o único campo que chega à tela.
	assert.Contains(t, definicao.Nome, "14724", "A11/C5: o nome carrega o identificador da norma")

	// §5.1 — A4 e margens do anverso; §5.2 — entrelinha 1,5.
	assert.Equal(t, 21.0, definicao.Pagina.LarguraCM)
	assert.Equal(t, 29.7, definicao.Pagina.AlturaCM)
	assert.Equal(t, domainruleset.Margens{SuperiorCM: 3, InferiorCM: 2, EsquerdaCM: 3, DireitaCM: 2}, definicao.Pagina.Margens)
	assert.Equal(t, 12.0, definicao.Corpo.TamanhoPT)
	assert.Equal(t, 1.5, definicao.Corpo.Entrelinha)

	// Os cinco campos que a 14724:2024 NÃO define precisam sair nil. Preencher
	// qualquer um deles é inventar norma.
	assert.Nil(t, definicao.Corpo.Fonte, "A11: a norma não define família de fonte")
	assert.Nil(t, definicao.Corpo.Alinhamento, "A11: a norma não define alinhamento do corpo")
	assert.Nil(t, definicao.Corpo.RecuoCM, "A11: a norma não define recuo de primeira linha")
	assert.Nil(t, definicao.Corpo.EspacoAntesPT, "A11: a norma não define espaço antes do parágrafo")
	assert.Nil(t, definicao.Corpo.EspacoDepoisPT, "A11: a norma não define espaço depois do parágrafo")

	// Nenhuma rede é tocada no carregamento (carregarBloqueado): a URL é
	// rastro para humano, não dependência.
	assert.Equal(t, "https://www.abntcatalogo.com.br/", definicao.Fonte)
}

// ---------------------------------------------------------------------------
// RECORTE f3-mutadores-por-papel — C2, critério A14
//
// FORMA DO ARQUIVO, que é o que o adiamento do v2.yaml protege. O schema é o
// gate REAL: carregar.go decodifica YAML para objeto genérico e faz
// json.Marshal/Unmarshal, então a tag `yaml` do struct não participa da
// leitura de perfil. Sem este critério, nome errado da propriedade ou
// aninhamento dentro de `corpo` deixaria todos os outros critérios verdes e o
// erro só apareceria dentro de um arquivo imutável.
//
// O critério afere RECUSA e a mensagem FIXA, e NÃO distingue qual gate
// recusou: erroConteudo é a única saída de erro de Carregar, usada por schema,
// json.Unmarshal e Definicao.Validar. A separação vem dos CENÁRIOS.
// ---------------------------------------------------------------------------

const mensagemConteudoInvalido = "conteúdo de ruleset inválido"

func TestCarregarEntrelinhaPorPapelNaRaizDoArquivo(t *testing.T) {
	require.NotContains(t, perfilSilente, "entrelinha_por_papel",
		"pré-condição: a base do caso não declara a chave nova")

	t.Run("(a) chave na RAIZ carrega", func(t *testing.T) {
		definicao, err := Carregar([]byte(perfilSilente + "entrelinha_por_papel:\n  citacao: 1.0\n"))
		require.NoError(t, err, "A14: prova o nome da propriedade NA RAIZ, irmã de pagina e corpo")
		require.NoError(t, definicao.Validar())
		assert.Equal(t, map[string]float64{"citacao": 1.0}, definicao.EntrelinhaPorPapel)
	})

	recusados := []struct {
		nome   string
		sufixo string
		prova  string
	}{
		{"(b) medida zero", "entrelinha_por_papel:\n  citacao: 0\n", "exclusiveMinimum 0 na propriedade do papel"},
		{"(c) medida como texto", "entrelinha_por_papel:\n  citacao: \"simples\"\n", "type number na propriedade do papel"},
		{"(d) chave aninhada dentro de corpo", "  entrelinha_por_papel:\n    citacao: 1.0\n", "additionalProperties false em corpo"},
		{"(e) papel fora da lista branca", "entrelinha_por_papel:\n  paragrafo: 1.0\n", "a lista branca atravessa o arquivo"},
	}
	for _, caso := range recusados {
		t.Run(caso.nome, func(t *testing.T) {
			err := exigirRejeicao(t, []byte(perfilSilente+caso.sufixo))

			var alvo *erros.ErroValidacao
			require.ErrorAs(t, err, &alvo)
			require.NotEmpty(t, alvo.Campos)
			assert.Equal(t, "ruleset", alvo.Campos[0].Campo, "A14: o campo é sempre \"ruleset\" (%s)", caso.prova)
			assert.Contains(t, err.Error(), mensagemConteudoInvalido,
				"A14: a mensagem é FIXA; causa de parser ou de schema não pode ecoar o conteúdo de origem")
		})
	}
}
