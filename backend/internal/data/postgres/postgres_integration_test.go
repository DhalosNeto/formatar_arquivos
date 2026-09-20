//go:build integration

// Imagem fixada por digest (mesmo padrão de
// internal/infra/storage/s3_integration_test.go): a tag postgres:16-alpine é
// móvel e uma atualização de patch upstream não pode quebrar a suíte sozinha.
// Digest resolvido nesta máquina em 2026-09-18 via
// `podman image inspect postgres:16-alpine --format '{{.Digest}}'`:
// sha256:075f7ba66bc9b3ce7d6b8b635208ff61cd7cf1a67d71ec530eec5d7ae0cbe571
package postgres_test

import (
	"context"
	"database/sql"
	"encoding/json"
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
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

const imagemPostgresDescartavel = "postgres@sha256:075f7ba66bc9b3ce7d6b8b635208ff61cd7cf1a67d71ec530eec5d7ae0cbe571"

// bancoSQL e gerente são populados uma única vez em TestMain e compartilhados
// por todos os testes do arquivo: subir o container leva ~7s, e cada teste se
// isola por dados novos (UUID novo por caso), não por container novo.
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
		// Contexto novo e próprio: o contexto do setup pode já ter estourado
		// quando o teardown roda, e Terminate precisa de um contexto vivo.
		// Mesmo padrão de backend/migrations/documento_dono_test.go.
		ctxCleanup, cancelarCleanup := context.WithTimeout(context.Background(), 30*time.Second) //nolint:contextcheck // ver comentário acima.
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
	// Nada de SetMaxOpenConns(1) aqui: o teste de concorrência da chave de
	// idempotência precisa de paralelismo real contra o Postgres real.
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

// criarUsuario insere a linha auxiliar em usuarios exigida pela FK de
// documentos.usuario_id e devolve o id gerado.
func criarUsuario(t *testing.T) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := bancoSQL.ExecContext(context.Background(),
		`INSERT INTO usuarios (id, email, senha_hash, nome) VALUES ($1, $2, 'ficticio', 'Teste')`,
		id, id.String()+"@example.invalid")
	require.NoError(t, err)
	return id
}

// inserirDocumento monta um Documento válido para dono e o grava pelo
// repositório sob teste.
func inserirDocumento(ctx context.Context, t *testing.T, dono vo.Dono, tamanhoBytes int64) documentoentity.Documento {
	t.Helper()
	documento, err := documentoentity.NovoDocumento(dono, "artigo.docx", vo.FormatoDocx, tamanhoBytes)
	require.NoError(t, err)
	require.NoError(t, gerente.Documentos().Inserir(ctx, documento))
	return documento
}

// inserirJobFixtureSQL grava um job diretamente por SQL, contornando a
// autorização de CriacaoJobRepo.InserirOuObter — serve só para preparar dado
// de leitura (ObterPorID, ListarPorDocumento).
func inserirJobFixtureSQL(ctx context.Context, t *testing.T, documentoID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := bancoSQL.QueryRowContext(ctx,
		`INSERT INTO jobs (documento_id, tipo, chave_idempotencia) VALUES ($1, 'analisar', gen_random_uuid()) RETURNING id`,
		documentoID).Scan(&id)
	require.NoError(t, err)
	return id
}

