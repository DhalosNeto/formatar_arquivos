// Package repository declara a porta de acesso a dados de job. A interface
// mora no domínio; a implementação vive em internal/data.
package repository

import (
	"context"

	"github.com/daniel-halos/formatador/internal/domain/job/entity"
)

// ReivindicacaoJobRepo é a porta da fila: pega o próximo trabalho disponível.
//
// Porta separada de ExecucaoJobRepo de propósito, seguindo o padrão das demais
// (consulta, criação, execução): reivindicar é o caso de uso de QUEM PROCURA
// trabalho, enquanto executar é o de quem JÁ TEM um job em mãos.
// job/execucao.ServicoInterno nunca reivindica, e a fila nunca transita status
// — juntar as duas numa interface só obrigaria cada lado a declarar um método
// que não usa.
type ReivindicacaoJobRepo interface {
	// Reivindicar seleciona o job pendente mais antigo com FOR UPDATE SKIP
	// LOCKED, marca status=executando, incrementa tentativas e grava
	// iniciado_em, tudo em uma única transação. Trabalhadores concorrentes
	// pulam a linha travada em vez de esperar por ela.
	//
	// ok=false com err nil significa fila vazia — é o caso normal de um worker
	// ocioso, nunca um erro.
	Reivindicar(ctx context.Context) (job entity.Job, ok bool, err error)
}
