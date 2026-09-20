// Package telemetry concentra métricas Prometheus e tracing OpenTelemetry.
package telemetry

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metricas agrupa os coletores do projeto. Use NovasMetricas para construir.
type Metricas struct {
	RequisicoesHTTP  *prometheus.CounterVec
	DuracaoHTTP      *prometheus.HistogramVec
	DuracaoJob       *prometheus.HistogramVec
	ProfundidadeFila *prometheus.GaugeVec
	ErrosParseDocx   prometheus.Counter
	TokensLLM        *prometheus.CounterVec
	FallbackLLM      prometheus.Counter
}

// NovasMetricas registra os coletores no registrador informado.
// Receber o registrador por parâmetro (em vez de usar o global) permite
// construir métricas isoladas em teste.
func NovasMetricas(registrador prometheus.Registerer) *Metricas {
	fabrica := promauto.With(registrador)

	return &Metricas{
		RequisicoesHTTP: fabrica.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requisicoes_total",
			Help: "Total de requisições HTTP atendidas.",
		}, []string{"metodo", "rota", "status"}),

		DuracaoHTTP: fabrica.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_duracao_segundos",
			Help:    "Duração das requisições HTTP em segundos.",
			Buckets: prometheus.DefBuckets,
		}, []string{"metodo", "rota"}),

		DuracaoJob: fabrica.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "job_duracao_segundos",
			Help:    "Duração dos jobs de processamento de documento em segundos.",
			Buckets: []float64{0.5, 1, 2.5, 5, 10, 30, 60, 120, 300},
		}, []string{"tipo", "status"}),

		ProfundidadeFila: fabrica.NewGaugeVec(prometheus.GaugeOpts{
			Name: "fila_profundidade",
			Help: "Quantidade de jobs aguardando processamento.",
		}, []string{"tipo"}),

		ErrosParseDocx: fabrica.NewCounter(prometheus.CounterOpts{
			Name: "docx_erros_parse_total",
			Help: "Total de falhas ao interpretar um arquivo DOCX.",
		}),

		TokensLLM: fabrica.NewCounterVec(prometheus.CounterOpts{
			Name: "llm_tokens_total",
			Help: "Tokens consumidos pelo classificador de estrutura.",
		}, []string{"tipo"}),

		FallbackLLM: fabrica.NewCounter(prometheus.CounterOpts{
			Name: "llm_fallback_total",
			Help: "Total de blocos enviados ao LLM por baixa confiança da heurística.",
		}),
	}
}
