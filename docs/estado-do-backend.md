# Estado do backend — resumo executivo

**Atualizado:** 2026-09-20 · **Commit:** `c30d18d`

Este arquivo responde a duas perguntas: **em que pé está cada fase** e **o que
já existe de verdade**. Para o plano do que falta, ver `plano-backend.md`. Para
o contrato que o frontend consome, ver `contrato-api.md`.

> O frontend passou a ser responsabilidade de outra pessoa. O que existe hoje em
> `frontend/` é uma prova de fluxo, não produto — ver a seção final.

---

## Quadro das fases

| Fase | Escopo (backend) | Estado |
|---|---|---|
| **F0** Fundação | esqueleto hexagonal, infra, compose, CI | ✅ concluída |
| **F1** Ingestão e preview | upload, storage, conversão, fila, listagem | ✅ concluída |
| **F2** Parser e CDM | `ooxml.Abrir`/`Salvar`, CDM, heurística | ⬜ não iniciada |
| **F3** Motor de formatação | ruleset, mutadores OOXML, ABNT 14724 | ⬜ não iniciada |
| **F4** Citações e referências | parser, ABNT 6023/10520, APA 7 | ⬜ não iniciada |
| **F5** LLM fallback | cliente Anthropic, limiar, teto de custo | ⬜ não iniciada |
| **F6** Revistas reais | tabelas/figuras, rulesets de periódicos | ⬜ não iniciada |
| **F7** Auth e formatos extras | JWT, rate limit, LaTeX, entrada PDF | ⬜ não iniciada |

**Duas de oito fases fechadas.** Em linhas de código isso subestima o avanço —
o que está pronto é a parte cara de errar (modelagem, autorização, persistência).
Em valor para o usuário final superestima: **o marco de produto é a F3**, quando
alguém baixa um DOCX formatado em ABNT que abre no Word. Tudo até a F2 é
encanamento necessário e invisível.

---

## O que existe hoje

### Números

```
produção     ~5.000 linhas Go
testes      ~12.300 linhas      (2,5 linhas de teste por linha de código)
             233 testes unitários
              30 testes de integração (PostgreSQL, MinIO e LibreOffice reais)
```

Cobertura do domínio, contra o mínimo de 80% do `CLAUDE.md`: **nenhum pacote
abaixo de 95%**.

### Camada de domínio — `internal/domain/`

| Pacote | Cobertura | O que resolve |
|---|---|---|
| `vo` | 100% | `Dono`, `ChaveStorage`, `FormatoArquivo` |
| `documento/entity` | 98,3% | entidade, status e transições |
| `documento/service` | 95,3% | ingestão, consulta e listagem autorizadas |
| `documento/processamento` | 96,8% | transições do worker, com compare-and-set |
| `job/entity` | 100% | job, tipo e status |
| `job/criacao` | 100% | criação idempotente |
| `job/consulta` | 100% | consulta autorizada |
| `job/execucao` | 97,6% | transições do worker |

**`vo.Dono` torna IDOR impossível por construção.** Value object comparável com
campos privados; `==` não autoriza, só `PodeAcessar(recurso)` autoriza. Toda
operação recebe o solicitante, e o filtro acontece **no WHERE do SQL** — nunca
busca-e-compara, que viraria canal lateral de tempo.

### Persistência — `internal/data/`

`contracts.GerenciadorDados` é a fachada única de acesso a dados; só
`internal/data` pode importar `pgx`, e há teste de arquitetura garantindo isso.

Quatro portas separadas por caso de uso: `DocumentoRepo`/`DocumentoInternoRepo`,
`ConsultaJobRepo`, `CriacaoJobRepo`, `ExecucaoJobRepo` e `ReivindicacaoJobRepo`.

**`InserirOuObter` é idempotente e reautoriza na mesma transação:** `FOR SHARE`
na linha do documento, `ON CONFLICT DO NOTHING RETURNING`, releitura quando não
retorna linha. ⚠️ Depende de **READ COMMITTED** — sob REPEATABLE READ a releitura
não enxerga a linha concorrente e a idempotência vira erro.

Migrations goose `00001`–`00003`, com testes de integração que tiram snapshot do
schema e verificam Up/Down/Up.

### Infraestrutura — `internal/infra/`

