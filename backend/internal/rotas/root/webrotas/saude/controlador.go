// Package saude expõe os endpoints de liveness e readiness da API.
package saude

import (
	"context"
	"net/http"
	"time"

	"github.com/daniel-halos/formatador/internal/infra/log"
	"github.com/daniel-halos/formatador/internal/rotas"
)

// Verificador é uma dependência externa cuja saúde é checada no readiness.
type Verificador interface {
	Nome() string
	Verificar(ctx context.Context) error
}

// tempoLimitePorVerificacao evita que uma dependência travada segure o readiness.
const tempoLimitePorVerificacao = 3 * time.Second

// Estados possíveis de uma dependência no readiness.
const (
	EstadoDisponivel   = "disponivel"
	EstadoIndisponivel = "indisponivel"
)

// motivoIndisponivel é o que o cliente vê quando uma dependência falha. O erro
// real vai só para o log: /prontidao é público e sem auth, e a mensagem do
// driver carrega endpoint, bucket e fragmento de credencial.
const motivoIndisponivel = "dependência indisponível"

// RespostaSaude é o corpo devolvido por /saude.
type RespostaSaude struct {
	Estado string `json:"estado"`
	Versao string `json:"versao"`
}

// RespostaProntidao é o corpo devolvido por /prontidao.
type RespostaProntidao struct {
	Pronto       bool                `json:"pronto"`
	Dependencias []EstadoDependencia `json:"dependencias"`
}

// EstadoDependencia descreve a situação de uma dependência externa.
type EstadoDependencia struct {
	Nome   string `json:"nome"`
	Estado string `json:"estado"`
	Erro   string `json:"erro,omitempty"`
}

// Controlador atende os endpoints de saúde.
type Controlador struct {
	versao        string
	verificadores []Verificador
}

// NovoControlador cria o controlador de saúde com as dependências a verificar.
func NovoControlador(versao string, verificadores ...Verificador) *Controlador {
	return &Controlador{versao: versao, verificadores: verificadores}
}

// TratarSaude responde ao liveness: se o processo atende, ele está vivo.
func (c *Controlador) TratarSaude(_ context.Context, _ rotas.Requisicao, resposta rotas.Resposta) error {
	return resposta.Ok(RespostaSaude{Estado: "ok", Versao: c.versao})
}

// TratarProntidao responde ao readiness, verificando cada dependência externa.
// Devolve 503 quando qualquer uma falha, para o orquestrador tirar a instância
// de rotação em vez de mandar tráfego para um processo que não consegue servir.
func (c *Controlador) TratarProntidao(ctx context.Context, _ rotas.Requisicao, resposta rotas.Resposta) error {
	estados := make([]EstadoDependencia, 0, len(c.verificadores))
	pronto := true

	for _, verificador := range c.verificadores {
		estado := c.verificar(ctx, verificador)
		if estado.Estado == EstadoIndisponivel {
			pronto = false
		}
		estados = append(estados, estado)
	}

	corpo := RespostaProntidao{Pronto: pronto, Dependencias: estados}
	if !pronto {
		return resposta.Erro(http.StatusServiceUnavailable, "dependencia_indisponivel", "alguma dependência está indisponível", razoesDe(estados))
	}
	return resposta.Ok(corpo)
}

func (c *Controlador) verificar(ctx context.Context, verificador Verificador) EstadoDependencia {
	ctx, cancelar := context.WithTimeout(ctx, tempoLimitePorVerificacao)
	defer cancelar()

	if err := verificador.Verificar(ctx); err != nil {
		log.De(ctx).Error("dependência indisponível no readiness", "dependencia", verificador.Nome(), "erro", err.Error())
		return EstadoDependencia{Nome: verificador.Nome(), Estado: EstadoIndisponivel, Erro: motivoIndisponivel}
	}
	return EstadoDependencia{Nome: verificador.Nome(), Estado: EstadoDisponivel}
}

func razoesDe(estados []EstadoDependencia) []string {
	var razoes []string
	for _, estado := range estados {
		if estado.Estado == EstadoIndisponivel {
			razoes = append(razoes, estado.Nome+": "+motivoIndisponivel)
		}
	}
	return razoes
}
