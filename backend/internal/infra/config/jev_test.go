package config_test

import (
	"fmt"
	"github.com/daniel-halos/formatador/internal/infra/config"
	"strings"
	"testing"
)

func TestJevPadraoEAtivacao(t *testing.T) {
	t.Setenv("POSTGRES_DSN", dsnDeTeste)
	t.Setenv("JEV_HABILITADO", "false")
	cfg, err := config.Carregar()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Jev.Habilitado || cfg.Jev.Modelo != "jev-latest" {
		t.Fatal("padrões incorretos")
	}
	t.Setenv("JEV_HABILITADO", "true")
	t.Setenv("TYPESAFE_API_KEY", "segredo-exclusivo-jev")
	cfg, err = config.Carregar()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Jev.Habilitado || cfg.Jev.LimiteAutomatico != 0.95 {
		t.Fatal("ativação incorreta")
	}
	for _, valor := range []any{cfg.Jev, cfg} {
		for _, formato := range []string{"%v", "%+v", "%#v"} {
			if strings.Contains(fmt.Sprintf(formato, valor), "segredo-exclusivo-jev") {
				t.Fatal("vazou chave")
			}
		}
	}
}
func TestJevRecusaConfiguracaoInvalida(t *testing.T) {
	for _, caso := range []struct{ chave, valor string }{
		{"JEV_HABILITADO", "talvez"},
		{"TYPESAFE_API_KEY", ""},
		{"JEV_LIMITE_CONSULTA", "NaN"},
		{"JEV_LIMITE_CONFIRMACAO", "+Inf"},
		{"JEV_LIMITE_AUTOMATICO", "1.1"},
		{"JEV_LIMITE_AUTOMATICO", "0.6"},
		{"JEV_LIMITE_CONFIRMACAO", "0.95"},
	} {
		t.Run(caso.chave+caso.valor, func(t *testing.T) {
			t.Setenv("POSTGRES_DSN", dsnDeTeste)
			t.Setenv("JEV_HABILITADO", "true")
			t.Setenv("TYPESAFE_API_KEY", "fake-chave")
			t.Setenv(caso.chave, caso.valor)
			if _, err := config.Carregar(); err == nil {
				t.Fatal("configuração inválida aceita")
			}
		})
	}
}
