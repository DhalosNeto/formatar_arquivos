package arquitetura_test

import (
	"os/exec"
	"strings"
	"testing"
)

const pacoteProcessamento = "github.com/daniel-halos/formatador/internal/domain/documento/processamento"

func contemProcessamento(dependencias string) bool {
	for _, pacote := range strings.Fields(dependencias) {
		if pacote == pacoteProcessamento || strings.HasPrefix(pacote, pacoteProcessamento+"/") {
			return true
		}
	}
	return false
}

func TestDetectorDeFronteira(t *testing.T) {
	for _, caso := range []struct {
		nome, dependencias string
		proibido           bool
	}{
		{"sem dependência interna", "context\n" + pacoteProcessamento + "_test", false},
		{"serviço interno", "context\n" + pacoteProcessamento, true},
		{"subpacote interno", pacoteProcessamento + "/auxiliar", true},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			if contemProcessamento(caso.dependencias) != caso.proibido {
				t.Fatal("detector não reconheceu a fronteira de processamento")
			}
		})
	}
}

func TestHTTPNaoDependeDoProcessamentoInterno(t *testing.T) {
	// -deps inclui caminhos indiretos por application, servidor e adaptadores.
	comando := exec.CommandContext(t.Context(), "go", "list", "-deps", "./cmd/api", "./internal/rotas/...")
	comando.Dir = "../.."
	saida, err := comando.CombinedOutput()
	if err != nil {
		t.Fatalf("listar dependências HTTP: %v\n%s", err, saida)
	}
	if contemProcessamento(string(saida)) {
		t.Fatal("HTTP alcança o serviço interno de processamento; use apenas o serviço com dono")
	}
}

const prefixoPgx = "github.com/jackc/"
const prefixoDataInterno = "github.com/daniel-halos/formatador/internal/data/"

// contemPgx informa se algum dos campos de importações (separados por
// espaço) tem o prefixo do driver Postgres.
func contemPgx(importacoes string) bool {
	for _, pacote := range strings.Fields(importacoes) {
		if strings.HasPrefix(pacote, prefixoPgx) {
			return true
		}
	}
	return false
}

func TestDetectorDePgx(t *testing.T) {
	for _, caso := range []struct {
		nome, importacoes string
		proibido          bool
	}{
		{"sem pgx", "context fmt", false},
		{"pgx direto", "github.com/jackc/pgx/v5", true},
		{"subpacote pgxpool", "context github.com/jackc/pgx/v5/pgxpool", true},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			if contemPgx(caso.importacoes) != caso.proibido {
				t.Fatal("detector não reconheceu importação de pgx")
			}
		})
	}
}

// TestPgxSoVazDentroDeInternalData garante que nenhum pacote fora de
// internal/data importa github.com/jackc/pgx diretamente — senão cmd/api,
// rotas ou o domínio seriam obrigados a conhecer o driver concreto do banco.
//
// A checagem usa .Imports (importação direta), não -deps: cmd/api vai
// importar internal/data/postgres no futuro, e -deps de ./cmd/api conteria
// pgx legitimamente por esse caminho indireto, fazendo este teste nunca
// passar. O que se proíbe é o import DIRETO fora de internal/data/**.
func TestPgxSoVazDentroDeInternalData(t *testing.T) {
	comando := exec.CommandContext(t.Context(), "go", "list", "-f", `{{.ImportPath}}|{{join .Imports " "}}`, "./...")
	comando.Dir = "../.."
	saida, err := comando.CombinedOutput()
	if err != nil {
		t.Fatalf("listar importações diretas: %v\n%s", err, saida)
	}

	for _, linha := range strings.Split(strings.TrimRight(string(saida), "\n"), "\n") {
		if linha == "" {
			continue
		}
		caminho, importacoes, encontrado := strings.Cut(linha, "|")
		if !encontrado {
			t.Fatalf("linha de saída sem separador: %q", linha)
		}
		if strings.HasPrefix(caminho, prefixoDataInterno) {
			continue
		}
		if contemPgx(importacoes) {
			t.Fatalf("%s importa pgx diretamente; só internal/data pode importar github.com/jackc/pgx", caminho)
		}
	}
}