// TestObterPorIDTerceiroIgualAInexistente é o cenário 1 do portão de saída:
// documento/job de terceiro e documento/job inexistente têm que devolver
// exatamente o mesmo erro — mesmo tipo concreto e mesma mensagem — para que a
// resposta não vaze a existência de recurso alheio. O caso
// "usuario_id_igual_sessao_id_de_outro_documento" prova que a query decide
// pela espécie do dono, não por um OR cego entre usuario_id e sessao_id: um
// usuário cujo próprio id numérico é igual ao sessao_id de um documento de
// sessão não pode enxergá-lo.
func TestObterPorIDTerceiroIgualAInexistente(t *testing.T) {
	ctx := context.Background()
	docRepo := gerente.Documentos()
	jobConsulta := gerente.JobsConsulta()

	usuarioB := criarUsuario(t)
	donoB, err := vo.NovoDonoUsuario(usuarioB)
	require.NoError(t, err)
	sessaoY, err := vo.NovoDonoSessao(uuid.New())
	require.NoError(t, err)

	usuarioA := criarUsuario(t)
	donoA, err := vo.NovoDonoUsuario(usuarioA)
	require.NoError(t, err)
	sessaoX, err := vo.NovoDonoSessao(uuid.New())
	require.NoError(t, err)

	usuarioGemeo := criarUsuario(t)
	donoGemeo, err := vo.NovoDonoUsuario(usuarioGemeo)
	require.NoError(t, err)
	// Mesmo valor numérico do usuário acima, espécie sessão: o molde exato da
	// confusão de identidade citada no CLAUDE.md.
	sessaoGemea, err := vo.NovoDonoSessao(usuarioGemeo)
	require.NoError(t, err)

	docUsuarioB := inserirDocumento(ctx, t, donoB, 100)
	docSessaoY := inserirDocumento(ctx, t, sessaoY, 100)
	docSessaoGemea := inserirDocumento(ctx, t, sessaoGemea, 100)

	jobUsuarioB := inserirJobFixtureSQL(ctx, t, docUsuarioB.ID)
	jobSessaoY := inserirJobFixtureSQL(ctx, t, docSessaoY.ID)
	jobSessaoGemea := inserirJobFixtureSQL(ctx, t, docSessaoGemea.ID)

	casos := []struct {
		nome        string
		solicitante vo.Dono
		documentoID uuid.UUID
		jobID       uuid.UUID
	}{
		{"usuario_vs_documento_de_usuario", donoA, docUsuarioB.ID, jobUsuarioB},
		{"usuario_vs_documento_de_sessao", donoA, docSessaoY.ID, jobSessaoY},
		{"sessao_vs_documento_de_usuario", sessaoX, docUsuarioB.ID, jobUsuarioB},
		{"sessao_vs_documento_de_sessao", sessaoX, docSessaoY.ID, jobSessaoY},
		{"usuario_id_igual_sessao_id_de_outro_documento", donoGemeo, docSessaoGemea.ID, jobSessaoGemea},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			_, errTerceiro := docRepo.ObterPorID(ctx, caso.solicitante, caso.documentoID)
			_, errInexistente := docRepo.ObterPorID(ctx, caso.solicitante, uuid.New())
			var tipoTerceiro, tipoInexistente *errors.ErroNaoEncontrado
			require.ErrorAsf(t, errTerceiro, &tipoTerceiro, "documento de terceiro deveria dar não-encontrado, deu %v", errTerceiro)
			require.ErrorAsf(t, errInexistente, &tipoInexistente, "documento inexistente deveria dar não-encontrado, deu %v", errInexistente)
			assert.Equal(t, errInexistente.Error(), errTerceiro.Error())
			assert.NotContains(t, errTerceiro.Error(), caso.documentoID.String())

			idPreviewInexistente := uuid.New()
			chaveAlvo, err := vo.NovaChavePreviewPDF(caso.documentoID)
			require.NoError(t, err)
			chaveInexistente, err := vo.NovaChavePreviewPDF(idPreviewInexistente)
			require.NoError(t, err)

			errTerceiroChave := docRepo.DefinirChavePreviewPDF(ctx, caso.solicitante, caso.documentoID, chaveAlvo)
			errInexistenteChave := docRepo.DefinirChavePreviewPDF(ctx, caso.solicitante, idPreviewInexistente, chaveInexistente)
			var tipoTerceiroChave, tipoInexistenteChave *errors.ErroNaoEncontrado
			require.ErrorAs(t, errTerceiroChave, &tipoTerceiroChave)
			require.ErrorAs(t, errInexistenteChave, &tipoInexistenteChave)
			assert.Equal(t, errInexistenteChave.Error(), errTerceiroChave.Error())
			assert.NotContains(t, errTerceiroChave.Error(), caso.documentoID.String())

			_, errJobTerceiro := jobConsulta.ObterPorID(ctx, caso.solicitante, caso.jobID)
			_, errJobInexistente := jobConsulta.ObterPorID(ctx, caso.solicitante, uuid.New())
			var tipoJobTerceiro, tipoJobInexistente *errors.ErroNaoEncontrado
			require.ErrorAs(t, errJobTerceiro, &tipoJobTerceiro)
			require.ErrorAs(t, errJobInexistente, &tipoJobInexistente)
			assert.Equal(t, errJobInexistente.Error(), errJobTerceiro.Error())
			assert.NotContains(t, errJobTerceiro.Error(), caso.jobID.String())
		})
	}
}

