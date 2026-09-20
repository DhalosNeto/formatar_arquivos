-- +goose Up
-- +goose StatementBegin

CREATE TABLE usuarios (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email       text        NOT NULL,
    senha_hash  text        NOT NULL,
    nome        text        NOT NULL,
    criado_em   timestamptz NOT NULL DEFAULT now(),
    atualizado_em timestamptz NOT NULL DEFAULT now()
);

-- E-mail é comparado sem distinção de caixa: a unicidade tem de valer sobre a
-- forma normalizada, senão "Ana@x.com" e "ana@x.com" viram contas diferentes.
CREATE UNIQUE INDEX usuarios_email_unico ON usuarios (lower(email));

CREATE TABLE rulesets (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    slug         text        NOT NULL,
    versao       integer     NOT NULL,
    nome         text        NOT NULL,
    definicao    jsonb       NOT NULL,
    checksum     text        NOT NULL,
    ativo        boolean     NOT NULL DEFAULT true,
    criado_em    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT rulesets_slug_versao_unico UNIQUE (slug, versao),
    CONSTRAINT rulesets_versao_positiva CHECK (versao > 0)
);

CREATE INDEX rulesets_ativos ON rulesets (slug) WHERE ativo;

CREATE TABLE documentos (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    usuario_id       uuid        REFERENCES usuarios (id) ON DELETE CASCADE,
    nome_original    text        NOT NULL,
    mime             text        NOT NULL,
    tamanho_bytes    bigint      NOT NULL,
    chave_storage    text        NOT NULL,
    chave_storage_pdf text,
    status           text        NOT NULL DEFAULT 'recebido',
    cdm              jsonb,
    criado_em        timestamptz NOT NULL DEFAULT now(),
    atualizado_em    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT documentos_tamanho_positivo CHECK (tamanho_bytes > 0),
    CONSTRAINT documentos_status_valido CHECK (
        status IN ('recebido', 'analisando', 'analisado', 'formatando', 'formatado', 'falhou')
    )
);

CREATE INDEX documentos_por_usuario ON documentos (usuario_id, criado_em DESC);

CREATE TABLE jobs (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    documento_id   uuid        NOT NULL REFERENCES documentos (id) ON DELETE CASCADE,
    ruleset_id     uuid        REFERENCES rulesets (id) ON DELETE RESTRICT,
    tipo           text        NOT NULL,
    status         text        NOT NULL DEFAULT 'pendente',
    tentativas     integer     NOT NULL DEFAULT 0,
    progresso      smallint    NOT NULL DEFAULT 0,
    erro           text,
    resultado      jsonb,
    criado_em      timestamptz NOT NULL DEFAULT now(),
    iniciado_em    timestamptz,
    finalizado_em  timestamptz,
    CONSTRAINT jobs_tipo_valido CHECK (tipo IN ('renderizar_preview', 'analisar', 'formatar')),
    CONSTRAINT jobs_status_valido CHECK (status IN ('pendente', 'executando', 'concluido', 'falhou', 'cancelado')),
    CONSTRAINT jobs_progresso_valido CHECK (progresso BETWEEN 0 AND 100),
    -- Formatar exige ruleset; preview e análise não usam nenhum.
    CONSTRAINT jobs_ruleset_obrigatorio_para_formatar CHECK (tipo <> 'formatar' OR ruleset_id IS NOT NULL)
);

CREATE INDEX jobs_por_documento ON jobs (documento_id, criado_em DESC);
CREATE INDEX jobs_pendentes ON jobs (status, criado_em) WHERE status IN ('pendente', 'executando');

CREATE TABLE artefatos (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id        uuid        NOT NULL REFERENCES jobs (id) ON DELETE CASCADE,
    formato       text        NOT NULL,
    chave_storage text        NOT NULL,
    tamanho_bytes bigint      NOT NULL,
    criado_em     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT artefatos_formato_valido CHECK (formato IN ('docx', 'pdf', 'tex')),
    -- Reprocessar um job não pode duplicar o mesmo formato.
    CONSTRAINT artefatos_job_formato_unico UNIQUE (job_id, formato)
);

CREATE TABLE relatorios_mudanca (
    id        uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id    uuid        NOT NULL REFERENCES jobs (id) ON DELETE CASCADE,
    mudancas  jsonb       NOT NULL,
    criado_em timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT relatorios_mudanca_job_unico UNIQUE (job_id)
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS relatorios_mudanca;
DROP TABLE IF EXISTS artefatos;
DROP TABLE IF EXISTS jobs;
DROP TABLE IF EXISTS documentos;
DROP TABLE IF EXISTS rulesets;
DROP TABLE IF EXISTS usuarios;
-- +goose StatementEnd
