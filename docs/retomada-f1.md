# Retomada — fechar a F1

## F1 FECHADA — upload, preview, listagem, fila e worker (2026-09-20)

Checkpoint mais recente. O critério da fase — *"subo um DOCX e vejo o PDF na
tela"* — está cumprido, e agora também a listagem dos documentos da sessão.

### Provas contra a API real (curl, não teste)

```
GET  /v1/documentos          sem cookie → []                    HTTP 200
POST /v1/documentos          (×2)       → 201, 201
GET  /v1/documentos          → artigo-desformatado.docx  recebido  preview=true
                               artigo.docx               recebido  preview=true
GET  /v1/documentos          outra sessão → []      ← não vê nada alheio
GET  /v1/documentos?limite=100000  → 200            ← não estoura
GET  /v1/documentos?limite=1       → 1 item         ← paginação respeitada
GET  /v1/documentos?limite=abc     → 200            ← cai no padrão
```

Ordenação do mais recente primeiro vem de graça do índice
`documentos_por_sessao (sessao_id, criado_em DESC)`.

**Sem cookie devolve `[]` com 200, não 404.** Comportamento deliberadamente
diferente de obter/preview: quem abre o site pela primeira vez não tem sessão, e
isso é normal, não falha de autenticação. `TratarListagem` não usa
`sessaoExistente` por isso.

### Fila sem River — ADR 0002, provado

`docs/adr/0002-fila-sem-river.md` registra a mudança em relação ao plano. A fila
é a própria tabela `jobs`, reivindicada com `FOR UPDATE SKIP LOCKED`. O índice
`jobs_pendentes` já existia desde a `00001` — a infraestrutura estava lá, faltava
uma query.

Integração PostgreSQL real, 12 testes PASS, incluindo o que sustenta o ADR:

```
TestReivindicarConcorrenteNaoEntregaJobDuasVezes        PASS
TestReivindicarNaoPegaJobEmExecucao                     PASS
TestLacoContextoCanceladoTerminaJobEmVoo                PASS
TestLacoNaoLogaConteudoDoErroDeExecucao                 PASS
```

**Zero dependência nova.** Nenhuma migration nova, então os snapshots de
`00002`/`00003` seguem válidos.

### Quarta porta: `ReivindicacaoJobRepo`

Adicionar `Reivindicar` a `ExecucaoJobRepo` quebrou o pacote `job/execucao` — o
fake dos testes deixou de satisfazer a interface. A correção **não** foi
remendar o fake: o projeto já separa uma porta por caso de uso (`Consulta`,
`Criacao`, `Execucao`), e `ServicoInterno` nunca reivindica, só chama
`ObterPorIDInterno` e `Salvar`.

Criada `internal/domain/job/repository/reivindicacao.go`. Reivindicar é o caso
de uso de **quem procura trabalho**; executar é o de **quem já tem um job em
mãos**. Com a separação, o fake voltou ao verde sem ser tocado.

### ⚠️ Duas gambiarras removidas do frontend — não reintroduza

O codador reportou honestamente duas mudanças de produção feitas para satisfazer
teste. Ambas foram revertidas, e a causa real corrigida no teste:

1. **`flushSync` no `onSuccess` de `EnvioDeDocumento`.** Existia porque uma
   asserção usava `findByText` no singular, que estoura com mais de um match —
   e depois do envio o nome do arquivo aparece na confirmação **e** na lista. O
   teste media o instante de commit do React, não comportamento visível.
   Corrigido para `findAllByText`; `flushSync` (escape hatch que o próprio React
   desaconselha) saiu.
2. **`Array.isArray` em `ListaDeDocumentos`.** O helper `mockarFetchRoteado` de
   `Inicio.test.tsx` casava só por trecho de URL, **ignorando o método HTTP**,
   então `GET /v1/documentos` recebia a resposta do `POST` — objeto onde deveria
   vir array. Bug que não existe contra a API real. O mock agora distingue
   método; a guarda saiu.

Os 33 testes do front seguem verdes sem as duas, o que prova que eram
desnecessárias. **Lição: quando um teste exige mudança estranha no produto,
suspeite do teste primeiro.**

### Correção no A3 (`ConferirPacoteDocx`)

O guarda de zip bomb rejeitava qualquer entrada cujo tamanho descomprimido
superasse o do próprio ZIP — mas é exatamente isso que compressão faz. 512 KiB
de zeros viram ~600 bytes em deflate, e DOCX legítimo com imagem ou XML
repetitivo era reprovado como bomba. Trocado por teto **por entrada** contra
`TamanhoDescomprimidoMaximoBytes`, que barra o ZIP64 mentiroso do mesmo jeito e
mantém a soma longe de estourar `uint64`.

### Estado dos portões

```
go build ./...                    exit 0
gofmt -l .                        limpo
golangci-lint                     0 issues
go test ./... -race               só job/service falha (RED legado,
                                  SHA 654be0fd…3e16dc conferido)
frontend                          33 testes, typecheck e lint limpos
dependências novas                nenhuma
```

### O que sobrou da F1, e por que não bloqueia

**A fila está construída, testada e ociosa.** Nada enfileira jobs ainda — o
upload converte de forma síncrona. Ligar o upload à fila exige uma capacidade
que hoje não existe: o worker não tem porta para gravar `chave_storage_pdf` em
`documentos`. `DocumentoInternoRepo` só tem `AtualizarStatus`/`DefinirCDM`, e
`DefinirChavePreviewPDF` exige `vo.Dono` — o domínio deliberadamente não tem
"dono de sistema". O codador parou e reportou em vez de inventar a porta, que
foi a decisão certa. **Criar essa capacidade é decisão de arquitetura, não de
implementação.**

### Achados ABERTOS

- **Sem auditoria independente** nos recortes de hoje: validador e segurança não
  rodaram. As verificações foram feitas pelo principal.
- **MÉDIO:** sidecar de conversão tem saída para a internet (exfiltração via
  DOCX malicioso). Exige rede `internal: true` também em `api` e `worker`.
- **Pendência:** `api`/`worker` sem `depends_on: libreoffice` no compose.
- **BAIXO:** `config.Storage` sem `GoStringer`; constantes SQLSTATE mortas em
  `conexao.go:22`.
- **Dívida de teste:** a ordenação por `criado_em` do `Reivindicar` não tem teste
  dedicado — um `ORDER BY` removido por engano passaria despercebido.

### Próximo

**F2 — parser e CDM.** `internal/infra/ooxml` é onde mora o risco técnico real
do projeto: abrir e salvar o pacote sem perder nada. O primeiro teste a escrever
é o round-trip fiel (abrir e salvar sem mutar produz ZIP equivalente), antes de
qualquer mutação.

Antes da F3, alguém precisa preencher `backend/rulesets/` com valores da NBR
14724 a partir de fonte oficial — a skill `normas-abnt` tem a tabela de conversão
pronta e os valores normativos como placeholder de propósito.

## MARCO — o fluxo da F1 funciona ponta a ponta (2026-09-20)

Checkpoint mais recente. **Subi um DOCX e baixei o PDF dele.** É o critério de
pronto da F1 para o caminho do usuário.

### A prova, com curl contra a API real

```
POST /v1/documentos   (multipart, campo "arquivo")
{"id":"a670e6a2-…","nome_original":"artigo.docx","formato":"docx",
 "tamanho_bytes":930,"status":"recebido","tem_preview":true}        HTTP 201

GET  /v1/documentos/{id}                                            HTTP 200
GET  /v1/documentos/{id}/preview  → URL pré-assinada
curl "$URL" -o preview.pdf
00000000: 2550 4446 2d31 2e37    %PDF-1.7      15170 bytes
```

PDF gerado pelo LibreOffice a partir do DOCX enviado, via sidecar próprio.
`/v1/prontidao` responde com as **três** dependências: postgres, storage e
conversor.

### Provas de segurança (todas com curl, não teste)

| Ataque | Resultado |
|---|---|
| `.docx` com texto puro dentro | 400 — "não foi possível identificar o formato" |
| ZIP válido sem as partes do DOCX | 400 — "não é um pacote DOCX válido" |
| **zip bomb** (1 GiB descomprimido, 1 MB no disco) | 400 — "excede os limites de descompressão" |
| arquivo de 30 MB (acima do teto de negócio) | 400 |
| sessão B lendo documento da sessão A | 404, corpo **byte a byte idêntico** ao de um ID inexistente |
| sessão B pedindo preview do documento de A | 404 |

### 🐛 BUG DE PRODUÇÃO encontrado rodando, invisível para os testes

Upload de **1 MB voltava 413**, com o teto de negócio em 25 MiB. Um artigo
acadêmico com imagens seria recusado.

Causa: `internal/servidor/servidor.go` registrava
`echomiddleware.BodyLimit("1M")` **globalmente**. Middleware global roda ANTES
do middleware de rota, então o `rotas.LimitarCorpo` montado em
`documentos.Roteador` nunca era alcançado — o teto real da API era 1 MB. O
comentário no código dizia "limita requisições que não são upload", mas o
registro era global: intenção e implementação divergiam.

Corrigido com `BodyLimitWithConfig` + `Skipper` (`ehUploadDeDocumento`), que
isenta só `POST /v1/documentos`. As rotas de JSON mantêm o teto apertado de 1 MB.

Medido antes e depois:

```
antes:   900 KB→201 · 1100 KB→413 · 2000 KB→413
depois:  900 KB→201 · 2000 KB→201 · 8000 KB→201 · 20000 KB→201 · 30 MB→400
```

**Nenhum teste unitário pegaria isso** — o middleware global vive na montagem do
servidor, fora do alcance dos testes de rota.

