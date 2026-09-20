# Plano — Formatador Acadêmico (monorepo Go + React)

## Context

Hoje um pesquisador que quer submeter um artigo para uma revista precisa reformatar o trabalho à mão para as diretrizes daquele periódico: margens, espaçamento, fontes, hierarquia de títulos, ordem das seções, legendas de tabelas/figuras e, o pior de tudo, o estilo de citações e da lista de referências. É um trabalho manual, repetitivo, propenso a erro, e precisa ser refeito a cada rejeição/resubmissão em outra revista.

O objetivo deste projeto é um sistema web onde o usuário envia o documento, escolhe a revista/norma de destino, vê o antes e o depois lado a lado e baixa o resultado em DOCX, PDF ou LaTeX. **Toda a lógica vive no backend Go**; o front é uma casca de telas que consome a API.

Decisões já tomadas com o usuário:

| Tema | Decisão |
|---|---|
| Escopo do motor v1 | Layout/tipografia + estrutura do artigo + citações/referências + tabelas/figuras/legendas |
| Parsing | Heurística determinística, LLM (Claude) só como fallback em blocos de baixa confiança |
| Entrada | DOCX (primário) e PDF (modo degradado) |
| Saída | DOCX, PDF e LaTeX |
| Regras das revistas | YAML versionado no repo, carregado no Postgres por seed; versão imutável por template |
| Preview | PDF renderizado dos dois lados, lado a lado, via PDF.js |
| Infra | Postgres + MinIO + Docker Compose, fila assíncrona, auth de usuários, observabilidade desde o dia 1 |
| Processo | TDD |
| Normas v1 | ABNT (NBR 14724/6023/10520), APA 7, Revista Caminhos da Geografia, Revista Geousp (Qualis A1) |

Base arquitetural: `~/Documentos/Projetos/functions-system-ff` (fora deste repositório) (hexagonal Go). Reaproveitamos as boas partes e corrigimos as fraquezas — detalhes na seção "O que herdamos".

---

## Ideia central: DOCX → DOCX in-place, com o CDM como índice semântico

A formatação é uma **transformação DOCX → DOCX**: abrimos o `.docx` original, alteramos apenas os nós XML que a norma exige e salvamos o mesmo pacote de volta. Nada é reconstruído do zero.

Por que in-place e não reconstrução: um `.docx` real carrega imagens, equações OMML, cabeçalhos/rodapés, notas de rodapé, campos, comentários, objetos incorporados e relacionamentos (`word/_rels/`). Reconstruir significa perder silenciosamente o que não soubermos reescrever. Mutando in-place, **tudo que não tocamos permanece byte a byte igual** — o risco de perda de conteúdo cai a quase zero, que é exatamente a garantia que o usuário precisa ao submeter um artigo.

```
   Upload         Parse                Classify            Format (in-place)        Download
  ┌──────┐    ┌────────────┐      ┌──────────────┐      ┌──────────────────┐     ┌──────┐
  │ DOCX │───▶│ ooxml.Abrir│─────▶│  Heurística  │─────▶│ mutação do XML   │────▶│ DOCX │
  └──────┘    │ (zip+xml)  │      │  + LLM       │      │ sectPr/pPr/rPr/  │     └──┬───┘
              └─────┬──────┘      │  fallback    │      │ styles/numbering │        │
                    │             └──────┬───────┘      └──────────────────┘   LibreOffice
                    ▼                    │                        ▲                  │
                   CDM  ◀────────────────┘                        │                  ▼
        (índice semântico: Role + ponteiro                    Ruleset YAML        ┌──────┐
         para o nó XML original)                                                 │ PDF  │
                                                                                 └──────┘
```

