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
	"time"

	"github.com/daniel-halos/formatador/internal/data/postgres"
	"github.com/daniel-halos/formatador/internal/domain/documento/processamento"
	"github.com/daniel-halos/formatador/internal/domain/job/execucao"
	"github.com/daniel-halos/formatador/internal/infra/config"
	"github.com/daniel-halos/formatador/internal/infra/fila"
	"github.com/daniel-halos/formatador/internal/infra/log"
	"github.com/daniel-halos/formatador/internal/infra/pdfconv"
	"github.com/daniel-halos/formatador/internal/infra/storage"
	"github.com/daniel-halos/formatador/internal/infra/telemetry"
)

// intervaloPolling é a espera do laço entre duas tentativas de reivindicar
// job quando a fila está vazia (ver docs/adr/0002-fila-sem-river.md).
const intervaloPolling = 2 * time.Second

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

	clienteStorage, err := storage.NovoClienteS3(ctx, cfg.Storage)
	if err != nil {
		return err
	}

	conversor, err := pdfconv.NovoCliente(cfg.Conversor)
	if err != nil {
		return err
	}

	documentosInternos, err := processamento.NovoServicoInterno(gerenciador.DocumentosInternos())
	if err != nil {
		return err
	}

	executor, err := fila.NovoExecutorDocumento(documentosInternos, clienteStorage, conversor)
	if err != nil {
		return err
	}

	finalizador, err := execucao.NovoServicoInterno(gerenciador.JobsExecucao())
	if err != nil {
		return err
	}

	laco, err := fila.NovoLaco(gerenciador.JobsReivindicacao(), executor, finalizador, intervaloPolling)
	if err != nil {
		return err
	}

	registrador.Info("worker iniciado", "ambiente", cfg.Ambiente, "versao", versao)

	if err := laco.Executar(ctx); err != nil {
		return err
	}
	registrador.Info("sinal de desligamento recebido, encerrando worker")

	return desligarTracing(context.Background())
}
