// Command rulesetctl valida e semeia os rulesets das revistas.
//
// Na F0 o comando apenas confere que todo arquivo do diretório é YAML legível.
// A validação contra o schema e a carga no Postgres entram na F3, junto com o
// motor de formatação.
package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
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
		return fmt.Errorf("uso: rulesetctl <%s|%s> --dir <diretorio>", comandoValidar, comandoSemear)
	}

	comando := argumentos[0]
	conjunto := flag.NewFlagSet(comando, flag.ExitOnError)
	diretorio := conjunto.String("dir", "rulesets", "diretório com os YAMLs das revistas")
	if err := conjunto.Parse(argumentos[1:]); err != nil {
		return err
	}

	switch comando {
	case comandoValidar:
		return validar(*diretorio)
	case comandoSemear:
		return fmt.Errorf("semear ainda não implementado: entra na F3, junto com o motor de formatação")
	default:
		return fmt.Errorf("comando desconhecido: %s", comando)
	}
}

// validar percorre o diretório de rulesets com os.Root, de modo que nenhum
// link simbólico consiga escapar da árvore informada.
func validar(diretorio string) error {
	raiz, err := os.OpenRoot(diretorio)
	if err != nil {
		return fmt.Errorf("diretório de rulesets inacessível (%s): %w", diretorio, err)
	}
	defer func() { _ = raiz.Close() }()

	arquivos, err := localizarYAMLs(raiz.FS())
	if err != nil {
		return err
	}

	if len(arquivos) == 0 {
		fmt.Printf("nenhum ruleset em %s — as revistas entram na F3\n", diretorio)
		return nil
	}

	for _, arquivo := range arquivos {
		conteudo, err := fs.ReadFile(raiz.FS(), arquivo)
		if err != nil {
			return fmt.Errorf("ao ler %s: %w", arquivo, err)
		}

		var documento map[string]any
		if err := yaml.Unmarshal(conteudo, &documento); err != nil {
			return fmt.Errorf("%s não é um YAML válido: %w", arquivo, err)
		}

		fmt.Printf("ok: %s\n", arquivo)
	}

	fmt.Printf("%d ruleset(s) validado(s)\n", len(arquivos))
	return nil
}

func localizarYAMLs(sistemaDeArquivos fs.FS) ([]string, error) {
	var arquivos []string

	err := fs.WalkDir(sistemaDeArquivos, ".", func(caminho string, entrada fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Arquivos prefixados com "_" são auxiliares (ex.: _schema.json).
		if entrada.IsDir() || strings.HasPrefix(entrada.Name(), "_") {
			return nil
		}
		if extensao := filepath.Ext(caminho); extensao == ".yaml" || extensao == ".yml" {
			arquivos = append(arquivos, caminho)
		}
		return nil
	})

	return arquivos, err
}