- **`ooxml.Documento`** = o pacote ZIP aberto e desserializado (`document.xml`, `styles.xml`, `numbering.xml`, `settings.xml`, `_rels`), mantido inteiro em memória. É o que sofre mutação e é o que é salvo.
- **CDM** = camada semântica *por cima* dele, não uma cópia: `Bloco{Papel, TextoResumo, Confianca, Origem, RefXML}`, onde `RefXML` aponta para o nó `w:p`/`w:tbl` correspondente. O CDM diz *o quê* cada parágrafo é; o `ooxml.Documento` é *onde* mexer.
  `Papel` ∈ Titulo, ListaAutores, Resumo, PalavrasChave, Secao(nível), Paragrafo, Citacao, ItemLista, Tabela, Figura, Legenda, Equacao, Referencia, NotaRodape. `Origem` ∈ estilo-docx | heuristica | llm | usuario.
- **Motor de formatação** aplica o ruleset escrevendo diretamente: `w:sectPr` (margens, tamanho de página, numeração), `w:pPr` (entrelinha, recuo, alinhamento, espaço antes/depois, `w:keepNext`), `w:rPr` (fonte, tamanho, negrito), `styles.xml` (redefine os estilos nomeados), `numbering.xml` (numeração de seções), além de reordenar blocos e reescrever as entradas de referência.
- **Os dois downloads saem do mesmo artefato:** o DOCX formatado é a fonte da verdade, e o PDF é a conversão dele pelo LibreOffice. É impossível o PDF divergir do Word. LaTeX (F7) é a única saída gerada a partir do CDM, e fica explicitamente marcada como best-effort.

**Invariante não-negociável, com teste automatizado:** o texto do usuário nunca é reescrito. Um teste de integridade extrai a sequência de runes de todo `w:t` antes e depois da formatação; qualquer divergência fora das transformações declaradas (reordenação de blocos, reformatação de referências) falha o job. Um segundo teste compara a lista de partes do ZIP e o hash das partes não-alvo (mídia, embeds, rels) — elas têm que sair idênticas.

---

## Estrutura do monorepo

```
formatar-concursos/
├── backend/                        # módulo Go
│   ├── cmd/
│   │   ├── api/main.go             # servidor HTTP
│   │   ├── worker/main.go          # consumidor da fila
│   │   └── rulesetctl/main.go      # valida/semeia YAMLs de ruleset
│   ├── internal/
│   │   ├── domain/
│   │   │   ├── document/{entity,repository,service}
│   │   │   ├── job/{entity,repository,service}
│   │   │   ├── ruleset/{entity,repository,service}
│   │   │   ├── usuario/{entity,repository,service}
│   │   │   ├── cdm/                # o Modelo Canônico (entidade pura, zero deps)
│   │   │   ├── formatter/          # o motor: engine, structure, citations, tables
│   │   │   └── vo/                 # Length(cm/pt/mm), FontSize, LineSpacing, ISBN/DOI, Email
│   │   ├── application/
│   │   │   └── web/{webmodel,webservices}
│   │   ├── data/
│   │   │   ├── contracts/          # DataManager + aliases das interfaces de repo
│   │   │   └── postgres/           # implementações (pgx)
│   │   ├── infra/
│   │   │   ├── errors/  log/  config/  uuid/  telemetry/
│   │   │   ├── storage/            # S3/MinIO
│   │   │   ├── queue/              # River
│   │   │   ├── ooxml/              # Abrir/Salvar DOCX + mutadores de pPr/rPr/sectPr/styles
│   │   │   ├── pdfconv/            # cliente LibreOffice/unoserver
│   │   │   ├── pdfextract/         # entrada PDF (modo degradado)
│   │   │   ├── latex/              # writer LaTeX
│   │   │   └── llm/                # cliente Anthropic
│   │   └── routes/
│   │       ├── contract.go         # Request/Response/Handler/Router (agnóstico de framework)
│   │       ├── echoadapter.go
│   │       ├── middleware/         # auth, log, recover, requestid, ratelimit, metrics
│   │       ├── routesutil/         # HandleError + errorcode
│   │       └── root/webroutes/{documentos,jobs,rulesets,auth,health}
│   ├── rulesets/                   # ⭐ YAML versionado — uma pasta por revista
│   │   ├── _schema.json
│   │   ├── abnt-nbr-14724/v1.yaml
│   │   ├── apa-7/v1.yaml
│   │   ├── caminhos-da-geografia/v1.yaml
│   │   └── geousp/v1.yaml
│   ├── migrations/                 # goose
│   └── testdata/                   # fixtures .docx + golden .json/.xml
├── frontend/                       # Vite + React + TS
├── deploy/                         # docker-compose, Dockerfiles, dashboards, alertas
└── docs/                           # ADRs, CLAUDE.md, formato do ruleset
```

