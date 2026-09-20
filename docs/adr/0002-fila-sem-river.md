# ADR 0002 — Fila na própria tabela `jobs`, sem River

**Data:** 2026-09-20 · **Status:** aceita · **Substitui:** a escolha de River em `docs/plano.md`

## Contexto

O plano previa `riverqueue/river` como fila sobre Postgres. Ao chegar o momento
de implementar, duas coisas ficaram visíveis.

Primeiro, **o sistema de jobs já existia**. A migration `00001` criou a tabela
`jobs` com `status`, `tentativas`, `progresso`, `erro`, `resultado`,
`iniciado_em`, `finalizado_em`, CHECK dos estados válidos, e — o detalhe
decisivo — o índice parcial:

```sql
CREATE INDEX jobs_pendentes ON jobs (status, criado_em)
  WHERE status IN ('pendente', 'executando');
```

Isso é um índice de fila. A `00003` acrescentou `chave_idempotencia` com
unicidade por documento. O domínio `job/execucao.ServicoInterno` já faz
`Iniciar`/`Concluir`/`Falhar` com compare-and-set, coberto a 97,6%, e a porta
`ExecucaoJobRepo` já existe com `Salvar` que trata zero linhas afetadas como
conflito.

Segundo, **o River traria tabelas próprias** (`river_job`, `river_leader`, …)
com o sistema de migrations dele, enquanto as nossas são goose com testes que
tiram snapshot do schema. Resolver essa convivência estava travando a fase.

## Decisão

Não adotar River. Implementar a reivindicação de trabalho na tabela `jobs` com
o primitivo padrão do Postgres:

```sql
UPDATE jobs SET status = 'executando', tentativas = tentativas + 1, iniciado_em = now()
WHERE id = (
    SELECT id FROM jobs WHERE status = 'pendente'
    ORDER BY criado_em
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
RETURNING …
```

`FOR UPDATE SKIP LOCKED` é o que o próprio River usa por baixo: a linha
reivindicada fica travada para a transação, e trabalhadores concorrentes pulam
a linha travada em vez de bloquear.

## Consequências

**Ganhos.** Nenhuma dependência nova. Um único conceito de job, não dois — sem
a pergunta "esse trabalho está na tabela `jobs` ou na `river_job`?". Reuso do
domínio já testado e do índice já criado. Nenhuma migration nova, então os
testes de snapshot de `00002`/`00003` seguem válidos.

**Custos.** Backoff, limite de tentativas e desligamento gracioso passam a ser
nossos, e precisam de teste de integração real com concorrência — não basta
fake. Não temos painel de administração da fila nem agendamento por cron.

**Quando reconsiderar.** Se aparecer necessidade de agendamento periódico,
prioridade entre filas, workflows com dependência entre jobs ou observabilidade
pronta de fila. Nenhuma delas está no escopo até a F7; a F1 precisa de uma
coisa só — pegar o próximo `renderizar_preview` e executá-lo.

**Risco conhecido.** Polling tem latência de até um intervalo e gasta uma query
por ciclo ocioso. Para o volume da F1 é irrelevante; se incomodar, o upgrade
barato é `LISTEN/NOTIFY` do Postgres avisando o worker, mantendo o polling como
rede de segurança.
