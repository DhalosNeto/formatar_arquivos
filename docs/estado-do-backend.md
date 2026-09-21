# Estado do backend — resumo executivo

**Atualizado:** 2026-09-20 · Revisão do código e medições de 20/09, incluindo WIP local

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
| **F1** Ingestão e preview | upload, storage, conversão síncrona, listagem | ✅ funcional fechada; sem prontidão de produção |
| **F2** Parser e CDM | `ooxml.Abrir`/`Salvar`, CDM, heurística | 🟡 extração, CDM e as duas camadas de classificação verdes; falta persistir o CDM e as rotas |
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

### Evidência da revisão de 20/09

Medições fornecidas na revisão, sem repetir integrações nesta edição documental:

- `go build ./...`: **PASS**. `go test ./...` e `go vet ./...` globais:
  **FAIL**, por `job/service` legado e os testes WIP de CDM/OOXML.
- Frontend: **33 testes PASS**, typecheck e lint **PASS**.
- Integrações: Postgres **PASS (6,927 s)**, storage **PASS (12,806 s)**,
  fila **PASS (6,825 s)** e migrations **PASS (10,480 s)**.
- `pdfconv` unitário: **PASS, 94,2%**. Conversão contra o sidecar real
  **não repetida** nesta revisão; existe evidência histórica separada.
- Round-trip: **PASS, 96,4%**, executando apenas `pacote.go` e
  `pacote_test.go`; **não** é resultado do pacote OOXML inteiro.

### Medição de 20/09 — camada 2 (heurística estrutural)

O critério de pronto da F2 — "identifica título, resumo, palavras-chave, seções
e referências num artigo real" — está **medido**, não presumido:
`TestAplicarHeuristicaFixtureRealIdentificaEstruturaDoArtigo` roda o pipeline
inteiro (extrair → camada 1 → camada 2) sobre `artigo-real-libreoffice.docx` e
confere o papel dos **40 blocos, um por um**. PASS.

- `go test ./internal/domain/cdm/ -race -cover`: **PASS, 95,5%**.
- `go test ./internal/infra/ooxml/ -race -cover`: **PASS, 92,3%**.
- Todo `internal/domain/` verde, acima do mínimo de 80%, exceto `job/service`
  (spec legada, ver pendências) — é a **única** falha da suíte global.

As cinco regras da camada 2, todas por evidência textual ou posicional:
rótulo de região (`RESUMO`/`ABSTRACT` e `REFERÊNCIAS`/`REFERENCIAS`/
`REFERENCES`), palavras-chave por prefixo, legenda (`Tabela N`/`Figura N`/
`Quadro N`/`Fonte:`), seção numerada (`2.1 ` → `Secao(2)`).

⚠️ **O que a camada 2 deliberadamente NÃO faz:** identificar `ListaAutores`.
Nenhuma evidência textual confiável separa "Maria Eduarda Nogueira Prado" de um
parágrafo comum. Os blocos 1 a 5 do fixture (título em inglês, autores,
afiliações) ficam como `Paragrafo` com confiança baixa, que é o sinal correto
para a camada de LLM (F5) ou para a correção manual do usuário. Inventar uma
regra posicional ali produziria classificação errada com cara de certa.

**Concordância não enfraquece o bloco.** A camada 2 só reclassifica quando o
papel resultante difere do que o bloco já tem. Um `Heading1` com texto
`1 INTRODUÇÃO` bate nas duas camadas; reescrevê-lo trocaria `OrigemEstiloDocx`
/0,95 por `OrigemHeuristica`/0,8 — duas evidências independentes concordando
sairiam valendo menos que uma sozinha. No fixture real isso atingiria seis
cabeçalhos. Travado por teste.

### Medição de 20/09 — CDM e extração de blocos

Os testes que estavam RED passaram a verde com a implementação:

- `go test ./internal/infra/ooxml/ -race -cover`: **PASS, 92,3%** (pacote
  inteiro, não mais o recorte de `pacote.go`).
- `go test ./internal/domain/cdm/ -race -cover`: **PASS, 92,9%**.
- `go build ./...`: **PASS**. `gofmt -l .`: limpo.
- `go vet ./...` global: continua **FAIL** por `internal/domain/job/service`,
  spec legada RED — é a única falha restante, e não tem relação com a F2.
- Integrações **não repetidas** nesta edição; a regra "não existe verde sem
  integração executada" segue valendo para o que toca serviço real. A extração
  de blocos não fala com serviço externo: roda contra fixture em disco.

**F2 não está fechada.** O que entrou foi a camada 1 (estilos nomeados do
DOCX); heurística estrutural, persistência do CDM e as rotas de análise
continuam pendentes. As coberturas são por pacote medido, não uma aprovação
global do domínio; o mínimo exigido continua 80%.

### Camada de domínio — `internal/domain/`