---

## Stack

**Backend** — Go 1.23+; Echo v4 atrás de `routes/contract.go`; pgx/v5 + goose (migrations) + sqlc (queries tipadas); River (fila sobre Postgres — sem Redis extra); aws-sdk-go-v2/s3 contra MinIO; `log/slog` estruturado; OpenTelemetry + Prometheus; `golang-jwt/v5` + argon2id; testify + `testcontainers-go`.

**DOCX** — pacote `internal/infra/ooxml` próprio sobre `archive/zip` + `encoding/xml`: `Abrir(r)` desserializa as partes que nos interessam e **guarda as demais como bytes crus**, `Salvar(w)` reescreve o ZIP preservando as partes intocadas. Não existe biblioteca Go madura para isso; a nossa dá controle total sobre `w:pPr`/`w:rPr`/`w:sectPr`/`styles.xml`, é 100% testável com golden files e é o diferencial do produto. Escopo restrito ao subconjunto que o motor precisa mutar.

**PDF** — geração por LibreOffice headless (`unoserver`) num container sidecar, chamado via HTTP, sempre a partir do DOCX já formatado. Entrada PDF via `pdftotext -layout` + heurística — modo explicitamente degradado (sem DOCX in-place; nesse caminho o DOCX é montado a partir do CDM), com aviso na UI.

**LLM** — `github.com/anthropics/anthropic-sdk-go`, modelo `claude-opus-5`, `thinking: {type: "adaptive"}` e **structured outputs** (`output_config.format` com JSON Schema) para devolver `[]{block_id, role, confidence}`. Entra só quando a heurística fica abaixo do limiar; envia apenas os primeiros ~200 caracteres de cada bloco candidato + features estruturais, nunca o documento inteiro. Atrás da interface `domain/formatter.StructureClassifier`, com implementação `NoopClassifier` para rodar 100% offline.

**Frontend** — Vite + React 18 + TypeScript; TanStack Query + Router; Tailwind + shadcn/ui; `react-pdf` (PDF.js) para o preview; `react-dropzone`; Vitest + Testing Library + Playwright (E2E); cliente de API gerado a partir do OpenAPI que o backend publica.

---

## O que herdamos de `functions-system-ff` — e o que mudamos

**Copiar (o que é bom lá):**
- `uni-api/routes/contract.go` + `echoadapter.go` — isolamento total do Echo; handlers recebem `(context.Context, routes.Request, routes.Response)`. É a melhor parte daquele projeto.
- `uni-api/infra/errors/errors.go` — `Wrap` com caller path + tipos `ValidationError`/`NotFoundError`/`ConflictError`/`ForbiddenError`.
- `uni-api/routes/routesutil/` — `HandleError` mapeando erro→status e o corpo `{codigo, descricao, razoes[]}`; códigos em português.
- `data/contracts` como fachada única de acesso a dados, com `DataManager`/`Repo()`.
- Layout `domain/<módulo>/{entity,repository,service}` e conversores `FromXxxEntity` / `ToEntity` nos `webmodel`.

