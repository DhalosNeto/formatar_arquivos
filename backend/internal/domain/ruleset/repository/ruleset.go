package repository

import (
	"context"

	"github.com/google/uuid"

	"github.com/daniel-halos/formatador/internal/domain/ruleset"
)

// RulesetRepo persiste todo o lote ou nada. Versões existentes são imutáveis.
type RulesetRepo interface {
	Semear(context.Context, []ruleset.Definicao) error
}

// ConsultaRulesetRepo lê um perfil já semeado pelo seu ID.
//
// Porta separada de RulesetRepo porque ler catálogo e semear catálogo são casos
// de uso distintos: quem semeia (cmd/rulesetctl) não lê, e quem lê não semeia.
//
// O segundo retorno é o flag ATIVO da linha, DEVOLVIDO e não aplicado: a
// leitura por ID não filtra por ativo, para que um job já existente continue
// executável depois de a versão sair de circulação.
//
// O gate pertence à CRIAÇÃO do job e NÃO consome esta porta: ele faz query
// própria (data/postgres/job.go), porque precisa ler `ativo` com FOR SHARE
// dentro da MESMA transação que insere o job, e porque ObterPorID
// materializaria a `definicao` inteira (até 64 KiB) para ler um booleano.
//
// Não leva vo.Dono: ruleset é catálogo global, a tabela não tem coluna de dono.
type ConsultaRulesetRepo interface {
	ObterPorID(context.Context, uuid.UUID) (ruleset.Definicao, bool, error)
}
