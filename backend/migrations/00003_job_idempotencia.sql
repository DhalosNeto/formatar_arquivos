-- +goose Up
-- +goose StatementBegin

ALTER TABLE jobs ADD COLUMN chave_idempotencia uuid;

UPDATE jobs SET chave_idempotencia = gen_random_uuid();

ALTER TABLE jobs
    ALTER COLUMN chave_idempotencia SET NOT NULL,
    ADD CONSTRAINT jobs_chave_idempotencia_nao_zero CHECK (chave_idempotencia <> '00000000-0000-0000-0000-000000000000'::uuid),
    ADD CONSTRAINT jobs_documento_chave_idempotencia_unica UNIQUE (documento_id, chave_idempotencia);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Down perde todas as chaves; um novo Up gera novas chaves.
ALTER TABLE jobs
    DROP CONSTRAINT jobs_documento_chave_idempotencia_unica,
    DROP CONSTRAINT jobs_chave_idempotencia_nao_zero,
    DROP COLUMN chave_idempotencia;

-- +goose StatementEnd