**Mudar deliberadamente:**
| Lá | Aqui | Porquê |
|---|---|---|
| `Instance()` + `sync.Once` global | `New(deps...)` puro | Singleton global inviabiliza TDD e testes paralelos |
| Nenhum validador de DTO | `go-playground/validator` nos `webmodel` + regras de negócio no service | Menos validação manual repetida |
| Sem CI de teste/lint | CI com `go build ./... && go vet && golangci-lint && go test -race -cover` | TDD exige portão automático |
| Sem Docker/compose | Compose completo | Dev local reproduzível, e LibreOffice precisa de container |
| Métodos em português | **Manter português** (`Obter`, `Salvar`, `Listar`) | Consistência com o time; só nomes de tipos genéricos ficam em inglês |
| Trailing slash obrigatório | Sem `AddTrailingSlash` | Pegadinha desnecessária |

---

## Formato do ruleset (arquivo YAML — o contrato central)

```yaml
id: caminhos-da-geografia
version: 1
nome: "Revista Caminhos da Geografia"
idiomas: [pt-BR, en]
pagina:
  tamanho: A4
  margens: { superior: 3cm, inferior: 2cm, interna: 3cm, externa: 2cm }
  numeracao: { posicao: canto-superior-direito, inicio: 1 }
estilos:                       # um bloco por Role do CDM
  Title:      { fonte: Times New Roman, tamanho: 14pt, negrito: true, alinhamento: centro, espaco_depois: 12pt }
  Heading1:   { fonte: Times New Roman, tamanho: 12pt, negrito: true, alinhamento: esquerda, numeracao: arabica }
  Paragraph:  { fonte: Times New Roman, tamanho: 12pt, entrelinha: 1.5, recuo_primeira_linha: 1.25cm, alinhamento: justificado }
  Abstract:   { tamanho: 11pt, entrelinha: 1.0, recuo_primeira_linha: 0 }
  Quote:      { tamanho: 10pt, entrelinha: 1.0, recuo_esquerdo: 4cm }
  Caption:    { tamanho: 10pt, alinhamento: centro }
estrutura:                     # ordem e obrigatoriedade das seções
  ordem: [Title, AuthorList, Abstract, Keywords, AbstractEn, KeywordsEn, Body, References]
  obrigatorias: [Title, Abstract, Keywords, References]
  limites: { Abstract: { max_palavras: 250 }, Keywords: { min: 3, max: 5 } }
citacao:
  estilo: abnt-autor-data      # abnt-autor-data | apa-7 | vancouver
  referencias: { ordenacao: alfabetica, recuo: hanging, entrelinha: 1.0 }
tabelas_figuras:
  tabela:  { legenda: acima,  prefixo: "Tabela",  numeracao: sequencial-arabica }
  figura:  { legenda: abaixo, prefixo: "Figura",  numeracao: sequencial-arabica }
```

Regras de operação: cada arquivo é **imutável** depois de semeado; mudança de diretriz cria `v2.yaml`. Um documento já formatado guarda `ruleset_id + version`, então o resultado é sempre reproduzível. `cmd/rulesetctl` valida contra `_schema.json` no CI e roda o seed.

---

## Modelo de dados (Postgres)

```
usuarios(id, email unique, senha_hash, nome, criado_em)
documentos(id, usuario_id, nome_original, mime, tamanho, storage_key,
           storage_key_pdf, status, cdm_jsonb, criado_em)
rulesets(id, slug, versao, nome, definicao_jsonb, checksum, ativo,
         UNIQUE(slug, versao))
jobs(id, documento_id, ruleset_id, tipo, status, tentativas, erro,
     progresso, resultado_jsonb, criado_em, iniciado_em, finalizado_em)
artefatos(id, job_id, formato /*docx|pdf|tex*/, storage_key, bytes, criado_em)
relatorios_mudanca(id, job_id, mudancas_jsonb)
```

Tabelas do River convivem no mesmo banco. Nada de arquivo em disco local: tudo em MinIO/S3, o banco guarda só a chave.

---

## Fluxo de uma formatação