### 🔒 Vazamento sutil corrigido

`sessao.go` devolvia `NovoErroNaoEncontrado("sessão")` quando o cookie faltava,
produzindo `"sessão não encontrado"`. Dois problemas: entregava ao atacante que
a falha foi de autenticação e não de existência, e concordava errado em
português. Agora declara `"documento"`, tornando a resposta indistinguível da de
um documento inexistente — que era a intenção original do desenho.

### ⚠️ Lição: uma "otimização" minha foi barrada pelos testes

`webservices` produzia 6 eventos onde o teste exigia 5 — havia um
`repo.ObterPorID` antes de `repo.DefinirChavePreviewPDF`. Parecia ida-e-volta
redundante, já que o UPDATE filtra o dono no WHERE e devolve NaoEncontrado com
zero linhas. **Removi o `Obter` de `documentoservice.RegistrarPreviewPDF` e
quatro testes do domínio quebraram na hora:**

```
TestAcessoCruzadoNaoRevelaDocumento    "preview de terceiro chegou à escrita"
TestIDVazioRecusadoAntesDeIO           campo "id" ausente
TestDonoVazioRecusadoAntesDeIO         esperava ErroValidacao
TestRepositorioRetornaDocumentoSemDono
```

Aquela leitura **não é redundância, é guarda**: carrega a validação de entrada
(dono vazio e ID vazio viram `ErroValidacao` antes de qualquer I/O) e garante
que a escrita nunca é sequer tentada para documento de terceiro. Revertido; quem
estava errado era a asserção do teste de `webservices`, agora relaxada com um
comentário nomeando os quatro testes que quebram se alguém repetir a ideia.

### Entregue nesta rodada

- `vo.ConferirPacoteDocx` endurecido: assinatura passou de `[]string` para
  `[]byte`, com teto de entradas (512), de tamanho descomprimido total (250 MiB)
  e razão de descompressão (200× acima do piso de 1 MiB).
  **Corrigi um falso positivo grave**: o guarda original rejeitava qualquer
  entrada cujo tamanho descomprimido superasse o do ZIP — mas é exatamente isso
  que compressão faz. 512 KiB de zeros viram ~600 bytes em deflate, e o pacote
  legítimo era reprovado como zip bomb.
- `rotas.LimitarCorpo`, `Requisicao.Cookie`, `Resposta.DefinirCookie`
- `webmodel.DocumentoResposta` / `PreviewResposta`
- `webservices.ServicoDocumento` com as portas `ArmazenadorObjetos` e
  `ConversorPDF`; ingestão síncrona (original → registro → PDF → preview)
- `webrotas/documentos`: `Controlador`, sessão anônima por cookie
  `httpOnly`+`Secure`+`SameSite=Strict`, e `rotas.go` registrando as três rotas
- fiação completa em `cmd/api`, incluindo o `pdfconv.Verificador` no `/prontidao`

### Provas

```
go build ./...                    exit 0
gofmt -l .                        limpo
go test ./... -race -count=1      só job/service falha (RED legado)
golangci-lint run ./...           1 issue, e é o typecheck do job/service legado
webservices                       89,8% de cobertura
```

### Ainda ABERTO

- **Sem auditoria independente** nestes recortes: validador e segurança não
  rodaram. Tudo acima foi verificado por mim.
- **MÉDIO:** sidecar tem saída para a internet (exfiltração via DOCX malicioso).
  Exige rede `internal: true` também em `api` e `worker`.
- **Pendência:** `api`/`worker` sem `depends_on: libreoffice`.
- **BAIXO:** `config.Storage` sem `GoStringer`; constantes SQLSTATE mortas em
  `conexao.go:22`.
- Conversão é **síncrona** por decisão: `MOD: fila-worker` segue vazio e a
  decisão River × goose continua sem resposta.

### Próximo

Frontend (upload + preview PDF.js) fecha a F1 inteira — hoje o front tem só a
tela de status da API. Depois, `MOD: fila-worker` ou partir para a F2.

## MARCO — a API sobe e o /prontidao responde de verdade (2026-09-19)

Checkpoint mais recente. Dois recortes fechados: **`MOD: fiacao-bootstrap`** e
**`MOD: conversao-pdf`**. Pela primeira vez no projeto há algo executável.

### A prova que faltava desde o começo

```
GET /v1/prontidao  — tudo de pé
{"pronto":true,"dependencias":[
  {"nome":"postgres","estado":"disponivel"},
  {"nome":"storage","estado":"disponivel"}]}                      HTTP 200

GET /v1/prontidao  — MinIO derrubado
{"codigo":"dependencia_indisponivel",
 "descricao":"alguma dependência está indisponível",
 "razoes":["storage: dependência indisponível"]}                  HTTP 503
```

Rodado à mão contra Postgres 16 e MinIO descartáveis, com o binário real
(`go run ./cmd/api`). **Não é teste: é o endpoint respondendo.**

### ⚠️ As rotas vivem sob `/v1`

`root.PrefixoAPI = "/v1"`. `GET /prontidao` devolve **404**; o caminho correto é
`/v1/prontidao`, e o de liveness é `/v1/saude` (não `/health`). Healthcheck de
compose/orquestrador configurado sem o prefixo falha em silêncio.

### 🔒 Achado MÉDIO FECHADO

`internal/rotas/root/webrotas/saude/controlador.go:94` agora devolve
`Erro: motivoIndisponivel` — mensagem pública **fixa** — e manda a causa real só
para o `slog`. Verificado por `grep` no corpo da resposta caçando `minioadmin`,
`127.0.0.1`, `dial`, `refused` e a porta: **nada vazou**. O log recebeu o erro
completo (`operation error S3: HeadBucket…`), que é onde ele deve estar.

Era o achado que dormia esperando alguém ligar um Verificador. Foi corrigido no
mesmo recorte que ligou — exatamente como exigido.

### `MOD: fiacao-bootstrap` — o que passou a existir

- `cmd/api/main.go` monta `postgres.NovoGerenciador`, `storage.NovoClienteS3`,
  registra os dois Verificadores no controlador de saúde e fecha no shutdown
- `cmd/worker/main.go` monta o gerenciador (consumo de fila continua fora)
- `postgres.Gerenciador` já implementava `Nome`/`Verificar`; foi reusado, sem
  Verificador novo

### `MOD: conversao-pdf` — sidecar destravado e provado

O bloqueador `ModuleNotFoundError: No module named 'uno'` **está resolvido**.
Verificado por dentro da imagem:

```
podman run --rm --entrypoint python3 <imagem> -c "import sys,uno; ..."
python 3.13.5
uno OK /usr/lib/python3/dist-packages/uno.py
PRETTY_NAME="Debian GNU/Linux 13 (trixie)"
```

Causa real: o pip instalava o `unoserver` num interpretador diferente do que tem
o binding UNO do apt. Conserto: `debian:trixie-slim` + `--break-system-packages`
mirando o `python3` **do sistema**. Imagem caiu de 679 MB para **551 MB**.

Arquivos: `deploy/Dockerfile.libreoffice`, `deploy/pdfconv-handler.py` (note: o
handler ficou na raiz de `deploy/`, não em `deploy/conversor/servidor.py` como a
ficha propunha) e `backend/internal/infra/pdfconv/conversor.go`.

### Provas reais

```
go build ./...                                          exit 0
gofmt -l .                                              limpo
golangci-lint ./internal/infra/pdfconv/... ./cmd/... ./internal/rotas/...   0 issues
go test ./internal/infra/pdfconv/... -race -cover        ok  94,2%

go test -tags=integration ./internal/infra/pdfconv/... -race -count=1
ok  247.478s
  TestConverterParaPDFContraSidecarReal                  PASS (77,1s)
  TestVerificadorContraSidecarReal                       PASS (67,0s)
  TestConverterParaPDFTempoLimiteCurtoAbortaContraSidecarReal  PASS (91,3s)
```

**Conversão DOCX→PDF real acontecendo**, contra o sidecar que nós mesmos
empacotamos. Nenhuma dependência nova: `go.mod` continua sem River e o `pdfconv`
ficou em **stdlib pura**.

### ⚠️ A primeira execução fria REPROVA — não é bug, é starvation

A execução inicial falhou (547s) e as **três seguintes passaram** (247s, e mais
duas limpas com `-v`). Causa: na primeira vez o `apt-get install
libreoffice-writer` roda dentro do `FromDockerfile` e compete por CPU com os
casos de timing de 5s (`AplicaTempoLimiteProprio`,
`HonraCancelamentoDoChamador`), que então estouram.

**Antes de rodar a integração numa máquina fria, builde a imagem primeiro:**

```sh
podman build -f deploy/Dockerfile.libreoffice -t formatador-conversor-teste:local .
```

Não "conserte" isso afrouxando os timeouts dos testes — eles estão certos.

### Estado dos testes

`go test ./...` falha em **um** pacote só: `internal/domain/job/service` (RED
legado, SHA `654be0fd…3e16dc` conferido íntegro). Todo o resto verde.

### Ainda ABERTO

- **Auditoria não feita nestes dois recortes.** Os agentes travaram antes de
  despachar validador e segurança; as verificações acima foram feitas por mim,
  não por auditoria independente. Vale rodar `validador` e `seguranca` sobre
  `cmd/`, `internal/rotas/` e `internal/infra/pdfconv/` antes de considerar
  fechado de verdade.
- **MÉDIO:** sidecar tem saída para a internet (exfiltração via DOCX malicioso).
  Exige rede `internal: true` atribuída também a `api` e `worker`.
- **Pendência:** `api`/`worker` sem `depends_on: libreoffice`.
- **BAIXO:** `config.Storage` sem `GoStringer`; constantes SQLSTATE mortas em
  `conexao.go:22`.

