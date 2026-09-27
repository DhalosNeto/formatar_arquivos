package ruleset

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	domainruleset "github.com/daniel-halos/formatador/internal/domain/ruleset"
	erros "github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

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
	esperado := domainruleset.Definicao{Slug: "perfil-sintetico", Versao: 1, Nome: "Perfil sintético de teste", Fonte: "https://example.org/perfil", Pagina: domainruleset.Pagina{LarguraCM: 20, AlturaCM: 28, Margens: domainruleset.Margens{SuperiorCM: 2, InferiorCM: 2, EsquerdaCM: 2, DireitaCM: 2}}, Corpo: domainruleset.Corpo{Fonte: "Fonte Sintética", TamanhoPT: 11, Entrelinha: 1.25, RecuoCM: 0, EspacoAntesPT: 0, EspacoDepoisPT: 0, Alinhamento: "justificado"}}
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