1. `POST /v1/documentos` (multipart) → valida MIME/tamanho, grava no MinIO, cria `documentos`, enfileira job `render_preview` → responde `202 {documento_id}`.
2. Worker `render_preview`: DOCX→PDF pelo LibreOffice, salva `storage_key_pdf`.
3. `POST /v1/documentos/{id}/analisar` → job `parse`: OOXML→CDM, heurística de estrutura, LLM nos blocos de baixa confiança. Persiste `cdm_jsonb`.
4. `GET /v1/documentos/{id}/estrutura` → front mostra a estrutura detectada; usuário pode corrigir um `Role` via `PATCH .../estrutura` (a correção manual sempre vence e vira `Source=user`).
5. `POST /v1/documentos/{id}/formatar {ruleset_id, formatos:[docx,pdf,tex]}` → job `format`: engine aplica o ruleset ao CDM, writers geram os artefatos, LibreOffice gera o PDF, grava `artefatos` + `relatorios_mudanca`.
6. `GET /v1/jobs/{id}` (polling) **e** `GET /v1/jobs/{id}/eventos` (SSE) para progresso.
7. `GET /v1/documentos/{id}/preview?lado=original|formatado` → URL pré-assinada do PDF; front renderiza os dois com PDF.js.
8. `GET /v1/artefatos/{id}/download` → URL pré-assinada.

---

## Telas do frontend

| Rota | Conteúdo |
|---|---|
| `/` | Upload (drag-and-drop) + lista dos documentos do usuário |
| `/documentos/:id` | Estrutura detectada, com destaque nos blocos de baixa confiança e edição inline do `Role` |
| `/documentos/:id/formatar` | Catálogo de revistas (busca + filtro por área), preview das regras do ruleset escolhido |
| `/documentos/:id/resultado` | **Preview lado a lado** (PDF.js, scroll sincronizado) + painel de mudanças aplicadas + botões de download DOCX/PDF/LaTeX |
| `/entrar`, `/criar-conta` | Auth |

---

## Estratégia de testes (TDD)

A ordem é sempre teste primeiro. Quatro camadas:

1. **Unit puro, table-driven** — `domain/cdm`, `domain/vo` (conversão de unidades cm/pt/mm), parser de referências, regras de ordenação. Sem I/O.
2. **Golden files** — o coração. `testdata/artigo-bagunçado.docx` → `testdata/artigo-bagunçado.cdm.golden.json` (classificação), e `(docx, ruleset)` → `golden.document.xml` + `golden.styles.xml` (mutação in-place). Flag `-update` regenera os goldens. Cada bug de formatação vira um fixture novo antes do fix. Somados a eles, os dois testes de invariante: integridade do texto e preservação byte a byte das partes não-alvo do ZIP.
3. **Integração** — repositórios contra Postgres real via `testcontainers-go`; storage contra MinIO; conversão contra o container LibreOffice. Rodam com tag `//go:build integration`.
4. **E2E** — Playwright: upload → analisar → formatar → baixar, conferindo que o DOCX baixado abre e tem as margens certas.

Portões: `golangci-lint`, `go test -race`, cobertura mínima de 80% em `domain/`, e um **teste de conformidade por ruleset** (todo YAML em `rulesets/` precisa formatar o artigo de fixture sem erro e produzir um PDF com as margens declaradas). LLM nos testes sempre via `NoopClassifier` ou fake — nenhum teste chama a API de verdade.

---

## Observabilidade (dia 1)

- `slog` JSON com `request_id`, `user_id`, `job_id` propagados via context.
- `/health` (liveness) e `/ready` (checa Postgres, MinIO, LibreOffice).
- Prometheus em `/metrics`: `http_requests_total`, `http_request_duration_seconds`, `job_duration_seconds{tipo,status}`, `job_queue_depth`, `docx_parse_errors_total`, `llm_tokens_total{tipo}`, `llm_fallback_ratio`.
- OpenTelemetry tracing ponta a ponta (HTTP → job → LibreOffice), exportado pro Jaeger no compose.
- Alertas de exemplo em `deploy/alerts/`: profundidade de fila, taxa de falha de job, p99 de conversão, custo de LLM.

---

