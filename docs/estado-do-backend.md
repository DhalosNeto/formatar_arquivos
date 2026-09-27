# Estado do backend — resumo executivo

**Atualizado:** 2026-09-27 · Medições históricas de 20/09 a 27/09 preservadas abaixo

F3: seed imutável Postgres e CLI semear entregues; testes unitários com race,
integrações reais e revisão aprovados. Motor, perfis normativos e endpoints
ainda pendentes. Ponto de pausa e próxima tarefa em `docs/retomada.md`.

## Refatoração de 27/09 — auditoria do backend inteiro

Auditoria de funções, duplicação, fronteiras e documentação. Suíte verde entre
cada bloco de mudança. **Nenhuma alteração de comportamento observável pela API.**

Medição real ao fechar:

```
go build ./...                     PASS
go vet ./...                       limpo
go vet -tags=integration ./...     limpo
gofmt -l .                         limpo
go test ./... -race -count=1       PASS, 31 pacotes, 0 falhas
```

| Achado | Gravidade | Como foi fechado |
|---|---|---|
| Sessão implementada **duas vezes**, com duas constantes `"sessao_id"` | ALTO | `internal/rotas/sessao` passa a ser o único dono do cookie. Trocar uma constante e esquecer a outra derrubaria metade da API sem erro de compilação — e é identificador de credencial |
| A regra nº 1 da arquitetura sem teste | ALTO | `TestDominioNaoImportaInfra`. Estava respeitada por disciplina; basta um autoimport da IDE para quebrá-la. Conferido introduzindo uma violação de propósito |
| Reclassificação de CDM corrompido em 3 lugares, 2 variantes | MÉDIO | `cdm.ErroIndiceCorrompido` + `cdm.ErroRefXMLDuplicado`. Uma das variantes descartava a causa |
| `documentos` com 2 controladores e 2 roteadores para o mesmo recurso | MÉDIO | Um controlador, um roteador, uma linha de registro na raiz |
| Worker fechava o tracing fora de `defer` | MÉDIO | `telemetry.Encerrar`. **Bug real**: spans pendentes se perdiam sempre que o laço devolvia erro — o caso em que o trace é mais útil |
| `cmd/api` lia `os.Getenv` direto | BAIXO | `cfg.OrigensCORS`. `config` é o único lugar do projeto que lê o ambiente |
| `executar` com 114 linhas | BAIXO | `montarDependencias` extraída; caiu para 70. O ciclo de vida do processo ficou legível |
| ~80 símbolos exportados sem godoc | BAIXO | Zero restantes. Os arquivos novos não documentavam nada; os antigos explicavam cada decisão |
| `ServicoJobs` plural entre singulares | BAIXO | `ServicoJob` |

Examinado e **deliberadamente mantido**: `lerCorrecao` (parser JSON por tokens,
40 linhas) rejeita chaves duplicadas numa fronteira de confiança — não se
simplifica controle de entrada; `normalizarErro` duplicado em `job/consulta` e
`job/criacao` difere só na mensagem de contexto, e extrair criaria um pacote
compartilhado para dois chamadores.

Quatro funções seguem acima de 60 linhas, todas com motivo: `InserirOuObter`
(uma transação atômica — dividir esconderia a fronteira), os dois `executar`
(ciclo de vida sequencial) e `ConferirPacoteDocx` (guarda de zip bomb).

Medidas do código: 7800 linhas de produção, 19022 de teste (2,4:1). Nenhum
arquivo acima de 331 linhas. Zero `TODO`/`FIXME`. Quatro atalhos deliberados
marcados com `ponytail:`.

**Recomendação não aplicada:** `cmd/api` e `cmd/worker` ainda repetem sete
blocos de bootstrap (config, logger, sinal, tracing, gerenciador, storage,
conversor). Essa duplicação **já produziu um bug** — o do `defer` do tracing.
Extrair para um pacote compartilhado é o próximo passo natural, mas atravessa os
dois binários e não cabia neste recorte. Ao fazer, cuidado: o pacote não pode
arrastar `domain/documento/processamento`, senão
`TestHTTPNaoDependeDoProcessamentoInterno` reprova a API.

### Lint estrito — a rodada que mostrou o furo da auditoria

A auditoria inicial de 27/09 foi mecânica (tamanho, godoc, imports, duplicação
buscada por padrão) e **não rodou o `golangci-lint`**, que é o portão do próprio
`make lint`. Ele achou **11 problemas** que a varredura não pegava, um deles em
código escrito naquela mesma sessão.

