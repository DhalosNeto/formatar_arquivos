//go:build integration

// Harness próprio deste pacote, no mesmo padrão de
// internal/data/postgres/postgres_integration_test.go (TestMain sobe um
// único container Postgres descartável, compartilhado pelos testes deste
// arquivo) e de backend/migrations/documento_dono_test.go (cada pacote com
// teste //go:build integration tem seu próprio bootstrap: funções de teste
// não exportadas não atravessam pacotes). Mesma imagem, mesmo digest.
//
// RED (TDD): estes testes chamam gerente.JobsReivindicacao().Reivindicar(ctx),
// que não existe ainda em jobrepo.ExecucaoJobRepo nem em
// postgres.RepositorioJob. Falha esperada por símbolo ausente em
// `go vet -tags=integration`. É exatamente a query do ADR 0002:
//
//	UPDATE jobs SET status='executando', tentativas=tentativas+1, iniciado_em=now()
//	WHERE id = (
//	    SELECT id FROM jobs WHERE status='pendente'
//	    ORDER BY criado_em
//	    FOR UPDATE SKIP LOCKED
//	    LIMIT 1
//	)
//	RETURNING ...
//
// numa única transação, devolvendo (entity.Job{}, false, nil) quando não há
// linha pendente — fila vazia não é erro.
//
// Digest resolvido nesta máquina em 2026-09-18 via
// `podman image inspect postgres:16-alpine --format '{{.Digest}}'` — o mesmo
// já usado em internal/data/postgres/postgres_integration_test.go, reutilizado
// aqui (não é uma imagem nova).
package fila_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/netip"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/daniel-halos/formatador/internal/data/contracts"
	"github.com/daniel-halos/formatador/internal/data/postgres"
	documentoentity "github.com/daniel-halos/formatador/internal/domain/documento/entity"
	"github.com/daniel-halos/formatador/internal/domain/job/entity"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/config"
)

const imagemPostgresDescartavel = "postgres@sha256:075f7ba66bc9b3ce7d6b8b635208ff61cd7cf1a67d71ec530eec5d7ae0cbe571"

var (
	bancoSQL *sql.DB
	gerente  contracts.GerenciadorDados
)

func TestMain(m *testing.M) {
	os.Exit(executarComPostgresDescartavel(m))
}

func executarComPostgresDescartavel(m *testing.M) int {
	ctx, cancelar := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancelar()

	instancia, dsn, err := subirPostgres(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "subir postgres descartável:", err)
		return 1
	}
	defer func() {
		ctxCleanup, cancelarCleanup := context.WithTimeout(context.Background(), 30*time.Second) //nolint:contextcheck // container já pode ter passado do prazo do setup.
		defer cancelarCleanup()
		if err := instancia.Terminate(ctxCleanup); err != nil {
			fmt.Fprintln(os.Stderr, "remover postgres descartável:", err)
		}
	}()

	banco, err := sql.Open("pgx", dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "abrir conexão de fixtures:", err)
		return 1
	}
	defer banco.Close()
	if err := banco.PingContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "ping postgres:", err)
		return 1
	}

	migrador, err := goose.NewProvider(goose.DialectPostgres, banco, os.DirFS("../../../migrations"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "provedor de migrations:", err)
		return 1
	}
	if _, err := migrador.Up(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "aplicar migrations:", err)
		return 1
	}

	g, err := postgres.NovoGerenciador(ctx, config.Postgres{DSN: dsn, MaxConexoes: 8, TempoLimiteConexao: 5 * time.Second})
	if err != nil {
		fmt.Fprintln(os.Stderr, "novo gerenciador postgres:", err)
		return 1
	}
	defer g.Fechar()

	bancoSQL = banco
	gerente = g

	return m.Run()
}

func subirPostgres(ctx context.Context) (testcontainers.Container, string, error) {
	instancia, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        imagemPostgresDescartavel,
			Env:          map[string]string{"POSTGRES_DB": "teste", "POSTGRES_USER": "teste", "POSTGRES_HOST_AUTH_METHOD": "trust"},
			ExposedPorts: []string{"5432/tcp"},
			HostConfigModifier: func(cfg *container.HostConfig) {
				cfg.NetworkMode = container.NetworkMode(os.Getenv("MIGRACOES_REDE_CONTAINER"))
				cfg.PortBindings = network.PortMap{network.MustParsePort("5432/tcp"): {{HostIP: netip.MustParseAddr("127.0.0.1"), HostPort: "0"}}}
			},
			WaitingFor: wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(time.Minute),
		},
		Started: true,
	})
	if err != nil {
		return nil, "", err
	}
	porta, err := instancia.MappedPort(ctx, "5432/tcp")
	if err != nil {
		return nil, "", err
	}
	dsn := "postgres://teste@127.0.0.1:" + porta.Port() + "/teste?sslmode=disable"
	return instancia, dsn, nil
}

