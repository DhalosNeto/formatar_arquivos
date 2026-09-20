// Command worker consome a fila de jobs de processamento de documento.
//
// O worker já abre e fecha o pool do Postgres; o consumo da fila é o próximo
// recorte.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/daniel-halos/formatador/internal/data/postgres"
	"github.com/daniel-halos/formatador/internal/infra/config"
	"github.com/daniel-halos/formatador/internal/infra/log"
	"github.com/daniel-halos/formatador/internal/infra/telemetry"
)

var versao = "dev"

func main() {
	if err := executar(); err != nil {
		fmt.Fprintln(os.Stderr, "falha ao iniciar o worker:", err)
		os.Exit(1)
	}
}

func executar() error {
	cfg, err := config.Carregar()
	if err != nil {
		return err
	}

	registrador := log.NovoPadrao(cfg.NivelLog)

	ctx, cancelar := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancelar()

	desligarTracing, err := telemetry.IniciarTracing(ctx, cfg.Telemetria.EndpointOTLP, cfg.Telemetria.NomeServico, cfg.Ambiente)
	if err != nil {
		return err
	}

	gerenciador, err := postgres.NovoGerenciador(ctx, cfg.Postgres)
	if err != nil {
		return err
	}
	defer gerenciador.Fechar()

	registrador.Info("worker iniciado", "ambiente", cfg.Ambiente, "versao", versao)

	<-ctx.Done()
	registrador.Info("sinal de desligamento recebido, encerrando worker")

	return desligarTracing(context.Background())
}