Depois disso, os dois lados passaram a rodar conjunto **estrito**, declarado em
`backend/.golangci.yml` e `frontend/.oxlintrc.json`:

| Linter novo | O que cobre | Achados |
|---|---|---|
| `gosec` | superfície de ataque | 0 |
| `errcheck` | erro descartado (regra 2) | 0 |
| `contextcheck` | contexto não propagado (regra 3) | 0 |
| `bodyclose`, `sqlclosecheck`, `rowserrcheck` | recurso não fechado | 0 |
| `errorlint` | comparação de erro embrulhado | 6, corrigidos |
| `noctx` | requisição sem contexto | 2, corrigidos |
| `nilerr`, `unconvert`, `wastedassign`, `predeclared`, `usestdlibvars` | — | 0 |
| oxlint `correctness`+`suspicious` (front) | — | `<iframe>` sem `sandbox` |

**Os linters foram verificados, não assumidos:** plantei um `md5`, um
`os.WriteFile` com erro descartado e uma escrita em `/tmp` — `gosec` acusou os
três e voltou a zero após restaurar.

`misspell` foi deliberadamente **não** habilitado: o dicionário é inglês e
acusou 50 falsos positivos em nomes portugueses ("dependencias", "metricas").
Linter que grita sem motivo treina todo mundo a ignorar linter. Motivo de cada
regra desligada no front está em `frontend/LINT.md`.

### Frontend — auditado por inteiro em 27/09

São ~600 linhas de produção em 12 arquivos; li todas. Três correções, cada uma
com teste que falha sem ela:

| Achado | Consequência real |
|---|---|
| `cliente.ts` espalhava `...opcoes` **depois** de `credentials` e `headers` | quem passasse `credentials` desligava o `include` e a requisição virava anônima — a API responderia 404 como se o documento não existisse. Latente: nenhum chamador passava |
| `ListaDeDocumentos` não tratava `isPending` | a tela dizia **"Nenhum documento enviado ainda"** enquanto carregava, para quem tinha documentos |
| `RespostaPreview` omitia `expira_em` | a URL pré-assinada morre em 15 min e o `<iframe>` ficava em branco **sem erro**: a consulta à API tinha sucesso, quem recusava era o storage. Agora a renovação é agendada a partir do próprio `expira_em`, com um minuto de folga |

Frontend ao fechar: **38 testes** (eram 33), lint estrito e `tsc` limpos.

## Organização da documentação — 27/09

`docs/` tinha 17 arquivos, cinco deles artefatos de sessão que se referenciavam
em círculo, um órfão e um rascunho obsoleto. Agora:

- `docs/README.md` — índice de entrada, com ordem de leitura e o que **não** é
  fonte de verdade.
- `docs/retomada.md` — **um** arquivo de "onde paramos e o que vem agora", em vez
  de cinco.
- Artefatos de sessão (passagens entre IDEs, retomadas antigas, rascunhos)
  **não são versionados**: viviam se referenciando em círculo e nada ali descreve
  o estado atual. O que tinha valor durável foi migrado para este arquivo,
  `plano-backend.md` e `mapa-modulos.md`.

Este arquivo responde a duas perguntas: **em que pé está cada fase** e **o que
já existe de verdade**. Para o plano do que falta, ver `plano-backend.md`. Para
o contrato que o frontend consome, ver `contrato-api.md`.

> O frontend passou a ser responsabilidade de outra pessoa. O que existe hoje em
> `frontend/` é uma prova de fluxo, não produto — ver a seção final.

---

## Medição de 22/09 — fechamento dos endpoints F2

- `go test ./... -race -count=1`: PASS global, inclusive `infra/pdfconv`.
  O bloqueio anterior de listener era do sandbox; a execução autorizada passou.
- `go test -tags=integration ./internal/data/postgres -race -count=1`:
  PASS em 12,125s, via Podman/Postgres. Inclui CAS concorrente, igualdade jsonb,
  isolamento entre espécie de dono e HTTP PATCH/GET com banco real.
- `go build ./...`, `go vet ./...`, `gofmt -l .`: PASS, sem saída.
- Cobertura de domínio: CDM 97,6%; documento entity 98,3%, processamento 93,8%,
  service 95,3%; job consulta/criacao/entity 100%, execucao 97,6%; VO 98,4%.
- Cinco testes Python passaram; sincronização de instruções e diff sem erros.
- Auditor independente Huygens: APROVADO no delta PATCH/GET, 0 críticos,
  0 altos, 0 médios; 1 baixo documental corrigido no contrato e mapa.
  A revisão não substitui auditoria das camadas anteriores nem E2E de browser.