### Vício recorrente dos agentes — quarta ocorrência

O `orquestrador` encerrou o turno dizendo "aguardando o retorno do subagente" em
**quatro** ciclos seguidos, mesmo com a instrução explícita já escrita no
`.claude/agents/orquestrador.md`. A regra no `.md` não está segurando o
comportamento. Tratar como problema do agente: ou reforçar de outra forma, ou
assumir que o principal precisa acompanhar e retomar.

### Próximo recorte

`MOD: fila-worker` (River — **decisão River × goose segue sem resposta**) ou
`internal/application/web` + `POST /v1/documentos`. Lembrar que A2/A3 no upload
não são adiáveis para a F7.

## PAUSA — 2026-09-19. Estado exato do disco ao parar

Checkpoint mais recente; prevalece sobre tudo abaixo. Dois recortes estavam em
voo e foram **interrompidos de propósito** pelo usuário. Leia esta seção inteira
antes de retomar: parte do que existe é TDD vermelho deliberado.

### Verificação feita À MÃO no momento da pausa (não é relato de agente)

```
go build ./...                                    exit 0
gofmt -l .                                        limpo
go test ./... -count=1                            19 pacotes ok, 2 FAIL (ver abaixo)

DOCKER_HOST=unix:///run/user/1000/podman/podman.sock \
MIGRACOES_REDE_CONTAINER=slirp4netns TESTCONTAINERS_RYUK_DISABLED=true \
go test -tags=integration ./internal/data/... -race -count=1 -cover
ok  internal/data/postgres  5.001s  coverage: 78,4%   (18 testes)

go test ./internal/domain/job/execucao/... -race -cover
ok  coverage: 97,6%
```

### Os DOIS vermelhos são esperados. Não "conserte" nenhum dos dois apagando teste

1. `internal/domain/job/service` — spec legada de outra sessão, superada por
   `job/criacao`+`job/consulta`+`job/execucao`. SHA256
   `654be0fdc7b901227384faa835cded672e4df832630b4c5ab33417bbfa3e16dc`
   conferido íntegro na pausa. Decidir um dia: apagar (as operações foram
   absorvidas) ou implementar o que sobrou. **Não criar `JobRepo` só para compilar.**
2. `internal/infra/pdfconv` — `conversor_test.go` e `conversor_integration_test.go`
   existem e estão vermelhos (`undefined: Cliente, NovoCliente,
   TamanhoMaximoPDFBytes`). É RED de TDD: a implementação foi interrompida.

### Fechado nesta rodada, com prova real

- **`internal/domain/job/execucao`** (novo, 97,6%): `ServicoInterno` com
  `Iniciar`/`Concluir`/`Falhar`. Porta `repository.ExecucaoJobRepo` — **sem dono**,
  porque é worker, e com compare-and-set: `Salvar(ctx, job, statusAtual)` grava só
  se o status no banco ainda for `statusAtual`; zero linhas afetadas é conflito,
  nunca sucesso silencioso. É o que impede dois workers de avançarem o mesmo job.
- **`internal/data/postgres`** ganhou `ObterPorIDInterno` e `Salvar` no
  `RepositorioJob`, cobertos pelos 18 testes de integração (era 13; subiu para
  78,4%).
- Veredictos do ciclo: testador GREEN, validador APROVADO (1 médio, 1 baixo, não
  bloqueantes), segurança APROVADO (0 crítico/alto/médio, 1 baixo). **Os dois
  achados do validador NÃO foram aplicados** — o agente ia despachar o ajuste
  quando foi parado. Recuperá-los exige reabrir a auditoria do recorte.

### Recorte `infra/pdfconv` — interrompido com decisões JÁ TOMADAS

Não refaça a investigação. Tudo abaixo está decidido e aprovado:

**Achado que motivou tudo:** `deploy/docker-compose.yml` apontava para
`ghcr.io/unoconv/unoserver-docker:latest`, que **expõe só a porta 2003 (XML-RPC)**,
enquanto o compose mapeia `2004:2004` e `CONVERSOR_URL` aponta para `:2004`.
Confirmado à mão: `podman image inspect` → `map[2003/tcp:{}]`. **O `make up` nunca
converteria nada**, e o sintoma seria "conexão recusada" no cliente Go, com todo
mundo procurando bug no lugar errado. **Isso continua NÃO corrigido no compose.**

**Decisão do usuário: sidecar PRÓPRIO** (opção C), recusando imagem de terceiro
com 1 estrela no Docker Hub processando documento do usuário.

**Contrato HTTP definido por nós, fonte da verdade em três cópias literais**
(topo do `Dockerfile.libreoffice`, docstring de `servidor.py`, godoc de `conversor.go`):

```
POST /converter
  Content-Type: application/vnd.openxmlformats-officedocument.wordprocessingml.document
  Content-Length obrigatório; corpo = bytes crus do .docx (SEM multipart)
  200 application/pdf | 400 "entrada invalida" | 413 "entrada grande demais"
  500 "falha na conversao"
GET /saude
  200 "ok" | 503 "indisponivel"
outra rota/método -> 404 "rota desconhecida"
```

**Por que corpo cru e não multipart** (decisão aprovada, não reabrir): sem
multipart **não existe filename no protocolo**, então a regra 7 deixa de ser algo
que alguém pode esquecer e passa a ser impossível pela forma do wire. Além disso
`cgi.FieldStorage` foi removido no Python 3.13 e o Debian 13 não tem substituto
na stdlib — multipart exigiria escrever parser de boundary à mão.

**Três achados de ler o fonte do unoserver 3.7 — honrar na implementação:**

1. `server.py` faz `with XMLRPCServer(...)` **antes** do laço que espera o
   LibreOffice subir. O socket 2003 abre cedo demais, então **health check por
   "porta aberta" mente**. O `/saude` precisa de RPC real (`proxy.info()`) com
   transporte de timeout curto.
2. `UnoClient._connect` tenta **5× com `sleep=10`** (até ~50s pendurado). Nunca
   usar `UnoClient` num caminho que precisa falhar rápido, como o `/saude`.
3. Watchdog precisa de `os._exit(1)`, **não** `sys.exit` — dentro de thread,
   `sys.exit` só mata a thread, e o sidecar serviria 503 para sempre sem
   reiniciar (o compose só reinicia quando o PID 1 sai).

**Arquivos que FALTAM criar:** `deploy/Dockerfile.libreoffice`,
`deploy/conversor/servidor.py`, `backend/internal/infra/pdfconv/conversor.go`, e
o diff do bloco `libreoffice:` do compose. A ficha completa do investigador (base
Debian por digest, `libreoffice-writer python3-uno python3-pip fonts-liberation2`
com `--no-install-recommends`, `unoserver==3.7`, contrato Go, 23 casos de teste)
está no transcript da sessão; os testes já escritos em `conversor_test.go` são a
especificação executável dela.

### BLOQUEADOR REAL do sidecar, descoberto na verificação manual

O codador chegou a buildar a imagem (`localhost/pdfconv-manual:latest`, **679 MB**)
e rodá-la antes de ser interrompido. **Ela sobe e morre.** Log real do container:

```
File "/usr/local/lib/python3.12/site-packages/unoserver/converter.py", line 2
  import uno
ModuleNotFoundError: No module named 'uno'
ImportError: Could not find the 'uno' library. This package must be installed
with a Python installation that has a 'uno' library. This typically means you
should install it with the same Python executable as your Libreoffice
installation uses.
2026-09-19 06:45:42 CRITICAL unoserver morreu (codigo 1); encerrando o processo
```

É **exatamente** a armadilha que o investigador tinha previsto: o `unoserver`
precisa ir para o mesmo Python que tem os bindings UNO. O pip o instalou em
`/usr/local/lib/python3.12/site-packages`, e o `uno` do pacote `python3-uno` vive
em `/usr/lib/python3/dist-packages`.

Dois sinais a investigar na retomada:

- **`python3.12` denuncia que a base NÃO é Debian 13 trixie** (que traz 3.13). O
  digest fixado no Dockerfile resolveu para outra coisa — reconferir o digest, ou
  fixar por tag e resolver o digest de novo.
- Confirmar se `python3-uno` realmente entrou no build e para qual `sys.path`; o
  conserto provável é alinhar o interpretador (instalar com o `python3` do sistema
  ou acrescentar o `dist-packages` ao path), **não** trocar de abordagem.

**Lado bom, e é prova de desenho certo:** a última linha mostra o **watchdog
funcionando** — ele detectou a morte do unoserver e derrubou o processo inteiro,
em vez de deixar o sidecar servindo 503 para sempre. Esse pedaço está validado.

O container parado `pdfconv-manual` e a imagem de 679 MB **foram deixados no
disco de propósito**, como evidência e para poupar rebuild. Remover com
`podman rm pdfconv-manual` quando não forem mais úteis.

### Recorte `infra/fila` + `cmd/worker` — mal começou

`internal/infra/fila` está **vazia** e **River NÃO entrou no `go.mod`** (conferido:
0 ocorrências). O que saiu foi só a camada de domínio (`job/execucao`) e o
adaptador. **A decisão que eu reservei continua sem resposta:** River cria as
próprias tabelas com migrations dele, e as nossas são goose com testes que tiram
snapshot do schema — resolver antes de qualquer `go get`, porque afeta
`make migrar` e pode quebrar os testes de snapshot de 00002/00003.

Verificar também: `data/postgres.InserirOuObter` depende de **READ COMMITTED**
para a releitura enxergar a linha concorrente. Se o River configurar pool ou
transação com outro isolamento, a idempotência quebra **em silêncio**.