func criarUsuario(t *testing.T) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := bancoSQL.ExecContext(context.Background(),
		`INSERT INTO usuarios (id, email, senha_hash, nome) VALUES ($1, $2, 'ficticio', 'Teste')`,
		id, id.String()+"@example.invalid")
	require.NoError(t, err)
	return id
}

func inserirDocumento(ctx context.Context, t *testing.T, dono vo.Dono, tamanhoBytes int64) documentoentity.Documento {
	t.Helper()
	documento, err := documentoentity.NovoDocumento(dono, "artigo.docx", vo.FormatoDocx, tamanhoBytes)
	require.NoError(t, err)
	require.NoError(t, gerente.Documentos().Inserir(ctx, documento))
	return documento
}

// inserirJobPendenteSQL grava um job "pendente" direto por SQL, pronto para
// ser reivindicado — contorna JobsCriacao().InserirOuObter porque este
// arquivo testa Reivindicar isoladamente, sem envolver idempotência.
func inserirJobPendenteSQL(ctx context.Context, t *testing.T, documentoID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := bancoSQL.QueryRowContext(ctx,
		`INSERT INTO jobs (documento_id, tipo, chave_idempotencia) VALUES ($1, 'analisar', gen_random_uuid()) RETURNING id`,
		documentoID).Scan(&id)
	require.NoError(t, err)
	return id
}

// inserirJobFixtureComStatusSQL grava um job já num status conhecido —
// mesmo padrão de internal/data/postgres/postgres_integration_test.go —
// necessário para o cenário "job em execução não é reivindicado".
func inserirJobFixtureComStatusSQL(
	ctx context.Context, t *testing.T, documentoID uuid.UUID,
	status entity.StatusJob, tentativas, progresso int,
) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := bancoSQL.QueryRowContext(ctx,
		`INSERT INTO jobs (documento_id, tipo, status, tentativas, progresso, chave_idempotencia)
		 VALUES ($1, 'analisar', $2, $3, $4, gen_random_uuid()) RETURNING id`,
		documentoID, string(status), tentativas, progresso).Scan(&id)
	require.NoError(t, err)
	return id
}

// novoDocumentoDeTeste monta um documento novo com dono próprio, para que
// cada teste tenha um espaço de jobs isolado por documento (a autorização não
// importa aqui — Reivindicar não tem dono — mas a FK exige um documento
// válido).
func novoDocumentoDeTeste(ctx context.Context, t *testing.T) documentoentity.Documento {
	t.Helper()
	usuarioID := criarUsuario(t)
	dono, err := vo.NovoDonoUsuario(usuarioID)
	require.NoError(t, err)
	return inserirDocumento(ctx, t, dono, 100)
}

// drenarFila reivindica repetidamente até a fila ficar vazia, devolvendo os
// IDs reivindicados nessa ordem. Usado para deixar os testes independentes
// de ordem de execução: nenhum assume que a tabela jobs está vazia no
// começo, só que fica vazia depois de drenar.
func drenarFila(ctx context.Context, t *testing.T, reivindicacao contracts.ReivindicacaoJobRepo) []uuid.UUID {
	t.Helper()
	var ids []uuid.UUID
	for {
		job, ok, err := reivindicacao.Reivindicar(ctx)
		require.NoError(t, err)
		if !ok {
			return ids
		}
		ids = append(ids, job.ID)
	}
}

// TestReivindicarFilaVaziaNaoEhErro drena qualquer pendente deixado por
// outros testes deste arquivo (não depende de ordem de execução) e prova que
// a fila vazia devolve ok=false e err=nil, nunca um erro.
func TestReivindicarFilaVaziaNaoEhErro(t *testing.T) {
	ctx := context.Background()
	reivindicacao := gerente.JobsReivindicacao()

	drenarFila(ctx, t, reivindicacao)

	job, ok, err := reivindicacao.Reivindicar(ctx)
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, entity.Job{}, job)
}

// TestReivindicarJobSaiDePendenteENaoEhReivindicadoDeNovo prova que o job
// reivindicado muda de status no banco (não só no valor devolvido) e nunca
// reaparece numa reivindicação seguinte.
func TestReivindicarJobSaiDePendenteENaoEhReivindicadoDeNovo(t *testing.T) {
	ctx := context.Background()
	reivindicacao := gerente.JobsReivindicacao()
	documento := novoDocumentoDeTeste(ctx, t)
	jobID := inserirJobPendenteSQL(ctx, t, documento.ID)

	job, ok, err := reivindicacao.Reivindicar(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, jobID, job.ID)
	assert.Equal(t, entity.StatusExecutando, job.Status)
	assert.NotNil(t, job.IniciadoEm)

	var status string
	require.NoError(t, bancoSQL.QueryRowContext(ctx, `SELECT status FROM jobs WHERE id=$1`, jobID).Scan(&status))
	assert.Equal(t, string(entity.StatusExecutando), status, "o status tem que mudar no banco, não só no valor devolvido")

	for _, idDrenado := range drenarFila(ctx, t, reivindicacao) {
		assert.NotEqualf(t, jobID, idDrenado, "job já reivindicado voltou a ser entregue")
	}
}

