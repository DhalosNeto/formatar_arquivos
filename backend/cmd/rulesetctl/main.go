// Command rulesetctl valida e semeia os rulesets das revistas.
//
// validar confere schema, regras de domínio e caminho canônico.
// semear cadastra versões no Postgres sem sobrescrever regras existentes.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/daniel-halos/formatador/internal/data/contracts"
	"github.com/daniel-halos/formatador/internal/data/postgres"
	domainruleset "github.com/daniel-halos/formatador/internal/domain/ruleset"
	"github.com/daniel-halos/formatador/internal/infra/config"
	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/daniel-halos/formatador/internal/infra/ruleset"
)

const (
	comandoValidar = "validar"
	comandoSemear  = "semear"
)

func main() {
	if err := executar(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}
}

func executar(argumentos []string) error {
	if len(argumentos) == 0 {
		return errors.NovoErroValidacao("comando", "uso: rulesetctl <validar|semear> --dir <diretorio>")
	}

	comando := argumentos[0]
	conjunto := flag.NewFlagSet(comando, flag.ContinueOnError)
	conjunto.SetOutput(io.Discard)
	diretorio := conjunto.String("dir", "rulesets", "diretório com os YAMLs das revistas")
	if err := conjunto.Parse(argumentos[1:]); err != nil {
		return errors.NovoErroValidacao("argumentos", "opções inválidas para rulesetctl")
	}
	if conjunto.NArg() != 0 {
		return errors.NovoErroValidacao("argumentos", "argumentos posicionais não são aceitos")
	}

	switch comando {
	case comandoValidar:
		return validar(context.Background(), *diretorio)
	case comandoSemear:
		return executarSeed(context.Background(), *diretorio)
	default:
		return errors.NovoErroValidacao("comando", "comando desconhecido")
	}
}

// validar percorre o diretório de rulesets com os.Root, de modo que nenhum
// link simbólico consiga escapar da árvore informada.
func validar(ctx context.Context, diretorio string) error {
	definicoes, err := carregarDiretorio(ctx, diretorio)
	if err != nil {
		return err
	}
	if len(definicoes) == 0 {
		fmt.Println("nenhum perfil publicado no diretório de rulesets")
		return nil
	}
	fmt.Printf("%d ruleset(s) validado(s)\n", len(definicoes))
	return nil
}

func carregarDiretorio(ctx context.Context, diretorio string) ([]domainruleset.Definicao, error) {
	raiz, err := os.OpenRoot(diretorio)
	if err != nil {
		return nil, errors.NovoErroAplicacao("diretório de rulesets inacessível")
	}
	defer func() { _ = raiz.Close() }()

	arquivos, err := localizarYAMLs(ctx, diretoriosConfinados{raiz: raiz})
	if err != nil {
		return nil, err
	}
	definicoes := make([]domainruleset.Definicao, 0, len(arquivos))
	for _, arquivo := range arquivos {
		definicao, err := carregarArquivo(ctx, raiz, arquivo)
		if err != nil {
			return nil, err
		}
		definicoes = append(definicoes, definicao)
	}
	return definicoes, nil
}

func carregarArquivo(ctx context.Context, raiz *os.Root, caminho string) (domainruleset.Definicao, error) {
	var vazio domainruleset.Definicao
	if err := ctx.Err(); err != nil {
		return vazio, errors.Envolver(err, "validação interrompida")
	}
	// O backend roda em Linux: O_NONBLOCK impede FIFO trocado após a enumeração
	// de bloquear a abertura. O descritor ainda precisa ser arquivo regular.
	arquivo, err := raiz.OpenFile(caminho, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return vazio, errors.NovoErroAplicacao("não foi possível abrir o ruleset")
	}
	defer func() { _ = arquivo.Close() }()
	estado, err := arquivo.Stat()
	if err != nil || !estado.Mode().IsRegular() {
		return vazio, errors.NovoErroValidacao("arquivo", "ruleset deve ser arquivo regular")
	}
	conteudo, err := io.ReadAll(io.LimitReader(arquivo, ruleset.TamanhoMaximoBytes+1))
	if err != nil {
		return vazio, errors.NovoErroAplicacao("não foi possível ler o ruleset")
	}
	definicao, err := ruleset.Carregar(conteudo)
	if err != nil {
		return vazio, err
	}
	if caminho != fmt.Sprintf("%s/v%d.yaml", definicao.Slug, definicao.Versao) {
		return vazio, errors.NovoErroValidacao("arquivo", "caminho deve corresponder a slug/vN.yaml do ruleset")
	}
	return definicao, nil
}