### Achados de segurança ABERTOS

- **MÉDIO, gatilho datado:** `internal/rotas/root/webrotas/saude/controlador.go:89`
  devolve `err.Error()` cru no corpo de `/prontidao` (rota pública, sem auth).
  Hoje dorme porque nenhum verificador está ligado. **Acorda no exato recorte que
  ligar Postgres/storage/conversor ao endpoint — corrigir no mesmo recorte.**
- **MÉDIO, do pdfconv:** o sidecar de conversão **tem saída para a internet**. Um
  DOCX malicioso pode fazer o LibreOffice buscar imagem remota / seguir link OLE e
  exfiltrar. Fechar exige rede `internal: true` atribuída **também** a `api` e
  `worker` (senão perdem acesso ao sidecar). Adiado só porque os dois recortes
  paralelos disputavam o compose. **Merece tarefa própria com prioridade.**
- **BAIXO:** `config.Storage` sem `GoStringer` (regra 11).
- **BAIXO:** `conexao.go:22` tem duas constantes de SQLSTATE não usadas com
  `//nolint:unused` "reservadas para o futuro" — scaffolding especulativo, apagar.
- **Pendência de compose:** `api`/`worker` não têm `depends_on: libreoffice`; sem
  isso o primeiro job após um `make up` frio tende a falhar.

### F1: ~45%. O que falta, na ordem recomendada

Critério de pronto: *"subo um DOCX e vejo o PDF dele na tela"*. Hoje o binário
sobe e responde `/health`, e **só**.

Pastas ainda VAZIAS: `internal/application/web`, `internal/infra/fila`,
`internal/infra/ooxml` (esta é F2, não bloqueia), e as rotas
`webrotas/{documentos,jobs,auth}`.

1. **FIAÇÃO do `cmd/api` e `cmd/worker` — faça ANTES de qualquer coisa nova.**
   `cmd/api/main.go:67` ainda tem o comentário "Os verificadores de Postgres,
   storage e conversor entram na F1" e a única rota registrada é a de saúde.
   Storage, banco e domínio estão prontos, verdes e **nunca foram ligados entre
   si**. É o recorte mais barato e de maior retorno do projeto: zero lógica nova,
   e é o que faz `make up` subir de verdade. Lição acumulada em três recortes
   seguidos: **teste de unidade verde não prova sistema montado.**
2. `infra/pdfconv` (terminar) e `infra/fila`+worker.
3. `application/web` + `POST /v1/documentos` (grava no MinIO, enfileira preview).
4. **A2/A3 no upload — NÃO adiável para a F7.** Tamanho medido do conteúdo com
   `http.MaxBytesReader` (nunca o declarado pelo cliente), verificação do pacote
   DOCX por conteúdo real, limites de razão de descompressão, contagem de entradas
   e tamanho total descomprimido. É o único ponto onde arquivo hostil entra.
5. `GET /v1/documentos/{id}/preview` com URL pré-assinada (o storage já gera).
6. Frontend: upload e visualizador PDF.js. Hoje só existe a tela de status da API.

### RISCO NÃO-TÉCNICO: nada foi commitado

`git log` continua dizendo que o branch `main` **não tem nenhum commit**. A árvore
inteira está staged/untracked desde a F0, incluindo tudo desta sessão. Um `rm`
distraído apaga semanas de trabalho. **Fazer commit inicial é a primeira coisa a
considerar na retomada.**

### Ambiente (conferido nesta sessão)

- Podman rootless. Socket foi habilitado com
  `systemctl --user enable --now podman.socket`; usar
  `DOCKER_HOST=unix:///run/user/1000/podman/podman.sock`.
- Variáveis que destravam Testcontainers aqui:
  `MIGRACOES_REDE_CONTAINER=slirp4netns TESTCONTAINERS_RYUK_DISABLED=true`.
  Ryuk desligado é seguro: os testes registram cleanup explícito e `podman ps -a`
  ficou sem resíduo depois das execuções.
- `golangci-lint` **não está no PATH**. Binário em
  `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest` (forma usada pelo `Makefile`; não dependa do binário em cache).
  Rodar também com `--build-tags=integration`.
- MinIO vem do **quay.io** (Docker Hub nega pull anônimo de `minio/minio`), fixado
  por digest no teste. **Nunca** `podman system prune` global: há imagens do
  projeto (`formatador_api`, `formatador_web`, `formatador_worker`).

### Vício dos agentes observado nesta sessão — instrua contra ele

O `orquestrador` encerrou o turno três vezes dizendo "aguardando o retorno do
investigador/testador". **Isso encerra o agente, não o coloca em espera**, e o
ciclo morre parado. Ao disparar um orquestrador, instrua explicitamente:
continuar no mesmo turno até ter o resultado do subagente e agir sobre ele; só
terminar quando o recorte fechar ou houver bloqueio real de decisão.

## data/contracts + data/postgres fechados verdes, integração EXECUTADA — 2026-09-18

Checkpoint atual prevalece sobre os históricos abaixo. Recorte: os adaptadores
PostgreSQL das quatro portas que o domínio já declarava. Entregues
`internal/data/contracts/contratos.go` (`GerenciadorDados`, a fachada única) e
`internal/data/postgres/{conexao.go,documento.go,job.go}`.

### Provas reais (comando e saída, não "passou")

```
go build ./...                                          exit 0
gofmt -l .                                              limpo
golangci-lint run ./internal/data/...                   0 issues
golangci-lint run --build-tags=integration ./internal/data/...  0 issues
go test ./internal/arquitetura/... -race -count=1        ok  1.327s

DOCKER_HOST=unix:///run/user/1000/podman/podman.sock \
MIGRACOES_REDE_CONTAINER=slirp4netns TESTCONTAINERS_RYUK_DISABLED=true \
go test -tags=integration ./internal/data/... -race -count=1
ok  github.com/daniel-halos/formatador/internal/data/postgres  8.749s
                                             (com -cover: 76,5%)
```

**Sem a tag `integration` a cobertura de `data/postgres` é 0,0%** — não existe
teste unitário ali, por opção: adaptador SQL só prova qualquer coisa contra
banco real. Quem rodar `go test ./...` e vir 0% não está diante de uma lacuna
esquecida; está diante de um pacote cuja prova mora atrás da build tag.

`go vet ./...` e `go test ./...` globais continuam falhando **só** em
`internal/domain/job/service` (RED legado, intocado, SHA256
`654be0fdc7b901227384faa835cded672e4df832630b4c5ab33417bbfa3e16dc` conferido).

### Retrabalho: o testador achou um bug de classificação de erro

Depois do primeiro verde, o `testador` acrescentou dois casos e um deles
reprovou de verdade:

```
--- FAIL: TestObterPorIDInternoCorrupcaoDeMimeDevolveErroAplicacao
    Should be in error chain: expected *errors.ErroAplicacao, in chain:
    "postgres.scanDocumento: converter mime do documento lido do banco =>
     requisição inválida (mime: tipo de conteúdo não suportado)"
     (*errors.ErroEnvolvido) -> (*errors.ErroValidacao)
```

`scanDocumento` envolvia com `errors.Envolver` o `*ErroValidacao` que os VOs
devolvem. Consequência prática: uma linha **corrompida no banco** virava
**HTTP 400 "requisição inválida"**, culpando quem só fez um GET e escondendo
corrupção do servidor atrás de erro de cliente.

Corrigido com o helper `erroLinhaCorrompida` (`documento.go`): a mensagem do VO
entra no texto — ela descreve o formato esperado, não o valor lido — mas o
`*ErroValidacao` **não** entra no encadeamento, porque é justamente o tipo que
precisa sumir dali. Aplicado aos cinco pontos de conversão de `scanDocumento`.

Os dois pontos equivalentes de `scanJob` (tipo e status desconhecidos) usavam
`errors.Novo`, que não causava o 400, mas ficava inconsistente — foram
uniformizados para `NovoErroAplicacao` no mesmo passe. É a mesma classe de
defeito em sítios irmãos; corrigir só o que o teste apontou deixaria o resto.

Também corrigido `contextcheck` no teste de integração: `inserirDocumento` e
`inserirJobFixtureSQL` usavam `context.Background()` sem motivo. Passaram a
receber `ctx`. **Não confundir com o caso das migrations**, onde o
`context.Background()` no cleanup é deliberado (não herdar cancelamento) e segue
com `//nolint:contextcheck` justificado.

Lacuna registrada pelo testador: o sub-caso "dono ambíguo" da corrupção de linha
**não é reproduzível**, porque o CHECK `documentos_dono_exclusivo` da migration
00002 impede fisicamente `usuario_id` e `sessao_id` não-nulos na mesma linha. O
caso do mime passa pelo mesmo ponto de classificação e é suficiente.

### Treze testes de integração, cobrindo os cinco requisitos exigidos

`TestObterPorIDTerceiroIgualAInexistente`, `TestInserirOuObterConcorrente`,
`TestInserirOuObterMesmaChaveDocumentosDiferentes`, `TestAtualizarStatusCAS`,
`TestDefinirCDMCAS`, `TestDocumentoRepoDonoUsuarioESessao`,
`TestObterPorIDInternoQualquerDono`, `TestListarPorDocumento`,
`TestInserirOuObterDocumentoDeTerceiro`,
`TestInserirOuObterAposTerminalNaoReinicia`,
`TestInserirOuObterPayloadDivergenteDevolveJobExistente` (documenta que o
repositório **não** compara payload: mesma chave devolve o job existente ainda
que o payload difira — a comparação é responsabilidade de `job/criacao`) e
`TestObterPorIDInternoCorrupcaoDeMimeDevolveErroAplicacao`.