// TestInserirOuObterConcorrente é o cenário 2 do portão de saída: só oito
// goroutines reais concorrendo contra Postgres real provam idempotência — um
// UNIQUE de schema, sozinho, não prova nada sobre a decisão em cima dele.
// Rodar com -race.
func TestInserirOuObterConcorrente(t *testing.T) {
	ctx := context.Background()
	criacao := gerente.JobsCriacao()

	usuarioID := criarUsuario(t)
	dono, err := vo.NovoDonoUsuario(usuarioID)
	require.NoError(t, err)
	documento := inserirDocumento(ctx, t, dono, 512)

	chave := uuid.New()
	const goroutines = 8

	var largada sync.WaitGroup
	largada.Add(1)
	var concluidas sync.WaitGroup
	concluidas.Add(goroutines)

	resultados := make([]entity.Job, goroutines)
	erros := make([]error, goroutines)

	for indice := range goroutines {
		go func(indice int) {
			defer concluidas.Done()
			// Cada goroutine monta seu próprio job candidato, com ID próprio,
			// para dar para ver quem "ganhou" a corrida de inserção.
			job, errJob := entity.NovoJob(documento.ID, entity.TipoAnalisar, nil)
			if errJob != nil {
				erros[indice] = errJob
				return
			}
			largada.Wait()
			resultado, err := criacao.InserirOuObter(ctx, dono, job, chave)
			resultados[indice] = resultado
			erros[indice] = err
		}(indice)
	}
	largada.Done()
	concluidas.Wait()

	for indice, err := range erros {
		require.NoErrorf(t, err, "goroutine %d", indice)
	}

	primeiro := resultados[0]
	for indice := 1; indice < goroutines; indice++ {
		assert.Equalf(t, primeiro.ID, resultados[indice].ID, "goroutine %d deveria convergir no mesmo job", indice)
		assert.Equalf(t, primeiro, resultados[indice], "goroutine %d deveria devolver o job idêntico campo a campo", indice)
	}

	var contagem int
	require.NoError(t, bancoSQL.QueryRowContext(ctx,
		`SELECT count(*) FROM jobs WHERE documento_id=$1 AND chave_idempotencia=$2`,
		documento.ID, chave).Scan(&contagem))
	assert.Equal(t, 1, contagem)
}

// TestInserirOuObterMesmaChaveDocumentosDiferentes é o cenário 3: a
// unicidade é (documento_id, chave), então a mesma chave em documentos
// diferentes tem que produzir jobs distintos, cada um preso ao seu documento.
func TestInserirOuObterMesmaChaveDocumentosDiferentes(t *testing.T) {
	ctx := context.Background()
	criacao := gerente.JobsCriacao()

	usuarioID := criarUsuario(t)
	dono, err := vo.NovoDonoUsuario(usuarioID)
	require.NoError(t, err)

	doc1 := inserirDocumento(ctx, t, dono, 100)
	doc2 := inserirDocumento(ctx, t, dono, 100)

	chave := uuid.New()

	job1, err := entity.NovoJob(doc1.ID, entity.TipoAnalisar, nil)
	require.NoError(t, err)
	resultado1, err := criacao.InserirOuObter(ctx, dono, job1, chave)
	require.NoError(t, err)

	job2, err := entity.NovoJob(doc2.ID, entity.TipoAnalisar, nil)
	require.NoError(t, err)
	resultado2, err := criacao.InserirOuObter(ctx, dono, job2, chave)
	require.NoError(t, err)

	assert.NotEqual(t, resultado1.ID, resultado2.ID)
	assert.Equal(t, doc1.ID, resultado1.DocumentoID)
	assert.Equal(t, doc2.ID, resultado2.DocumentoID)

	repetido1, err := criacao.InserirOuObter(ctx, dono, job1, chave)
	require.NoError(t, err)
	assert.Equal(t, resultado1.ID, repetido1.ID)
	assert.Equal(t, doc1.ID, repetido1.DocumentoID, "não pode vazar para o job do outro documento")

	repetido2, err := criacao.InserirOuObter(ctx, dono, job2, chave)
	require.NoError(t, err)
	assert.Equal(t, resultado2.ID, repetido2.ID)
	assert.Equal(t, doc2.ID, repetido2.DocumentoID, "não pode vazar para o job do outro documento")
}

// TestAtualizarStatusCAS é a metade "status" do cenário 4: a segunda chamada
// com o mesmo statusAtual, já obsoleto, tem que devolver conflito sem mudar
// o status gravado.
func TestAtualizarStatusCAS(t *testing.T) {
	ctx := context.Background()
	docInterno := gerente.DocumentosInternos()

	usuarioID := criarUsuario(t)
	dono, err := vo.NovoDonoUsuario(usuarioID)
	require.NoError(t, err)
	documento := inserirDocumento(ctx, t, dono, 100)

	require.NoError(t, docInterno.AtualizarStatus(ctx, documento.ID, documentoentity.StatusRecebido, documentoentity.StatusAnalisando))

	err = docInterno.AtualizarStatus(ctx, documento.ID, documentoentity.StatusRecebido, documentoentity.StatusAnalisando)
	var conflito *errors.ErroConflito
	require.ErrorAsf(t, err, &conflito, "repetir a transição a partir do status obsoleto deveria dar conflito, deu %v", err)
	assert.NotContains(t, err.Error(), documento.ID.String())

	var status string
	require.NoError(t, bancoSQL.QueryRowContext(ctx, `SELECT status FROM documentos WHERE id=$1`, documento.ID).Scan(&status))
	assert.Equal(t, string(documentoentity.StatusAnalisando), status, "o status no banco não pode ter mudado com a transição recusada")
}

