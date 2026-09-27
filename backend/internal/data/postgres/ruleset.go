package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	stderrors "errors"
	"sort"
	"time"

	"github.com/daniel-halos/formatador/internal/data/contracts"
	"github.com/daniel-halos/formatador/internal/domain/ruleset"
	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RepositorioRuleset é o adaptador Postgres da porta de rulesets.
type RepositorioRuleset struct{ pool *pgxpool.Pool }

// NovoRepositorioRuleset cria o adaptador de rulesets.
func NovoRepositorioRuleset(pool *pgxpool.Pool) *RepositorioRuleset {
	return &RepositorioRuleset{pool: pool}
}

type registroRuleset struct {
	definicao ruleset.Definicao
	json      []byte
	checksum  string
}

func prepararRulesets(definicoes []ruleset.Definicao) ([]registroRuleset, error) {
	if len(definicoes) > 4096 {
		return nil, errors.NovoErroValidacao("rulesets", "lote excede 4096 definições")
	}
	registros := make([]registroRuleset, 0, len(definicoes))
	for _, definicao := range definicoes {
		if err := definicao.Validar(); err != nil {
			return nil, err
		}
		dados, err := json.Marshal(definicao)
		if err != nil {
			return nil, errors.NovoErroAplicacao("não foi possível serializar o ruleset")
		}
		checksum := sha256.Sum256(dados)
		registros = append(registros, registroRuleset{definicao, dados, hex.EncodeToString(checksum[:])})
	}
	// Ordem comum para evitar inversões de locks em lotes concorrentes.
	sort.Slice(registros, func(i, j int) bool {
		if registros[i].definicao.Slug != registros[j].definicao.Slug {
			return registros[i].definicao.Slug < registros[j].definicao.Slug
		}
		return registros[i].definicao.Versao < registros[j].definicao.Versao
	})
	return registros, nil
}

// Semear insere o lote de perfis de forma idempotente e imutável.
//
// Valida o lote inteiro ANTES de tocar SQL, ordena por slug/versão, e usa
// INSERT ON CONFLICT DO NOTHING com SELECT separado para comparar nome e
// definição. Divergência em qualquer perfil desfaz o lote inteiro: mudança de
// diretriz exige versão nova, nunca UPDATE.
//
// Isso garante imutabilidade PELA PORTA DE SEED; não impede alteração por
// quem tem SQL direto no banco.
func (repositorio *RepositorioRuleset) Semear(ctx context.Context, definicoes []ruleset.Definicao) (retorno error) {
	ctx, cancelar := context.WithTimeout(ctx, 2*time.Minute)
	defer cancelar()
	registros, err := prepararRulesets(definicoes)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return errors.Envolver(err, "seed interrompido")
	}
	if len(registros) == 0 {
		return nil
	}
	transacao, err := repositorio.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return erroSeed(ctx)
	}
	defer func() {
		limpeza, cancelar := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancelar()
		if err := transacao.Rollback(limpeza); err != nil && !stderrors.Is(err, pgx.ErrTxClosed) {
			retorno = erroSeed(ctx)
		}
	}()
	for _, registro := range registros {
		if err := inserirRuleset(ctx, transacao, registro); err != nil {
			return err
		}
	}
	if err := transacao.Commit(ctx); err != nil {
		return erroSeed(ctx)
	}
	return nil
}

func inserirRuleset(ctx context.Context, transacao pgx.Tx, registro registroRuleset) error {
	definicao := registro.definicao
	_, err := transacao.Exec(ctx, `INSERT INTO rulesets (slug,versao,nome,definicao,checksum)
 VALUES ($1,$2,$3,$4::jsonb,$5) ON CONFLICT (slug,versao) DO NOTHING`, definicao.Slug, definicao.Versao, definicao.Nome, registro.json, registro.checksum)
	if err != nil {
		return erroSeed(ctx)
	}
	var igual bool
	// Nova instrução obtém novo snapshot READ COMMITTED após INSERT concorrente.
	err = transacao.QueryRow(ctx, `SELECT nome=$3 AND definicao=$4::jsonb FROM rulesets
 WHERE slug=$1 AND versao=$2 FOR UPDATE`, definicao.Slug, definicao.Versao, definicao.Nome, registro.json).Scan(&igual)
	if err != nil {
		return erroSeed(ctx)
	}
	if !igual {
		return errors.NovoErroConflito("versão de ruleset já existe com definição diferente")
	}
	return nil
}

func erroSeed(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return errors.Envolver(err, "seed interrompido")
	}
	// Não encadear causas do driver: podem conter DSN, SQL ou valores de coluna.
	return errors.NovoErroAplicacao("não foi possível persistir o lote de rulesets")
}

var _ contracts.RulesetRepo = (*RepositorioRuleset)(nil)