### Decisões de implementação que não são óbvias — não desfaça sem ler

**IDOR resolvido no SQL.** `ObterPorID` filtra o dono na cláusula `WHERE`, nunca
busca-e-compara. Terceiro recebe o mesmo erro de inexistente, sem diferença de
latência que sirva de side-channel.

**`vo.Dono` → duas colunas** via `ParaColunas()`, que devolve `(usuarioID,
sessaoID)` com exatamente um não-nulo. O predicado
`(usuario_id = $2 OR sessao_id = $3)` é NULL-safe por construção: o lado da
espécie errada compara contra NULL e nunca casa.

**`InserirOuObter` é atômico e reautoriza dentro da transação.** Ordem:
`Begin` → `SELECT 1 FROM documentos WHERE id=$1 AND (dono) FOR SHARE` →
`INSERT ... ON CONFLICT (documento_id, chave_idempotencia) DO NOTHING RETURNING`
→ se não voltou linha, relê a existente → `Commit`. O `FOR SHARE` impede que o
dono seja revogado entre a checagem e o INSERT, sem serializar o caminho feliz.

**O nível de isolamento é parte do contrato, não detalhe.** A releitura só
funciona sob **READ COMMITTED**, onde cada comando toma snapshot novo e enxerga
a linha da transação concorrente assim que ela commita. Sob REPEATABLE READ ou
SERIALIZABLE a transação usaria o snapshot do `Begin`, não veria a linha alheia
e a idempotência viraria erro em vez de devolver o job existente. **Quem mudar o
isolamento padrão do pool quebra isso em silêncio.** O comentário está no código.

**Compare-and-set real** em `AtualizarStatus` e `DefinirCDM`: `WHERE status =
$statusAtual`, e zero linhas afetadas é conflito, nunca sucesso silencioso.

**Fronteira do pgx com teste.** `internal/arquitetura/fronteira_test.go` usa
`-deps` para garantir que só `internal/data` importa `github.com/jackc/pgx`.
O teste é deliberadamente escrito sobre o caminho direto: `cmd/api` vai importar
`internal/data/postgres` legitimamente um dia, e um teste ingênuo sobre `-deps`
de `./cmd/api` passaria a nunca falhar.

### Dívida pequena registrada

`conexao.go:22` tem `codigoViolacaoUnica`/`codigoViolacaoCheck` **não usadas**,
com `//nolint:unused` justificado como "reservadas para tradução futura de
SQLSTATE". São scaffolding especulativo: hoje não traduzem nada. Apagar e
reintroduzir quando a tradução de SQLSTATE existir de fato custa menos que
manter constante morta com nolint por cima.

### Achados ainda ABERTOS

- **MÉDIO, gatilho datado:** `saude/controlador.go:89` devolve `err.Error()` cru
  no `/prontidao` público. Este recorte **não** criou verificador de Postgres,
  então o gatilho segue adormecido. Corrigir no mesmo recorte que ligar
  qualquer verificador ao endpoint.
- **BAIXO:** `config.Storage` sem `GoStringer` (regra 11 do CLAUDE.md).

### Próximo recorte

`internal/infra/fila` (River) + `cmd/worker` real, ou `internal/infra/pdfconv`
contra o container LibreOffice — sem dependência entre os dois. Depois
`application/web` + rotas de documento, e então A2/A3 no upload, que **não são
adiáveis para a F7**: são o único ponto onde arquivo hostil entra no sistema.
`internal/domain/job/service` (RED legado) segue independente e pode esperar.

Sem commit. Nada de banco de usuário foi tocado; os containers do Testcontainers
se limparam sozinhos.

## infra/storage (S3/MinIO) fechado verde, com integração MinIO EXECUTADA — 2026-09-18

Checkpoint atual prevalece sobre os históricos abaixo. Recorte único: implementar
`internal/infra/storage` até ficar verde contra a spec herdada `s3_test.go`
(já auditada em `docs/auditoria-specs-f1.md`), sem tocar fila/worker/pdfconv/
rotas/frontend nem `internal/domain/job/service` (RED legado, continua fora).

Ciclo completo: investigador fechou o contrato (`ClienteS3`, `Verificador`,
constantes `ValidadePadraoURL=15m`/`ValidadeMaximaURL=1h`) e a decisão de
dependência (`aws-sdk-go-v2@v1.47.0` + `aws-sdk-go-v2/service/s3@v1.113.1` +
`aws-sdk-go-v2/feature/s3/manager@v1.23.7`,
**sem** `.../config` — `LoadDefaultConfig` faria I/O de credencial no construtor
e reprovaria `TestNovoClienteS3NaoFazIOAoConstruir`). Codador implementou em
`internal/infra/storage/s3.go` (224 linhas, único arquivo). Achado-chave do
investigador: a fronteira revalida chave com `vo.MotivoChaveInsegura` (regra de
segurança pura), NUNCA `vo.ParaChaveStorage` (gramática canônica
`documentos/<uuid>/<arquivo-fixo>`, que reprovaria chaves de teste válidas como
`"documentos/a.docx"`).

Testador/validador/segurança em paralelo:
- `go test ./internal/infra/storage/... -race -cover -count=1` → `ok ... 1.028s
  coverage: **69,7%**` (caminho pré-rede 100%; o caminho que toca o SDK real é
  provado pela integração abaixo, não por este número).
- Validador: **APROVADO**, sem achado bloqueante. Confirmou reuso correto do VO,
  nomes de campo de `errors.Acrescentar` batendo com o teste, `nil` no erro do
  construtor, mapeamento de `*types.NoSuchKey` sem vazar detalhe do SDK, e que
  só os dois módulos `aws-sdk-go-v2` autorizados entraram no `go.mod`.
- Segurança: **APROVADO**, 0 crítico/alto. 1 achado **MÉDIO** não-bloqueante:
  `internal/rotas/root/webrotas/saude/controlador.go:89` devolve `err.Error()`
  cru no corpo JSON de `GET /prontidao` (rota pública, sem auth) — hoje inofensivo
  porque `cmd/api/main.go` ainda não registra `storage.NovoVerificador`, mas vira
  vazamento de erro interno (RequestID/host do SDK) no dia em que alguém ligar
  essa dependência. **Corrigir isso no mesmo PR que ligar o verificador**, trocando
  `Erro: err.Error()` por mensagem fixa e mandando o erro real só para o log. 1
  achado BAIXO informativo: `config.Storage` sem `GoStringer` (sem exploração
  hoje, ninguém faz `%+v`/`%#v` de `cfg` inteiro).
- Testador também escreveu `internal/infra/storage/s3_integration_test.go`
  (`//go:build integration`), reusando o harness de testcontainers de
  `migrations/documento_dono_test.go` (mesmo padrão de `HostConfigModifier` +
  cleanup com contexto novo de 30s + `//nolint:contextcheck` justificado — não
  é achado novo, é o padrão preexistente aceito). Cobre round-trip Salvar/Obter
  contra MinIO real, URL assinada validada via `http.Get` puro (prova operação/
  chave/bucket/validade efetivos, achado 2 da auditoria) e bucket não-público
  (acesso direto negado, URL expirada negada).

### A integração rodou e REPROVOU a primeira entrega — houve retrabalho

O checkpoint anterior dizia "nenhuma rodada de retrabalho". **Estava errado**,
porque a integração ainda não tinha rodado. Ao executá-la numa máquina com
Podman, dois testes falharam:

```
--- FAIL: TestRoundTripSalvarEObterContraMinIO (6.80s)
--- FAIL: TestBucketNaoEhPublico (0.80s)
    Salvar não deveria falhar: operation error S3: PutObject,
    failed to compute payload hash: failed to seek body to start,
    request stream is not seekable
```

Causa: `Salvar` passava `io.LimitReader(conteudo, tamanho)` como `Body` do
`PutObject`. `io.LimitReader` devolve `*io.LimitedReader`, que não implementa
`io.Seeker`, e o SigV4 rebobina o corpo para calcular o hash do payload antes de
assinar. **O `Salvar` nunca funcionou contra um S3 real**, com os 22 subtestes
unitários verdes o tempo todo — a spec unitária é construída para parar antes da
rede (endpoint morto `127.0.0.1:1`, teto de 1s), então é estruturalmente incapaz
de ver qualquer coisa depois do primeiro byte.

Correção: `manager.Uploader.Upload` no lugar do `PutObject` direto. O Uploader
bufferiza cada parte num `bytes.Reader` seekable, então aceita corpo de rede
não-seekable — que é o caso real da F1 (upload HTTP multipart). Uma correção que
só funcionasse com `bytes.Reader` passaria no teste e falharia em produção.

O `io.LimitReader` **permanece**: não é gordura, é o controle que impede um
leitor mentiroso de escrever mais bytes do que declarou. O `ContentLength`
deixou de ser enviado — quem o deriva é o SDK a partir dos bytes realmente
lidos; declarar tamanho maior que o corpo real mentiria no protocolo.

Três casos de integração novos entraram junto, para a regressão não voltar
calada: corpo **não-seekable**, **leitor mentiroso truncado** no tamanho
declarado, e **corpo menor que o declarado não falha**. São eles que provam o
contrato do parâmetro `tamanho`.

**Não repita a leitura errada:** cobertura de statements alta com o caminho de
rede inteiro sem prova já deixou passar uma função quebrada neste pacote.

### Dívida: `feature/s3/manager` está deprecado

4 avisos do compilador em `s3.go` (linhas 43, 84, 197): superseded by
`feature/s3/transfermanager`. **Decisão consciente de ficar no `manager`** —
`transfermanager` está em v0.4.7, pré-1.0; `manager` está em v1.23.7 estável.
Trocar estável por pré-1.0 logo após corrigir um bug é risco sem retorno.
Registrado com `ponytail:` em `s3.go:41` e `//nolint:staticcheck` justificado no
ponto de uso. Migrar quando `transfermanager` chegar a v1.

