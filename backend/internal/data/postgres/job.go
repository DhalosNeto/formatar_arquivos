package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/daniel-halos/formatador/internal/domain/job/entity"
	jobrepo "github.com/daniel-halos/formatador/internal/domain/job/repository"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// colunasJob é a lista de colunas em comum a todas as leituras de job, na
// ordem esperada por scanJob. erro vem por coalesce porque a coluna é
// nullable e a entidade guarda string, nunca ponteiro.
const colunasJob = "id, documento_id, ruleset_id, tipo, status, tentativas, progresso, " +
	"coalesce(erro, ''), resultado, criado_em, iniciado_em, finalizado_em"

// RepositorioJob implementa jobrepo.ConsultaJobRepo e jobrepo.CriacaoJobRepo.
type RepositorioJob struct {
	pool *pgxpool.Pool
}

// NovoRepositorioJob cria o repositório de jobs.
func NovoRepositorioJob(pool *pgxpool.Pool) *RepositorioJob {
	return &RepositorioJob{pool: pool}
}

// ObterPorID restringe a consulta ao dono do documento associado.
func (r *RepositorioJob) ObterPorID(ctx context.Context, solicitante vo.Dono, id uuid.UUID) (entity.Job, error) {
	usuarioID, sessaoID := solicitante.ParaColunas()
	linha := r.pool.QueryRow(ctx,
		`SELECT `+colunasJobComPrefixo("j.")+`
		 FROM jobs j JOIN documentos d ON d.id = j.documento_id
		 WHERE j.id = $1 AND (d.usuario_id = $2 OR d.sessao_id = $3)`,
		id, usuarioID, sessaoID)
	job, err := scanJob(linha)
	if err != nil {
		if errors.E(err, pgx.ErrNoRows) {
			return entity.Job{}, errors.NovoErroNaoEncontrado("job")
		}
		return entity.Job{}, err
	}
	return job, nil
}

