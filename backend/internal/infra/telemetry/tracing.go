package telemetry

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"

	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// Desligar encerra o provedor de tracing, liberando os spans pendentes.
type Desligar func(context.Context) error

// IniciarTracing configura o provedor OpenTelemetry. Quando o endpoint OTLP
// está vazio, o tracing é desativado e o desligamento vira um no-op — assim a
// aplicação sobe em desenvolvimento sem depender do coletor.
func IniciarTracing(ctx context.Context, endpointOTLP, nomeServico, ambiente string) (Desligar, error) {
	if endpointOTLP == "" {
		return func(context.Context) error { return nil }, nil
	}

	exportador, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpointOTLP))
	if err != nil {
		return nil, errors.Envolver(err, "ao criar o exportador OTLP")
	}

	// A versão do semconv importado acima precisa bater com a que
	// resource.Default() usa no SDK em uso: Merge RECUSA schemas
	// conflitantes em vez de escolher um. Com v1.34.0 contra o Default do
	// SDK 1.46.0 (v1.43.0), isto falhava e derrubava api e worker na
	// partida — mas SÓ com OTEL_EXPORTER_OTLP_ENDPOINT definido, que é o
	// caso do compose e não o do `go run` local. Ao subir o SDK, confira
	// este import junto.
	recursos, err := resource.Merge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(nomeServico),
		semconv.DeploymentEnvironmentNameKey.String(ambiente),
	))
	if err != nil {
		return nil, errors.Envolver(err, "ao montar os atributos do serviço")
	}

	provedor := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exportador),
		sdktrace.WithResource(recursos),
	)

	otel.SetTracerProvider(provedor)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return func(ctx context.Context) error {
		ctx, cancelar := context.WithTimeout(ctx, 5*time.Second)
		defer cancelar()
		return provedor.Shutdown(ctx)
	}, nil
}
