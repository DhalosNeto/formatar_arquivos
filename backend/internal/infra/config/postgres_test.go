package config

import (
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestCarregarPostgresIsolado(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://usuario:senha@example.invalid/banco")
	t.Setenv("AMBIENTE", "producao")
	t.Setenv("STORAGE_ACCESS_KEY", "")
	t.Setenv("STORAGE_SECRET_KEY", "")
	t.Setenv("LLM_HABILITADO", "true")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("POSTGRES_MAX_CONEXOES", "3")
	t.Setenv("POSTGRES_TEMPO_LIMITE", "2s")
	cfg, err := CarregarPostgres()
	require.NoError(t, err)
	require.EqualValues(t, 3, cfg.MaxConexoes)
	require.Equal(t, 2*time.Second, cfg.TempoLimiteConexao)
	t.Setenv("POSTGRES_DSN", "")
	_, err = CarregarPostgres()
	require.Error(t, err)
}
