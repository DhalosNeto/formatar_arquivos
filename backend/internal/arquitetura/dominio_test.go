package arquitetura_test

import (
	"os/exec"
	"strings"
	"testing"
)

const (
	prefixoDominio = "github.com/daniel-halos/formatador/internal/domain/"
	prefixoInfra   = "github.com/daniel-halos/formatador/internal/infra/"

	// pacoteErros é a ÚNICA exceção autorizada à regra "o domínio não importa
	// infra", documentada no CLAUDE.md: todo erro do projeto nasce nos
	// construtores desse pacote, inclusive os de regra de negócio.
	//
	// Separar o pacote de erros exigiria um recorte próprio. Enquanto não
	// acontecer, a exceção é UMA e está listada aqui — não é licença para a
	// próxima.
	pacoteErros = "github.com/daniel-halos/formatador/internal/infra/errors"
)

// infraProibidaImportada devolve os pacotes de infra que a lista de
// importações contém e que não são a exceção autorizada.
func infraProibidaImportada(importacoes string) []string {
	var proibidos []string
	for _, pacote := range strings.Fields(importacoes) {
		if !strings.HasPrefix(pacote, prefixoInfra) {
			continue
		}
		if pacote == pacoteErros {
			continue
		}
		proibidos = append(proibidos, pacote)
	}
	return proibidos
}

// TestDetectorDeInfraNoDominio exercita o detector antes de confiar nele.
// Sem isto, um detector quebrado faria o teste abaixo passar sempre — que é a
// pior forma de falha possível num teste de fronteira: silenciosa e tranquila.
func TestDetectorDeInfraNoDominio(t *testing.T) {
	for _, caso := range []struct {
		nome        string
		importacoes string
		esperado    []string
	}{
		{"sem infra", "context github.com/google/uuid", nil},
		{"só a exceção de erros", "context " + pacoteErros, nil},
		{"storage é proibido", pacoteErros + " " + prefixoInfra + "storage", []string{prefixoInfra + "storage"}},
		{"ooxml é proibido", prefixoInfra + "ooxml", []string{prefixoInfra + "ooxml"}},
		{"subpacote de erros não é a exceção", prefixoInfra + "errors/interno", []string{prefixoInfra + "errors/interno"}},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			obtido := infraProibidaImportada(caso.importacoes)
			if strings.Join(obtido, ",") != strings.Join(caso.esperado, ",") {
				t.Fatalf("detector devolveu %v, esperava %v", obtido, caso.esperado)
			}
		})
	}
}

// TestDominioNaoImportaInfra é a regra número 1 da arquitetura hexagonal
// deste projeto: a dependência aponta sempre para dentro.
//
// Ela não tinha teste. Estava respeitada por disciplina — e é justamente a
// regra mais fácil de quebrar sem perceber, porque basta um autoimport da IDE
// ao digitar `storage.` ou `ooxml.` dentro de um serviço de domínio. O
// resultado compila, passa nos testes e só aparece quando alguém tenta testar
// o domínio sem subir infra.
//
// A checagem usa .Imports (importação DIRETA), não -deps: o domínio importa
// infra/errors legitimamente, e -deps arrastaria as dependências transitivas
// do próprio errors, produzindo falso positivo.
func TestDominioNaoImportaInfra(t *testing.T) {
	comando := exec.CommandContext(t.Context(), "go", "list", "-f", `{{.ImportPath}}|{{join .Imports " "}}`, "./internal/domain/...")
	comando.Dir = "../.."
	saida, err := comando.CombinedOutput()
	if err != nil {
		t.Fatalf("listar importações do domínio: %v\n%s", err, saida)
	}

	var pacotesVerificados int
	for _, linha := range strings.Split(strings.TrimRight(string(saida), "\n"), "\n") {
		if linha == "" {
			continue
		}
		caminho, importacoes, encontrado := strings.Cut(linha, "|")
		if !encontrado {
			t.Fatalf("linha de saída sem separador: %q", linha)
		}
		if !strings.HasPrefix(caminho, prefixoDominio) {
			continue
		}
		pacotesVerificados++

		if proibidos := infraProibidaImportada(importacoes); len(proibidos) > 0 {
			t.Errorf("%s importa infra: %v\nsó %s é permitido (CLAUDE.md, regra 2)",
				caminho, proibidos, pacoteErros)
		}
	}

	// Sem esta guarda, um erro no filtro de prefixo verificaria zero pacotes e
	// o teste passaria anunciando uma garantia que não checou nada.
	if pacotesVerificados == 0 {
		t.Fatal("nenhum pacote de domínio foi verificado: o filtro de prefixo está errado")
	}
}