// TestDefinirCDMCAS é a metade "CDM" do cenário 4: repetir com o statusAtual
// velho falha em conflito sem sobrescrever o CDM já gravado. O CDM usa acento
// e emoji para provar round-trip de UTF-8 fora do BMP através do jsonb.
func TestDefinirCDMCAS(t *testing.T) {
	ctx := context.Background()
	docInterno := gerente.DocumentosInternos()

	usuarioID := criarUsuario(t)
	dono, err := vo.NovoDonoUsuario(usuarioID)
	require.NoError(t, err)
	documento := inserirDocumento(ctx, t, dono, 100)

	require.NoError(t, docInterno.AtualizarStatus(ctx, documento.ID, documentoentity.StatusRecebido, documentoentity.StatusAnalisando))

	cdmOriginal := json.RawMessage(`{"texto":"ação 文 😀"}`)
	require.NoError(t, docInterno.DefinirCDM(ctx, documento.ID, cdmOriginal, documentoentity.StatusAnalisando, documentoentity.StatusAnalisado))

	cdmTentativaConflito := json.RawMessage(`{"texto":"outro conteudo, nunca deveria ser gravado"}`)
	err = docInterno.DefinirCDM(ctx, documento.ID, cdmTentativaConflito, documentoentity.StatusAnalisando, documentoentity.StatusAnalisado)
	var conflito *errors.ErroConflito
	require.ErrorAsf(t, err, &conflito, "repetir DefinirCDM com statusAtual velho deveria dar conflito, deu %v", err)
	assert.NotContains(t, err.Error(), "outro conteudo")

	obtido, err := docInterno.ObterPorIDInterno(ctx, documento.ID)
	require.NoError(t, err)
	assert.Equal(t, documentoentity.StatusAnalisado, obtido.Status)
	assert.JSONEq(t, string(cdmOriginal), string(obtido.CDM), "o CDM da tentativa em conflito não pode ter sobrescrito o original")

	var textoObtido struct {
		Texto string `json:"texto"`
	}
	require.NoError(t, json.Unmarshal(obtido.CDM, &textoObtido))
	assert.Equal(t, "ação 文 😀", textoObtido.Texto, "acento e emoji precisam sobreviver ao round-trip pelo jsonb")
}

// TestDocumentoRepoDonoUsuarioESessao é o cenário 5: as duas espécies de
// dono precisam funcionar nos dois sentidos — inserir/obter, paginação e
// preview — e um dono sem documento nenhum não pode virar erro.
func TestDocumentoRepoDonoUsuarioESessao(t *testing.T) {
	ctx := context.Background()
	docRepo := gerente.Documentos()

	casos := []struct {
		nome  string
		criar func(t *testing.T) vo.Dono
	}{
		{"usuario", func(t *testing.T) vo.Dono {
			id := criarUsuario(t)
			dono, err := vo.NovoDonoUsuario(id)
			require.NoError(t, err)
			return dono
		}},
		{"sessao", func(t *testing.T) vo.Dono {
			dono, err := vo.NovoDonoSessao(uuid.New())
			require.NoError(t, err)
			return dono
		}},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Run("inserir_obter_preview", func(t *testing.T) {
				dono := caso.criar(t)
				documento := inserirDocumento(ctx, t, dono, 4096)

				obtido, err := docRepo.ObterPorID(ctx, dono, documento.ID)
				require.NoError(t, err)
				assert.True(t, dono.PodeAcessar(obtido.Dono))
				assert.Equal(t, vo.FormatoDocx, obtido.Formato)
				assert.Equal(t, documentoentity.StatusRecebido, obtido.Status)
				assert.True(t, documento.CriadoEm.Truncate(time.Microsecond).Equal(obtido.CriadoEm),
					"criado_em esperado %v, obtido %v", documento.CriadoEm, obtido.CriadoEm)
				assert.True(t, documento.AtualizadoEm.Truncate(time.Microsecond).Equal(obtido.AtualizadoEm),
					"atualizado_em esperado %v, obtido %v", documento.AtualizadoEm, obtido.AtualizadoEm)

				chave, err := vo.NovaChavePreviewPDF(documento.ID)
				require.NoError(t, err)
				require.NoError(t, docRepo.DefinirChavePreviewPDF(ctx, dono, documento.ID, chave))

				reobtido, err := docRepo.ObterPorID(ctx, dono, documento.ID)
				require.NoError(t, err)
				require.NotNil(t, reobtido.ChaveStoragePDF)
				assert.Equal(t, chave, *reobtido.ChaveStoragePDF)
			})

			t.Run("listar_por_dono_paginacao", func(t *testing.T) {
				dono := caso.criar(t)
				documentos := make([]documentoentity.Documento, 0, 3)
				for range 3 {
					documentos = append(documentos, inserirDocumento(ctx, t, dono, 1024))
					// Garante criado_em estritamente crescente: timestamptz tem
					// precisão de microssegundo e chamadas em sequência podem
					// colidir na mesma janela sem essa folga.
					time.Sleep(2 * time.Millisecond)
				}

				pagina, err := docRepo.ListarPorDono(ctx, dono, 2, 1)
				require.NoError(t, err)
				require.Len(t, pagina, 2)
				assert.Equal(t, documentos[1].ID, pagina[0].ID)
				assert.Equal(t, documentos[0].ID, pagina[1].ID)
			})

			t.Run("listar_por_dono_vazio", func(t *testing.T) {
				dono := caso.criar(t)
				lista, err := docRepo.ListarPorDono(ctx, dono, 10, 0)
				require.NoError(t, err)
				assert.Empty(t, lista)
			})
		})
	}
}