// ListarPorDocumento restringe a listagem ao dono do documento, sem erro para
// documento sem job algum.
func (r *RepositorioJob) ListarPorDocumento(ctx context.Context, solicitante vo.Dono, documentoID uuid.UUID) ([]entity.Job, error) {
	usuarioID, sessaoID := solicitante.ParaColunas()
	linhas, err := r.pool.Query(ctx,
		`SELECT `+colunasJobComPrefixo("j.")+`
		 FROM jobs j JOIN documentos d ON d.id = j.documento_id
		 WHERE j.documento_id = $1 AND (d.usuario_id = $2 OR d.sessao_id = $3)
		 ORDER BY j.criado_em DESC, j.id DESC`,
		documentoID, usuarioID, sessaoID)
	if err != nil {
		return nil, envolverPostgres(err, "listar jobs por documento")
	}
	defer linhas.Close()

	jobs := make([]entity.Job, 0)
	for linhas.Next() {
		job, err := scanJob(linhas)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	if err := linhas.Err(); err != nil {
		return nil, envolverPostgres(err, "listar jobs por documento")
	}
	return jobs, nil
}

// InserirOuObter é a única transação do sistema: revalida a autorização do
// documento, aplica o gate de perfil de formatação e decide entre inserir o
// candidato ou devolver o job já existente para a mesma chave, tudo
// atomicamente. A ordem é autorização → gate → INSERT → releitura: nenhuma
// repetição é resolvida antes de o documento ser reautorizado, senão quem
// conhecesse dois UUIDs receberia job de documento alheio.
//
// O gate recusa job NOVO cujo tipo exige ruleset contra perfil ausente ou
// inativo, mas repetição vence o gate: chave já ocupada devolve o job
// existente qualquer que seja o estado atual do perfil. Ele roda ANTES do
// INSERT, e não depois, por motivo de LOCK: a checagem de FK do INSERT toma
// FOR KEY SHARE na linha de rulesets, que NÃO conflita com o FOR NO KEY UPDATE
// de um `UPDATE ativo=false` pendente — validar depois nunca obteria o lock
// necessário. Secundariamente, evita acoplar mensagem de domínio a nome de
// constraint (SQLSTATE 23503/23514, que hoje virariam 500).
func (r *RepositorioJob) InserirOuObter(ctx context.Context, solicitante vo.Dono, job entity.Job, chave uuid.UUID) (entity.Job, error) {
	if chave == uuid.Nil {
		return entity.Job{}, errors.NovoErroValidacao("chave_idempotencia", "obrigatória")
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return entity.Job{}, envolverPostgres(err, "abrir transação de criação de job")
	}
	// Rollback após Commit devolve pgx.ErrTxClosed; é esperado e descartado, não
	// há nada a fazer com esse erro depois que a transação já foi decidida.
	defer func() { _ = tx.Rollback(ctx) }()

	// Esta transação espera por lock em dois pontos (documentos e rulesets) e o
	// pool tem só 10 conexões por padrão (config.go:77), sem statement_timeout
	// nem prazo por requisição: sem teto, uma chamada presa retém uma das 10
	// indefinidamente.
	//
	// O teto efetivo em produção é ESTE valor, não o prazo de quem chama: não
	// existe deadline por requisição no caminho HTTP — os únicos WithTimeout do
	// projeto são o do health check (saude/controlador.go:91) e o do desligamento
	// (cmd/api/main.go:121). E lock_timeout é POR AQUISIÇÃO, então documentos
	// mais rulesets somam até o dobro retendo a mesma conexão. Dez requisições
	// concorrentes ainda esgotam o pool por esse intervalo se um operador deixar
	// transação aberta em rulesets; fechar isso exige prazo por requisição, que
	// é recorte próprio.
	//
	// Nos testes de integração o contexto vence primeiro, porque eles passam
	// deadline curto de propósito para provar o modo de lock — não confundir
	// esse comportamento de teste com o de produção.
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '3s'`); err != nil {
		return entity.Job{}, envolverPostgres(err, "limitar espera por lock na criação de job")
	}

	usuarioID, sessaoID := solicitante.ParaColunas()
	// FOR SHARE trava a linha do documento até o fim da transação, para que um
	// dono não possa ser revogado entre esta checagem e o INSERT abaixo. Não
	// serializa o caminho feliz: outra transação que só LÊ o documento (sem
	// tentar mudar dono) segue sem bloqueio.
	var existe int
	err = tx.QueryRow(ctx,
		`SELECT 1 FROM documentos WHERE id = $1 AND (usuario_id = $2 OR sessao_id = $3) FOR SHARE`,
		job.DocumentoID, usuarioID, sessaoID).Scan(&existe)
	if err != nil {
		if errors.E(err, pgx.ErrNoRows) {
			return entity.Job{}, errors.NovoErroNaoEncontrado("job")
		}
		return entity.Job{}, envolverPostgres(err, "reautorizar documento na criação de job")
	}

	if job.Tipo.ExigeRuleset() {
		// FOR SHARE na linha do perfil, na MESMA transação: segura a desativação
		// até o INSERT. RulesetID nil não consulta nada e não é desreferenciado —
		// InserirOuObter é porta pública e recebe entity.Job arbitrária. nil só
		// pode significar ausente CONFIRMADO (pgx.ErrNoRows); qualquer outro erro
		// é infraestrutura e sai por envolverPostgres, nunca como validação.
		var ativo *bool
		if job.RulesetID != nil {
			var lido bool
			err = tx.QueryRow(ctx, `SELECT ativo FROM rulesets WHERE id = $1 FOR SHARE`, *job.RulesetID).Scan(&lido)
			switch {
			case err == nil:
				ativo = &lido
			case errors.E(err, pgx.ErrNoRows):
				ativo = nil
			default:
				return entity.Job{}, envolverPostgres(err, "ler perfil de formatação na criação de job")
			}
		}
		if erroGate := job.Tipo.ValidarRulesetParaNovoJob(ativo); erroGate != nil {
			// Repetição vence o gate: a chave já ocupada devolve o job existente
			// mesmo que o perfil tenha sido desativado depois de ele ser criado.
			existente, achou, err := obterJobDaChave(ctx, tx, job.DocumentoID, chave)
			if err != nil {
				return entity.Job{}, err
			}
			if !achou {
				// Rollback pelo defer: nada foi inserido.
				return entity.Job{}, erroGate
			}
			if err := tx.Commit(ctx); err != nil {
				return entity.Job{}, envolverPostgres(err, "confirmar releitura de job existente")
			}
			return existente, nil
		}
	}

	var resultado []byte
	if len(job.Resultado) > 0 {
		resultado = job.Resultado
	}
	linha := tx.QueryRow(ctx,
		`INSERT INTO jobs (
			id, documento_id, ruleset_id, tipo, status, tentativas, progresso, erro, resultado, chave_idempotencia, criado_em
		 ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		 ON CONFLICT (documento_id, chave_idempotencia) DO NOTHING
		 RETURNING `+colunasJob,
		job.ID, job.DocumentoID, job.RulesetID, string(job.Tipo), string(job.Status), job.Tentativas, job.Progresso,
		textoOuNulo(job.Erro), resultado, chave, job.CriadoEm)
	inserido, err := scanJob(linha)
	if err == nil {
		if err := tx.Commit(ctx); err != nil {
			return entity.Job{}, envolverPostgres(err, "confirmar criação de job")
		}
		return inserido, nil
	}
	if !errors.E(err, pgx.ErrNoRows) {
		return entity.Job{}, err
	}

	// ON CONFLICT DO NOTHING não devolveu linha: outra transação já ocupa essa
	// chave. Sob READ COMMITTED, cada comando desta transação toma um snapshot
	// novo do banco, então este SELECT enxerga a linha da transação concorrente
	// assim que ela commitar — é o que torna a releitura abaixo confiável.
	// Sob REPEATABLE READ/SERIALIZABLE a transação inteira usaria o snapshot do
	// início do Begin, o SELECT não veria a linha alheia mesmo após o commit
	// dela, e esta idempotência viraria erro em vez de devolver o job existente.
	jobExistente, achou, err := obterJobDaChave(ctx, tx, job.DocumentoID, chave)
	if err != nil {
		return entity.Job{}, err
	}
	if !achou {
		return entity.Job{}, errors.NovoErroAplicacao("criação de job entrou em conflito sem linha correspondente")
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.Job{}, envolverPostgres(err, "confirmar releitura de job existente")
	}
	return jobExistente, nil
}

// obterJobDaChave lê, na transação em curso, o job que ocupa a chave de
// idempotência do documento. Chave livre não é erro: devolve achou=false.
func obterJobDaChave(ctx context.Context, tx pgx.Tx, documentoID, chave uuid.UUID) (entity.Job, bool, error) {
	linha := tx.QueryRow(ctx,
		`SELECT `+colunasJob+` FROM jobs WHERE documento_id = $1 AND chave_idempotencia = $2`,
		documentoID, chave)
	job, err := scanJob(linha)
	if err != nil {
		if errors.E(err, pgx.ErrNoRows) {
			return entity.Job{}, false, nil
		}
		return entity.Job{}, false, envolverPostgres(err, "reler job existente")
	}
	return job, true, nil
}

// ObterPorIDInterno lê um job por ID sem restrição de dono: porta exclusiva
// do worker de execução.
func (r *RepositorioJob) ObterPorIDInterno(ctx context.Context, id uuid.UUID) (entity.Job, error) {
	linha := r.pool.QueryRow(ctx, `SELECT `+colunasJob+` FROM jobs WHERE id = $1`, id)
	job, err := scanJob(linha)
	if err != nil {
		if errors.E(err, pgx.ErrNoRows) {
			return entity.Job{}, errors.NovoErroNaoEncontrado("job")
		}
		return entity.Job{}, err
	}
	return job, nil
}

// Salvar grava status, tentativas, progresso, erro, resultado, iniciado_em e
// finalizado_em apenas se o status no banco ainda for statusAtual (CAS).
// Zero linhas afetadas é conflito, nunca sucesso silencioso.
func (r *RepositorioJob) Salvar(ctx context.Context, job entity.Job, statusAtual entity.StatusJob) error {
	var resultado []byte
	if len(job.Resultado) > 0 {
		resultado = job.Resultado
	}
	marca, err := r.pool.Exec(ctx,
		`UPDATE jobs SET status=$1, tentativas=$2, progresso=$3, erro=$4, resultado=$5, iniciado_em=$6, finalizado_em=$7
		 WHERE id=$8 AND status=$9`,
		string(job.Status), job.Tentativas, job.Progresso, textoOuNulo(job.Erro), resultado,
		job.IniciadoEm, job.FinalizadoEm, job.ID, string(statusAtual))
	if err != nil {
		return envolverPostgres(err, "salvar execução do job")
	}
	if marca.RowsAffected() == 0 {
		return errors.NovoErroConflito("status do job mudou durante a execução")
	}
	return nil
}

// Reivindicar seleciona o job pendente mais antigo com FOR UPDATE SKIP
// LOCKED, marca status=executando, incrementa tentativas e grava
// iniciado_em, tudo em uma única instrução — dois workers concorrentes nunca
// recebem o mesmo job (ver docs/adr/0002-fila-sem-river.md). Fila vazia
// (pgx.ErrNoRows) não é erro: devolve (entity.Job{}, false, nil).
func (r *RepositorioJob) Reivindicar(ctx context.Context) (entity.Job, bool, error) {
	linha := r.pool.QueryRow(ctx,
		`UPDATE jobs SET status='executando', tentativas=tentativas+1, iniciado_em=now()
		 WHERE id = (
		     SELECT id FROM jobs WHERE status='pendente'
		     ORDER BY criado_em
		     FOR UPDATE SKIP LOCKED
		     LIMIT 1
		 )
		 RETURNING `+colunasJob)
	job, err := scanJob(linha)
	if err != nil {
		if errors.E(err, pgx.ErrNoRows) {
			return entity.Job{}, false, nil
		}
		return entity.Job{}, false, envolverPostgres(err, "reivindicar job")
	}
	return job, true, nil
}

// colunasJobComPrefixo repete colunasJob com o prefixo de alias de tabela,
// necessário nas consultas com JOIN para desambiguar colunas homônimas.
func colunasJobComPrefixo(prefixo string) string {
	return prefixo + "id, " + prefixo + "documento_id, " + prefixo + "ruleset_id, " + prefixo + "tipo, " +
		prefixo + "status, " + prefixo + "tentativas, " + prefixo + "progresso, coalesce(" + prefixo + "erro, ''), " +
		prefixo + "resultado, " + prefixo + "criado_em, " + prefixo + "iniciado_em, " + prefixo + "finalizado_em"
}

// scanJob lê uma linha nas colunas de colunasJob e monta a entidade.
func scanJob(linha pgx.Row) (entity.Job, error) {
	var (
		id, documentoID          uuid.UUID
		rulesetID                *uuid.UUID
		tipo, status             string
		tentativas, progresso    int
		erroTexto                string
		resultado                json.RawMessage
		criadoEm                 time.Time
		iniciadoEm, finalizadoEm *time.Time
	)
	if err := linha.Scan(&id, &documentoID, &rulesetID, &tipo, &status, &tentativas, &progresso,
		&erroTexto, &resultado, &criadoEm, &iniciadoEm, &finalizadoEm); err != nil {
		return entity.Job{}, err
	}

	tipoJob := entity.TipoJob(tipo)
	if !tipoJob.Valido() {
		return entity.Job{}, errors.NovoErroAplicacao("converter tipo do job lido do banco: tipo desconhecido")
	}
	statusJob := entity.StatusJob(status)
	if !statusJob.Valido() {
		return entity.Job{}, errors.NovoErroAplicacao("converter status do job lido do banco: status desconhecido")
	}

	if iniciadoEm != nil {
		convertido := iniciadoEm.UTC()
		iniciadoEm = &convertido
	}
	if finalizadoEm != nil {
		convertido := finalizadoEm.UTC()
		finalizadoEm = &convertido
	}

	return entity.Job{
		ID:           id,
		DocumentoID:  documentoID,
		RulesetID:    rulesetID,
		Tipo:         tipoJob,
		Status:       statusJob,
		Tentativas:   tentativas,
		Progresso:    progresso,
		Erro:         erroTexto,
		Resultado:    resultado,
		CriadoEm:     criadoEm.UTC(),
		IniciadoEm:   iniciadoEm,
		FinalizadoEm: finalizadoEm,
	}, nil
}

var (
	_ jobrepo.ConsultaJobRepo = (*RepositorioJob)(nil)
	_ jobrepo.CriacaoJobRepo  = (*RepositorioJob)(nil)
	_ jobrepo.ExecucaoJobRepo = (*RepositorioJob)(nil)
)