## Docker Compose (dev)

`api`, `worker`, `web`, `postgres:16`, `minio` + `createbuckets`, `libreoffice` (unoserver), `jaeger`, `prometheus`, `grafana`. `make up` sobe tudo, `make seed` carrega os rulesets, `make test` roda a suíte inteira.

---

## Time de agentes (`.claude/agents/`)

Seis agentes especializados, criados como arquivos Markdown com frontmatter em `.claude/agents/`. Ficam versionados no repo, então valem para qualquer sessão do projeto.

| Arquivo | Papel | Ferramentas | Entrega |
|---|---|---|---|
| `orquestrador.md` | Comanda o ciclo: decide quem trabalha, em que ordem, quando para | `Agent`, `SendMessage`, `Read`, `Grep`, `Glob`, `TodoWrite`, `Bash` (leitura) | Uma tarefa fechada e aprovada, ou um bloqueio reportado |
| `investigador.md` | Estuda o código e a norma, escolhe o caminho, **escreve o prompt do codador** | Somente leitura: `Read`, `Grep`, `Glob`, `Bash`, `WebFetch`, `WebSearch` | Ficha técnica + prompt de implementação |
| `codador.md` | Só escreve código de produção, seguindo a ficha | `Read`, `Write`, `Edit`, `Bash`, `Grep`, `Glob` | Código compilando + resumo do diff |
| `testador.md` | Escreve e roda os testes de cada linha entregue | `Read`, `Write`, `Edit`, `Bash`, `Grep`, `Glob` | Testes unit/golden/integração + saída real da execução |
| `validador.md` | Audita arquitetura, lógica, clean code, modelagem de dados | Somente leitura + `ReportFindings` | Veredito APROVADO/REPROVADO + achados |
| `seguranca.md` | Audita superfície de ataque e dados sensíveis | Somente leitura + `ReportFindings` | Veredito + achados por severidade |

### Ciclo de trabalho

```
        ┌──────────────┐
        │ orquestrador │  quebra a fase em tarefas pequenas e fecháveis
        └──────┬───────┘
               │ 1. tarefa
               ▼
        ┌──────────────┐
        │ investigador │  lê o código, a norma, os precedentes
        └──────┬───────┘  ▶ ficha técnica + prompt pronto
               │ 2. prompt
               ▼
        ┌──────────────┐
        │   codador    │  implementa, e só isso
        └──────┬───────┘
               │ 3. diff
     ┌─────────┼─────────┐        (rodam em paralelo)
     ▼         ▼         ▼
 ┌────────┐┌──────────┐┌───────────┐
 │testador││validador ││ seguranca │
 └────┬───┘└────┬─────┘└─────┬─────┘
      └─────────┼────────────┘
                │ 4. vereditos
                ▼
         reprovou? ──▶ volta pro codador com os achados (máx. 3 rodadas,
                       depois escala pro usuário)
         aprovou?  ──▶ orquestrador fecha a tarefa e puxa a próxima
```

Regras duras do ciclo, escritas nos próprios agentes:
- **TDD de verdade:** na maioria das tarefas o testador escreve o teste que falha *antes* do codador implementar. O orquestrador decide a ordem e informa o codador quando o teste já existe.
- O codador **não** escreve testes (é do testador) e **não** decide arquitetura (é do investigador). Se discordar da ficha, devolve pro orquestrador em vez de improvisar.
- Validador e segurança são **somente leitura** — nunca corrigem o que encontram; reportam, e o codador corrige. Isso evita que o auditor aprove o próprio conserto.
- Nenhum agente declara sucesso sem colar a saída real do comando (`go test`, `golangci-lint`, `go build`). Teste que não rodou não conta.
- O orquestrador para e chama o usuário quando: 3 rodadas sem aprovação, achado de segurança crítico, ou necessidade de decisão de produto.

### Conteúdo de cada agente (resumo do que vai no arquivo)