O contrato público está em `contrato-api.md`; a decisão já consultada ao Jev
e os limites da comparação otimista estão nas pendências abaixo.

## Quadro das fases

| Fase | Escopo (backend) | Estado |
|---|---|---|
| **F0** Fundação | esqueleto hexagonal, infra, compose, CI | ✅ concluída |
| **F1** Ingestão e preview | upload, storage, conversão síncrona, listagem | ✅ funcional fechada; sem prontidão de produção |
| **F2** Parser e CDM | `ooxml.Abrir`/`Salvar`, CDM, heurística | ✅ escopo funcional entregue; suíte global e integração Postgres verdes; delta PATCH/GET auditado |
| **F3** Motor de formatação | ruleset, mutadores OOXML, ABNT 14724 | 🟡 unidades, schema, loader e seed implementados; perfil normativo, motor e endpoints pendentes |
| **F4** Citações e referências | parser, ABNT 6023/10520, APA 7 | ⬜ não iniciada |
| **F5** LLM fallback | cliente Anthropic, limiar, teto de custo | ⬜ não iniciada |
| **F6** Revistas reais | tabelas/figuras, rulesets de periódicos | ⬜ não iniciada |
| **F7** Auth e formatos extras | JWT, rate limit, LaTeX, entrada PDF | ⬜ não iniciada |

**Três de oito fases funcionalmente fechadas.** Em linhas de código isso subestima o avanço —
o que está pronto é a parte cara de errar (modelagem, autorização, persistência).
Em valor para o usuário final superestima: **o marco de produto é a F3**, quando
alguém baixa um DOCX formatado em ABNT que abre no Word. Tudo até a F2 é
encanamento necessário e invisível.

---

## O que existe hoje

### Evidência histórica da revisão inicial de 20/09/2026

Medições fornecidas naquela revisão, antes das entregas de CDM/OOXML descritas
abaixo; não representam uma nova execução nesta edição documental:

- `go build ./...`: **PASS**. `go test ./...` e `go vet ./...` globais:
  **FAIL**, por `job/service` legado e os testes WIP de CDM/OOXML.
- Frontend: **33 testes PASS**, typecheck e lint **PASS**.
- Integrações: Postgres **PASS (6,927 s)**, storage **PASS (12,806 s)**,
  fila **PASS (6,825 s)** e migrations **PASS (10,480 s)**.
- `pdfconv` unitário: **PASS, 94,2%**. Conversão contra o sidecar real
  **não repetida** nesta revisão; existe evidência histórica separada.
- Round-trip: **PASS, 96,4%**, executando apenas `pacote.go` e
  `pacote_test.go`; **não** é resultado do pacote OOXML inteiro.

**Estado atual da validação:** CDM e análise via fila estão implementados. A
auditoria aprovou o arquivamento integral da spec legada de `job/service` em
histórico do git, sem duplicar o arquivo em `docs/`;
quatro asserções compatíveis foram migradas para `job/execucao` e passaram.
A suíte global aguarda nova medição nesta retomada;
os resultados históricos dos recortes não autorizam declará-la verde.

### Medição de 21/09 — análise ponta a ponta, contra a stack real

Executado contra Postgres, MinIO, sidecar e worker de verdade, não dublês:

```
POST /v1/documentos                      201  status=recebido
POST /v1/documentos/{id}/analisar        202  job pendente
POST /v1/documentos/{id}/analisar (2ª)   202  MESMO job id  <- idempotência
GET  /v1/documentos/{id}                      status=analisado
GET  /v1/documentos/{id}/estrutura       200  versao=1, 40 blocos
```

Papéis conferidos na resposta real: `titulo`, `secao(1)`, `resumo`,
`palavras_chave`, `secao(2)`, `legenda`, `tabela`, `referencia`. A chave
`nivel` sai apenas em `secao`.

**Regra 7 verificada na linha do banco**, não só por teste:
`jobs.resultado` = `{"blocos": 40}`. O CDM completo, que carrega
`texto_resumo`, fica no documento — onde a autorização por dono se aplica —
e não é replicado numa tabela com outra fronteira de acesso.

**O bug de classificação de erro foi testado corrompendo o CDM no banco de
verdade:**

```
GET /estrutura  ->  500 {"codigo":"erro_interno"}   (nunca 400)
ocorrências do texto do documento no log da api: 0
```

