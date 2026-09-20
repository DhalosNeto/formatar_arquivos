//go:build integration

package migrations_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pressly/goose/v3"
)

func TestJobIdempotencia(t *testing.T) {
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
	verificarVersao := func(esperada int64) {
		t.Helper()
		versao, err := migrador.GetDBVersion(ctx)
		if err != nil || versao != esperada {
			t.Fatalf("migration obrigatória: versão esperada=%d obtida=%d erro=%v", esperada, versao, err)
		}
	}
	if _, err := migrador.UpTo(ctx, 2); err != nil {
		t.Fatal(err)
	}
	verificarVersao(2)
	executar(`
INSERT INTO documentos (sessao_id,nome_original,mime,tamanho_bytes,chave_storage)
 SELECT gen_random_uuid(),'documento-'||n,'application/test',42,'teste/'||n FROM generate_series(1,2) n;
INSERT INTO rulesets (slug,versao,nome,definicao,checksum)
 SELECT 'teste-'||n,1,'Teste','{}','checksum-'||n FROM generate_series(1,2) n;
INSERT INTO jobs (documento_id,ruleset_id,tipo,status,tentativas,progresso,erro,resultado,criado_em,iniciado_em,finalizado_em)
 SELECT d.id,r.id,'analisar',s,2,50,'erro fictício','{"teste":true}',
 '2026-01-01 UTC','2026-01-02 UTC','2026-01-03 UTC'
 FROM documentos d CROSS JOIN rulesets r
 CROSS JOIN unnest(ARRAY['pendente','executando','concluido','falhou','cancelado']) s
 WHERE d.nome_original='documento-1' AND r.slug='teste-1';
INSERT INTO jobs (documento_id,tipo) SELECT id,'renderizar_preview' FROM documentos WHERE nome_original='documento-1';
INSERT INTO artefatos (job_id,formato,chave_storage,tamanho_bytes) SELECT id,'docx','teste/artefato',42 FROM jobs;
INSERT INTO relatorios_mudanca (job_id,mudancas) SELECT id,'[{"teste":true}]' FROM jobs;
CREATE VIEW jobs_dependentes AS SELECT id,documento_id FROM jobs;`)
	// Snapshot inclui IDs, todos os campos legados, dependentes e identidade das tabelas.
	const estado = `SELECT jsonb_build_object(
 'objetos',(SELECT jsonb_agg(jsonb_build_array(c.oid,c.relname,c.relkind) ORDER BY c.oid) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public'),
 'colunas',(SELECT jsonb_agg(jsonb_build_array(a.attrelid,a.attnum,a.attname,a.atttypid,a.atttypmod,a.attnotnull,pg_get_expr(d.adbin,d.adrelid)) ORDER BY a.attrelid,a.attnum) FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid JOIN pg_namespace n ON n.oid=c.relnamespace LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum WHERE n.nspname='public' AND c.relkind IN ('r','v') AND a.attnum>0 AND NOT a.attisdropped AND a.attname<>'chave_idempotencia'),
 'constraints',(SELECT jsonb_agg(jsonb_build_array(c.oid,c.conrelid,c.conname,pg_get_constraintdef(c.oid)) ORDER BY c.oid) FROM pg_constraint c JOIN pg_namespace n ON n.oid=c.connamespace WHERE n.nspname='public'),
 'indices',(SELECT jsonb_agg(jsonb_build_array(i.indexrelid,pg_get_indexdef(i.indexrelid)) ORDER BY i.indexrelid) FROM pg_index i JOIN pg_class c ON c.oid=i.indrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public'),
 'documentos',(SELECT jsonb_agg(to_jsonb(d) ORDER BY d.id) FROM documentos d),
 'rulesets',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM rulesets r),
 'jobs',(SELECT jsonb_agg(to_jsonb(j)-'chave_idempotencia' ORDER BY j.id) FROM jobs j),
 'artefatos',(SELECT jsonb_agg(to_jsonb(a) ORDER BY a.id) FROM artefatos a),
 'relatorios',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM relatorios_mudanca r),
 'dependente',(SELECT jsonb_agg(to_jsonb(v) ORDER BY v.id) FROM jobs_dependentes v))`
	var antes string
	if err := banco.QueryRowContext(ctx, estado).Scan(&antes); err != nil {
		t.Fatal(err)
	}
	verificarChaves := func() {
		t.Helper()
		verificar(`SELECT count(*)=6 AND count(chave_idempotencia)=6 AND count(DISTINCT chave_idempotencia)=6
 AND bool_and(chave_idempotencia<>'00000000-0000-0000-0000-000000000000'::uuid) FROM jobs`)
		verificar(`SELECT data_type='uuid' AND is_nullable='NO' AND column_default IS NULL
 FROM information_schema.columns WHERE table_schema='public' AND table_name='jobs' AND column_name='chave_idempotencia'`)
		// Igualdade nos dados, inclusão apenas no catálogo que recebe novas constraints/índices.
		verificar("SELECT $1::jsonb <@ atual AND (atual - ARRAY['objetos','constraints','indices']) = ($1::jsonb - ARRAY['objetos','constraints','indices']) FROM ("+estado+") s(atual)", antes)
	}
	if _, err := migrador.UpTo(ctx, 3); err != nil {
		t.Fatalf("Up 3 obrigatório (00003_job_idempotencia.sql): %v", err)
	}
	verificarVersao(3) // UpTo sem migration não pode produzir um falso GREEN.
	verificarChaves()
	executar(`CREATE TEMP TABLE chaves_antes AS SELECT id,chave_idempotencia FROM jobs`)
	const origem = ` FROM jobs WHERE status='concluido'`
	for _, caso := range []struct{ nome, comando, codigo string }{
		{"chave_null", `INSERT INTO jobs (documento_id,tipo,chave_idempotencia) SELECT documento_id,'analisar',NULL` + origem, "23502"},
		{"chave_omitida", `INSERT INTO jobs (documento_id,tipo) SELECT documento_id,'analisar'` + origem, "23502"},
		{"chave_zero", `INSERT INTO jobs (documento_id,tipo,chave_idempotencia) SELECT documento_id,'analisar','00000000-0000-0000-0000-000000000000'` + origem, "23514"},
		{"mesmo_pedido", `INSERT INTO jobs (documento_id,tipo,ruleset_id,status,chave_idempotencia) SELECT documento_id,tipo,ruleset_id,status,chave_idempotencia` + origem, "23505"},
		{"outro_tipo", `INSERT INTO jobs (documento_id,tipo,ruleset_id,status,chave_idempotencia) SELECT documento_id,'formatar',ruleset_id,status,chave_idempotencia` + origem, "23505"},
		{"outro_ruleset", `INSERT INTO jobs (documento_id,tipo,ruleset_id,status,chave_idempotencia) SELECT documento_id,tipo,(SELECT id FROM rulesets WHERE slug='teste-2'),status,chave_idempotencia` + origem, "23505"},
		{"outro_status", `INSERT INTO jobs (documento_id,tipo,ruleset_id,status,chave_idempotencia) SELECT documento_id,tipo,ruleset_id,'pendente',chave_idempotencia` + origem, "23505"},
		{"ruleset_null", `INSERT INTO jobs (documento_id,tipo,chave_idempotencia) SELECT documento_id,tipo,chave_idempotencia` + origem, "23505"},
		{"update_null", `UPDATE jobs SET chave_idempotencia=NULL WHERE status='concluido'`, "23502"},
		{"update_zero", `UPDATE jobs SET chave_idempotencia='00000000-0000-0000-0000-000000000000' WHERE status='concluido'`, "23514"},
		{"update_duplicado", `UPDATE jobs SET chave_idempotencia=(SELECT chave_idempotencia FROM jobs WHERE status='concluido') WHERE status='cancelado'`, "23505"},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			_, err := banco.ExecContext(ctx, caso.comando)
			var erroPG *pgconn.PgError
			if !errors.As(err, &erroPG) || erroPG.Code != caso.codigo {
				t.Fatalf("SQLSTATE esperado=%s erro=%v", caso.codigo, err)
			}
		})
	}
	// Reverte apenas as inserções de aceitação para comparar o ciclo ao legado.
	transacao, err := banco.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, comando := range []string{
		`INSERT INTO jobs (documento_id,tipo,chave_idempotencia) SELECT (SELECT id FROM documentos WHERE nome_original='documento-2'),'analisar',chave_idempotencia` + origem,
		`INSERT INTO jobs (documento_id,tipo,chave_idempotencia) SELECT documento_id,'analisar',gen_random_uuid()` + origem,
	} {
		resultado, err := transacao.ExecContext(ctx, comando)
		if err != nil {
			t.Error(err)
			break
		}
		quantidade, err := resultado.RowsAffected()
		if err != nil || quantidade != 1 {
			t.Errorf("inserção válida deve criar um job: quantidade=%d erro=%v", quantidade, err)
		}
	}
	if err := transacao.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := migrador.DownTo(ctx, 2); err != nil {
		t.Fatal(err)
	}
	verificarVersao(2)
	verificar(`SELECT NOT EXISTS (SELECT FROM information_schema.columns WHERE table_schema='public' AND table_name='jobs' AND column_name='chave_idempotencia')`)
	verificar("SELECT $1::jsonb <@ atual AND atual <@ $1::jsonb FROM ("+estado+") s(atual)", antes)
	if _, err := migrador.UpTo(ctx, 3); err != nil {
		t.Fatal(err)
	}
	verificarVersao(3)
	verificarChaves()
	verificar(`SELECT count(*)=6 AND bool_and(j.chave_idempotencia<>a.chave_idempotencia)
 FROM jobs j JOIN chaves_antes a USING(id)`)
}