**`investigador`** — mapeia os arquivos relevantes e os padrões já existentes antes de propor qualquer coisa (reuso vem antes de código novo); quando a tarefa envolve norma (ABNT/APA/revista), busca e cita a diretriz oficial. Entrega uma ficha fixa: *objetivo, arquivos a tocar, padrões existentes a reusar, contrato das funções (assinatura + erros), casos de teste obrigatórios, armadilhas, critério de pronto* — e fecha com o prompt literal para o codador.

**`codador`** — **nomes de variáveis, funções, tipos e pacotes em português** (`ObterDocumento`, `aplicarMargens`, `blocosNaoClassificados`), seguindo o padrão do `functions-system-ff`. Clean code: funções curtas com um propósito só, sem comentário que repete o código, erro sempre via `infra/errors` com `Wrap`, `context.Context` como primeiro parâmetro, zero variável global, construtor `New*` explícito (nunca `sync.Once`). Termina toda entrega com `go build ./... && go vet ./...`.

**`testador`** — cobre cada função entregue: table-driven para lógica pura, golden files para OOXML/CDM, `testcontainers-go` para repositórios, e sempre os casos de erro e de borda (documento vazio, sem referências, sem resumo, caractere fora do BMP, arquivo corrompido). Mantém a cobertura mínima de 80% em `domain/`. Nunca chama a API do Claude de verdade — usa fake. Roda `go test ./... -race -cover` e cola a saída.

**`validador`** — checklist: a dependência aponta para dentro (domain não importa infra/routes)? O acesso a dados passa só por `data/contracts`? A regra de negócio está no domain e não no handler? Nomes em português e descritivos? Função grande demais / aninhamento profundo / duplicação? Modelagem: tipo certo na coluna, índice onde precisa, nullable coerente, migration reversível? Erro tratado ou engolido? Devolve APROVADO ou REPROVADO com a lista do que consertar.

**`seguranca`** — checklist web: JWT **fora do localStorage** (cookie `httpOnly` + `Secure` + `SameSite=Strict`, com refresh rotativo); rate limit por IP e por usuário nos endpoints de upload, login e formatação; upload validado por magic bytes e não pela extensão, com teto de tamanho e de páginas; zip-bomb e XXE bloqueados no parser OOXML (entidades externas desabilitadas, limite de razão de descompressão); SSRF na conversão; path traversal nas chaves do storage; URLs pré-assinadas de curta duração e escopadas ao dono; IDOR (documento de um usuário acessível por outro); senha com argon2id; segredo/token nunca em log, nunca em mensagem de erro, nunca no repo; conteúdo do documento do usuário nunca em log; CORS restrito; headers de segurança; SQL sempre parametrizado; `.env` no gitignore. Classifica por severidade e bloqueia merge em crítico/alto.

**`orquestrador`** — mantém a lista de tarefas viva, respeita a ordem das fases F0→F7, dispara os agentes, agrega vereditos, decide retrabalho ou avanço, e reporta ao usuário em português com o estado de cada tarefa. Nunca escreve código.

---

## Fases de entrega

**F0 — Fundação.** Os seis agentes em `.claude/agents/`, monorepo, compose, esqueleto hexagonal com `/health`, migrations, `slog`+OTel+Prometheus, CI (build/vet/lint/test), `CLAUDE.md` com as regras de arquitetura. Os agentes vêm primeiro — todo o resto é construído por eles.
*Pronto quando:* `make up` sobe tudo e `make test` passa verde.

**F1 — Ingestão e preview.** Upload, MinIO, River, worker `render_preview`, LibreOffice, URLs pré-assinadas, telas de upload e de preview com PDF.js.
*Pronto quando:* subo um DOCX e vejo o PDF dele na tela.

**F2 — Parser e CDM.** `ooxml.Abrir`/`Salvar` com round-trip fiel (abrir e salvar sem mutar produz ZIP equivalente — primeiro teste a escrever), `domain/cdm`, heurística estrutural, tela de estrutura com correção manual. Fixtures e goldens.
*Pronto quando:* o sistema identifica título/resumo/palavras-chave/seções/referências num artigo real, e o round-trip não perde nada.

