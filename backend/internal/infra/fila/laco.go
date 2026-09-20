// Package fila implementa o laço de consumo da fila de jobs sobre a própria
// tabela `jobs` (ver docs/adr/0002-fila-sem-river.md): sem dependência nova,
// FOR UPDATE SKIP LOCKED faz o papel que o River faria.
package fila

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/daniel-halos/formatador/internal/domain/job/entity"
	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/daniel-halos/formatador/internal/infra/log"
)

// Reivindicador tira o próximo job pendente da fila. ok=false com job zero e
// err nil significa fila vazia — não é erro.
type Reivindicador interface {
	Reivindicar(ctx context.Context) (job entity.Job, ok bool, err error)
}

// Executor roda o trabalho de fato para o job reivindicado e devolve o
// resultado a persistir.
type Executor interface {
	Executar(ctx context.Context, job entity.Job) (resultado json.RawMessage, err error)
}

// Finalizador persiste a conclusão ou a falha de um job já reivindicado.
type Finalizador interface {
	Concluir(ctx context.Context, id uuid.UUID, resultado json.RawMessage) (entity.Job, error)
	Falhar(ctx context.Context, id uuid.UUID, motivo string) (entity.Job, error)
}

// Laco consome a fila de jobs por polling.
type Laco struct {
	reivindicador Reivindicador
	executor      Executor
	finalizador   Finalizador
	intervalo     time.Duration
}

// NovoLaco valida as dependências e monta o laço de consumo.
func NovoLaco(reivindicador Reivindicador, executor Executor, finalizador Finalizador, intervalo time.Duration) (*Laco, error) {
	if reivindicador == nil {
		return nil, errors.NovoErroArgumentoNulo("reivindicador")
	}
	if executor == nil {
		return nil, errors.NovoErroArgumentoNulo("executor")
	}
	if finalizador == nil {
		return nil, errors.NovoErroArgumentoNulo("finalizador")
	}
	if intervalo <= 0 {
		return nil, errors.NovoErroValidacao("intervalo", "precisa ser maior que zero")
	}
	return &Laco{reivindicador: reivindicador, executor: executor, finalizador: finalizador, intervalo: intervalo}, nil
}

// Executar roda até ctx ser cancelado e devolve nil no desligamento
// gracioso, nunca ctx.Err(). Um job já reivindicado é sempre terminado
// (Concluir ou Falhar) antes do laço checar de novo o cancelamento: o
// trabalho e a finalização rodam com context.WithoutCancel, para que o
// cancelamento externo não abandone um job em "executando".
func (l *Laco) Executar(ctx context.Context) error {
	for {
		job, ok, err := l.reivindicador.Reivindicar(ctx)
		if err != nil {
			log.De(ctx).Error("falha ao reivindicar job")
			if aguardarOuCancelar(ctx, l.intervalo) {
				return nil
			}
			continue
		}
		if !ok {
			if aguardarOuCancelar(ctx, l.intervalo) {
				return nil
			}
			continue
		}

		l.processar(ctx, job)

		select {
		case <-ctx.Done():
			// Desligamento gracioso: o job em voo já foi terminado por
			// l.processar acima; devolve nil por contrato, nunca ctx.Err().
			return nil
		default:
		}
	}
}

// processar executa e finaliza o job com context.WithoutCancel: o job já foi
// reivindicado (status=executando no banco), então o cancelamento do ctx
// externo não pode deixá-lo preso ali.
func (l *Laco) processar(ctx context.Context, job entity.Job) {
	ctxTrabalho := context.WithoutCancel(ctx)
	logger := log.De(ctxTrabalho).With(log.AtributoIDJob, job.ID.String(), "tipo_job", job.Tipo.String())

	resultado, err := l.executor.Executar(ctxTrabalho, job)
	if err != nil {
		logger.Warn("job falhou durante a execução")
		if _, err := l.finalizador.Falhar(ctxTrabalho, job.ID, "falha na execução do job"); err != nil {
			logger.Error("falha ao gravar job como falhou")
		}
		return
	}

	if _, err := l.finalizador.Concluir(ctxTrabalho, job.ID, resultado); err != nil {
		logger.Error("falha ao gravar job como concluído")
	}
}

// aguardarOuCancelar espera intervalo, respeitando o cancelamento do ctx.
// Devolve true quando o ctx foi cancelado durante a espera.
func aguardarOuCancelar(ctx context.Context, intervalo time.Duration) bool {
	temporizador := time.NewTimer(intervalo)
	defer temporizador.Stop()
	select {
	case <-ctx.Done():
		return true
	case <-temporizador.C:
		return false
	}
}
