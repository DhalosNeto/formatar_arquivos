package repository

import (
	"context"
	"github.com/daniel-halos/formatador/internal/domain/ruleset"
)

// RulesetRepo persiste todo o lote ou nada. Versões existentes são imutáveis.
type RulesetRepo interface {
	Semear(context.Context, []ruleset.Definicao) error
}