func executarSeed(ctx context.Context, diretorio string) error {
	ctx, cancelar := context.WithTimeout(ctx, 2*time.Minute)
	defer cancelar()
	cfg, err := config.CarregarPostgres()
	if err != nil {
		return err
	}
	gerente, err := postgres.NovoGerenciador(ctx, cfg)
	if err != nil {
		return errors.NovoErroAplicacao("não foi possível configurar o banco para seed")
	}
	defer gerente.Fechar()
	return semear(ctx, diretorio, gerente.Rulesets())
}

func semear(ctx context.Context, diretorio string, repositorio contracts.RulesetRepo) error {
	if repositorio == nil {
		return errors.NovoErroArgumentoNulo("repositorio")
	}
	definicoes, err := carregarDiretorio(ctx, diretorio)
	if err != nil {
		return err
	}
	if len(definicoes) == 0 {
		fmt.Println("nenhum perfil publicado no diretório de rulesets")
		return nil
	}
	if err := repositorio.Semear(ctx, definicoes); err != nil {
		return err
	}
	fmt.Printf("%d definição(ões) conferida(s) e persistida(s) sem sobrescrever versões\n", len(definicoes))
	return nil
}

// A travessia abre somente diretórios, sem bloquear em arquivo especial
// colocado por um escritor concorrente depois da enumeração.
type diretoriosConfinados struct{ raiz *os.Root }

func (sistema diretoriosConfinados) Open(nome string) (fs.File, error) {
	return sistema.raiz.OpenFile(nome, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NONBLOCK, 0)
}

func localizarYAMLs(ctx context.Context, sistemaDeArquivos fs.FS) ([]string, error) {
	var arquivos []string
	entradas := 1 // inclui a raiz
	err := percorrerDiretorio(ctx, sistemaDeArquivos, ".", &entradas, &arquivos)
	sort.Strings(arquivos)
	return arquivos, err
}

func percorrerDiretorio(ctx context.Context, sistema fs.FS, caminho string, total *int, arquivos *[]string) error {
	if err := ctx.Err(); err != nil {
		return errors.Envolver(err, "validação interrompida")
	}
	arquivo, err := sistema.Open(caminho)
	if err != nil {
		return errors.NovoErroAplicacao("não foi possível percorrer o diretório de rulesets")
	}
	defer func() { _ = arquivo.Close() }()
	diretorio, ok := arquivo.(fs.ReadDirFile)
	if !ok {
		return errors.NovoErroAplicacao("diretório não permite leitura limitada")
	}
	for {
		if err := ctx.Err(); err != nil {
			return errors.Envolver(err, "validação interrompida")
		}
		lote, err := diretorio.ReadDir(32)
		if err != nil && !errors.E(err, io.EOF) {
			return errors.NovoErroAplicacao("não foi possível ler o diretório de rulesets")
		}
		for _, entrada := range lote {
			*total++
			proximo := path.Join(caminho, entrada.Name())
			if *total > 4096 || strings.Count(proximo, "/") > 4 {
				return errors.NovoErroValidacao("diretorio", "diretório de rulesets excede os limites de travessia")
			}
			if strings.HasPrefix(entrada.Name(), "_") {
				continue
			}
			if entrada.IsDir() {
				if err := percorrerDiretorio(ctx, sistema, proximo, total, arquivos); err != nil {
					return err
				}
				continue
			}
			if extensao := path.Ext(proximo); extensao == ".yaml" || extensao == ".yml" {
				if !entrada.Type().IsRegular() {
					return errors.NovoErroValidacao("arquivo", "ruleset deve ser arquivo regular sem link simbólico")
				}
				*arquivos = append(*arquivos, proximo)
			}
		}
		if errors.E(err, io.EOF) || len(lote) == 0 {
			return nil
		}
	}
}
