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
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// limiteDefinicaoBytes é o mesmo teto que infra/ruleset/carregar.go aplica na
// ENTRADA (TamanhoMaximoBytes). Duplicado como const local, e não importado de
// infra/ruleset, porque esse pacote arrasta jsonschema e o embed do diretório
// rulesets para a camada de dados; data já importa infra/config e infra/errors,
// então a duplicação não é por regra de camada. A igualdade entre os dois tetos
// é amarrada em teste (ruleset_test.go, TestDecodificarRulesetTetoDeTamanho).
const limiteDefinicaoBytes = 65536

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

// ObterPorID lê um perfil já semeado e devolve, junto, o flag ativo da linha.
//
// O teto de tamanho vai NO SQL porque é ele que protege a memória do worker:
// Scan materializa o jsonb inteiro antes de qualquer verificação em Go. Acima
// do teto o CASE devolve NULL, então a recusa acontece sem transportar o
// payload. `definicao::text` aparece UMA vez de propósito: o Postgres não
// elimina subexpressão comum, e uma segunda ocorrência destoastaria e
// serializaria a linha outra vez por leitura.
//
// A guarda em Go é só `len(dados) == 0`, e isso equivale a "o CASE devolveu
// NULL", isto é, "acima do teto":
//   - destino *json.RawMessage cai em scanPlanJSONToJSONUnmarshal
//     (pgx v5.11.0, pgtype/json.go:169), que para src == nil zera a slice
//     (json.go:198-205) — NULL chega como len 0;
//   - nenhum valor jsonb produz texto de 0 byte (o mínimo é 2) e a coluna é
//     NOT NULL, então len 0 não tem outra origem;
//   - 'null'::jsonb chega com 4 bytes e é recusado depois, por Validar.
//
// Quem remover o CASE muda essa semântica em silêncio: sem ele, len 0 deixa de
// ser alcançável e nenhum payload grande é recusado.
//
// NÃO filtra por ativo: o flag é devolvido, não aplicado (ver ConsultaRulesetRepo).
//
// Confere PROCEDÊNCIA: slug e versão das colunas, que carregam a UNIQUE
// (slug,versao), têm de bater com os do JSON. A coerência entre os dois é
// garantida só pela porta de seed, e SQL direto não passa por ela; divergir
// faria o documento registrar procedência que não corresponde à linha lida.
func (repositorio *RepositorioRuleset) ObterPorID(ctx context.Context, id uuid.UUID) (ruleset.Definicao, bool, error) {
	const consulta = `SELECT CASE WHEN octet_length(definicao::text) <= $2 THEN definicao END,
 slug, versao, ativo FROM rulesets WHERE id=$1`
	var (
		dados  json.RawMessage
		slug   string
		versao int
		ativo  bool
	)
	if err := repositorio.pool.QueryRow(ctx, consulta, id, limiteDefinicaoBytes).Scan(&dados, &slug, &versao, &ativo); err != nil {
		return ruleset.Definicao{}, false, erroConsultaRuleset(ctx, err)
	}
	if len(dados) == 0 {
		return ruleset.Definicao{}, false, errors.NovoErroAplicacao(msgRulesetAcimaDoTeto)
	}
	definicao, err := decodificarRuleset(dados)
	if err != nil {
		return ruleset.Definicao{}, false, err
	}
	if definicao.Slug != slug || definicao.Versao != versao {
		return ruleset.Definicao{}, false, errors.NovoErroAplicacao(msgRulesetProcedencia)
	}
	return definicao, ativo, nil
}

func erroConsultaRuleset(ctx context.Context, err error) error {
	// Contexto tem precedência: cancelamento não é ausência de linha (regra 3).
	if erroCtx := ctx.Err(); erroCtx != nil {
		return errors.Envolver(erroCtx, "leitura de ruleset interrompida")
	}
	if errors.E(err, pgx.ErrNoRows) {
		return errors.NovoErroNaoEncontrado("ruleset")
	}
	// Idioma de erroSeed: não encadear causas do driver, que podem conter DSN,
	// usuário, banco ou SQL. envolverPostgres encadearia.
	return errors.NovoErroAplicacao("não foi possível ler o ruleset")
}

// Mensagens fixas. Nenhuma deriva do dado lido: json.SyntaxError cita um
// caractere do payload e json.UnmarshalTypeError cita `Definicao.<campo>`.
const (
	msgRulesetAcimaDoTeto = "ruleset armazenado fora do limite de tamanho"
	msgRulesetIlegivel    = "ruleset armazenado ilegível"
	msgRulesetProcedencia = "procedência do ruleset armazenado não corresponde à linha"
)

// decodificarRuleset reconstrói a Definicao gravada na coluna jsonb.
//
// Pura, sem ctx e sem I/O, como prepararRulesets. Falha de decodificação OU de
// Validar é CORRUPÇÃO DE SERVIDOR: dado que o próprio servidor validou e gravou
// voltando inválido não é culpa de quem fez a requisição, então vira
// *ErroAplicacao (500) e NUNCA *ErroValidacao (400).
//
// O *ErroValidacao original não é encadeado de propósito: errors.Envolver não
// reclassifica, e errors.Como acharia o *ErroValidacao, fazendo
// rotasutil.Classificar responder 400 com os nomes de campo em razoes. Da
// mensagem de Validar sai SÓ o nome do campo (vocabulário fechado de
// invalido()), nunca o valor lido nem CampoInvalido.Mensagem.
//
// Sem DisallowUnknownFields: o que quebraria não é ler linha antiga com binário
// novo (campo ausente vira zero-value), e sim ler linha NOVA com binário
// ANTIGO — rollback ou fleet em versões mistas viraria 500 em toda leitura.
func decodificarRuleset(dados []byte) (ruleset.Definicao, error) {
	// Segunda guarda, defesa em profundidade: quem protege a memória do worker é
	// o teto no SQL de ObterPorID, porque o Scan já materializou o payload aqui.
	if len(dados) == 0 || len(dados) > limiteDefinicaoBytes {
		return ruleset.Definicao{}, errors.NovoErroAplicacao(msgRulesetAcimaDoTeto)
	}
	var definicao ruleset.Definicao
	if err := json.Unmarshal(dados, &definicao); err != nil {
		return ruleset.Definicao{}, errors.NovoErroAplicacao(msgRulesetIlegivel)
	}
	if err := definicao.Validar(); err != nil {
		campo := "desconhecido"
		var validacao *errors.ErroValidacao
		if errors.Como(err, &validacao) && len(validacao.Campos) > 0 {
			campo = validacao.Campos[0].Campo
		}
		return ruleset.Definicao{}, errors.NovoErroAplicacao("ruleset armazenado reprovou no campo " + campo)
	}
	return definicao, nil
}

var _ contracts.ConsultaRulesetRepo = (*RepositorioRuleset)(nil)
