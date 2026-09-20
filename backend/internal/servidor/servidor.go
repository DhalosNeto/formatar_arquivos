// Package servidor monta o servidor HTTP da API: middlewares globais, rotas e
// o endpoint de métricas.
package servidor

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	echomiddleware "github.com/labstack/echo/v4/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/daniel-halos/formatador/internal/infra/config"
	"github.com/daniel-halos/formatador/internal/infra/telemetry"
	"github.com/daniel-halos/formatador/internal/rotas"
	"github.com/daniel-halos/formatador/internal/rotas/middleware"
	"github.com/daniel-halos/formatador/internal/rotas/root"
	"github.com/daniel-halos/formatador/internal/rotas/root/webrotas/documentos"
)

// CaminhoMetricas é onde o Prometheus raspa as métricas da aplicação.
const CaminhoMetricas = "/metrics"

// tamanhoMaximoCorpo limita o corpo de requisições que não são upload.
// O limite específico de upload é aplicado na rota de documentos, por
// rotas.LimitarCorpo.
//
// Este middleware é global e roda ANTES dos middlewares de rota, então sem o
// Skipper abaixo ele decidiria sozinho o teto de upload e o limite da rota
// nunca seria alcançado — era o que fazia um DOCX de 1 MB voltar 413.
const tamanhoMaximoCorpo = "1M"

// Opcoes reúne tudo que o servidor precisa para ser montado.
type Opcoes struct {
	Config       config.Config
	Metricas     *telemetry.Metricas
	Registrador  *prometheus.Registry
	Dependencias root.Dependencias
	OrigensCORS  []string
}

// Novo monta o servidor HTTP pronto para escutar.
func Novo(opcoes Opcoes) *http.Server {
	servidorEcho := echo.New()
	servidorEcho.HideBanner = true
	servidorEcho.HidePort = true

	aplicarMiddlewaresGlobais(servidorEcho, opcoes)

	servidorEcho.GET(CaminhoMetricas, echo.WrapHandler(promhttp.HandlerFor(
		opcoes.Registrador,
		promhttp.HandlerOpts{Registry: opcoes.Registrador},
	)))

	rotas.AplicarEmEcho(
		servidorEcho,
		root.Roteador(opcoes.Dependencias),
		root.PrefixoAPI,
		MiddlewaresPadrao(opcoes.Metricas)...,
	)

	return &http.Server{
		Addr:              ":" + opcoes.Config.Porta,
		Handler:           servidorEcho,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}
}

// ehUploadDeDocumento isenta a rota de upload do teto global de corpo. O teto
// real dela vem de rotas.LimitarCorpo, montado em documentos.Roteador com o
// valor de negócio — não com "1M".
func ehUploadDeDocumento(contexto echo.Context) bool {
	return contexto.Request().Method == http.MethodPost &&
		contexto.Path() == root.PrefixoAPI+documentos.CaminhoColecao
}

func aplicarMiddlewaresGlobais(servidorEcho *echo.Echo, opcoes Opcoes) {
	servidorEcho.Use(echomiddleware.BodyLimitWithConfig(echomiddleware.BodyLimitConfig{
		Limit:   tamanhoMaximoCorpo,
		Skipper: ehUploadDeDocumento,
	}))
	servidorEcho.Use(echomiddleware.SecureWithConfig(echomiddleware.SecureConfig{
		XFrameOptions:      "DENY",
		ContentTypeNosniff: "nosniff",
		HSTSMaxAge:         31536000,
		ReferrerPolicy:     "strict-origin-when-cross-origin",
	}))
	// AllowOrigins vazio faz o Echo liberar "*", o que somado a
	// AllowCredentials permitiria qualquer site ler respostas autenticadas.
	// Por isso a origem é sempre explícita.
	servidorEcho.Use(echomiddleware.CORSWithConfig(echomiddleware.CORSConfig{
		AllowOrigins:     origensPermitidas(opcoes),
		AllowCredentials: true,
		AllowHeaders:     []string{echo.HeaderContentType, echo.HeaderAccept, middleware.CabecalhoIDRequisicao},
		AllowMethods:     []string{http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		MaxAge:           600,
	}))
}

// origemPadraoDesenvolvimento é o endereço do front local servido pelo Vite.
const origemPadraoDesenvolvimento = "http://localhost:5173"

func origensPermitidas(opcoes Opcoes) []string {
	if len(opcoes.OrigensCORS) > 0 {
		return opcoes.OrigensCORS
	}
	if opcoes.Config.EhDesenvolvimento() {
		return []string{origemPadraoDesenvolvimento}
	}
	// Sem origem configurada em produção, nenhuma origem cruzada é aceita.
	return []string{}
}

// MiddlewaresPadrao é a pilha aplicada a toda rota da API, do mais externo ao
// mais interno.
func MiddlewaresPadrao(metricas *telemetry.Metricas) []rotas.Middleware {
	return []rotas.Middleware{
		middleware.IdentificarRequisicao(),
		middleware.RecuperarDePanico(),
		middleware.Medir(metricas),
		middleware.RegistrarAcesso(),
	}
}
