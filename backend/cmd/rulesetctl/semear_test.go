package main

import (
	"context"
	"github.com/daniel-halos/formatador/internal/domain/ruleset"
	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

type seedFake struct {
	chamadas   int
	quantidade int
	erro       error
}

func (fake *seedFake) Semear(ctx context.Context, definicoes []ruleset.Definicao) error {
	fake.chamadas++
	fake.quantidade = len(definicoes)
	return fake.erro
}

func TestSemearValidaLoteAntesDaPorta(t *testing.T) {
	diretorio := t.TempDir()
	dados, err := os.ReadFile("../../internal/infra/ruleset/testdata/perfil-sintetico.yaml")
	require.NoError(t, err)
	require.NoError(t, os.Mkdir(filepath.Join(diretorio, "perfil-sintetico"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(diretorio, "perfil-sintetico", "v1.yaml"), dados, 0600))
	repo := &seedFake{}
	require.NoError(t, semear(context.Background(), diretorio, repo))
	require.Equal(t, 1, repo.chamadas)
	require.Equal(t, 1, repo.quantidade)
	require.NoError(t, os.Mkdir(filepath.Join(diretorio, "z-invalido"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(diretorio, "z-invalido", "v1.yaml"), []byte("{}"), 0600))
	require.Error(t, semear(context.Background(), diretorio, repo))
	require.Equal(t, 1, repo.chamadas)
}

func TestSemearVazioNaoGrava(t *testing.T) {
	repo := &seedFake{}
	require.NoError(t, semear(context.Background(), t.TempDir(), repo))
	require.Zero(t, repo.chamadas)
}

func TestSemearPreservaErroDaPorta(t *testing.T) {
	diretorio := t.TempDir()
	dados, err := os.ReadFile("../../internal/infra/ruleset/testdata/perfil-sintetico.yaml")
	require.NoError(t, err)
	require.NoError(t, os.Mkdir(filepath.Join(diretorio, "perfil-sintetico"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(diretorio, "perfil-sintetico", "v1.yaml"), dados, 0600))
	conflito := errors.NovoErroConflito("versão diferente")
	require.ErrorIs(t, semear(context.Background(), diretorio, &seedFake{erro: conflito}), conflito)
}
