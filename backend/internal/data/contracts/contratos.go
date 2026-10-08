// Package contracts é a fachada única de acesso a dados (CLAUDE.md, regra 6).
// As demais camadas dependem só destes tipos, nunca de internal/data/postgres
// diretamente, para que a implementação concreta permaneça um detalhe de infra.
package contracts

import (
	documentorepo "github.com/daniel-halos/formatador/internal/domain/documento/repository"
	jobrepo "github.com/daniel-halos/formatador/internal/domain/job/repository"
	rulesetrepo "github.com/daniel-halos/formatador/internal/domain/ruleset/repository"
)

// Aliases (não tipos definidos) para preservar a identidade de tipo das
// interfaces do domínio: quem faz errors.As ou implementa a interface de um
// lado enxerga exatamente o mesmo tipo do outro lado.
type (
	RulesetRepo          = rulesetrepo.RulesetRepo
	ConsultaRulesetRepo  = rulesetrepo.ConsultaRulesetRepo
	DocumentoRepo        = documentorepo.DocumentoRepo
	EstruturaRepo        = documentorepo.EstruturaRepo
	DocumentoInternoRepo = documentorepo.DocumentoInternoRepo
	ConsultaJobRepo      = jobrepo.ConsultaJobRepo
	CriacaoJobRepo       = jobrepo.CriacaoJobRepo
	ExecucaoJobRepo      = jobrepo.ExecucaoJobRepo
	ReivindicacaoJobRepo = jobrepo.ReivindicacaoJobRepo
)

// GerenciadorDados agrupa os repositórios do sistema e o ciclo de vida da
// conexão com o banco. Não expõe Begin/Commit/Rollback: nenhum caso de uso
// atravessa dois repositórios numa transação. Transações de jobs e seed de
// rulesets ficam internas aos adaptadores.
type GerenciadorDados interface {
	Rulesets() RulesetRepo
	RulesetsConsulta() ConsultaRulesetRepo
	Documentos() DocumentoRepo
	Estruturas() EstruturaRepo
	DocumentosInternos() DocumentoInternoRepo
	JobsConsulta() ConsultaJobRepo
	JobsCriacao() CriacaoJobRepo
	JobsExecucao() ExecucaoJobRepo
	JobsReivindicacao() ReivindicacaoJobRepo
	Fechar()
}