// TestObterPorIDInternoQualquerDono cobre a porta exclusiva do worker: acha
// documento de qualquer dono e devolve não-encontrado para UUID inexistente.
func TestObterPorIDInternoQualquerDono(t *testing.T) {
	ctx := context.Background()
	docInterno := gerente.DocumentosInternos()

	usuarioID := criarUsuario(t)
	dono, err := vo.NovoDonoUsuario(usuarioID)
	require.NoError(t, err)
	documento := inserirDocumento(ctx, t, dono, 100)

	obtido, err := docInterno.ObterPorIDInterno(ctx, documento.ID)
	require.NoError(t, err)
	assert.Equal(t, documento.ID, obtido.ID)

	idInexistente := uuid.New()
	_, err = docInterno.ObterPorIDInterno(ctx, idInexistente)
	var naoEncontrado *errors.ErroNaoEncontrado
	require.ErrorAs(t, err, &naoEncontrado)
	assert.NotContains(t, err.Error(), idInexistente.String())
}

// TestListarPorDocumento cobre ordem decrescente e slice vazia (não
// nil-mais-erro) para documento sem job algum.
func TestListarPorDocumento(t *testing.T) {
	ctx := context.Background()
	jobConsulta := gerente.JobsConsulta()

	usuarioID := criarUsuario(t)
	dono, err := vo.NovoDonoUsuario(usuarioID)
	require.NoError(t, err)
	documento := inserirDocumento(ctx, t, dono, 100)

	semJob, err := jobConsulta.ListarPorDocumento(ctx, dono, documento.ID)
	require.NoError(t, err)
	assert.Empty(t, semJob)

	idsEmOrdemDeCriacao := make([]uuid.UUID, 0, 3)
	for range 3 {
		idsEmOrdemDeCriacao = append(idsEmOrdemDeCriacao, inserirJobFixtureSQL(ctx, t, documento.ID))
		time.Sleep(2 * time.Millisecond)
	}

	jobs, err := jobConsulta.ListarPorDocumento(ctx, dono, documento.ID)
	require.NoError(t, err)
	require.Len(t, jobs, 3)
	assert.Equal(t, idsEmOrdemDeCriacao[2], jobs[0].ID)
	assert.Equal(t, idsEmOrdemDeCriacao[1], jobs[1].ID)
	assert.Equal(t, idsEmOrdemDeCriacao[0], jobs[2].ID)
}

// TestInserirOuObterDocumentoDeTerceiro cobre que a tentativa de outro dono
// devolve não-encontrado e não deixa linha órfã em jobs.
func TestInserirOuObterDocumentoDeTerceiro(t *testing.T) {
	ctx := context.Background()
	criacao := gerente.JobsCriacao()

	usuarioDono := criarUsuario(t)
	donoReal, err := vo.NovoDonoUsuario(usuarioDono)
	require.NoError(t, err)
	documento := inserirDocumento(ctx, t, donoReal, 100)

	usuarioTerceiro := criarUsuario(t)
	terceiro, err := vo.NovoDonoUsuario(usuarioTerceiro)
	require.NoError(t, err)

	chave := uuid.New()
	job, err := entity.NovoJob(documento.ID, entity.TipoAnalisar, nil)
	require.NoError(t, err)

	_, err = criacao.InserirOuObter(ctx, terceiro, job, chave)
	var naoEncontrado *errors.ErroNaoEncontrado
	require.ErrorAsf(t, err, &naoEncontrado, "terceiro deveria receber não-encontrado, recebeu %v", err)
	assert.NotContains(t, err.Error(), documento.ID.String())

	var contagem int
	require.NoError(t, bancoSQL.QueryRowContext(ctx,
		`SELECT count(*) FROM jobs WHERE documento_id=$1 AND chave_idempotencia=$2`,
		documento.ID, chave).Scan(&contagem))
	assert.Equal(t, 0, contagem, "erro de autorização não pode deixar candidato inserido")
}

