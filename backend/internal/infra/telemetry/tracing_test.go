package telemetry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIniciarTracingComEndpointDefinidoMontaOsRecursos cobre o ramo que
// derrubou api e worker em produção containerizada e que nenhum teste tocava.
//
// Com OTEL_EXPORTER_OTLP_ENDPOINT vazio — o caso do `go run` local — a função
// retorna na primeira linha e nunca chega em resource.Merge. Só o compose
// define o endpoint, então o erro de schema conflitante só aparecia lá:
// "conflicting Schema URL: .../1.43.0 and .../1.34.0", e os dois processos
// morriam na partida, em laço de restart.
//
// O teste NÃO precisa de coletor de pé: otlptracehttp.New só monta o cliente,
// a conexão é preguiçosa. O que se verifica aqui é a montagem dos recursos.
func TestIniciarTracingComEndpointDefinidoMontaOsRecursos(t *testing.T) {
	desligar, err := IniciarTracing(
		context.Background(),
		"http://localhost:4318",
		"formatador-teste",
		"desenvolvimento",
	)

	require.NoError(t, err,
		"a versão do semconv importado precisa bater com a de resource.Default() do SDK em uso")
	require.NotNil(t, desligar)

	assert.NoError(t, desligar(context.Background()))
}

// TestIniciarTracingSemEndpointDesativaSemErro trava o contrato de
// desenvolvimento: sem coletor configurado, a aplicação sobe assim mesmo.
func TestIniciarTracingSemEndpointDesativaSemErro(t *testing.T) {
	desligar, err := IniciarTracing(context.Background(), "", "formatador-teste", "desenvolvimento")

	require.NoError(t, err)
	require.NotNil(t, desligar)
	assert.NoError(t, desligar(context.Background()), "o desligamento precisa ser no-op, não pânico")
}