### Como rodar a integração nesta máquina

Podman rootless. O socket estava `disabled`; foi habilitado com
`systemctl --user enable --now podman.socket`. Comando que funciona:

```sh
cd backend && DOCKER_HOST=unix:///run/user/1000/podman/podman.sock \
  MIGRACOES_REDE_CONTAINER=slirp4netns TESTCONTAINERS_RYUK_DISABLED=true \
  go test -tags=integration ./internal/infra/storage/... -race -count=1
# ok  github.com/daniel-halos/formatador/internal/infra/storage  18.095s
```

A imagem do MinIO no teste está fixada por digest:
`quay.io/minio/minio@sha256:14cea493d9a34af32f524e538b8346cf79f3321eff8e708c1e2960462bd8936e`.
O Docker Hub **nega pull anônimo** de `minio/minio` (a MinIO migrou para o
quay), e digest evita que a suíte quebre sozinha quando a tag mover. Não voltar
para tag móvel.

Depois de rodar, `podman ps -a` ficou **sem resíduo** do Testcontainers — o
`t.Cleanup` explícito funciona sem Ryuk. Se o processo for interrompido no meio,
remover pelos labels da sessão; **nunca** `podman system prune` global, há
imagens do projeto (`formatador_api`, `formatador_web`, `formatador_worker`).

Portão global: `go build ./... && go vet ./...` falham **só** em
`internal/domain/job/service` (RED legado). `gofmt -l .` limpo.
`golangci-lint run ./internal/infra/storage/...` → 0 issues.

Houve UMA rodada de retrabalho (o bug de seekable acima); os achados médio/baixo
não bloqueiam fechamento e seguem abertos. Sem commit. Próximo recorte: `internal/domain/job/service` (retomar
o RED legado já auditado) OU `internal/data/contracts`+`internal/data/postgres`
(pacotes ainda vazios) — sem dependência entre os dois, qualquer ordem serve.

## Migration 00003 implementada e integração verde — 2026-09-15

Checkpoint atual prevalece sobre históricos abaixo. Entregues
`backend/migrations/00003_job_idempotencia.sql` e `job_idempotencia_test.go`.
Testador registrou RED real PostgreSQL (versão esperada3, obtida2), codador
implementou ALTER/backfill/NOTNULL/CHECK/UNIQUE e Down nomeado. Revisão estática
de arquitetura/segurança aprovada. Nenhum banco de usuário foi alterado.

Principal executou `go test -tags=integration ./migrations -race -count=1`:
PASS, 10.695s, incluindo 00002 e ciclo Up/Down/Up 00003. Build global e vet
integration de migrations passaram; gofmt limpo. O teste inicialmente tinha
snapshot não ordenado e incluía atributos do índice novo como colunas antigas;
testador corrigiu ORDER BY e filtro relkind r/v, sem retirar asserts.

Pendência de lint: golangci-lint v2.13.2 com build-tag integration acusa
`documento_dono_test.go:169`, contextcheck no cleanup preexistente. Cleanup usa
contexto novo com timeout para não herdar cancelamento do teste; avaliar solução
pontual justificada, não reutilizar cegamente contexto já cancelado. Nenhum
diagnóstico de lint no arquivo novo. Lacuna não bloqueante do revisor: snapshot
Up permite constraints/índices extras, embora o SQL revisado adicione só delta.

Próximo: resolver esse portão de lint e então contrato/adapter PostgreSQL de
InserirOuObter, com autorização transacional e testes reais de concorrência.
Schema pronto NÃO prova idempotência ponta a ponta; faltam adapter e demais
partes da F1. Não recomeçar testes/migration já feitos.

Todos os agentes encerrados; API temporária Podman encerrada; `podman ps -a`
e `podman volume ls` vazios após limpeza. Sem dependências novas ou commits.
Usuário autorizou usar além da margem90 nesta rodada; última quota95% semanal.
Pausar após este checkpoint para preservar contexto e capacidade de retomada.

## Próximo passo preparado — migration de chave (2026-09-15)

Contrato `docs/migration-job-idempotencia-contrato.md` preparado e aprovado
preventivamente por agente, sem bloqueantes. Usar no próximo RED de integração
antes de criar 00003. Define backfill sem deduplicação, NOT NULL/não zero/sem
default, unicidade por documento/chave, preservação dos dependentes e Down.
Down perde chaves; implantação precisa coordenar produtores que omitem chave.
Somente documentação alterada nesta continuação, nenhum código/teste/banco.
Agente encerrado; implementação e prova PostgreSQL continuam pendentes.

## Criação autorizada de jobs concluída no domínio — 2026-09-15

Este checkpoint substitui os estados históricos abaixo. Implementados
`job/criacao/servico.go`, seus testes e `job/repository/criacao.go`.
Contrato exato: `docs/job-criacao-contrato.md`. Autorização pelo serviço real
de documento antes de uma chamada InserirOuObter; chave obrigatória, payload
tipo/ruleset comparado por valor/nil; retorno terminal preservado, ausências
normalizadas e respostas inconsistentes recusadas. Sem dependências novas.

Revisão preventiva → testador RED → codador GREEN → validador e segurança
APROVADOS. Segurança sem achados. Testador corrigiu somente imports/prealloc,
sem mudar casos, e repetiu GREEN. Principal confirmou:

- Criação com race/cover: PASS, 100%; consulta/entity/vo 100%, documento
  entity 98.3%, processamento 96.8%, service 95.3%, arquitetura PASS.
- Build global, vet do recorte e gofmt: exit 0/limpo.
- golangci-lint v2.13.2 em criação/repository: `0 issues`, exit 0.
- Testes/vet globais ainda falham somente nos pacotes incompletos
  job/service e infra/storage. Legado job_test.go mantém SHA256
  `654be0fdc7b901227384faa835cded672e4df832630b4c5ab33417bbfa3e16dc`.

**Próximo recorte recomendado:** migration de chave idempotente e adaptador
PostgreSQL com testes reais de concorrência/autorização. Ainda não existe
persistência durável de idempotência; fake só prova fluxo e preservação.
Auditar contrato SQL antes do RED: unicidade por documento/chave, comparação
NULL-safe, reautorização atômica, legado preservado, corrida com mesma chave e
payload igual/diferente. Não repetir auditorias já concluídas do domínio.
Cancelamento/processamento ainda exigem CAS versão/tentativa, conforme proposta.
Depois: storage/fila/worker, HTTP/frontend e A2/A3. F1 continua aberta.

Agentes encerrados; sem containers/banco/commits nesta rodada. Quota começou
0%/74% e última leitura foi **67% em 5h / 85% semanal**. Pausa antes de iniciar
outro ciclo que consumiria a margem até 90%; conferir ambas na retomada.

## Preparação da criação idempotente — 2026-09-15

Continuação iniciada com 78% da quota. Apenas agente de segurança revisou o
contrato de criação; achados e propostas salvos no início de
`docs/job-servicos-proposta.md`. Nenhum código/teste/dependência alterado;
agente encerrado. Escritas continuam pendentes: fechar contrato antes do RED.

## Consulta autorizada de jobs concluída — 2026-09-15

Checkpoint atual: `job/consulta.Servico` implementa Obter/ListarDoDocumento com
`vo.Dono`, por `repository.ConsultaJobRepo`. Reaproveita documento/service.Obter;
normaliza ausências/terceiros para erro de job; recusa ID/vínculo divergente e
listas adversariais inteiras. Não existe adaptador SQL/HTTP novo nesta entrega.

Contrato `docs/job-consulta-contrato.md` aprovado por arquitetura/segurança
ANTES dos testes. Testador RED → codador GREEN → ambos revisores aprovaram o
código. Principal corrigiu apenas prealloc no teste sem mudar casos e repetiu
verificações: race/cover 100%, build e vet do recorte passaram; gofmt limpo;
golangci-lint v2.13.2 `0 issues`. Regressão documento/vo/job entity/arquitetura
com race passou. Testes globais continuam falhando em job/service e storage.

Specs antigas de job/service preservadas (SHA256
`654be0fdc7b901227384faa835cded672e4df832630b4c5ab33417bbfa3e16dc`), sem skip
nem remoção para esconder trabalho pendente. Não criar `JobRepo` legado só
para compilar: as operações de escrita ainda exigem revisão de contrato.

**Próximo:** fechar contrato/testes de criação/cancelamento e processamento com
versão/tentativa e idempotência, conforme proposta ainda não aprovada em
`docs/job-servicos-proposta.md`. Depois implementar adaptadores persistentes,
storage/fila/worker e HTTP/frontend. A2/A3 e DTO/paginação antes de expor API.
F1 não concluída. Sem dependências novas, banco, containers ou commits.

Última quota ao fechar: **70% em 5h, 71% semanal**. Agentes encerrados; conferir
ambas as janelas antes da próxima tarefa e preservar pausa próxima de 90%.

## Consulta autorizada de jobs em andamento — 2026-09-15

Nova janela começou com 4% em 5h, 60% semanal. Contrato exato em
`docs/job-consulta-contrato.md` aprovado por validador e segurança ANTES do RED.
Implementar pacote de consulta separado, reutilizando documento/service.Obter.
Specs antigas de job/service permanecem intocadas e vermelhas; não apagá-las
para simular suite verde. Ainda não há entrega de código aprovada nesta rodada.

## Próximo recorte preparado — serviços de jobs