// TestInserirOuObterAposTerminalNaoReinicia cobre que repetir a chamada
// depois do job já ter virado terminal devolve o job nesse estado terminal,
// sem reiniciar tentativas nem progresso.
func TestInserirOuObterAposTerminalNaoReinicia(t *testing.T) {
	ctx := context.Background()
	criacao := gerente.JobsCriacao()

	usuarioID := criarUsuario(t)
	dono, err := vo.NovoDonoUsuario(usuarioID)
	require.NoError(t, err)
	documento := inserirDocumento(ctx, t, dono, 100)

	chave := uuid.New()
	job, err := entity.NovoJob(documento.ID, entity.TipoAnalisar, nil)
	require.NoError(t, err)

	inserido, err := criacao.InserirOuObter(ctx, dono, job, chave)
	require.NoError(t, err)
	require.Equal(t, entity.StatusPendente, inserido.Status)

	// Avança o job a um estado terminal por fora do repositório, simulando o
	// worker que já concluiu o processamento.
	_, err = bancoSQL.ExecContext(ctx,
		`UPDATE jobs SET status='concluido', progresso=100, finalizado_em=now() WHERE id=$1`,
		inserido.ID)
	require.NoError(t, err)

	repetido, err := criacao.InserirOuObter(ctx, dono, job, chave)
	require.NoError(t, err)
	assert.Equal(t, inserido.ID, repetido.ID)
	assert.Equal(t, entity.StatusConcluido, repetido.Status, "não pode reiniciar um job já terminal")
	assert.Equal(t, 100, repetido.Progresso)
}

// TestInserirOuObterPayloadDivergenteDevolveJobExistente documenta o
// comportamento real do repositório: ele NÃO compara Tipo/RulesetID contra o
// job já existente para a mesma (documento_id, chave_idempotencia) — só
// decide entre inserir o candidato ou devolver quem já ocupa a chave.
// Rejeitar payload divergente é responsabilidade de
// job/criacao.Servico.Criar (internal/domain/job/criacao/servico.go:57-59,
// coberto por servico_test.go:229-235), não do repositório: duplicar a regra
// aqui a faria divergir da camada que já é testada lá.
func TestInserirOuObterPayloadDivergenteDevolveJobExistente(t *testing.T) {
	ctx := context.Background()
	criacao := gerente.JobsCriacao()

	usuarioID := criarUsuario(t)
	dono, err := vo.NovoDonoUsuario(usuarioID)
	require.NoError(t, err)
	documento := inserirDocumento(ctx, t, dono, 100)

	chave := uuid.New()

	original, err := entity.NovoJob(documento.ID, entity.TipoAnalisar, nil)
	require.NoError(t, err)
	primeiro, err := criacao.InserirOuObter(ctx, dono, original, chave)
	require.NoError(t, err)
	require.Equal(t, entity.TipoAnalisar, primeiro.Tipo)

	rulesetID := uuid.New()
	divergente, err := entity.NovoJob(documento.ID, entity.TipoFormatar, &rulesetID)
	require.NoError(t, err)

	segundo, err := criacao.InserirOuObter(ctx, dono, divergente, chave)
	require.NoErrorf(t, err, "o repositório não compara payload: só decide inserir ou devolver quem já ocupa a chave, erro: %v", err)
	assert.Equal(t, primeiro.ID, segundo.ID)
	assert.Equal(t, entity.TipoAnalisar, segundo.Tipo, "devolve o job já existente, com o tipo original, não o divergente")
	assert.Nil(t, segundo.RulesetID)
}

// TestObterPorIDInternoCorrupcaoDeMimeDevolveErroAplicacao cobre o achado do
// validador: uma linha corrompida diretamente no banco (fora do fluxo normal)
// tem que virar *errors.ErroAplicacao (HTTP 500, sempre logado como erro),
// nunca *errors.ErroValidacao (HTTP 400, só logado em Info) — o dado não veio
// de entrada de usuário, então classificar como validação esconderia
// corrupção real atrás de uma resposta de "requisição inválida".
//
// O sub-caso "dono ambíguo" foi avaliado e descartado: CHECK
// documentos_dono_exclusivo (migration 00002) já impede fisicamente
// usuario_id e sessao_id não-nulos na mesma linha, então não há como montar
// essa corrupção por SQL direto sem a própria instrução da fixture falhar
// pelo CHECK. O caso de mime abaixo passa pelo mesmo ponto de classificação
// em scanDocumento e já é suficiente para provar o bug.
func TestObterPorIDInternoCorrupcaoDeMimeDevolveErroAplicacao(t *testing.T) {
	ctx := context.Background()
	docInterno := gerente.DocumentosInternos()

	usuarioID := criarUsuario(t)
	dono, err := vo.NovoDonoUsuario(usuarioID)
	require.NoError(t, err)
	documento := inserirDocumento(ctx, t, dono, 100)

	_, err = bancoSQL.ExecContext(ctx, `UPDATE documentos SET mime = 'aplicacao/invalida' WHERE id = $1`, documento.ID)
	require.NoError(t, err)

	_, err = docInterno.ObterPorIDInterno(ctx, documento.ID)
	require.Error(t, err)

	var erroAplicacao *errors.ErroAplicacao
	require.ErrorAsf(t, err, &erroAplicacao,
		"corrupção de linha no banco deveria virar erro de aplicação (500), deu %T: %v", err, err)

	var erroValidacao *errors.ErroValidacao
	assert.False(t, errors.Como(err, &erroValidacao),
		"corrupção de linha no banco não pode virar erro de validação (400)")
}

