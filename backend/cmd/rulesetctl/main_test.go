package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

func TestValidarRejeitaYAMLSemContrato(t *testing.T) {
	for _, conteudo := range []string{"{}", "segredo: CONTEUDO_PRIVADO", "[]", "slug: teste\n---\nslug: outro", strings.Repeat("x", 65537)} {
		t.Run(conteudo[:min(len(conteudo), 25)], func(t *testing.T) {
			diretorio := t.TempDir()
			require.NoError(t, os.Mkdir(filepath.Join(diretorio, "teste"), 0700))
			require.NoError(t, os.WriteFile(filepath.Join(diretorio, "teste", "v1.yaml"), []byte(conteudo), 0600))
			err := executar([]string{"validar", "--dir", diretorio})
			require.Error(t, err)
			require.NotContains(t, err.Error(), "CONTEUDO_PRIVADO")
		})
	}
}

func TestValidarRejeitaLinkSimbolico(t *testing.T) {
	diretorio := t.TempDir()
	fora := filepath.Join(t.TempDir(), "fora.yaml")
	require.NoError(t, os.WriteFile(fora, []byte("{}"), 0600))
	require.NoError(t, os.Symlink(fora, filepath.Join(diretorio, "v1.yaml")))
	require.Error(t, executar([]string{"validar", "--dir", diretorio}))
}

func TestExecutarRejeitaArgumentoPosicional(t *testing.T) {
	require.Error(t, executar([]string{"validar", "--dir", t.TempDir(), "ignorado"}))
}

func TestDiretorioSemPerfilPublicado(t *testing.T) {
	require.NoError(t, executar([]string{"validar", "--dir", t.TempDir()}))
}

func TestLocalizarIgnoraSubarvoreAuxiliar(t *testing.T) {
	arquivos, err := localizarYAMLs(context.Background(), fstest.MapFS{
		"_rascunhos/invalido.yaml": {Data: []byte("invalido")},
		"_schema.json":             {Data: []byte("{}")},
		"perfil/v1.yaml":           {Data: []byte("{}")},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"perfil/v1.yaml"}, arquivos)
}

func TestValidarCaminhoCanonico(t *testing.T) {
	// Perfil sintético: estes números não representam qualquer norma publicada.
	conteudo := []byte(`slug: perfil-sintetico
versao: 1
nome: Perfil sintético de teste
fonte: https://example.org/perfil
pagina:
  largura_cm: 20
  altura_cm: 28
  margens:
    superior_cm: 2
    inferior_cm: 2
    esquerda_cm: 2
    direita_cm: 2
corpo:
  fonte: Fonte Sintética
  tamanho_pt: 11
  entrelinha: 1.25
  recuo_cm: 0
  espaco_antes_pt: 0
  espaco_depois_pt: 0
  alinhamento: justificado
`)
	for _, caminho := range []string{"perfil-sintetico/v1.yaml", "outro/v1.yaml", "perfil-sintetico/v2.yaml", "perfil-sintetico/v01.yaml", "perfil-sintetico/v1.yml", "v1.yaml"} {
		t.Run(caminho, func(t *testing.T) {
			diretorio := t.TempDir()
			arquivo := filepath.Join(diretorio, caminho)
			require.NoError(t, os.MkdirAll(filepath.Dir(arquivo), 0700))
			require.NoError(t, os.WriteFile(arquivo, conteudo, 0600))
			err := executar([]string{"validar", "--dir", diretorio})
			if caminho == "perfil-sintetico/v1.yaml" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestArgumentosInvalidosNaoEncerramProcesso(t *testing.T) {
	for _, argumentos := range [][]string{nil, {"desconhecido"}, {"validar", "--inexistente"}, {"validar", "--dir"}, {"semear"}} {
		require.Error(t, executar(argumentos))
	}
}

func TestTravessiaLimitadaECancelavel(t *testing.T) {
	for _, sistema := range []fstest.MapFS{
		{"perfil/v1.yaml": {Mode: os.ModeNamedPipe}},
		{"a/b/c/d/e/f.yaml": {Data: []byte("{}")}},
	} {
		_, err := localizarYAMLs(context.Background(), sistema)
		require.Error(t, err)
	}
	ctx, cancelar := context.WithCancel(context.Background())
	cancelar()
	_, err := localizarYAMLs(ctx, fstest.MapFS{})
	require.ErrorIs(t, err, context.Canceled)
}

func TestFronteiraQuantidadeEntradas(t *testing.T) {
	sistema := fstest.MapFS{}
	for indice := 0; indice < 4095; indice++ {
		sistema[fmt.Sprintf("arquivo-%d.txt", indice)] = &fstest.MapFile{}
	}
	_, err := localizarYAMLs(context.Background(), sistema)
	require.NoError(t, err)
	sistema["excedente.txt"] = &fstest.MapFile{}
	_, err = localizarYAMLs(context.Background(), sistema)
	require.Error(t, err)
}

func TestTravessiaRecusaFIFOComoDiretorio(t *testing.T) {
	diretorio := t.TempDir()
	require.NoError(t, syscall.Mkfifo(filepath.Join(diretorio, "substituido"), 0600))
	raiz, err := os.OpenRoot(diretorio)
	require.NoError(t, err)
	defer raiz.Close()
	arquivo, err := (diretoriosConfinados{raiz: raiz}).Open("substituido")
	if arquivo != nil {
		arquivo.Close()
	}
	require.Error(t, err)
}