// TestReivindicarIncrementaTentativas prova que tentativas soma a cada
// reivindicação do mesmo job (nunca reinicia), simulando por SQL o retorno a
// "pendente" que uma retentativa real produziria.
func TestReivindicarIncrementaTentativas(t *testing.T) {
	ctx := context.Background()
	reivindicacao := gerente.JobsReivindicacao()
	documento := novoDocumentoDeTeste(ctx, t)
	jobID := inserirJobPendenteSQL(ctx, t, documento.ID)

	primeiro, ok, err := reivindicacao.Reivindicar(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, jobID, primeiro.ID)
	assert.Equal(t, 1, primeiro.Tentativas)

	_, err = bancoSQL.ExecContext(ctx, `UPDATE jobs SET status='pendente' WHERE id=$1`, jobID)
	require.NoError(t, err)

	segundo, ok, err := reivindicacao.Reivindicar(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, jobID, segundo.ID)
	assert.Equal(t, 2, segundo.Tentativas, "tentativas deve incrementar a cada reivindicação, nunca reiniciar")
}

// TestReivindicarNaoPegaJobEmExecucao prova que a query filtra por
// status='pendente': um job já "executando" não pode ser devolvido a um
// segundo worker, mesmo havendo linha na tabela.
func TestReivindicarNaoPegaJobEmExecucao(t *testing.T) {
	ctx := context.Background()
	reivindicacao := gerente.JobsReivindicacao()
	documento := novoDocumentoDeTeste(ctx, t)

	idExecutando := inserirJobFixtureComStatusSQL(ctx, t, documento.ID, entity.StatusExecutando, 1, 0)
	idPendente := inserirJobPendenteSQL(ctx, t, documento.ID)

	job, ok, err := reivindicacao.Reivindicar(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, idPendente, job.ID, "deveria pular o job já em execução e pegar o pendente")
	assert.NotEqual(t, idExecutando, job.ID)

	// idExecutando continua intocado: nem tentativas nem status mudaram.
	var status string
	var tentativas int
	require.NoError(t, bancoSQL.QueryRowContext(ctx,
		`SELECT status, tentativas FROM jobs WHERE id=$1`, idExecutando).Scan(&status, &tentativas))
	assert.Equal(t, string(entity.StatusExecutando), status)
	assert.Equal(t, 1, tentativas)
}

// TestReivindicarConcorrenteNaoEntregaJobDuasVezes é o cenário que justifica
// a decisão do ADR 0002: só concorrência real contra um Postgres real prova
// que FOR UPDATE SKIP LOCKED evita duas goroutines pegarem o mesmo job.
// Rodar com -race.
func TestReivindicarConcorrenteNaoEntregaJobDuasVezes(t *testing.T) {
	ctx := context.Background()
	reivindicacao := gerente.JobsReivindicacao()
	documento := novoDocumentoDeTeste(ctx, t)

	const totalJobs = 12
	esperados := make(map[uuid.UUID]bool, totalJobs)
	for range totalJobs {
		id := inserirJobPendenteSQL(ctx, t, documento.ID)
		esperados[id] = true
	}

	var largada sync.WaitGroup
	largada.Add(1)
	var concluidas sync.WaitGroup
	concluidas.Add(totalJobs)

	reivindicados := make([]uuid.UUID, totalJobs)
	encontrados := make([]bool, totalJobs)
	erros := make([]error, totalJobs)

	for indice := range totalJobs {
		go func(indice int) {
			defer concluidas.Done()
			largada.Wait()
			job, ok, err := reivindicacao.Reivindicar(ctx)
			reivindicados[indice] = job.ID
			encontrados[indice] = ok
			erros[indice] = err
		}(indice)
	}
	largada.Done()
	concluidas.Wait()

	vistos := make(map[uuid.UUID]int, totalJobs)
	for indice := range totalJobs {
		require.NoErrorf(t, erros[indice], "goroutine %d", indice)
		require.Truef(t, encontrados[indice], "goroutine %d deveria ter encontrado um job pendente", indice)
		require.Truef(t, esperados[reivindicados[indice]], "goroutine %d reivindicou job fora do conjunto inserido: %s", indice, reivindicados[indice])
		vistos[reivindicados[indice]]++
	}
	for id, contagem := range vistos {
		assert.Equalf(t, 1, contagem, "job %s foi entregue a mais de um worker", id)
	}
	assert.Len(t, vistos, totalJobs, "cada job deveria ter sido reivindicado exatamente uma vez, nenhum duplicado e nenhum perdido")

	// mais uma goroutine, sem job sobrando: prova que os 12 esgotaram a fila.
	_, ok, err := reivindicacao.Reivindicar(ctx)
	require.NoError(t, err)
	assert.False(t, ok)
}
