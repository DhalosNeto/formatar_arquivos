package config_test

import (
	"testing"
	"time"

	"github.com/daniel-halos/formatador/internal/infra/config"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

const dsnDeTeste = "postgres://formatador:formatador@localhost:5432/formatador?sslmode=disable"

func TestCarregarUsaPadroesEmDesenvolvimento(t *testing.T) {
	t.Setenv("POSTGRES_DSN", dsnDeTeste)

	cfg, err := config.Carregar()
	if err != nil {
		t.Fatalf("não esperava erro, obteve %v", err)
	}

	if cfg.Porta != "8080" {
		t.Fatalf("esperava porta 8080, obteve %q", cfg.Porta)
	}
	if !cfg.EhDesenvolvimento() {
		t.Fatalf("esperava ambiente de desenvolvimento, obteve %q", cfg.Ambiente)
	}
	if cfg.LLM.Modelo != "claude-opus-5" {
		t.Fatalf("esperava modelo claude-opus-5, obteve %q", cfg.LLM.Modelo)
	}
	if cfg.LLM.Habilitado {
		t.Fatal("esperava LLM desabilitado por padrão")
	}
}

func TestCarregarLeVariaveisDeAmbiente(t *testing.T) {
	t.Setenv("POSTGRES_DSN", dsnDeTeste)
	t.Setenv("PORTA", "9090")
	t.Setenv("AMBIENTE", "producao")
	t.Setenv("STORAGE_ACCESS_KEY", "chave")
	t.Setenv("STORAGE_SECRET_KEY", "segredo")
	t.Setenv("CONVERSOR_TEMPO_LIMITE", "30s")
	t.Setenv("POSTGRES_MAX_CONEXOES", "25")

	cfg, err := config.Carregar()
	if err != nil {
		t.Fatalf("não esperava erro, obteve %v", err)
	}

	if cfg.Porta != "9090" {
		t.Fatalf("esperava porta 9090, obteve %q", cfg.Porta)
	}
	if cfg.EhDesenvolvimento() {
		t.Fatal("esperava ambiente de produção")
	}
	if cfg.Conversor.TempoLimite != 30*time.Second {
		t.Fatalf("esperava 30s, obteve %v", cfg.Conversor.TempoLimite)
	}
	if cfg.Postgres.MaxConexoes != 25 {
		t.Fatalf("esperava 25 conexões, obteve %d", cfg.Postgres.MaxConexoes)
	}
}

func TestCarregarIgnoraValorInvalidoEUsaPadrao(t *testing.T) {
	t.Setenv("POSTGRES_DSN", dsnDeTeste)
	t.Setenv("POSTGRES_MAX_CONEXOES", "muitas")
	t.Setenv("CONVERSOR_TEMPO_LIMITE", "sempre")

	cfg, err := config.Carregar()
	if err != nil {
		t.Fatalf("não esperava erro, obteve %v", err)
	}

	if cfg.Postgres.MaxConexoes != 10 {
		t.Fatalf("esperava o padrão 10, obteve %d", cfg.Postgres.MaxConexoes)
	}
	if cfg.Conversor.TempoLimite != 2*time.Minute {
		t.Fatalf("esperava o padrão 2m, obteve %v", cfg.Conversor.TempoLimite)
	}
}

func TestCarregarReprovaConfiguracaoInvalida(t *testing.T) {
	casos := []struct {
		nome          string
		ambiente      map[string]string
		campoEsperado string
	}{
		{
			nome:          "dsn ausente",
			ambiente:      map[string]string{"POSTGRES_DSN": ""},
			campoEsperado: "POSTGRES_DSN",
		},
		{
			nome:          "llm habilitado sem chave",
			ambiente:      map[string]string{"POSTGRES_DSN": dsnDeTeste, "LLM_HABILITADO": "true", "ANTHROPIC_API_KEY": ""},
			campoEsperado: "ANTHROPIC_API_KEY",
		},
		{
			nome:          "producao sem credencial de storage",
			ambiente:      map[string]string{"POSTGRES_DSN": dsnDeTeste, "AMBIENTE": "producao"},
			campoEsperado: "STORAGE_ACCESS_KEY",
		},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			for chave, valor := range caso.ambiente {
				t.Setenv(chave, valor)
			}

			_, err := config.Carregar()
			if err == nil {
				t.Fatal("esperava erro de validação")
			}

			var invalido *errors.ErroValidacao
			if !errors.Como(err, &invalido) {
				t.Fatalf("esperava *ErroValidacao, obteve %T", err)
			}
			if !contemCampo(invalido, caso.campoEsperado) {
				t.Fatalf("esperava o campo %q entre %v", caso.campoEsperado, invalido.Campos)
			}
		})
	}
}

func contemCampo(erro *errors.ErroValidacao, campo string) bool {
	for _, invalido := range erro.Campos {
		if invalido.Campo == campo {
			return true
		}
	}
	return false
}
