-- +goose Up
-- +goose StatementBegin

ALTER TABLE documentos ADD COLUMN sessao_id uuid;

UPDATE documentos SET sessao_id = gen_random_uuid() WHERE usuario_id IS NULL;

ALTER TABLE documentos
    ADD CONSTRAINT documentos_dono_exclusivo CHECK (num_nonnulls(usuario_id, sessao_id) = 1),
    ADD CONSTRAINT documentos_usuario_nao_zero CHECK (usuario_id <> '00000000-0000-0000-0000-000000000000'::uuid),
    ADD CONSTRAINT documentos_sessao_nao_zero CHECK (sessao_id <> '00000000-0000-0000-0000-000000000000'::uuid);

CREATE INDEX documentos_por_sessao ON documentos (sessao_id, criado_em DESC)
    WHERE sessao_id IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Down perde os identificadores das sessões: exige backup prévio e avaliação operacional.
-- Um novo Up gera novas identidades, sem recuperar as sessões anteriores.
DROP INDEX documentos_por_sessao;
ALTER TABLE documentos
    DROP CONSTRAINT documentos_dono_exclusivo,
    DROP CONSTRAINT documentos_usuario_nao_zero,
    DROP CONSTRAINT documentos_sessao_nao_zero,
    DROP COLUMN sessao_id;

-- +goose StatementEnd