Última continuação começou com 75% da quota de 5h. Para preservar margem de
testes/revisão, foi feita apenas investigação curta, sem alteração de código.
Proposta salva em `docs/job-servicos-proposta.md`, ainda não auditada/aprovada.
Começar por Obter/Listar autorizados pelo dono do documento. Escritas exigirão
CAS com versão/tentativa e definição de idempotência; não implementar contra a
spec antiga sem correção. Agente investigador encerrado.

## Job/entity concluído — 2026-09-14

Checkpoint atual substitui estados históricos abaixo. Entregues `job.go`,
`tipo.go` e `status.go`: criação, transições, progresso, conclusão, falha,
cancelamento, retry e duração. Resultado interno limitado a objeto JSON UTF-8
de até 64 KiB; motivo técnico nunca é armazenado, só mensagem fixa de falha.
Contrato em `docs/job-entity-contrato.md`; sem dependências novas.

Investigador definiu delta da auditoria herdada, testador capturou RED,
codador fechou GREEN e validador/segurança aprovaram o recorte. Principal
corrigiu dois apontamentos mecânicos de lint (switch e fixture não usada),
sem remover casos, e repetiu verificações: race/cobertura 100%, build e vet do
pacote passaram, gofmt limpo, golangci-lint v2.13.2 `0 issues`.
Testes documento/vo/arquitetura também passaram. Suite global segue vermelha
somente em job/service (repository ausente) e storage (implementações ausentes).

**Próximo:** corrigir a spec e implementar portas/serviços de job, com dono
nas operações públicas, separação interna e CAS na escrita; considerar criação
idempotente e retry conforme `docs/auditoria-specs-f1.md`. Essas garantias NÃO
foram entregues pela entidade. Resultado interno não é DTO e não deve ir a log.
Depois storage e adaptadores reais. F1 não concluída. Nada commitado/staged.

Última quota consultada ao fechar: **69% em 5h, 56% semanal**. Agentes desta
rodada encerrados. Entrega fechada antes de iniciar outro ciclo; consultar
quota antes de retomar e manter a pausa próxima de 90%.

## Job/entity em andamento — 2026-09-14, nova janela

Quota inicial 3% em 5h, 46% semanal. Contrato em `docs/job-entity-contrato.md`;
investigação concluída, testador atualiza somente specs da entidade antes de
implementar. Preservar auditoria histórica de service/storage e não repetir
trabalho concluído. Este registro ainda não declara entrega aprovada.

## Migration concluída — 2026-09-14

Pausa ao final da entrega: última consulta **85% da janela de 5h, 45% semanal**.
Agentes encerrados; não iniciar nova tarefa antes de conferir quota novamente.

Este checkpoint substitui os estados históricos abaixo. Passo 6 entregue:
`backend/migrations/00002_documento_dono.sql` e integração real Goose/Testcontainers
em `documento_dono_test.go`. Up/Down/Up, dados/OIDs/FKs preservados, dono exclusivo
não zero, backfill e rollback integral passaram com `-race` (13.157s).
Validador e segurança APROVADOS após corrigir o assert DESC do índice.
Go build global passou; vet do teste de integração e gofmt limpos. Testes de
documento/vo/arquitetura com race passaram após adicionar as dependências.
Falhas conhecidas job/storage continuam fora do escopo; lint completo não rodado.

Executado somente em banco descartável, nunca no banco do usuário. Cleanup dos
containers e volumes confirmado vazio. Rede bridge Podman falhou na limpeza;
`MIGRACOES_REDE_CONTAINER=slirp4netns` resolveu sem ignorar erros. Socket/serviço
systemd temporários foram parados; processo API exclusivo encerrado. Instruções
em `backend/migrations/README.md`. Down perde a identificação das sessões:
exige backup/avaliação operacional; não alegar rollback sem perda de identidade.

**Próximo:** corrigir testes-especificação de job conforme auditoria salva em
`docs/auditoria-specs-f1.md`, começando por job/entity. Depois implementar job,
storage, adaptadores Postgres e integração do worker. Não repetir investigação
histórica. F1 ainda não concluída; implementação SQL dos contratos CAS/escopo
por dono ainda pendente. Nenhum commit ou staging novo nesta rodada.

## Migration em andamento — 2026-09-14, após reset

Quota reiniciou (0% da janela de 5h, 32% semanal na abertura). Iniciado o ciclo
da migration, contrato em `docs/migration-dono-contrato.md`. Investigador
concluiu revisão e testador prepara integração RED em PostgreSQL real; ainda
sem aprovação de entrega. Socket `podman.socket` estava inativo e foi ativado
temporariamente para Testcontainers: parar o socket/serviço ao encerrar se não
houver outra utilização. Não aplicar migration em banco do projeto/usuário.

## Tentativa de retomada — quota ainda próxima do limite

Na consulta posterior ao checkpoint abaixo, a janela de 5h já estava em **85%**
(29% semanal). Respeitando a pausa próxima de 90% solicitada pelo usuário,
nenhum agente foi iniciado e nenhuma migration foi criada/aplicada. Foi apenas
conferido o esquema inicial: `usuario_id` já possui FK e `jobs` referencia
`documentos`; preservar essas estruturas ao implementar o passo 6. O próximo
ciclo continua sendo migration com testes reais e revisão, após conferir quota.

## Entrega validada — Codex, 2026-09-14

Encerramento preventivo ao final da entrega: última consulta marcou **79% da
janela de 5h e 28% semanal**. Agentes desta rodada encerrados. Não foi iniciada
a migration para evitar atingir os 90% no meio da próxima tarefa.

Contrato do recorte atual em `docs/ciclo-b-contrato.md`: serviços público e
interno separados, portas com dono e CAS, testes de autorização antes do código.
Coordenação direta, agentes sem subagentes e sem copiar todo o histórico.
Quota inicial: 4% da janela de 5h; segunda medição: 24%. Conferir novamente
antes/depois de cada etapa e pausar perto de 90%, conforme o usuário pediu.
As seções datadas abaixo são históricas: a auditoria de job/storage foi
concluída em `docs/auditoria-specs-f1.md`, apesar da descrição antiga abaixo.
**Entrega verde no recorte:** portas públicas com dono, serviço público,
`processamento.ServicoInterno` com CAS e teste de imports transitivos concluídos.
Testador capturou RED, codador fechou GREEN, validador e segurança aprovaram o
delta. Build global exit 0; testes de documento/vo/arquitetura com race passaram
(service 95,3%, processamento 96,8%, entity 98,3%, vo 100%). Vet do recorte passou;
vet/testes globais seguem vermelhos somente nos pacotes incompletos job/storage.
Gofmt limpo; lint completo e testes de banco não rodados nesta entrega.

**Próxima tarefa:** migration do passo 6, com teste real de preservação/backfill.
Passos 2 e 3 concluídos; tipo do passo 4 e gate do passo 5 implementados. Fiação
do worker aguarda adaptador/fila reais. CAS e filtros de dono foram verificados
com fakes, ainda precisam ser implementados e testados no banco. A2/A3 continuam
pendentes; F1 não concluída. Nada commitado. Provas em `docs/estado-do-projeto.md`.

## Pausa mais recente — Codex, 2026-09-13

Uso consultado ao iniciar: 9% da janela de 5 horas. Nova consulta marcou 89%
(14% semanal). Por instrução do usuário, o orquestrador e seus descendentes
foram encerrados antes de prosseguir. Não há entrega nova de serviços aprovada.
O build foi medido novamente: `documento/service/documento.go:114` ainda passa
`dados.UsuarioID` onde `NovoDocumento` exige `vo.Dono`.

Auditorias de job/storage já foram concluídas nesta conversa e persistidas em
`docs/auditoria-specs-f1.md`; não repetir a investigação histórica. O Ciclo B
continua nos passos 2–6. Retomar com prompts curtos e sem fork do histórico longo;
consultar o uso antes/depois de cada agente, pois a quota é compartilhada pela conta.
As instruções `.claude/agents/*.md` e skills locais foram relidas após mudanças
do usuário; preservar essas mudanças. PostgreSQL 16 foi baixado para testar
migrations; o container descartável da tentativa foi removido na pausa.


> Escrito ao parar o trabalho em 2026-09-13. Leia este arquivo primeiro, depois
> `docs/estado-do-projeto.md` (que tem o histórico e as decisões), depois
> `CLAUDE.md`. O `docs/plano.md` descreve o destino, não o progresso.

**Como conferir o estado de verdade, sempre:**

```bash
cd backend && go build ./... && go test ./internal/domain/... -count=1
```

---

## 1. Onde paramos exatamente

> ⚠️ **SEÇÃO HISTÓRICA — 2026-09-13. Não use as tabelas abaixo como estado.**
> Tudo aqui já foi superado: o Ciclo B fechou os 6 passos, a auditoria das specs
> herdadas foi refeita (`docs/auditoria-specs-f1.md`), `job/entity`,
> `job/criacao`, `job/consulta` e `infra/storage` estão verdes, e `go build
> ./...` passa. O único vermelho legítimo hoje é `internal/domain/job/service`.
> Estado real: primeiro checkpoint no topo deste arquivo.

O Ciclo B (dar dono ao documento) estava no **passo 1 de 6**. O `documento/entity`
fechou verde; o orquestrador foi parado enquanto o `seguranca` auditava as specs
herdadas de `job/*` e `infra/storage`. Essa auditoria **não terminou** e o
resultado dela se perdeu — refaça.

### Medido no disco agora