`cdm.Desserializar` devolve `ErroValidacao` e `errors.Envolver` não
reclassifica — sem `erroCDMCorrompido` em `ObterEstrutura`, uma linha
corrompida viraria "requisição inválida" culpando o cliente por um GET. É o
mesmo bug que já aconteceu em `scanDocumento`.

⚠️ **Armadilha de ambiente, custou duas rodadas:** `podman-compose up -d <svc>`
**reaproveita o container existente** e não troca a imagem. Sem `podman rm -f`
antes, você depura código que não está rodando.

### Medição de 20/09 — serialização do CDM

O CDM agora atravessa o pipeline inteiro e volta:
`ExtrairBlocos` → camada 1 → camada 2 → `NovoIndice` → `Serializar` →
`Desserializar`, com os 40 blocos do artigo real idênticos na volta.

- `go test ./internal/domain/cdm/ -race -cover`: **PASS, 96,7%**.
- Fronteira verificada: a saída de `Serializar` passa em `entity.ValidarCDM`.
- `OrigemUsuario` sobrevive ao round-trip — se a origem se perdesse na ida ao
  banco, a correção manual deixaria de ser protegida na volta.

⚠️ **Número que desmente uma frase que estava na documentação:** o CDM do
artigo real ocupa **8411 bytes contra 6376 de texto — 132%**. O teto de 200
runas é um limite superior por bloco, não compressão: blocos curtos cabem
inteiros e as chaves JSON somam por cima. A garantia real é que o CDM **não
cresce com o tamanho do bloco** e não guarda formatação. A documentação foi
corrigida; a afirmação anterior era plausível e estava errada.

### Medição de 20/09 — camada 2 (heurística estrutural)

O critério de pronto da F2 — "identifica título, resumo, palavras-chave, seções
e referências num artigo real" — está **medido**, não presumido:
`TestAplicarHeuristicaFixtureRealIdentificaEstruturaDoArtigo` roda o pipeline
inteiro (extrair → camada 1 → camada 2) sobre `artigo-real-libreoffice.docx` e
confere o papel dos **40 blocos, um por um**. PASS.

- `go test ./internal/domain/cdm/ -race -cover`: **PASS, 95,5%**.
- `go test ./internal/infra/ooxml/ -race -cover`: **PASS, 92,3%**.
- Na medição de 20/09, foi registrado `internal/domain/` verde e acima do
  mínimo de 80%, exceto `job/service` (spec legada, ver pendências), apontado
  naquela execução como a única falha global. Não é uma medição do estado atual.

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
- `go vet ./...` global naquela medição: **FAIL** por
  `internal/domain/job/service`, spec legada RED, registrada então como a
  única falha restante.
- Integrações **não repetidas** nesta edição; a regra "não existe verde sem
  integração executada" segue valendo para o que toca serviço real. A extração
  de blocos não fala com serviço externo: roda contra fixture em disco.

**Escopo daquela medição:** camada 1 (estilos nomeados do DOCX). Heurística
estrutural, persistência do CDM e rotas de análise foram entregues depois desse
recorte, como registrado acima. `PATCH .../estrutura` e `GET /v1/jobs/{id}`
foram implementados e testados em 22/09. As coberturas são por pacote medido, não uma aprovação
global do domínio; o mínimo exigido continua 80%.

### Camada de domínio — `internal/domain/`

Coberturas históricas registradas em 20/09/2026, não remedidas nesta edição.

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
| `cdm` | 96,7% | papel do bloco, origem, proteção da correção manual, heurística da camada 2 e o formato persistido |

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
  `jobs`, selecionando somente `status='pendente'`. Consome a análise
  enfileirada por `POST .../analisar`; não há retry automático de `falhou`.
  Sem River; ver `adr/0002-fila-sem-river.md`.
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

O compose constrói essa imagem **sem publicar a porta 2004 no host**. API e
worker acessam o HTTP pela rede `sem-saida` (`internal: true`); a porta 2003
de XML-RPC é interna ao sidecar. Há limites de concorrência e prazo no handler
e de memória, CPU e processos no compose. Esses controles não equivalem a
prontidão de produção; ver os limites e as medições históricas abaixo.

### HTTP — `internal/rotas/`

