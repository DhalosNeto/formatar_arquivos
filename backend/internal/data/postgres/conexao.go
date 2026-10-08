// Package postgres é o único lugar do sistema que importa github.com/jackc/pgx:
// a fronteira é verificada por internal/arquitetura.TestPgxSoVazDentroDeInternalData.
package postgres

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/daniel-halos/formatador/internal/data/contracts"
	"github.com/daniel-halos/formatador/internal/infra/config"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// Nenhuma query deste pacote traduz SQLSTATE para erro de negócio, e é
// deliberado: colisão de UUID (23505) é sintoma de corrupção, não caso
// esperado. Quando uma constraint virar regra de negócio de verdade, a
// tradução nasce junto com ela — constante reservada "para depois" só
// envelhece atrás de um //nolint.

// Gerenciador é a implementação Postgres de contracts.GerenciadorDados.
type Gerenciador struct {
	pool *pgxpool.Pool
}

// NovoGerenciador valida a configuração e monta o pool de conexões. Não faz
// I/O: pgxpool.NewWithConfig só monta estruturas em memória e agenda, num
// goroutine à parte, um health-check que não bloqueia o retorno; a primeira
// conexão real só acontece na primeira query.
func NovoGerenciador(ctx context.Context, cfg config.Postgres) (*Gerenciador, error) {
	if strings.TrimSpace(cfg.DSN) == "" {
		return nil, errors.NovoErroValidacao("POSTGRES_DSN", "obrigatório")
	}

	pool, err := novoPool(ctx, cfg)
	if err != nil {
		return nil, envolverPostgres(err, "montar pool de conexões")
	}
	return &Gerenciador{pool: pool}, nil
}

// novoPool monta a configuração do pool a partir do DSN e dos limites da
// configuração da aplicação. pgxpool.NewWithConfig não faz round-trip de
// rede: a construção de cada conexão do puddle.Pool é preguiçosa (só roda na
// primeira Acquire); o goroutine que a função dispara internamente só chega a
// conectar se MinConns/MinIdleConns for maior que zero, o que não é o caso
// aqui. Por isso o ctx recebido pode seguir direto, sem violar "construtor
// sem I/O".
func novoPool(ctx context.Context, cfg config.Postgres) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, err
	}
	poolCfg.MaxConns = cfg.MaxConexoes
	poolCfg.ConnConfig.ConnectTimeout = cfg.TempoLimiteConexao

	return pgxpool.NewWithConfig(ctx, poolCfg)
}

// Documentos devolve o repositório de documentos com autorização por dono.
func (g *Gerenciador) Documentos() contracts.DocumentoRepo { return NovoRepositorioDocumento(g.pool) }

// Rulesets devolve o repositório de perfis de formatação.
func (g *Gerenciador) Rulesets() contracts.RulesetRepo { return NovoRepositorioRuleset(g.pool) }

// RulesetsConsulta devolve o repositório de leitura de perfis por ID.
func (g *Gerenciador) RulesetsConsulta() contracts.ConsultaRulesetRepo {
	return NovoRepositorioRuleset(g.pool)
}

// Estruturas devolve a porta pública de correção de estrutura por dono.
func (g *Gerenciador) Estruturas() contracts.EstruturaRepo { return NovoRepositorioDocumento(g.pool) }

// DocumentosInternos devolve o repositório de documentos sem restrição de
// dono, usado pelo worker de processamento.
func (g *Gerenciador) DocumentosInternos() contracts.DocumentoInternoRepo {
	return NovoRepositorioDocumento(g.pool)
}

// JobsConsulta devolve o repositório de leitura de jobs.
func (g *Gerenciador) JobsConsulta() contracts.ConsultaJobRepo { return NovoRepositorioJob(g.pool) }

// JobsCriacao devolve o repositório de criação idempotente de jobs.
func (g *Gerenciador) JobsCriacao() contracts.CriacaoJobRepo { return NovoRepositorioJob(g.pool) }

// JobsExecucao devolve o repositório de execução de jobs, sem restrição de
// dono, usado pelo worker de processamento.
func (g *Gerenciador) JobsExecucao() contracts.ExecucaoJobRepo { return NovoRepositorioJob(g.pool) }

// JobsReivindicacao devolve a porta de reivindicação de trabalho da fila.
//
// É separada de JobsExecucao porque reivindicar é o caso de uso de quem
// PROCURA trabalho, e executar é o de quem JÁ TEM um job em mãos.
func (g *Gerenciador) JobsReivindicacao() contracts.ReivindicacaoJobRepo {
	return NovoRepositorioJob(g.pool)
}

// Fechar encerra o pool de conexões.
func (g *Gerenciador) Fechar() { g.pool.Close() }

// Nome identifica esta dependência no readiness.
func (g *Gerenciador) Nome() string { return "postgres" }

// Verificar satisfaz, por duck typing, a interface Verificador de
// internal/rotas/root/webrotas/saude (não importada aqui: infra não importa rotas).
func (g *Gerenciador) Verificar(ctx context.Context) error {
	return envolverPostgres(g.pool.Ping(ctx), "verificar conexão")
}

// envolverPostgres ENCADEIA o erro do driver: errors.Envolver guarda o
// original e ErroEnvolvido.Error() o interpola com %v. Em pgx v5.11.0
// *pgconn.PgError.Error() não emite o Detail nem o SQL, mas ConnectError e
// perDialConnectError emitem usuário, banco, endereço e hostname — e
// rotasutil.go:22 grava a mensagem em log nível Error.
//
// Portanto este helper NÃO serve a caminho algum que não possa vazar
// conexão: quem precisa cortar a cadeia segue o idioma de erroSeed
// (ruleset.go), que devolve *ErroAplicacao sem encadear a causa.
func envolverPostgres(err error, operacao string) error {
	return errors.Envolver(err, operacao)
}

// textoOuNulo converte uma string vazia em ponteiro nulo, para gravar NULL em
// colunas de texto opcionais em vez do literal de string vazia.
func textoOuNulo(valor string) *string {
	if valor == "" {
		return nil
	}
	return &valor
}

var _ contracts.GerenciadorDados = (*Gerenciador)(nil)
