package config

import (
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// Jev configura o fallback experimental; os limites precisam de calibração no domínio.
type Jev struct {
	Habilitado        bool
	ChaveAPI          string
	Modelo            string
	LimiteConsulta    float64
	LimiteConfirmacao float64
	LimiteAutomatico  float64
}

func (j Jev) String() string   { return "config.Jev{credencial:omitida}" }
func (j Jev) GoString() string { return j.String() }

func carregarJev() (Jev, error) {
	cfg := Jev{ChaveAPI: texto("TYPESAFE_API_KEY", ""), Modelo: texto("JEV_MODELO", "jev-latest"), LimiteConsulta: 0.7, LimiteConfirmacao: 0.7, LimiteAutomatico: 0.95}
	if bruto := strings.TrimSpace(os.Getenv("JEV_HABILITADO")); bruto != "" {
		valor, err := strconv.ParseBool(bruto)
		if err != nil {
			return Jev{}, errors.NovoErroValidacao("JEV_HABILITADO", "valor booleano inválido")
		}
		cfg.Habilitado = valor
	}
	for _, item := range []struct {
		nome    string
		destino *float64
	}{
		{"JEV_LIMITE_CONSULTA", &cfg.LimiteConsulta},
		{"JEV_LIMITE_CONFIRMACAO", &cfg.LimiteConfirmacao},
		{"JEV_LIMITE_AUTOMATICO", &cfg.LimiteAutomatico},
	} {
		if bruto := strings.TrimSpace(os.Getenv(item.nome)); bruto != "" {
			valor, err := strconv.ParseFloat(bruto, 64)
			if err != nil || math.IsNaN(valor) || math.IsInf(valor, 0) || valor < 0 || valor > 1 {
				return Jev{}, errors.NovoErroValidacao(item.nome, "limite precisa ser finito entre zero e um")
			}
			*item.destino = valor
		}
	}
	if cfg.LimiteConfirmacao >= cfg.LimiteAutomatico || cfg.LimiteConsulta > cfg.LimiteAutomatico {
		return Jev{}, errors.NovoErroValidacao("JEV_LIMITES", "limites de confiança incompatíveis")
	}
	if cfg.Habilitado && (cfg.ChaveAPI == "" || strings.ContainsAny(cfg.ChaveAPI, "\r\n") || len(cfg.Modelo) > 80) {
		return Jev{}, errors.NovoErroValidacao("TYPESAFE_API_KEY", "credencial ou modelo obrigatório e válido com Jev habilitado")
	}
	return cfg, nil
}