Echo isolado atrás de `contrato.go`; handler nunca conhece o framework. Rotas
vivas hoje: saúde, prontidão, upload, obtenção, preview, listagem,
`POST /v1/documentos/{id}/analisar` e `GET /v1/documentos/{id}/estrutura`.
Também implementados: `PATCH /v1/documentos/{id}/estrutura` e `GET /v1/jobs/{id}`.
Detalhes em `contrato-api.md`.

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
| MÉDIO | `<iframe>` do preview ganhou `sandbox="allow-same-origin"` em 27/09, mas **não foi verificado em navegador real**: jsdom não renderiza PDF. Se o visualizador nativo precisar de mais permissão, o preview quebra. Conferir no Chrome e no Firefox. |
| MÉDIO | Prazo e concorrência do conversor são limitados no handler, não em cgroup do LibreOffice: um processo filho que ignore SIGKILL do `subprocess` ainda escaparia. `mem_limit`/`pids_limit` são a rede de segurança, não prova de contenção. |
| — | Compose usa `http://minio:9000` também nas URLs assinadas: risco de host inacessível ao browser, inferido do código; não validado por E2E nesta revisão. |
| — | Suíte global medida em 22/09 com `-race`: PASS após arquivamento da spec antiga e migração das quatro asserções compatíveis. O arquivo original permanece recuperável pelo histórico do git (`internal/domain/job/service/job_test.go`); não foi duplicado em docs. |
| — | A análise já usa a fila. O upload mantém o preview síncrono; para preview assíncrono, falta gravar a chave no documento pela porta interna (o executor só a grava no resultado do job). Isso não bloqueia a análise. |
| — | Após iniciar a análise, o executor tenta `MarcarFalha` se o processamento falhar. A falha dupla agora retorna `errors.ErroPersistirFalha`, preservando ambas as causas com mensagem fixa, e emite log fixo por logger injetado. A gravação no banco pode falhar e o documento permanecer `analisando`. Se `Concluir`/`Falhar` do job não persistir, ele pode permanecer `executando`. Recuperação e reconciliação continuam pendentes; `WithoutCancel` não garante persistência. |
| — | `InserirOuObter` requer READ COMMITTED, mas `Begin` herda o padrão da conexão. |
| — | `PATCH .../estrutura` e `GET /v1/jobs/{id}` implementados, testados com Postgres e auditados em 22/09; recorte fechado. |
| — | PATCH usa `EstruturaRepo.SalvarEstrutura` com dono e CAS do status/CDM. A comparação cobre concorrência durante a requisição; não há ETag/revisão do formulário no navegador. |
| — | A fila reivindica apenas `pendente`; `falhou` não é selecionado novamente. `tentativas` é incrementado, mas não implementa retry automático. Política de reenfileiramento, backoff e teto de tentativas permanece pendente. |
| — | `ListaAutores` não é identificada por nenhuma camada determinística; depende da F5 ou de correção manual. |
| — | Não há auditoria independente de `validador`/`seguranca` registrada aqui para os recortes do CDM (camadas 1 e 2); esta revisão documental não a substitui. |
| — | O **frontend está atrás da API**: não consome `POST .../analisar`, `GET/PATCH .../estrutura` nem `GET /v1/jobs/{id}`. Esperado — os endpoints são novos — mas significa que nenhum deles tem exercício por navegador. |
| — | Integrações com Podman **não foram executadas em 27/09**: `-tags=integration` foi compilado e passou no `vet`, não rodado. A última execução real é de 22/09 (12,125s) e a do seed em 27/09 (12,369s). |
| — | `internal/infra/errors` expõe `E(err, alvo)` como equivalente de `errors.Is`. São 13 usos em produção e o nome não comunica nada; renomear para `Is` é mecânico, mas é decisão de vocabulário do projeto. |
| — | `backend/rulesets/` contém schema técnico, mas nenhum perfil normativo publicável. Bloqueia conformidade normativa; testes do backend usam perfis sintéticos identificados. |

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

**Limite do registro de 20/09:** o healthcheck não foi verificado por `make up`
naquela edição, apenas por revisão estática e parsing do YAML. A medição de
21/09 contra a stack real está registrada separadamente acima.

---

## Sobre o frontend existente

`frontend/` tem upload com arrastar, preview em `<iframe>` e listagem da sessão
— 33 testes verdes na medição de 20/09/2026, sem dependência além de React,
TanStack Query e Tailwind.

**Foi construído como prova do fluxo funcional, não como produto nem evidência
de prontidão de produção.** Há listagem da sessão; estrutura detectada,
catálogo de revistas e resultado lado a lado continuam pendentes.

Para quem assumir o front, o que importa é `contrato-api.md`. O código atual
serve de referência de como consumir a API — em especial a sessão por cookie e
o formato de erro — e pode ser substituído à vontade.