- **`storage`** — S3/MinIO. URL pré-assinada exclusivamente **GET**, com teto de
  validade (padrão 15 min, máximo 1 h). `Salvar` usa `manager.Uploader` porque
  corpo vindo da rede não é seekable e o SigV4 precisa rebobinar.
- **`pdfconv`** — cliente do sidecar de conversão. Contrato HTTP próprio, corpo
  cru **sem multipart**: sem multipart não existe filename no protocolo, então
  vazamento de nome de arquivo do usuário vira impossível pela forma do wire.
- **`fila`** — laço de consumo com `FOR UPDATE SKIP LOCKED` sobre a tabela
  `jobs`. Sem River; ver `adr/0002-fila-sem-river.md`.
- `config`, `errors`, `log`, `telemetry` — desde a F0.

### Sidecar de conversão

Imagem própria (`deploy/Dockerfile.libreoffice`, 551 MB): Debian 13 + LibreOffice
headless + `unoserver`, com handler HTTP nosso. Decidiu-se não usar imagem de
terceiro pouco auditada para processar documento de usuário.

### HTTP — `internal/rotas/`

Echo isolado atrás de `contrato.go`; handler nunca conhece o framework. Rotas
vivas hoje: saúde, prontidão, upload, obtenção, preview e listagem. Detalhes em
`contrato-api.md`.

---

## Decisões de arquitetura que valem conhecer

**DOCX in-place** (`adr/0001`). O motor não reconstrói o documento: abre o
`.docx`, muta só os nós XML que a norma exige e salva o mesmo pacote. Tudo que
não é tocado — imagens, equações, cabeçalhos, `_rels` — sai byte a byte igual.

**Fila sem River** (`adr/0002`). A tabela `jobs` já era a fila: o índice parcial
`jobs_pendentes` existe desde a migration `00001`. Faltava uma query com
`FOR UPDATE SKIP LOCKED` — que é o que o River usa por baixo. Evitou dependência
nova e dois conceitos paralelos de job.

**Uma porta por caso de uso.** `ReivindicacaoJobRepo` é separada de
`ExecucaoJobRepo` porque reivindicar é o caso de uso de quem **procura**
trabalho, e executar é o de quem **já tem** um job em mãos.

---

## Bugs que só a integração real pegou

Registro deliberado: os quatro passaram por teste unitário verde.

| Bug | Sintoma |
|---|---|
| `Salvar` do storage | nunca gravou um byte — `io.LimitReader` não é seekable e o SigV4 falhava |
| Classificação de erro | linha corrompida no banco virava **HTTP 400**, culpando o cliente |
| `BodyLimit` global | todo upload limitado a **1 MB**, com o teto de negócio em 25 MiB |
| Guarda de zip bomb | rejeitava compressão legítima — 512 KiB de zeros viram ~600 bytes |

**Daí a regra em vigor: não existe "verde" sem integração executada contra o
serviço real.**

---

## Pendências abertas

| Severidade | Item |
|---|---|
| MÉDIO | Sidecar de conversão tem saída para a internet — DOCX malicioso pode buscar imagem remota e exfiltrar. Exige rede `internal: true` também em `api` e `worker`. |
| BAIXO | `config.Storage` sem `GoStringer` (regra 11 do `CLAUDE.md`) |
| BAIXO | Constantes SQLSTATE mortas em `data/postgres/conexao.go:22` |
| — | `api`/`worker` sem `depends_on: libreoffice` no compose |
| — | `internal/domain/job/service` é spec legada **vermelha de propósito**, superada por `job/criacao` + `job/consulta` + `job/execucao`. Decidir entre apagar ou implementar o que sobrou. |
| — | A fila está construída e **ociosa**: nada enfileira jobs, o upload converte síncrono. Ver F2 no `plano-backend.md`. |
| — | `backend/rulesets/` vazio: nenhum valor de norma escrito. Bloqueia a F3. |

---

## Sobre o frontend existente

`frontend/` tem upload com arrastar, preview em `<iframe>` e listagem da sessão
— 33 testes verdes, sem dependência além de React, TanStack Query e Tailwind.

**Foi construído como prova de que o backend funciona ponta a ponta, não como
produto.** As quatro telas do plano original (lista, estrutura detectada,
catálogo de revistas, resultado lado a lado) não existem.

Para quem assumir o front, o que importa é `contrato-api.md`. O código atual
serve de referência de como consumir a API — em especial a sessão por cookie e
o formato de erro — e pode ser substituído à vontade.