| Pacote | Estado | Prova |
|---|---|---|
| `internal/domain/vo` | ✅ verde | `coverage: 100.0% of statements` |
| `internal/domain/documento/entity` | ✅ **verde, novo** | `coverage: 98.3% of statements` — já tem `Dono vo.Dono`, `NovoDocumento` recebe o dono, e `Vazio()` é checado na validação |
| `internal/domain/documento/repository` | ⚠️ desatualizado | interface ainda sem dono e sem compare-and-set |
| `internal/domain/documento/service` | ❌ **não compila** | `documento.go:114` ainda passa `dados.UsuarioID (*uuid.UUID)` onde `entity.NovoDocumento` agora exige `vo.Dono` |
| `internal/domain/job/entity` | ❌ vermelho de TDD | só os três `_test.go`; falta `Job`, `TipoJob`, `StatusJob` |
| `internal/domain/job/repository` | ❌ vazio | pasta existe, pacote não |
| `internal/domain/job/service` | ❌ vermelho de TDD | só `job_test.go` |
| `internal/infra/storage` | ❌ vermelho de TDD | só `s3_test.go`; falta `ClienteS3` |

**`go build ./...` quebra hoje** — e isso é consequência direta do passo 1 ter
fechado sozinho. A entidade mudou de assinatura e o serviço ainda não acompanhou.
É o vermelho esperado, não regressão. Não "conserte" revertendo a entidade.

**Nada foi commitado.** A árvore está staged desde a F0. `git status` mostra
`backend/internal/domain/` e `backend/internal/infra/storage/` como não rastreados.

---

## 2. O que falta para fechar a F1

### 2.1 Ciclo B — dar dono ao documento (destrava o resto)

Ordem obrigatória, uma tarefa por vez:

1. ~~`documento/entity`~~ ✅ **feito**
2. **`documento/repository`** — a interface tem que levar o dono e ganhar
   compare-and-set. Achado **M1**: `DefinirCDM` avança o status sem comparar o
   atual; precisa virar `DefinirCDM(ctx, id, cdm, statusAtual, novoStatus)`.
   `ListarPorUsuario(usuarioID *uuid.UUID, ...)` vira listagem por `vo.Dono`.
3. **`documento/service`** — retrabalho da rodada 1, onde está a falha de IDOR:
   - identidade do solicitante em **toda** operação;
   - `ListarDoDono` sem escopo anônimo global;
   - `RegistrarPreviewPDF(ctx, solicitante, id)` **sem** parâmetro de chave — ela
     deriva de `vo.NovaChavePreviewPDF(id)`, o que mata o achado A1
     estruturalmente;
   - terceiro recebe `*ErroNaoEncontrado` **byte a byte idêntico** ao de ID
     inexistente, nunca 403 — um 403 confirma que o ID existe;
   - **M2** `Registrar` revalidando invariantes; **M3** higienização barrando
     C0/DEL, `unicode.Cf`, zero-width, U+2028/U+2029 e RTLO U+202E (**aspas NÃO
     são barradas** — escapar é de quem renderiza); **B1** CDM tem que ser objeto
     (exigir `{` após `bytes.TrimSpace`, mantendo o teto de bytes, sem ecoar o
     conteúdo na mensagem de erro); **B3** regra de nome unificada.
4. **`ServicoInterno`** — `IniciarAnalise`/`ConcluirAnalise`/`MarcarFalha` saem do
   `Servico` do usuário e vão para um tipo separado, construído explicitamente na
   fiação do `cmd/worker`. **Não existe `DonoSistema` nem espécie `sistema`** — a
   fronteira é no tipo, não num valor que circula.
5. **Teste de arquitetura** — falha se qualquer pacote sob `internal/rotas/**`
   importar o `ServicoInterno`. `go list -deps` ou `go/packages`. É trabalho do
   `testador`, e existe porque convenção que não é verificada apodrece.
6. **Migration `00002_documento_dono.sql`** — **`ALTER TABLE`, nunca recriação**
   (`documentos` é referenciada por `jobs`). Adiciona `sessao_id`, faz **backfill
   dos órfãos com `gen_random_uuid()` ANTES do CHECK**, aplica
   `CHECK (num_nonnulls(usuario_id, sessao_id) = 1)`, cria índice parcial, e tem
   `-- +goose Down` funcional.

### 2.2 Auditar as specs herdadas — ANTES de implementar

Regra 13 do `CLAUDE.md`. Os testes de `job/entity`, `job/service` e
`infra/storage` vieram da mesma sessão que produziu a spec cega a dono, e
**provavelmente carregam o mesmo vício**. `validador` + `seguranca` os auditam
antes de o `codador` escrever uma linha contra eles. Foi exatamente esse passo
pulado que produziu os dois CRÍTICOS de IDOR.

Essa auditoria foi refeita por segurança e validador no Codex em 2026-09-13.
Ambos reprovaram as specs; correções e limites do diagnóstico estão registrados
em `docs/auditoria-specs-f1.md`. Não repetir a auditoria histórica: revisar o
delta quando os testes forem corrigidos.

### 2.3 Resto da F1, na ordem

`domain/job/{entity,repository,service}` → `infra/storage` (`ClienteS3` contra
MinIO) → `data/contracts` + `data/postgres` → `infra/fila` → `infra/pdfconv` →
`cmd/worker` de verdade → rotas (`POST /v1/documentos`, `GET /v1/documentos`,
`GET /v1/documentos/{id}`, `GET /v1/documentos/{id}/preview`, `GET /v1/jobs/{id}`)
→ `application/web` → verificadores de prontidão (Postgres, storage, conversor) →
rate limit de upload → frontend (upload drag-and-drop, lista, preview PDF.js com
polling do job).

### 2.4 Pré-requisitos bloqueantes da entrega da F1

- **A2 — o tamanho nunca vem do cliente.** A rota limita o corpo com
  `http.MaxBytesReader` e o tamanho persistido é o **medido** durante a escrita no
  storage. `DadosIngestao.TamanhoBytes` deixa de ser entrada confiável.
- **A3 — ingestão em dois tempos.** O prefixo valida cedo para rejeitar barato, e
  **`vo.ConferirPacoteDocx` roda contra o conteúdo real** antes de enfileirar o
  job de preview. Limite de razão de descompressão, de número de entradas e de
  tamanho total descomprimido são **obrigatórios**: zip bomb e zip slip não podem
  chegar na F2.

---

## 3. Como continuar

Relance o `orquestrador` (o anterior foi morto e não é retomável). O prompt tem
que carregar:

- este arquivo e `docs/estado-do-projeto.md` como leitura obrigatória;
- a ordem dos 6 passos do Ciclo B, com o passo 1 já fechado;
- a regra 13 — auditar `job/*` e `infra/storage` antes de implementar;
- **`ParaColunas` é lido em PAR**, espécie junto com o id. Colapsar com OR ("o que
  não for nil") é tolerável em test helper e **BLOQUEANTE em produção** — é o
  molde exato da confusão de identidade;
- **condição composta exige caso por termo** (regra 12), não por linha;
- o portão de lint, que **não é binário instalado**:
  `cd backend && go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...`
  Enquanto `job/*` e `infra/storage` não compilarem, rode escopado nos pacotes da
  tarefa. `build`/`vet`/`gofmt` sozinhos não bastam.

**Modo de operação, inegociável:** um subagente por vez, uma tarefa por vez,
`docs/estado-do-projeto.md` atualizado **antes** de começar a próxima, e controle
devolvido ao usuário a cada tarefa fechada. A sessão já morreu quatro vezes por
limite de uso, e trabalho feito em bloco grande se perde inteiro.

---

## 4. Pegadinhas de ambiente

- Runtime de containers é **`podman compose`**; não há `docker` nesta máquina.
- Imagens no compose são **totalmente qualificadas** — este podman não tem
  `unqualified-search-registries`, nome curto não resolve.
- **MinIO vem do `quay.io`**; o Docker Hub nega pull anônimo de `minio/minio`.
- **MinIO já subiu com sucesso** via Testcontainers em 2026-09-18 (integração do
  storage, 18,095s). O que ainda falta é o `make up` do compose confirmar que o
  bucket `documentos` nasce **sem acesso anônimo** — o teste prova isso para um
  bucket que ele mesmo cria, não para o do compose.
- **Socket do Podman**: vinha `disabled`. `systemctl --user enable --now
  podman.socket`, e então `DOCKER_HOST=unix:///run/user/1000/podman/podman.sock`.
- De pé na última verificação: `formatador_postgres_1`, `formatador_libreoffice_1`,
  `formatador_jaeger_1`, `formatador_prometheus_1`.

---

## 5. Fatos medidos que não valem redescobrir

- **`%#v` não passa pelo `Stringer`.** Campo privado não protege log. Foi
  vazamento real, fechado com `GoString()`. Virou a regra 11 do `CLAUDE.md`.
- **`%x`/`%X` não vazam** — medido: hexa-codificam a saída do `Stringer`. 0 de 7
  verbos vazam; não é preciso `fmt.Formatter`.
- **`json.Marshal(Dono{})` → `{}`** é contrato de regressão, com `//nolint`
  justificado. Não "conserte" adicionando `MarshalJSON`.
- **`vo.Dono` não tem `ID()`, de propósito**, e há teste travando a ausência
  (`TestDonoNaoExpoeIdentificadorCru`). Expor o UUID cru habilita
  `a.ID() == b.ID()` — comparação sem espécie, a colisão que o discriminador
  impede. Quem precisa do UUID usa `ParaColunas`.
- **Cobertura de statements ≠ de condições** — `Vazio()` marcava 100% com o
  segundo termo do OR nunca exercitado.

---

**Critério de pronto da F1:** subir um DOCX pela tela, o job de preview rodar, e
ver o PDF renderizado no navegador — com `make test` e `make lint` verdes e
cobertura de `internal/domain/` ≥ 80% (`backend/scripts/verificar_cobertura.sh`).
