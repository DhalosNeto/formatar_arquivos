// Command api sobe o servidor HTTP do Formatador Acadêmico.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/daniel-halos/formatador/internal/data/postgres"
	"github.com/daniel-halos/formatador/internal/infra/config"
	"github.com/daniel-halos/formatador/internal/infra/log"
	"github.com/daniel-halos/formatador/internal/infra/storage"
	"github.com/daniel-halos/formatador/internal/infra/telemetry"
	"github.com/daniel-halos/formatador/internal/rotas/root"
	"github.com/daniel-halos/formatador/internal/rotas/root/webrotas/saude"
	"github.com/daniel-halos/formatador/internal/servidor"
)

// versao é sobrescrita na compilação via -ldflags.
var versao = "dev"

// prazoDesligamento é quanto tempo o servidor tem para terminar as requisições
// em andamento depois de receber SIGTERM.
const prazoDesligamento = 20 * time.Second

func main() {
	if err := executar(); err != nil {
		// O logger pode nem ter subido ainda, então este é o único lugar do
		// projeto que escreve direto no stderr.
		fmt.Fprintln(os.Stderr, "falha ao iniciar a api:", err)
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
	defer desligar(desligarTracing)

	registradorMetricas := prometheus.NewRegistry()
	metricas := telemetry.NovasMetricas(registradorMetricas)

	gerenciador, err := postgres.NovoGerenciador(ctx, cfg.Postgres)
	if err != nil {
		return err
	}
	defer gerenciador.Fechar()

	clienteStorage, err := storage.NovoClienteS3(ctx, cfg.Storage)
	if err != nil {
		return err
	}

	servidorHTTP := servidor.Novo(servidor.Opcoes{
		Config:      cfg,
		Metricas:    metricas,
		Registrador: registradorMetricas,
		OrigensCORS: origensCORS(),
		Dependencias: root.Dependencias{
			Saude: saude.NovoControlador(versao, gerenciador, storage.NovoVerificador(clienteStorage)),
		},
	})

	registrador.Info("api iniciando",
		"porta", cfg.Porta,
		"ambiente", cfg.Ambiente,
		"versao", versao,
	)

	erroServidor := make(chan error, 1)
	go func() {
		if err := servidorHTTP.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			erroServidor <- err
		}
	}()

	select {
	case err := <-erroServidor:
		return err
	case <-ctx.Done():
		registrador.Info("sinal de desligamento recebido, encerrando")
		return encerrar(servidorHTTP)
	}
}

func encerrar(servidorHTTP *http.Server) error {
	ctx, cancelar := context.WithTimeout(context.Background(), prazoDesligamento)
	defer cancelar()
	return servidorHTTP.Shutdown(ctx)
}

func desligar(fn telemetry.Desligar) {
	if err := fn(context.Background()); err != nil {
		log.De(context.Background()).Error("falha ao encerrar o tracing", "erro", err.Error())
	}
}

func origensCORS() []string {
	bruto := strings.TrimSpace(os.Getenv("CORS_ORIGENS"))
	if bruto == "" {
		return nil
	}

	var origens []string
	for _, origem := range strings.Split(bruto, ",") {
		if limpa := strings.TrimSpace(origem); limpa != "" {
			origens = append(origens, limpa)
		}
	}
	return origens
}
