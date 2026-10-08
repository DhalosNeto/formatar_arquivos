package main

import (
	"github.com/daniel-halos/formatador/internal/infra/config"
	"testing"
)

func TestMontarFallbackFlag(t *testing.T) {
	if fallback, err := montarFallback(config.Jev{}); err != nil || fallback != nil {
		t.Fatal("flag desligada precisa dispensar credencial")
	}
	if _, err := montarFallback(config.Jev{Habilitado: true}); err == nil {
		t.Fatal("ativação sem chave aceita")
	}
	cfg := config.Jev{Habilitado: true, ChaveAPI: "fake", Modelo: "jev-latest", LimiteConsulta: 0.7, LimiteConfirmacao: 0.7, LimiteAutomatico: 0.95}
	if fallback, err := montarFallback(cfg); err != nil || fallback == nil {
		t.Fatal("ativação válida falhou", err)
	}
}