// inserirJobFixtureComStatusSQL grava um job diretamente por SQL, já num
// status/tentativas/progresso/resultado conhecidos — necessário porque o
// caminho normal (JobsCriacao().InserirOuObter) só produz jobs em
// "pendente", e ExecucaoJobRepo precisa de fixtures em qualquer status para
// exercitar o CAS.
//
// RED (TDD): esta função e os testes abaixo dependem de
// gerente.JobsExecucao(), de contracts.ExecucaoJobRepo e dos métodos novos
// ObterPorIDInterno/Salvar em RepositorioJob — nenhum implementado ainda.
// Falha esperada por símbolo ausente em `go vet -tags=integration`.
func inserirJobFixtureComStatusSQL(
	ctx context.Context, t *testing.T, documentoID uuid.UUID,
	status entity.StatusJob, tentativas, progresso int, resultado json.RawMessage,
) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := bancoSQL.QueryRowContext(ctx,
		`INSERT INTO jobs (documento_id, tipo, status, tentativas, progresso, resultado, chave_idempotencia)
		 VALUES ($1, 'analisar', $2, $3, $4, $5, gen_random_uuid()) RETURNING id`,
		documentoID, string(status), tentativas, progresso, []byte(resultado)).Scan(&id)
	require.NoError(t, err)
	return id
}

// TestExecucaoObterPorIDInternoJobExistente cobre a leitura completa da
// porta exclusiva do worker: status, tentativas, progresso, resultado,
// iniciado_em e finalizado_em, incluindo acento e emoji no resultado jsonb.
func TestExecucaoObterPorIDInternoJobExistente(t *testing.T) {
	ctx := context.Background()
	execucao := gerente.JobsExecucao()

	usuarioID := criarUsuario(t)
	dono, err := vo.NovoDonoUsuario(usuarioID)
	require.NoError(t, err)
	documento := inserirDocumento(ctx, t, dono, 100)

	resultado := json.RawMessage(`{"paginas":7,"texto":"ação 文 😀"}`)
	jobID := inserirJobFixtureComStatusSQL(ctx, t, documento.ID, entity.StatusConcluido, 3, 100, resultado)
	_, err = bancoSQL.ExecContext(ctx,
		`UPDATE jobs SET iniciado_em = now() - interval '1 minute', finalizado_em = now() WHERE id = $1`, jobID)
	require.NoError(t, err)

	obtido, err := execucao.ObterPorIDInterno(ctx, jobID)
	require.NoError(t, err)
	assert.Equal(t, jobID, obtido.ID)
	assert.Equal(t, documento.ID, obtido.DocumentoID)
	assert.Equal(t, entity.StatusConcluido, obtido.Status)
	assert.Equal(t, 3, obtido.Tentativas)
	assert.Equal(t, 100, obtido.Progresso)
	require.NotNil(t, obtido.IniciadoEm)
	require.NotNil(t, obtido.FinalizadoEm)
	assert.True(t, obtido.IniciadoEm.Before(*obtido.FinalizadoEm))
	require.NotNil(t, obtido.Resultado)
	assert.JSONEq(t, string(resultado), string(obtido.Resultado))
}

// TestExecucaoObterPorIDInternoInexistente cobre id sem linha correspondente.
func TestExecucaoObterPorIDInternoInexistente(t *testing.T) {
	ctx := context.Background()
	execucao := gerente.JobsExecucao()

	idInexistente := uuid.New()
	_, err := execucao.ObterPorIDInterno(ctx, idInexistente)
	var naoEncontrado *errors.ErroNaoEncontrado
	require.ErrorAsf(t, err, &naoEncontrado, "id inexistente deveria dar não-encontrado, deu %v", err)
	assert.NotContains(t, err.Error(), idInexistente.String())
}