| Pacote | Cobertura | O que resolve |
|---|---|---|
| `vo` | 98,4% | `Dono`, `ChaveStorage`, `FormatoArquivo` |
| `documento/entity` | 98,3% | entidade, status e transições |
| `documento/service` | 95,3% | ingestão, consulta e listagem autorizadas |
| `documento/processamento` | 93,8% | transições do worker, com compare-and-set |
| `job/entity` | 100% | job, tipo e status |
| `job/criacao` | 100% | criação idempotente |
| `job/consulta` | 100% | consulta autorizada |
| `job/execucao` | 97,6% | transições do worker |
| `cdm` | 95,5% | papel do bloco, origem da classificação, proteção da correção manual e a heurística da camada 2 |

**`vo.Dono` concentra as regras de autorização por dono.** Value object
comparável com campos privados; `==` não autoriza, só `PodeAcessar(recurso)`
autoriza. As operações públicas recebem o solicitante e filtram o dono
**no WHERE do SQL**. Inexistente e terceiro usam o mesmo erro público. Esses
controles reduzem o risco de IDOR, mas não provam sua impossibilidade nem
latência constante. As portas internas do worker têm outra fronteira de acesso.

### Persistência — `internal/data/`

`contracts.GerenciadorDados` é a fachada única de acesso a dados; só
`internal/data` pode importar `pgx` diretamente, conforme teste de arquitetura.
A fronteira HTTP → processamento interno é outra checagem: usa `go list -deps`
para cobrir também dependências transitivas.

Portas separadas por caso de uso: `DocumentoRepo`/`DocumentoInternoRepo`,
`ConsultaJobRepo`, `CriacaoJobRepo`, `ExecucaoJobRepo` e `ReivindicacaoJobRepo`.

**`InserirOuObter` é idempotente e reautoriza na mesma transação:** `FOR SHARE`
na linha do documento, `ON CONFLICT DO NOTHING RETURNING`, releitura quando não
retorna linha. ⚠️ Depende de **READ COMMITTED** — sob REPEATABLE READ a releitura
não enxerga a linha concorrente e a idempotência vira erro. O código usa
`pool.Begin(ctx)`, que herda o isolamento padrão; não fixa READ COMMITTED.

Migrations goose `00001`–`00003`, com testes de integração que tiram snapshot do
schema e verificam Up/Down/Up.

### Infraestrutura — `internal/infra/`

- **`storage`** — S3/MinIO. URL pré-assinada exclusivamente **GET**, com teto de
  validade (padrão 15 min, máximo 1 h). `Salvar` usa `manager.Uploader` porque
  corpo vindo da rede não é seekable e o SigV4 precisa rebobinar.
- **`pdfconv`** — cliente do sidecar de conversão. Contrato HTTP próprio, corpo
  cru **sem multipart**, sem campo de nome original no protocolo. A regra de
  não registrar conteúdo do documento continua necessária.
- **`fila`** — laço de consumo com `FOR UPDATE SKIP LOCKED` sobre a tabela
  `jobs`. Sem River; ver `adr/0002-fila-sem-river.md`.
- **`ooxml`** — abre e salva o pacote DOCX com round-trip **byte a byte**
  (SHA256 idêntico nos fixtures testados). A **escrita** não
  desserializa: `zip.Writer.Copy` recopia cada entrada sem descomprimir.
  `ExtrairBlocos` parseia `word/document.xml` num caminho **paralelo e somente
  leitura**, reabrindo a entrada do ZIP — o round-trip continua byte a byte
  depois de extrair, e há teste travando isso. `ClassificarPorEstiloDocx` é a
  camada 1 do CDM (Title/HeadingN/Normal → papel do bloco).
- `config`, `errors`, `log`, `telemetry` — desde a F0.

### Sidecar de conversão

Imagem própria (`deploy/Dockerfile.libreoffice`, 551 MB): Debian 13 + LibreOffice
headless + `unoserver`, com handler HTTP nosso. Decidiu-se não usar imagem de
terceiro pouco auditada para processar documento de usuário.

O compose já constrói essa imagem e publica `2004:2004` para HTTP; a porta
2003 de XML-RPC é interna ao sidecar. Isso fecha a ligação funcional, não os
controles necessários à produção descritos abaixo.

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
| MÉDIO | Prazo e concorrência do conversor são limitados no handler, não em cgroup do LibreOffice: um processo filho que ignore SIGKILL do `subprocess` ainda escaparia. `mem_limit`/`pids_limit` são a rede de segurança, não prova de contenção. |
| — | Compose usa `http://minio:9000` também nas URLs assinadas: risco de host inacessível ao browser, inferido do código; não validado por E2E nesta revisão. |
| — | `internal/domain/job/service` é spec legada RED, superada por `job/criacao` + `job/consulta` + `job/execucao`; mantém a suíte global vermelha. Não implica decisão de implementar a API antiga. |
| — | Worker já executa o laço, mas o upload converte síncrono e não enfileira. Falta gravar o preview no documento pela porta interna; hoje o executor grava a chave no resultado do job. |
| — | Se a finalização (`Concluir`/`Falhar`) falha, falta recuperação do job preso em `executando`; `WithoutCancel` não resolve falha de persistência. |
| — | `InserirOuObter` requer READ COMMITTED, mas `Begin` herda o padrão da conexão. |
| — | F2 aberta: falta **serializar** o CDM para `cdm_jsonb` (`Papel` tem campos privados, não tem `MarshalJSON`; `entity.ValidarCDM` exige objeto JSON) e expor `POST .../analisar`, `GET/PATCH .../estrutura` e `GET /v1/jobs/{id}`. |
| — | `ListaAutores` não é identificada por nenhuma camada determinística; depende da F5 ou de correção manual. |
| — | Nenhuma auditoria independente de `validador`/`seguranca` rodou sobre os recortes do CDM (camadas 1 e 2). |
| — | `backend/rulesets/` vazio: nenhum valor de norma escrito. Bloqueia a F3. |