**F3 — Motor de formatação.** Schema e loader de ruleset, `rulesetctl`, ABNT NBR 14724, mutadores de `sectPr`/`pPr`/`rPr`/`styles.xml`, conversão do DOCX resultante para PDF, downloads nos dois formatos, preview lado a lado, os dois testes de invariante.
*Pronto quando:* baixo o **DOCX** formatado em ABNT que abre no Word com margens e tipografia corretas e com as imagens intactas, e o **PDF** correspondente. **Este é o marco de produto.**

**F4 — Citações e referências.** Parser de entradas de referência, normalização, formatadores ABNT 6023/10520 e APA 7, reescrita das citações no texto, detecção de referência citada-mas-ausente e vice-versa.

**F5 — LLM fallback e relatório.** Cliente Anthropic com structured outputs, limiar de confiança, cache por hash de bloco, teto de custo por job, `relatorios_mudanca` e o painel de mudanças na UI.

**F6 — Tabelas, figuras e revistas reais.** Renumeração e legendas, Caminhos da Geografia e Geousp, catálogo de revistas no front, testes de conformidade por ruleset.

**F7 — Auth, histórico, formatos extras.** Cadastro/login JWT, documentos por usuário, rate limit, writer LaTeX, entrada PDF em modo degradado.

---

## Riscos e mitigações

| Risco | Mitigação |
|---|---|
| Mexer em OOXML à mão é mais trabalhoso que parece | Mutação in-place só nos nós necessários (bem menor que reconstruir); teste de round-trip antes de qualquer mutação; goldens desde o primeiro commit; F3 prova a abordagem cedo |
| Perder conteúdo do usuário (imagem, equação, nota) | É o motivo da abordagem in-place; teste que compara hash das partes não-alvo do ZIP antes/depois |
| LibreOffice lento/instável sob carga | Sidecar isolado, timeout por job, retry com backoff, métrica de p99, escala horizontal do worker |
| Heurística falha em documentos muito bagunçados | Três camadas: estilos do DOCX → heurística → LLM → correção manual do usuário (que sempre vence) |
| Reformatar referências corromper dados do autor | Nunca inventar campo ausente; entrada não-parseável é preservada literal e marcada como "revisar" no relatório |
| Custo do LLM escapar | Só blocos de baixa confiança, trecho curto, cache por hash, teto por job, métrica de tokens |
| Diretriz da revista mudar | Ruleset versionado e imutável; documento guarda a versão usada |

---

## Verificação

```bash
make up                     # compose completo
make seed                   # carrega rulesets no Postgres
make test                   # unit + golden, -race, cobertura
make test-integration       # testcontainers: Postgres, MinIO, LibreOffice
make test-e2e               # Playwright
make lint
```

Smoke manual ponta a ponta, com um artigo real desformatado:

```bash
curl -F arquivo=@backend/testdata/artigo-real.docx localhost:8080/v1/documentos
curl -X POST localhost:8080/v1/documentos/$ID/analisar
curl localhost:8080/v1/documentos/$ID/estrutura          # confere os Roles detectados
curl -X POST localhost:8080/v1/documentos/$ID/formatar \
     -d '{"ruleset_id":"abnt-nbr-14724@1","formatos":["docx","pdf"]}'
curl localhost:8080/v1/jobs/$JOB                          # até status=concluido
```

Depois abrir `http://localhost:5173/documentos/$ID/resultado`, conferir o lado a lado, baixar o DOCX e validar no Word/LibreOffice que margens, fonte, entrelinha e a lista de referências batem com a norma. Checar traces no Jaeger e métricas no Grafana.

---

## Pendências para decidir na execução

- Nome do produto — a pasta hoje é `formatar-concursos`, mas o domínio é artigo/revista acadêmica. Módulo Go definido: `github.com/daniel-halos/formatador`.
