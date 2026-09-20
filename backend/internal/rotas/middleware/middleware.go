// Package middleware reúne os middlewares HTTP do projeto, escritos sobre o
// contrato de rotas (não sobre o Echo).
package middleware

import (
	"context"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/daniel-halos/formatador/internal/infra/log"
	"github.com/daniel-halos/formatador/internal/infra/telemetry"
	"github.com/daniel-halos/formatador/internal/rotas"
	"github.com/daniel-halos/formatador/internal/rotas/rotasutil"
)

// CabecalhoIDRequisicao é o cabeçalho usado para correlacionar requisições.
const CabecalhoIDRequisicao = "X-Request-Id"

// IdentificarRequisicao garante que toda requisição tenha um identificador de
// correlação, reaproveitando o do cliente quando ele envia um.
func IdentificarRequisicao() rotas.Middleware {
	return func(proximo rotas.Manipulador) rotas.Manipulador {
		return func(ctx context.Context, requisicao rotas.Requisicao, resposta rotas.Resposta) error {
			id := requisicao.Cabecalho(CabecalhoIDRequisicao)
			if id == "" {
				id = uuid.NewString()
			}

			resposta.DefinirCabecalho(CabecalhoIDRequisicao, id)
			return proximo(log.ComIDRequisicao(ctx, id), requisicao, resposta)
		}
	}
}

// RegistrarAcesso loga início e fim de cada requisição. Nunca loga corpo:
// conteúdo de documento do usuário não vai para o log.
func RegistrarAcesso() rotas.Middleware {
	return func(proximo rotas.Manipulador) rotas.Manipulador {
		return func(ctx context.Context, requisicao rotas.Requisicao, resposta rotas.Resposta) error {
			inicio := time.Now()

			err := proximo(ctx, requisicao, resposta)

			log.De(ctx).Info("requisição atendida",
				"metodo", requisicao.Metodo(),
				"rota", requisicao.Caminho(),
				"status", resposta.Status(),
				"duracao_ms", time.Since(inicio).Milliseconds(),
			)
			return err
		}
	}
}

// RecuperarDePanico transforma um panic em erro interno, mantendo o servidor de pé.
func RecuperarDePanico() rotas.Middleware {
	return func(proximo rotas.Manipulador) rotas.Manipulador {
		return func(ctx context.Context, requisicao rotas.Requisicao, resposta rotas.Resposta) (err error) {
			defer func() {
				if recuperado := recover(); recuperado != nil {
					log.De(ctx).Error("panic durante a requisição",
						"rota", requisicao.Caminho(),
						"panico", recuperado,
					)
					err = rotasutil.TratarErro(ctx, resposta, errors.NovoErroAplicacao("falha inesperada"))
				}
			}()

			return proximo(ctx, requisicao, resposta)
		}
	}
}

// Medir alimenta as métricas Prometheus de contagem e duração das requisições.
// A rota usada como rótulo é o padrão registrado (/documentos/:id), nunca o
// caminho concreto, para não explodir a cardinalidade.
func Medir(metricas *telemetry.Metricas) rotas.Middleware {
	return func(proximo rotas.Manipulador) rotas.Manipulador {
		return func(ctx context.Context, requisicao rotas.Requisicao, resposta rotas.Resposta) error {
			inicio := time.Now()

			err := proximo(ctx, requisicao, resposta)

			metodo, rota := requisicao.Metodo(), requisicao.Caminho()

			metricas.DuracaoHTTP.With(prometheus.Labels{
				"metodo": metodo,
				"rota":   rota,
			}).Observe(time.Since(inicio).Seconds())

			metricas.RequisicoesHTTP.With(prometheus.Labels{
				"metodo": metodo,
				"rota":   rota,
				"status": strconv.Itoa(resposta.Status()),
			}).Inc()

			return err
		}
	}
}