---

## Bug que só o compose pegou: api e worker não subiam

Registro à parte porque a lição é diferente das quatro da tabela acima.

`telemetry.IniciarTracing` chamava `resource.Merge` com `semconv/v1.34.0`
contra o `resource.Default()` do SDK 1.46.0, que usa `1.43.0`. **`Merge`
recusa schemas conflitantes em vez de escolher um**, então os dois processos
morriam na partida, em laço de restart:

```
falha ao iniciar o worker: telemetry.IniciarTracing: ao montar os atributos
do serviço => conflicting Schema URL: .../1.43.0 and .../1.34.0
```

Por que ninguém viu: a função **retorna na primeira linha** quando
`OTEL_EXPORTER_OTLP_ENDPOINT` está vazio, que é o caso do `go run` local e o
de toda a suíte. Só o compose define o endpoint. `internal/infra/telemetry`
não tinha nenhum arquivo de teste — o ramo que quebra nunca foi executado.

Corrigido subindo o import para `semconv/v1.43.0`
(`DeploymentEnvironmentName` virou `DeploymentEnvironmentNameKey.String`) e
com `tracing_test.go` cobrindo os dois ramos. O teste foi conferido
revertendo o import: falha com a mensagem exata acima.

**A regra "não existe verde sem integração executada" tem um segundo gume:**
rodar o binário direto não é o mesmo que rodar o compose. Configuração que só
existe no container é caminho não testado.

## Pendências fechadas em 20/09

| Item | Como foi fechado |
|---|---|
| Porta 2004 publicada sem autenticação (ALTO) | `ports:` removido do serviço `libreoffice`. O sidecar só é alcançável por `api` e `worker`, pela rede interna. Medido: `curl http://localhost:2004/saude` do host → conexão recusada. |
| Conversor sem saída bloqueada (MÉDIO) | Rede `sem-saida` com `internal: true`; `api`/`worker` ficam nas duas redes. Medido de dentro do container: `1.1.1.1:53`, `8.8.8.8:443` e HTTP externo todos bloqueados. É o que impede um DOCX com referência remota de virar SSRF ou exfiltração. |
| Conversor sem limite de concorrência e prazo (ALTO) | Semáforo de `PDFCONV_MAXIMO_SIMULTANEAS` (padrão 2) → 503 quando saturado; conversão movida para **subprocesso** `unoconvert` com `subprocess.run(timeout=)` → 504. O subprocesso foi necessário porque `UnoClient.convert` retenta 5× com 10s e Python não mata thread, só processo. Mais `mem_limit: 2g`, `cpus: 2.0`, `pids_limit: 512`. Coberto por `deploy/test_pdfconv_handler.py` (6 testes, sem container) e ligado ao CI. |
| `config` sem `GoStringer` (regra 11) | `String()`+`GoString()` em `Config`, `Postgres`, `Storage` e `LLM`, redigindo DSN, access/secret key e chave da API. Teste `redacao_test.go` exercita `%v`, `%+v`, `%s` e `%#v` separadamente — **`%#v` vazava a credencial viva mesmo com `String()` definido**, que é exatamente o que a regra 11 descreve. Endpoint, bucket e região continuam visíveis: redigir demais torna o log inútil. |
| Constantes SQLSTATE mortas | Removidas com o `//nolint:unused`. O raciocínio (colisão de UUID é corrupção, não caso de negócio) ficou como comentário. |
| `api`/`worker` sem `depends_on: libreoffice` | Declarado com `condition: service_healthy`, e o sidecar ganhou `healthcheck` batendo em `/saude` (que faz RPC de verdade ao unoserver, não teste de porta). `start_period: 60s` porque subida fria do LibreOffice é lenta. Usa `python3`, já presente na imagem, em vez de instalar `curl`. |

**Não verificado por `make up`** nesta edição: o healthcheck é revisão estática do compose, validada só por parsing do YAML.

---

## Sobre o frontend existente

`frontend/` tem upload com arrastar, preview em `<iframe>` e listagem da sessão
— 33 testes verdes, sem dependência além de React, TanStack Query e Tailwind.

**Foi construído como prova do fluxo funcional, não como produto nem evidência
de prontidão de produção.** Há listagem da sessão; estrutura detectada,
catálogo de revistas e resultado lado a lado continuam pendentes.

Para quem assumir o front, o que importa é `contrato-api.md`. O código atual
serve de referência de como consumir a API — em especial a sessão por cookie e
o formato de erro — e pode ser substituído à vontade.