// TestExecucaoSalvarComStatusAtualCorreto cobre o caminho feliz do CAS:
// grava status, progresso, resultado e finalizado_em quando statusAtual
// ainda confere com o banco.
func TestExecucaoSalvarComStatusAtualCorreto(t *testing.T) {
	ctx := context.Background()
	execucao := gerente.JobsExecucao()

	usuarioID := criarUsuario(t)
	dono, err := vo.NovoDonoUsuario(usuarioID)
	require.NoError(t, err)
	documento := inserirDocumento(ctx, t, dono, 100)
	jobID := inserirJobFixtureComStatusSQL(ctx, t, documento.ID, entity.StatusExecutando, 1, 20, nil)

	job, err := execucao.ObterPorIDInterno(ctx, jobID)
	require.NoError(t, err)
	require.NoError(t, job.Concluir(json.RawMessage(`{"ok":true}`)))

	require.NoError(t, execucao.Salvar(ctx, job, entity.StatusExecutando))

	relido, err := execucao.ObterPorIDInterno(ctx, jobID)
	require.NoError(t, err)
	assert.Equal(t, entity.StatusConcluido, relido.Status)
	assert.Equal(t, 100, relido.Progresso)
	assert.JSONEq(t, `{"ok":true}`, string(relido.Resultado))
	require.NotNil(t, relido.FinalizadoEm)
}

// TestExecucaoSalvarComStatusAtualDivergenteDevolveConflito cobre a outra
// metade do CAS: statusAtual obsoleto (outro worker já mudou o status)
// devolve conflito e não altera a linha.
func TestExecucaoSalvarComStatusAtualDivergenteDevolveConflito(t *testing.T) {
	ctx := context.Background()
	execucao := gerente.JobsExecucao()

	usuarioID := criarUsuario(t)
	dono, err := vo.NovoDonoUsuario(usuarioID)
	require.NoError(t, err)
	documento := inserirDocumento(ctx, t, dono, 100)
	jobID := inserirJobFixtureComStatusSQL(ctx, t, documento.ID, entity.StatusExecutando, 1, 20, nil)

	job, err := execucao.ObterPorIDInterno(ctx, jobID)
	require.NoError(t, err)
	require.NoError(t, job.Concluir(json.RawMessage(`{"ok":true}`)))

	// statusAtual divergente do que está no banco ("executando"): simula outra
	// goroutine que já mudou o status entre a leitura e esta escrita.
	err = execucao.Salvar(ctx, job, entity.StatusPendente)
	var conflito *errors.ErroConflito
	require.ErrorAsf(t, err, &conflito, "CAS com statusAtual obsoleto deveria dar conflito, deu %v", err)
	assert.NotContains(t, err.Error(), jobID.String())

	relido, err := execucao.ObterPorIDInterno(ctx, jobID)
	require.NoError(t, err)
	assert.Equal(t, entity.StatusExecutando, relido.Status, "a linha não pode ter mudado quando o CAS perde")
	assert.Equal(t, 20, relido.Progresso)
	assert.Empty(t, relido.Resultado, "escrita perdida não pode ter gravado o resultado")
}

// TestExecucaoSalvarConcorrente é o cenário de concorrência real: oito
// goroutines leem o mesmo job em "executando" e disputam Salvar com o mesmo
// statusAtual original — só uma pode vencer, mesmo padrão de
// TestInserirOuObterConcorrente/TestAtualizarStatusCAS acima. Rodar com -race.
func TestExecucaoSalvarConcorrente(t *testing.T) {
	ctx := context.Background()
	execucao := gerente.JobsExecucao()

	usuarioID := criarUsuario(t)
	dono, err := vo.NovoDonoUsuario(usuarioID)
	require.NoError(t, err)
	documento := inserirDocumento(ctx, t, dono, 100)
	jobID := inserirJobFixtureComStatusSQL(ctx, t, documento.ID, entity.StatusExecutando, 1, 10, nil)

	const goroutines = 8
	var largada sync.WaitGroup
	largada.Add(1)
	var concluidas sync.WaitGroup
	concluidas.Add(goroutines)
	erros := make([]error, goroutines)

	for indice := range goroutines {
		go func(indice int) {
			defer concluidas.Done()
			job, err := execucao.ObterPorIDInterno(ctx, jobID)
			if err != nil {
				erros[indice] = err
				return
			}
			if err := job.Concluir(json.RawMessage(fmt.Sprintf(`{"vencedor":%d}`, indice))); err != nil {
				erros[indice] = err
				return
			}
			largada.Wait()
			erros[indice] = execucao.Salvar(ctx, job, entity.StatusExecutando)
		}(indice)
	}
	largada.Done()
	concluidas.Wait()

	vencedoras, conflitos := 0, 0
	for indice, err := range erros {
		switch {
		case err == nil:
			vencedoras++
		default:
			var conflito *errors.ErroConflito
			require.ErrorAsf(t, err, &conflito, "goroutine %d: erro inesperado %v", indice, err)
			conflitos++
		}
	}
	assert.Equal(t, 1, vencedoras, "só uma goroutine pode vencer o CAS")
	assert.Equal(t, goroutines-1, conflitos)

	relido, err := execucao.ObterPorIDInterno(ctx, jobID)
	require.NoError(t, err)
	assert.Equal(t, entity.StatusConcluido, relido.Status, "a vencedora precisa ter persistido de fato")
}
