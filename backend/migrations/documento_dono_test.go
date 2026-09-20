//go:build integration

package migrations_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestDocumentoDono(t *testing.T) {
	for _, cenario := range []string{"ciclo_up_down_up", "rollback_usuario_zero"} {
		t.Run(cenario, func(t *testing.T) {
			ctx, cancelar := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancelar()
			banco := novoPostgres(t, ctx)
			migrador, err := goose.NewProvider(goose.DialectPostgres, banco, os.DirFS("."))
			if err != nil {
				t.Fatal(err)
			}
			executar := func(comando string) {
				t.Helper()
				if _, err := banco.ExecContext(ctx, comando); err != nil {
					t.Fatal(err)
				}
			}
			verificar := func(consulta string, argumentos ...any) {
				t.Helper()
				var valido bool
				if err := banco.QueryRowContext(ctx, consulta, argumentos...).Scan(&valido); err != nil {
					t.Fatal(err)
				}
				if !valido {
					t.Fatalf("invariante violada: %s", consulta)
				}
			}
			if _, err := migrador.UpTo(ctx, 1); err != nil {
				t.Fatal(err)
			}
			executar(`
INSERT INTO usuarios (id,email,senha_hash,nome) VALUES
 ('00000000-0000-0000-0000-000000000001','teste@example.invalid','ficticio','Teste');
INSERT INTO documentos (usuario_id,nome_original,mime,tamanho_bytes,chave_storage,cdm)
 SELECT CASE WHEN n=1 THEN '00000000-0000-0000-0000-000000000001'::uuid END,
 'legado-'||n,'application/test',42,'teste/'||n,'{"texto":"ação 文 😀"}'::jsonb
 FROM generate_series(1,3) n;
INSERT INTO jobs (documento_id,tipo) SELECT id,'analisar' FROM documentos;
INSERT INTO artefatos (job_id,formato,chave_storage,tamanho_bytes) SELECT id,'docx','teste/artefato',42 FROM jobs;
INSERT INTO relatorios_mudanca (job_id,mudancas) SELECT id,'[]' FROM jobs;
CREATE VIEW dependente AS SELECT id,nome_original FROM documentos;`)
			if cenario == "rollback_usuario_zero" {
				executar(`INSERT INTO usuarios (id,email,senha_hash,nome)
 VALUES ('00000000-0000-0000-0000-000000000000','zero@example.invalid','ficticio','Zero');
 UPDATE documentos SET usuario_id='00000000-0000-0000-0000-000000000000' WHERE nome_original='legado-1';`)
			}
			// Inclui OIDs, definições de colunas/índices/FKs, dados e dependentes.
			const estado = `SELECT jsonb_build_object(
 'objetos',(SELECT jsonb_agg(jsonb_build_array(c.oid,c.relname,c.relkind)) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public'),
 'colunas',(SELECT jsonb_agg(jsonb_build_array(a.attrelid,a.attnum,a.attname,a.atttypid,a.atttypmod,a.attnotnull,pg_get_expr(d.adbin,d.adrelid))) FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid JOIN pg_namespace n ON n.oid=c.relnamespace LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum WHERE n.nspname='public' AND a.attnum>0 AND NOT a.attisdropped AND a.attname<>'sessao_id'),
 'constraints',(SELECT jsonb_agg(jsonb_build_array(c.oid,c.conrelid,pg_get_constraintdef(c.oid))) FROM pg_constraint c JOIN pg_namespace n ON n.oid=c.connamespace WHERE n.nspname='public'),
 'indices',(SELECT jsonb_agg(jsonb_build_array(i.indexrelid,pg_get_indexdef(i.indexrelid))) FROM pg_index i JOIN pg_class c ON c.oid=i.indrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public'),
 'documentos',(SELECT jsonb_agg(to_jsonb(d)-'sessao_id') FROM documentos d),
 'usuarios',(SELECT jsonb_agg(to_jsonb(u)) FROM usuarios u),
 'jobs',(SELECT jsonb_agg(to_jsonb(j)) FROM jobs j),
 'artefatos',(SELECT jsonb_agg(to_jsonb(a)) FROM artefatos a),
 'relatorios',(SELECT jsonb_agg(to_jsonb(r)) FROM relatorios_mudanca r),
 'dependente',(SELECT jsonb_agg(to_jsonb(v)) FROM dependente v))`
			var antes string
			if err := banco.QueryRowContext(ctx, estado).Scan(&antes); err != nil {
				t.Fatal(err)
			}
			_, err = migrador.UpTo(ctx, 2)
			if cenario == "rollback_usuario_zero" {
				var erroPG *pgconn.PgError
				if !errors.As(err, &erroPG) || erroPG.Code != "23514" {
					t.Fatalf("Up 2 deve falhar por CHECK (23514), não por migration ausente: %v", err)
				}
				verificar("SELECT $1::jsonb <@ atual AND atual <@ $1::jsonb FROM ("+estado+") s(atual)", antes)
				verificar(`SELECT NOT EXISTS (SELECT FROM information_schema.columns WHERE table_name='documentos' AND column_name='sessao_id')`)
				versao, err := migrador.GetDBVersion(ctx)
				if err != nil || versao != 1 {
					t.Fatalf("rollback deve manter versão 1: versão=%d erro=%v", versao, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Up 2 obrigatório (00002_documento_dono.sql): %v", err)
			}
			versao, err := migrador.GetDBVersion(ctx)
			if err != nil || versao != 2 {
				t.Fatalf("migration 00002_documento_dono.sql ausente ou não aplicada: versão=%d erro=%v", versao, err)
			}
			verificar("SELECT $1::jsonb <@ ("+estado+")", antes)
			verificar(`SELECT count(*)=2 AND count(DISTINCT sessao_id)=2 AND bool_and(sessao_id<>'00000000-0000-0000-0000-000000000000') FROM documentos WHERE usuario_id IS NULL`)
			verificar(`SELECT sessao_id IS NULL AND usuario_id='00000000-0000-0000-0000-000000000001' FROM documentos WHERE nome_original='legado-1'`)
			verificar(`SELECT data_type='uuid' AND column_default IS NULL FROM information_schema.columns WHERE table_name='documentos' AND column_name='sessao_id'`)
			verificar(`SELECT count(*)=1 FROM pg_index WHERE indrelid='documentos'::regclass AND NOT indisunique AND indnkeyatts=2 AND indnatts=2 AND pg_get_indexdef(indexrelid,1,true)='sessao_id' AND pg_get_indexdef(indexrelid,2,true)='criado_em' AND (indoption[1] & 1)=1 AND pg_get_expr(indpred,indrelid)='(sessao_id IS NOT NULL)'`)
			executar(`CREATE TEMP TABLE sessoes_antes AS SELECT id,sessao_id FROM documentos WHERE usuario_id IS NULL;
 INSERT INTO usuarios (id,email,senha_hash,nome) VALUES ('00000000-0000-0000-0000-000000000000','zero@example.invalid','ficticio','Zero');`)
			for _, caso := range []struct{ nome, usuario, sessao string }{
				{"nenhum", "NULL", "NULL"},
				{"ambos", "'00000000-0000-0000-0000-000000000001'", "gen_random_uuid()"},
				{"usuario_zero", "'00000000-0000-0000-0000-000000000000'", "NULL"},
				{"sessao_zero", "NULL", "'00000000-0000-0000-0000-000000000000'"},
			} {
				t.Run(caso.nome, func(t *testing.T) {
					for _, comando := range []string{
						fmt.Sprintf("INSERT INTO documentos (usuario_id,sessao_id,nome_original,mime,tamanho_bytes,chave_storage) VALUES (%s,%s,'novo','teste',1,'teste')", caso.usuario, caso.sessao),
						fmt.Sprintf("UPDATE documentos SET usuario_id=%s,sessao_id=%s WHERE nome_original='legado-1'", caso.usuario, caso.sessao),
					} {
						_, err := banco.ExecContext(ctx, comando)
						var erroPG *pgconn.PgError
						if !errors.As(err, &erroPG) || erroPG.Code != "23514" {
							t.Errorf("esperado CHECK 23514: %v", err)
						}
					}
				})
			}
			// As escritas válidas também são revertidas para comparar o Down ao original.
			executar(`BEGIN;
 INSERT INTO documentos (usuario_id,nome_original,mime,tamanho_bytes,chave_storage) VALUES ('00000000-0000-0000-0000-000000000001','novo','teste',1,'teste');
 INSERT INTO documentos (sessao_id,nome_original,mime,tamanho_bytes,chave_storage) SELECT sessao_id,'novo','teste',1,'teste' FROM sessoes_antes CROSS JOIN generate_series(1,2);
 UPDATE documentos SET usuario_id=NULL,sessao_id=gen_random_uuid() WHERE nome_original='legado-1';
 UPDATE documentos SET usuario_id='00000000-0000-0000-0000-000000000001',sessao_id=NULL WHERE nome_original='legado-2';
 ROLLBACK;
 DELETE FROM usuarios WHERE id='00000000-0000-0000-0000-000000000000';`)
			if _, err := migrador.DownTo(ctx, 1); err != nil {
				t.Fatal(err)
			}
			verificar("SELECT $1::jsonb <@ atual AND atual <@ $1::jsonb FROM ("+estado+") s(atual)", antes)
			verificar(`SELECT NOT EXISTS (SELECT FROM information_schema.columns WHERE table_name='documentos' AND column_name='sessao_id')`)
			if _, err := migrador.UpTo(ctx, 2); err != nil {
				t.Fatal(err)
			}
			verificar(`SELECT count(*)=2 AND count(DISTINCT d.sessao_id)=2 AND bool_and(d.sessao_id<>a.sessao_id AND d.sessao_id<>'00000000-0000-0000-0000-000000000000') FROM documentos d JOIN sessoes_antes a USING(id)`)
			verificar("SELECT $1::jsonb <@ ("+estado+")", antes)
		})
	}
}

func novoPostgres(t *testing.T, ctx context.Context) *sql.DB {
	t.Helper()
	instancia, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "postgres:16-alpine",
			Env:          map[string]string{"POSTGRES_DB": "teste", "POSTGRES_USER": "teste", "POSTGRES_HOST_AUTH_METHOD": "trust"},
			ExposedPorts: []string{"5432/tcp"},
			HostConfigModifier: func(config *container.HostConfig) {
				config.NetworkMode = container.NetworkMode(os.Getenv("MIGRACOES_REDE_CONTAINER"))
				config.PortBindings = network.PortMap{network.MustParsePort("5432/tcp"): {{HostIP: netip.MustParseAddr("127.0.0.1"), HostPort: "0"}}}
			},
			WaitingFor: wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(time.Minute),
		}, Started: true,
	})
	if instancia != nil {
		t.Cleanup(func() {
			ctx, cancelar := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancelar()
			if err := instancia.Terminate(ctx); err != nil {
				t.Errorf("remover PostgreSQL descartável: %v", err)
			}
		})
	}
	if err != nil {
		t.Fatal(err)
	}
	porta, err := instancia.MappedPort(ctx, "5432/tcp")
	if err != nil {
		t.Fatal(err)
	}
	banco, err := sql.Open("pgx", "postgres://teste@127.0.0.1:"+porta.Port()+"/teste?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	banco.SetMaxOpenConns(1) // Mantém tabelas temporárias na mesma conexão.
	t.Cleanup(func() {
		if err := banco.Close(); err != nil {
			t.Errorf("fechar conexão: %v", err)
		}
	})
	if err := banco.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	return banco
}
