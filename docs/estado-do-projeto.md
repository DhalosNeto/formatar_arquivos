# Estado do projeto — ponto de retomada

> **Atualize este arquivo a cada tarefa fechada, não só no fim da fase.**
> Ele é o que permite retomar o trabalho depois de uma interrupção (limite de
> sessão, queda, troca de máquina). Quem retoma lê este arquivo primeiro, depois
> `docs/plano.md` e `CLAUDE.md`.

**Última atualização:** 2026-09-19
**Fase corrente:** F1 — Ingestão e preview

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
  `/home/daniel-halos/.cache/go-build/a7/a7b195705bae821695a7b931e2f355e7c4037848574f7c96ebf04fed6923c4e7-d/golangci-lint`.
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

## Entrega atual — infra/storage (S3/MinIO) verde COM integração real (2026-09-18)

Recorte fechado: `internal/infra/storage/s3.go` (`ClienteS3`, `NovoClienteS3`,
`URLPreAssinada`, `Salvar`, `Obter`, `Verificador`/`NovoVerificador`) contra a
spec herdada `s3_test.go`, já auditada em `docs/auditoria-specs-f1.md`.

Dependências novas autorizadas: `aws-sdk-go-v2@v1.47.0`,
`aws-sdk-go-v2/service/s3@v1.113.1` e `aws-sdk-go-v2/feature/s3/manager@v1.23.7`
— sem `.../config`, que faria I/O de resolução de credencial no construtor e o
teste de ausência de I/O proíbe.

### O bug que só a integração pegou — leia antes de confiar em cobertura

A primeira entrega foi declarada verde com 68,8% e `go test` unitário PASS. Ela
**nunca funcionou contra um S3 real**. `Salvar` passava
`io.LimitReader(conteudo, tamanho)` como `Body` do `PutObject`; `io.LimitReader`
devolve `*io.LimitedReader`, que não implementa `io.Seeker`, e o SigV4 precisa
rebobinar o corpo para calcular o hash do payload antes de assinar:

```
--- FAIL: TestRoundTripSalvarEObterContraMinIO (6.80s)
--- FAIL: TestBucketNaoEhPublico (0.80s)
    Salvar não deveria falhar: operation error S3: PutObject,
    failed to compute payload hash: failed to seek body to start,
    request stream is not seekable
```

Nenhum teste unitário podia pegar isso: a spec inteira é construída para parar
ANTES da rede (endpoint morto `127.0.0.1:1` + teto de 1s denunciam validação
preguiçosa por tempo). O verde dela nunca significou "o Salvar salva".
**Lição de processo: cobertura de statements com o caminho de rede inteiro sem
prova permitiu fechar como pronta uma função quebrada.**

Correção: `manager.Uploader.Upload` no lugar de `PutObject` direto — o Uploader
bufferiza cada parte num `bytes.Reader` seekable, então funciona com corpo de
rede não-seekable, que é o caso real da F1 (upload HTTP multipart). O
`io.LimitReader` foi **mantido**: é controle de segurança, impede leitor
mentiroso de escrever mais do que declarou.

Achado fino do codador na mesma correção: `ContentLength` deixou de ser enviado.
Quem o deriva é o SDK, a partir dos bytes realmente lidos — declarar um tamanho
maior que o corpo real mentiria no protocolo.

### Provas reais (comando e saída, não "passou")

```
go test ./internal/infra/storage/... -race -cover -count=1
ok  github.com/daniel-halos/formatador/internal/infra/storage  1.028s  coverage: 69.7%

DOCKER_HOST=unix:///run/user/1000/podman/podman.sock \
MIGRACOES_REDE_CONTAINER=slirp4netns TESTCONTAINERS_RYUK_DISABLED=true \
go test -tags=integration ./internal/infra/storage/... -race -count=1
ok  github.com/daniel-halos/formatador/internal/infra/storage  18.095s
```

`go build ./...` exit 0. `gofmt -l .` limpo.
`golangci-lint run ./internal/infra/storage/...` → 0 issues.
`go vet ./...` e `go test ./...` globais falham **só** em
`internal/domain/job/service` (RED legado, intocado, SHA256
`654be0fdc7b901227384faa835cded672e4df832630b4c5ab33417bbfa3e16dc` conferido).

Seis testes de integração (`//go:build integration`, harness reusado de
`migrations/documento_dono_test.go`): round-trip Salvar/Obter, URL assinada via
`http.Get` puro (operação/chave/bucket/validade efetivos — achado 2 da
auditoria), bucket não-público, **corpo não-seekable**, **leitor mentiroso
truncado no tamanho declarado** e **corpo menor que o declarado não falha**.
Os três últimos entraram depois do bug, justamente para provar o contrato do
`tamanho` — sem eles a regressão volta sem ninguém ver.

### Dívida registrada

`feature/s3/manager` está **deprecado** em favor de `feature/s3/transfermanager`
(4 avisos em `s3.go`, linhas 43, 84 e 197). Decisão consciente de ficar:
`transfermanager` está em **v0.4.7, pré-1.0**, contra `manager` v1.23.7 estável.
Marcado com `ponytail:` em `s3.go:41` e `//nolint:staticcheck` justificado no
ponto de uso. Migrar quando `transfermanager` chegar a v1.

Auditoria: validador APROVADO sem achado. Segurança APROVADO, 0 crítico/alto.
1 MÉDIO **ainda aberto, com gatilho datado**: quando `storage.NovoVerificador`
for ligado ao `/prontidao` público em `cmd/api/main.go`,
`saude/controlador.go:89` devolve `err.Error()` cru no JSON — vazaria endpoint,
bucket e fragmento de credencial do SDK numa rota sem auth. **Corrigir no mesmo
recorte que ligar o verificador, não depois.** 1 BAIXO informativo:
`config.Storage` sem `GoStringer` (regra 11 do CLAUDE.md), sem exploração hoje.

Próximo recorte: `internal/data/contracts` + `internal/data/postgres`, depois
retomar `job/service` (achados de IDOR já documentados abaixo) — ou vice-versa,
sem dependência entre os dois. Sem commit nesta rodada.

## Entrega atual — migration 00003 com integração verde (2026-09-15)

Migration de chave idempotente e teste `job_idempotencia_test.go` implementados.
TDD com agentes, RED real por versão3 ausente, revisão estática aprovada e
GREEN PostgreSQL: `go test -tags=integration ./migrations -race -count=1`
PASS em 10.695s, incluindo regressão 00002 e preservação Up/Down/Up. Build e
vet do recorte passaram; gofmt limpo. Sem banco de usuário/dependências/commits.
Ponytail orientou reuso do harness existente. Containers/volumes e agentes limpos.

Lint integration tem 1 pendência preexistente: contextcheck no cleanup de
`documento_dono_test.go:169`. Registrar/resolver sem herdar contexto cancelado.
Detalhes em `docs/retomada-f1.md`, inclusive correções do snapshot do teste.
Schema está validado, mas adaptador atômico de criação e concorrência ainda
não existem. Próximo: portão lint, contrato e implementação do adaptador.
Quota semanal chegou a95% com autorização de avançar além de90; pausa salva.

## Preparação mais recente — migration de idempotência (2026-09-15)

Contrato em `docs/migration-job-idempotencia-contrato.md`, revisado por agente e
APROVADO para começar TDD de integração. Define preservação do legado, chaves
obrigatórias sem default, unicidade documento/chave e perda de chaves no Down.
Nenhuma migration/código/teste implementado ou banco alterado nesta continuação.
Próximo: testador RED usando harness PostgreSQL existente, depois migration e
revisões. Agente encerrado. Entrega funcional anterior permanece a seguinte.

## Entrega atual — criação autorizada de jobs (2026-09-15)

Prevalece sobre o histórico abaixo. Serviço `job/criacao` e porta
`repository.CriacaoJobRepo` implementados; contrato em
`docs/job-criacao-contrato.md`. Reusa autorização de documento e validação de
Job; exige chave, delega InserirOuObter uma vez, verifica retorno, mantém
estado de jobs repetidos e normaliza ausência sem revelar terceiros.
Ponytail orientou reuso e recorte sem dependências/abstrações adicionais.

Ciclo com agentes: contrato revisado antes do RED; testes separados do código;
validador e segurança aprovaram implementação, sem bloqueantes. Race/cover
criação 100%, regressão dos pacotes já entregues PASS. Build global e vet do
recorte PASS; gofmt limpo; golangci-lint v2.13.2 `0 issues`. Testador fez ajuste
mecânico de imports/prealloc, sem mudar casos. Testes/vet globais seguem
vermelhos somente em job/service legado e storage; não houve skip/remoção.

Limite explícito: ainda NÃO há adaptador SQL, migration de chave, endpoint ou
fila. Idempotência durável/concorrência precisa de integração PostgreSQL, não
é demonstrada pelos fakes. Próximo recorte recomendado: contrato SQL + migration
preservando legado + adapter atômico e testes concorrentes. Restam também
cancelamento/processamento CAS, storage/fila/worker, HTTP/frontend e A2/A3.

Detalhes/provas e ordem em `docs/retomada-f1.md`. Todos os agentes encerrados,
sem dependências novas/containers/commits. Última quota: 67% em 5h, 85% semanal;
preservada margem em vez de iniciar mais um ciclo. F1 não concluída.

## Preparação posterior — criação idempotente

Após consulta autorizada, revisão preventiva de segurança em 2026-09-15
identificou definições de atomicidade, chaves legadas, NULL e retorno de jobs
encerrados. Registradas em `docs/job-servicos-proposta.md`; sem código ou teste
novo, sem aprovação final de contrato de escrita. Rodada começou com quota78%.

## Entrega mais recente — consulta autorizada de jobs (2026-09-15)

Criados `job/repository/consulta.go`, `job/consulta/servico.go` e seus testes.
Obter/ListarDoDocumento exigem dono, autorizam pelo serviço real de documento,
checando IDs/vínculos antes de retornar dados; listas inconsistentes são
rejeitadas integralmente. Ausente e terceiro retornam o mesmo não encontrado.
Implementação de domínio/porta apenas: ainda sem SQL, endpoint ou DTO público.

Contrato aprovado previamente por validador/segurança. Testador capturou RED,
codador entregou GREEN; revisão estática de código aprovada pelos dois agentes.
Principal corrigiu prealloc no teste (sem alterar casos) e confirmou:

- `go test ./internal/domain/job/consulta -race -count=1 -cover`: PASS, 1.017s,
  100% de cobertura de statements.
- `go build ./...`, vet de consulta/repository: exit 0; gofmt limpo.
- golangci-lint v2.13.2 nesses dois pacotes: `0 issues`, exit 0.
- Regressão com race em job/entity, documento, vo e arquitetura: PASS.
- `go test ./... -count=1`: falha SOMENTE nos pacotes ainda incompletos
  job/service (`repository.JobRepo`, NovoServico/DadosNovoJob ausentes) e
  infra/storage (`ClienteS3` etc.). Vet global também falha nesses pacotes.

Testes herdados de job/service permanecem idênticos; não apagados nem pulados.
Consulta está separada para não implementar escrita contra spec insegura.
Próximo: contrato de criação/cancelamento/processamento, CAS versão/tentativa,
idempotência; depois adaptadores reais. `docs/job-servicos-proposta.md` segue
proposta para escritas, não aprovação automática. `docs/job-consulta-contrato.md`
é o contrato entregue. F1 aberta. Sem dependências novas ou commit.

Última quota consultada: **70% de 5h, 71% semanal**; agentes encerrados.

## Preparação da próxima entrega — serviços de jobs

Investigação curta após job/entity, iniciada com quota de 5h em 75%. Nenhum
código ou teste alterado nesta continuação. `docs/job-servicos-proposta.md`
registra proposta ainda não aprovada: começar com leitura autorizada, depois
escritas com proteção contra tentativa obsoleta e idempotência por pedido.
Não confundir investigação com entrega: serviços/repositórios seguem pendentes.

## Entrega mais recente — job/entity (2026-09-14)

Entidade de job implementada e validada. `NovoJob` copia o ruleset e valida
tipo/documento; transições/progresso/retry seguem contrato. Resultado nil ou
objeto JSON UTF-8 de até 64 KiB, com cópia defensiva. `Falhar` grava mensagem
fixa, não o motivo técnico. Tipos/status reaproveitam specs previamente auditadas;
spec da entidade corrigida conforme `docs/job-entity-contrato.md`.

Agentes: investigador, testador (RED), codador (GREEN), validador e segurança
(APROVADOS). Principal fez limpeza mecânica de lint (switch e lista de teste
nunca usada), sem remover casos; verificações finais após essa limpeza:

- `go test ./internal/domain/job/entity -race -count=1 -cover`: PASS, 1.066s,
  **100% de cobertura de statements** (não alegar prova de todas as condições).
- `go build ./...` e `go vet ./internal/domain/job/entity`: exit 0.
- `gofmt -l .`: sem saída.
- golangci-lint v2.13.2 no pacote: `0 issues`, exit 0.
- Testes documento/vo/arquitetura com race: PASS; coberturas preservadas.
- `go test ./... -count=1`: ainda falha em job/service (porta repository
  ausente) e infra/storage (ClienteS3 e outros tipos ainda ausentes).
  Vet global também não passa nesses componentes. Nenhum teste foi pulado.

Sem dependências novas, banco/container ou commit nesta rodada. Campos da
entidade são exportados; cópias de entrada não a tornam totalmente imutável.
Validação estrutural de Resultado não sanitiza seu conteúdo: segue interno.

Próximo ciclo: portas e serviços de job com dono, fronteira interna, CAS e
idempotência, corrigindo specs segundo auditoria salva. Não repetir investigação
histórica nem declarar essas garantias resolvidas pela entidade. Storage e
integração ponta a ponta ainda pendentes; F1 aberta.

Última quota consultada: **69% em 5h, 56% semanal**; agentes encerrados.

## Entrega mais recente — migration de dono (2026-09-14)

Encerrada preventivamente com **85% de uso em 5h e 45% semanal**. Contexto salvo
e agentes encerrados, respeitando a pausa próxima de 90% solicitada pelo usuário.

Passo 6 do Ciclo B concluído: `00002_documento_dono.sql` adiciona sessão, preenche
órfãos antes dos CHECKs, exige exatamente um dono não zero e cria índice parcial.
Sem recriar tabelas. Down documenta perda de identidade da sessão; novo Up não
recupera sessões anteriores. Nada aplicado em banco do projeto/usuário.

Testador capturou RED real, codador entregou SQL, validador encontrou assert
incorreto de índice (pg_get_indexdef por coluna omite DESC), testador corrigiu
com indoption; validador e segurança APROVADOS. Principal executou GREEN:

- `go test -tags=integration ./migrations -race -count=1 -v`: PASS, 13.157s;
  lifecycle Up/Down/Up e rollback de usuário legado zero. Verifica dados,
  OIDs/FKs/índices e dependentes, INSERT/UPDATE inválidos e backfill.
- `go build ./...`: exit 0.
- `go vet -tags=integration ./migrations`: exit 0; `gofmt -l .` sem saída.
- Testes documento/vo/arquitetura com race: PASS após novas dependências.
- `go vet ./...` segue com falhas conhecidas de job/storage incompletos.
  Lint completo não executado neste recorte.

Dependências de integração adicionadas: Testcontainers v0.44.0, Goose v3.28.0
e driver pgx (versões resolvidas em go.mod/go.sum). Sem migração da 00001.
Podman bridge rootless teve erro de limpeza; slirp4netns local passou com
cleanup estrito. Containers e volumes descartáveis removidos; serviços
temporários encerrados. Como repetir: `backend/migrations/README.md`.

Próximo: atualizar specs de job/entity com base na auditoria já salva, depois
implementar a cadeia job/storage/data/fila/worker/HTTP/frontend. O Ciclo B tem
seus artefatos de domínio/migration entregues; fiação worker e adaptadores reais
ainda pendentes. A2/A3 continuam bloqueantes antes do upload/parser. F1 aberta.

## Entrega atual — serviços de documento com dono (2026-09-14)

Este checkpoint substitui os estados históricos abaixo. **Build global voltou
a passar.** Repositório e serviço públicos agora exigem `vo.Dono`; não existe
listagem anônima compartilhada. Registro revalida a entidade e recusa CDM/preview
antecipados. Preview deriva do ID autorizado. Terceiros e IDs inexistentes
produzem o mesmo erro público na leitura.

`documento/processamento.ServicoInterno` concentra as transições do worker;
`DocumentoInternoRepo` exige status anterior na escrita de status e CDM (CAS).
`internal/arquitetura/fronteira_test.go` barra imports diretos e transitivos da
API/rotas para processamento. A fiação real do worker e o adaptador persistente
ainda não existem; os testes de CAS desta entrega usam fake, não banco.

Agentes desta entrega, sem subagentes: testador (RED), codador (GREEN), validador
(APROVADO) e segurança (APROVADO no delta; nenhum achado). Coordenação executou
novamente os testes e acrescentou o gate de arquitetura, revisado pelos auditores.

Provas executadas em `backend`, com `GOCACHE=/tmp/formatador-ciclo-b-go-cache`:

- `go build ./...`: exit 0.
- `go test ./internal/domain/documento/... ./internal/domain/vo/... ./internal/arquitetura/... -race -count=1 -cover`: passou.
- Cobertura: service 95,3%; processamento 96,8%; entity 98,3%; vo 100%.
- `go vet ./internal/domain/documento/... ./internal/domain/vo/... ./internal/arquitetura/...`: exit 0.
- `gofmt -l .`: sem saída.
- `go test ./... -count=1` e `go vet ./...`: falham nos pacotes incompletos
  job/entity, job/service e infra/storage (tipos/implementações ausentes).
- `golangci-lint` e integração com Postgres não executados neste recorte.

**Próximo:** migration `00002_documento_dono.sql` via ALTER TABLE, com backfill
antes do CHECK, testes reais de ida/volta e preservação de dados/FKs. Depois,
corrigir specs job/storage conforme `docs/auditoria-specs-f1.md`, sem repetir a
investigação já concluída. A2 (bytes efetivamente recebidos) e A3 (validação real
do ZIP) continuam pré-requisitos antes de habilitar upload/parser. F1 NÃO fechada.

Contrato desta entrega: `docs/ciclo-b-contrato.md`. Nenhum commit ou staging novo.
Pausa preventiva após a entrega: **79% da janela de 5h, 28% semanal** na última
consulta. Agentes desta rodada encerrados; não iniciar migration sem nova
consulta de quota. Respeitar a pausa próxima de 90% pedida pelo usuário.

## Retomada no Codex — pausada por limite de uso (2026-09-13)

Última consulta: **89% da janela de cinco horas, 14% semanal**. Orquestrador e
descendentes encerrados conforme limite solicitado pelo usuário. Sem entrega
nova aprovada de repository/service. Build continua falhando em
`documento/service/documento.go:114` pelo contrato antigo `*uuid.UUID`.
Container PostgreSQL descartável removido na pausa. Ver `docs/retomada-f1.md`.

- Specs herdadas de job/storage auditadas novamente por dois agentes separados;
  resultados persistidos em `docs/auditoria-specs-f1.md`. Reprovadas, aguardam
  correção dos contratos antes da implementação; não repetir auditoria histórica.
- Ciclo B de documentos pendente após interrupção dos agentes: repositório, serviço de usuário,
  serviço interno separado, testes de autorização/arquitetura e migration.
  Ainda sem veredito final; manter a entidade com `vo.Dono`.
- Ambiente consultado nesta retomada: nenhum container ou imagem antigo existia.
  Foi baixado PostgreSQL 16 e criado `formatador-migracao-f1-20260913`, descartável,
  sem rede, portas ou volumes do projeto, para testar a migration. Remover ao final.
- Limite de uso consultado no início: 9% da janela de cinco horas, 1% semanal.
  Consultar novamente entre tarefas e salvar contexto/parar ao aproximar de 90%.

**Como verificar o estado de verdade:** `cd backend && go build ./... && go test ./...`

---

## Resumo em uma linha

F0 concluída. F1 em andamento: `domain/vo` e `domain/documento/entity` verdes.
`domain/documento/service` foi implementado, reprovado na auditoria por ausência
de controle de dono, e está **em retrabalho (rodada 1)** com a spec autorizada a
mudar — ver "Decisões do usuário".

---

## Fases

| Fase | Estado |
|---|---|
| F0 — Fundação | ✅ concluída e verificada |
| F1 — Ingestão e preview | 🔄 em andamento |
| F2 — Parser e CDM | ⬜ |
| F3 — Motor de formatação | ⬜ |
| F4 — Citações e referências | ⬜ |
| F5 — Fallback de LLM | ⬜ |
| F6 — Tabelas/figuras e revistas reais | ⬜ |
| F7 — Auth e formatos extras | ⬜ |

---

## DECISÕES DO USUÁRIO (2026-09-13) — bloqueio destravado

A tarefa `domain/documento/service` foi implementada, ficou verde nos testes
(91,9%) e **foi reprovada** pelo `seguranca` (2 CRÍTICOS, 3 ALTOS) e pelo
`validador` (3 bloqueantes). Raiz única: **a especificação congelada não tinha
noção de dono**. O usuário decidiu:

### 1. Modelo de dono: sessão anônima opaca por cookie

O upload anônimo **continua existindo na F1**, mas "anônimo" deixa de ser escopo
global. Todo visitante recebe no primeiro acesso um **identificador de sessão
opaco** (UUID aleatório) em cookie `httpOnly` + `Secure` + `SameSite=Strict`.

Um documento tem **dono**, e o dono é `usuario_id` (login, F7) **ou** `sessao_id`
(agora). Deve existir um tipo de domínio (`vo.Dono` / `entity.Proprietario` — a
forma exata cabe ao `investigador` justificar) que encapsule "de quem é isto", em
vez de espalhar dois ponteiros nulos por todas as assinaturas. **Não negociável:
toda operação sobre um documento recebe a identidade do solicitante e compara com
o dono.** Na F7 isso vira só uma ligação sessão→usuário, sem redesenho.

Exige **migration nova** para a coluna do dono, com `-- +goose Down` funcional.

### 2. Alteração da spec: AUTORIZADA

`internal/domain/documento/service/documento_test.go` deve ser alterado nos
quatro pontos: (a) identidade do solicitante em `Obter` e nas transições;
(b) `ListarDoUsuario` sem escopo anônimo global; (c) `RegistrarPreviewPDF(ctx,
id)` sem parâmetro de chave — a chave deriva do `id`; (d) `DefinirCDM` levando
`statusAtual`. Aqueles testes foram escritos por um agente em sessão anterior e
codificaram uma falha de desenho; corrigi-los é o ciclo funcionando.

### 3. A2 e A3 — pré-requisito bloqueante da entrega da F1

- **A2:** o tamanho **nunca** vem do cliente. A rota limita o corpo com
  `http.MaxBytesReader` e o tamanho gravado é o **medido** durante a escrita no
  storage. `DadosIngestao.TamanhoBytes` deixa de ser entrada confiável.
- **A3:** ingestão em dois tempos — prefixo valida cedo para rejeitar barato, e
  **`vo.ConferirPacoteDocx` roda contra o conteúdo real** (reader limitado ou o
  objeto já gravado) antes de enfileirar o job de preview. Limite de razão de
  descompressão, de número de entradas e de tamanho total descomprimido são
  **obrigatórios**: zip bomb e zip slip não podem chegar na F2.

### 4. Achados médios no escopo da tarefa

M1 (compare-and-set no `DefinirCDM`), M2 (`Registrar` revalidando invariantes),
B3 (regra de nome unificada), M3 (higienização barrando `unicode.Cf`, RTLO,
zero-width) e B1 (CDM com `json.Valid` e teto de bytes) entram todos nesta tarefa.

---

## FICHA DO INVESTIGADOR — pronta, com 1 pergunta travando

O `investigador` entregou o desenho completo do dono (tipo, spec, migration,
prompt do codador). Resumo do que ficou decidido tecnicamente:

- **`vo.Dono`** em `internal/domain/vo/dono.go`: struct de campos privados
  (`especie EspecieDono` + `id uuid.UUID`), **comparável** com `==`, imutável,
  construível só pelos construtores. Espécies: `sessao`, `usuario`, `sistema`.
- **`==` NÃO é autorização** (dois zero values são iguais). Autorização só via
  `PodeAcessar(recurso Dono) bool`, que é false se qualquer lado for vazio.
  `Igual` existe à parte, sem privilégio, para a listagem.
- **`String()` devolve só a espécie, nunca o UUID** — um `Dono` caído em log não
  pode virar correlacionador de sessão (regra 7 do CLAUDE.md).
- **Terceiro recebe `*ErroNaoEncontrado` idêntico ao de ID inexistente**, nunca
  `ErroProibido` — 403 confirma que o ID existe. A mensagem tem que bater byte a
  byte, e o teste compara os dois `err.Error()`.
- **`RegistrarPreviewPDF(ctx, solicitante, id)` deixa de receber chave**; ela é
  derivada de `vo.NovaChavePreviewPDF(id)`. O A1 morre estruturalmente.
- **Migration é `ALTER TABLE`, não recriação**: `usuario_id` já existe em
  `00001_esquema_inicial.sql:34` com FK, e `documentos` é referenciada por `jobs`.
  `00002_documento_dono.sql` adiciona `sessao_id`, faz backfill dos órfãos com
  `gen_random_uuid()` **antes** do CHECK, aplica
  `CHECK (num_nonnulls(usuario_id, sessao_id) = 1)` e cria índice parcial.

### Decisões que EU (orquestrador) tomei

1. **Autorizado alterar também `entity/documento_test.go`.** Não é contornável:
   `NovoDocumento` muda de assinatura e há 21 call sites. A autorização do
   usuário para "corrigir spec herdada" cobre o caso.
2. **Tarefa quebrada em 2 ciclos**, alinhado ao modo de operação: **Ciclo A** =
   `vo/dono.go` + `vo/dono_test.go` (isolado, fecha verde sozinho, não quebra
   nada); **Ciclo B** = entity + repository + service + migration + as 2 specs.

### Identidade do worker — RESOLVIDA (opção ii)

**O worker não tem cookie de sessão.** `IniciarAnalise`/`ConcluirAnalise`/
`MarcarFalha` são chamados pelo worker de job, que não tem identidade de dono. Se
toda operação exige `solicitante vo.Dono`, o worker precisa de uma. As opções:

- **(i) `vo.DonoSistema()`** — proposta do investigador. Espécie `sistema`,
  `PodeAcessar` sempre true, nunca produzível por `ParaDono` (não vem do banco),
  não é o zero value, recusada em `ListarDoDono`. **Risco:** é uma função pública
  sem erro; qualquer código — inclusive um handler HTTP por engano — pode chamá-la
  e obter acesso universal. O bypass vira um *valor* que circula.
- **(ii) Fronteira no tipo, não no valor** — o serviço expõe as transições de
  ciclo de vida por um tipo separado (ex.: `ServicoInterno`, construído
  explicitamente pelo worker), e o `Servico` voltado ao usuário só tem operações
  que exigem dono real. O bypass deixa de ser um valor circulante e passa a exigir
  construção deliberada. Mais verboso, sem chave-mestra solta.

**Recomendação do orquestrador: (ii).** Uma identidade com privilégio universal
disponível como função pública sem erro é exatamente a forma de furo que a
auditoria acabou de encontrar. Mas é decisão do usuário.

### Pendências menores levantadas, não implementar sem resposta

- **Aspas em nome de arquivo:** a auditoria M3 citou aspas; o investigador
  recomenda **não** barrá-las (são legítimas; escape é de quem renderiza). Barrar
  só o invisível. Decisão de produto se quiser o contrário.
- **`json.Valid` aceita `null`, `123` e `"texto"`.** Se o CDM tiver que ser
  objeto, exigir também `{` após `bytes.TrimSpace`. Precisa de decisão.

---

### RESPOSTAS DO USUÁRIO (2026-09-13, rodada 2)

**1. Identidade do worker: opção (ii) — fronteira no TIPO, não no valor.**
**`vo.DonoSistema()` e a espécie `sistema` estão DESCARTADOS.** Motivo aceito: um
bypass exposto como função pública sem erro vira um *valor que circula*, e o
acesso indevido passa a exigir só uma linha distraída. Em vigor:

- As transições de ciclo de vida saem do `Servico` do usuário e vão para um
  **`ServicoInterno`, construído explicitamente na fiação do worker em
  `cmd/worker`**. O `Servico` voltado ao usuário não expõe transição nenhuma.
- **Exigência nova do usuário:** a convenção vira **portão automático** — um
  **teste de arquitetura** que falha se qualquer pacote sob `internal/rotas/**`
  importar o `ServicoInterno` (via `go/packages` ou `go list -deps`). Justificativa
  do usuário: "convenção que não é verificada apodrece"; tem que sobreviver à F3 e
  à F7, quando ninguém lembrar por que existe. É trabalho do `testador`.
- Consequência na ficha: `ListarDoDono` não precisa mais recusar dono sistema, e
  `vo.Dono` tem só duas espécies (`sessao`, `usuario`).

**2. Aspas em nome de arquivo: NÃO barrar.** Escapar é de quem renderiza; barrar
na ingestão quebra usuário honesto. Barrar só invisível/enganoso: C0/DEL,
`unicode.Cf`, zero-width, U+2028/U+2029 e sobretudo os de reordenação
bidirecional (RTLO U+202E), que existem para disfarçar extensão.

**3. CDM: `json.Valid` NÃO basta — exigir objeto.** `null`, `123` e `"texto"`
passam e nenhum é CDM. Exigir `{` após `bytes.TrimSpace`, **mantendo o teto de
bytes**. A mensagem de erro não pode ecoar o conteúdo recebido.

### Ciclos da tarefa

- **Ciclo A — `vo.Dono`: ✅ CONCLUÍDO E AUDITADO** (ver abaixo).
- **Ciclo B — passo 1 (`entity`): ✅ FECHADO VERDE** em 2026-09-13.
  `documento/entity` tem `Dono vo.Dono`, `NovoDocumento` recebe o dono, e a
  validação checa `Vazio()`. `go test ./internal/domain/documento/entity/ -race
  -count=1 -cover` → `ok ... coverage: 98.3% of statements`.
  **Consequência: `documento/service` parou de compilar** (`documento.go:114`
  ainda passa `*uuid.UUID` para `entity.NovoDocumento`). É o passo 3; não reverta
  a entidade para "consertar".
- **Ciclo B — passos 2 a 6: PENDENTES.** A auditoria das specs herdadas de
  `job/*` e `infra/storage` (regra 13) foi **iniciada e interrompida** no meio,
  com o `seguranca` ainda rodando — o resultado se perdeu, refaça do zero.
- **Trabalho pausado a pedido do usuário em 2026-09-13.** Ponto de retomada
  detalhado em `docs/retomada-f1.md` — leia-o primeiro.
- **Ciclo B** — entity + repository + service + `ServicoInterno` + migration
  `00002_documento_dono.sql` + as duas specs + o teste de arquitetura do portão.

---

## CICLO A CONCLUÍDO — `vo.Dono` (2026-09-13)

**Arquivos:** `backend/internal/domain/vo/dono.go` + `dono_test.go` (ambos novos).

**Provas (saída real):**
- `go test ./internal/domain/vo/ -race -count=1 -cover` → `ok ... coverage: 100.0% of statements`
- `gofmt -l .` limpo · `go vet ./internal/domain/vo/` limpo · `go build ./...` ok
- `golangci-lint run ./internal/domain/vo/` → `0 issues. exit=0`

**Auditorias:** `seguranca` APROVADO (0 críticos, 0 altos) e, após o polimento,
reauditoria APROVADO (0 críticos, 0 altos, 0 médios). `validador` APROVADO, sem
bloqueantes.

### Contrato final (difere da ficha do investigador — atenção no Ciclo B)

```go
type EspecieDono string
const (EspecieSessao EspecieDono = "sessao"; EspecieUsuario EspecieDono = "usuario")
func (e EspecieDono) Valida() bool          // whitelist exata; "SESSAO" e "sessao " são inválidas

type Dono struct{ /* privados: especie, id */ }  // COMPARÁVEL com ==

func NovoDonoSessao(uuid.UUID) (Dono, error)
func NovoDonoUsuario(uuid.UUID) (Dono, error)
func ParaDono(usuarioID, sessaoID *uuid.UUID) (Dono, error)

func (d Dono) Vazio() bool        // !especie.Valida() || id == uuid.Nil
func (d Dono) PodeAcessar(recurso Dono) bool   // ÚNICO caminho de autorização
func (d Dono) Igual(outro Dono) bool           // valor/mapa/teste — NUNCA autorização
func (d Dono) Especie() EspecieDono
func (d Dono) ParaColunas() (usuarioID, sessaoID *uuid.UUID)  // sem aliasing
func (d Dono) String() string    // só a espécie
func (d Dono) GoString() string  // idem, para %#v
```

**`ID()` NÃO EXISTE — foi removido de propósito** (decisão do orquestrador):
expor o UUID cru habilita `a.ID() == b.ID()`, comparação sem espécie, que é
exatamente a colisão que o discriminador impede. Há teste travando a ausência
(`TestDonoNaoExpoeIdentificadorCru`). Não reintroduza. Quem precisa do UUID usa
`ParaColunas`. **Não existe `DonoSistema` nem espécie `sistema`.**

### Fatos medidos que valem para o resto do projeto

- **`%#v` NÃO passa por `Stringer`.** Campo privado não protege log: sem
  `GoStringer`, o `fmt` imprime os campos e despeja o UUID em bytes hex. Foi um
  vazamento real, fechado por `GoString()`. Como o `sessao_id` é o conteúdo do
  cookie, seria um bearer vivo em log. **Todo VO que carregue identificador
  sensível precisa de `String()` E `GoString()`.**
- **`%x`/`%X` NÃO vazam** — medido: `%x` de um `Stringer` hexa-codifica a string
  retornada (`646f6e6f2873657373616f29` = `"dono(sessao)"`), não os campos. O
  `fmt` aplica `Stringer` em `v, s, q, x, X`. **0 de 7 verbos vazam**; não é
  preciso `fmt.Formatter`. (Suposição inicial do orquestrador de que havia dívida
  aqui estava ERRADA — medir derrubou o raciocínio plausível.)
- **`json.Marshal(Dono{})` → `{}`**, fixado como contrato de regressão. O
  `staticcheck` acusa SA9005 nisso; está suprimido com `//nolint` justificado em
  `dono_test.go:735`, porque a ausência de campos exportados é o contrato sob
  teste. **Não "conserte" isso adicionando `MarshalJSON`.**
- **Cobertura de statements ≠ cobertura de condições.** `Vazio()` é um OR cujo
  segundo termo nunca era exercitado apesar dos 100%. Fechado montando estados
  forjáveis por literal dentro do pacote.

### Dívida registrada (BAIXO, pré-existente) — levar ao Ciclo B

`backend/internal/domain/vo/dono_test.go:25-38`, helper `idDeDono`: colapsa o par
de `ParaColunas` com um OR ("o que não for nil"), descartando a espécie. É
test-only e correto no contexto, mas é **o molde exato do bug de confusão de
identidade** e os repositórios da F1 em diante vão escrever algo parecido.
Ação: advertência no comentário do helper; e os repositórios devem passar o par
direto ao SQL, sem intermediário que colapse.

---

## REGRAS NOVAS A APLICAR NO `CLAUDE.md` (pendente do usuário)

> O orquestrador **não edita `CLAUDE.md`** por instrução de agente — só o usuário
> aplica. Texto pronto para colar nas "Regras de código":

**11. VO que carregue identificador sensível precisa de `String()` E `GoString()`.**
`%#v` não passa pelo `Stringer`: sem `GoStringer`, o `fmt` imprime os campos
privados. Campo privado **não** protege log. Quando o identificador é um
`sessao_id` de cookie, o que vaza é **credencial viva**, não só privacidade.
Vale para a F7 inteira (token de sessão, refresh token, hash de senha).

**12. Em decisão composta, exija caso por termo, não por linha.** Cobertura de
*statements* não é de *condições*: um OR cujo segundo termo nunca é exercitado
marca 100% e ninguém quebra teste ao remover metade da condição. Se o `testador`
reportar cobertura alta em código com condição composta, desconfie antes de
aprovar.

### Regra operacional do Ciclo B (decidida com o usuário)

**`ParaColunas` tem que ser lido em PAR — espécie junto com o id.** Colapsar com
OR ("o que não for nil") é o molde da confusão de identidade. Em test helper é
tolerável; **em código de produção é BLOQUEANTE**. Repositórios passam o par
direto ao SQL, sem intermediário que colapse.

---

## REGRA PERMANENTE DO CICLO (nova, vale a partir de agora)

> **Especificação herdada é auditada antes de ser implementada.** Quando os
> testes vierem prontos de uma sessão anterior, o `validador` e o `seguranca` os
> auditam **ANTES** de o `codador` escrever qualquer linha contra eles. Foi
> exatamente o passo pulado que produziu os dois CRÍTICOS acima.
>
> Vale já para `job/entity`, `job/service` e `infra/storage`, cujos testes vieram
> da mesma sessão e **provavelmente carregam o mesmo vício de ausência de dono**.

---

## F1 — detalhe por tarefa

### Verde (implementado e testado)

| Pacote | Cobertura | Observação |
|---|---|---|
| `internal/domain/vo` | 100,0% | `chavestorage.go`, `formatoarquivo.go`, **`dono.go` (novo, auditado)** |
| `internal/domain/documento/entity` | 97,0% | `documento.go`, `status.go` |
| `internal/domain/documento/repository` | — | interface `DocumentoRepo`, sem teste (só interface); auditada, ver M1 |
| `internal/infra/storage` | 69,7% | `s3.go` — `ClienteS3`, `Verificador`. Caminho pré-rede 100%; caminho SDK real provado por 6 testes de integração contra MinIO, **executados e verdes** (18,095s) |

### Implementado, testes verdes, auditoria REPROVADA

| Pacote | Situação |
|---|---|
| `internal/domain/documento/service` | EM RETRABALHO (rodada 1). `documento.go` (232 linhas). `go test ./internal/domain/documento/... -race -count=1` verde, 91,9%. `gofmt` e `go vet` do pacote limpos. **golangci-lint ainda NÃO foi rodado nesta tarefa.** Reprovado por segurança e arquitetura — ver "Bloqueio aberto". |

### Vermelho (teste escrito, implementação pendente)

| Pacote | Arquivos de teste | O que falta |
|---|---|---|
| `domain/job/entity` | `job_test.go`, `status_test.go`, `tipo_test.go` | `Job`, `TipoJob`, `StatusJob` (hoje `undefined: TipoJob` quebra o build do pacote) |
| `domain/job/repository` | — | o pacote **não existe**; `job/service/job_test.go` já o importa |
| `domain/job/service` | `job_test.go` | `Servico`, `NovoServico`, `DadosNovoJob` |

Consequência: `go vet ./...` e `go test ./...` **não fecham verde hoje** —
quebram só em `internal/domain/job/service` (`job/entity` e `job/criacao` e
`job/consulta` já estão verdes; `job/repository` é só interface). É o estado
vermelho esperado do TDD, não regressão.

### Não iniciado

`internal/data/contracts`, `internal/data/postgres`, `internal/infra/fila`,
`internal/infra/pdfconv`, `cmd/worker` real, rotas de documento/job/preview,
`application/web`, verificadores de prontidão, rate limit de upload, e todo o
frontend da fase (upload, lista, preview com PDF.js).

### Pendência de `vo` — RESOLVIDA

O ramo `default: return false` de `segueGramaticaCanonica`
(`vo/chavestorage.go:113-114`) **já estava coberto**: os casos
`documentos/<uuid>/original.exe` e `documentos/<uuid>/relatorio.docx` existem em
`chavestorage_test.go:140-141`. Provado por perfil de cobertura (contagem 2 no
ramo). `internal/domain/vo` está em 100,0%.

---

## Decisões tomadas que não estão no plano

- **Nomes de pasta em português vencem o plano.** O `docs/plano.md` usa `document`,
  `formatter`, `queue`, `routes`; o código real usa `documento`, `formatador`,
  `fila`, `rotas`. Siga o código.
- **Validação de chave de storage mora só no VO.** Estava duplicada em
  `vo.ChaveStorage` e `storage.ValidarChave`; `internal/infra/storage/chave_test.go`
  foi removido e a regra ficou em `vo`. O `infra/storage` recebe o VO já validado.
- **`ErroValidacao.Acrescentar` não devolve nada** (era fluente e devolvia algo que
  satisfaz `error`, o que confundia leitor e linter). Use `TemCampos()`.
- **`POSTGRES_DSN` não tem default.** Um default apontando para localhost é
  armadilha em produção, e tornava a validação de configuração morta.
- **`misspell` removido do `.golangci.yml`** — acusava português como erro de inglês.
- **Revista Qualis A1 da F6: Geousp.**
- **Na tarefa de `service` pulou-se o `investigador` e o passo "teste que falha"**,
  porque a suíte já existia e era a especificação. Foi a exceção certa; o custo
  apareceu depois: ninguém auditou a *especificação* antes de codar contra ela, e
  ela é a origem dos achados críticos. **Lição: quando a spec vem pronta de uma
  sessão anterior, mande auditá-la ANTES do codador.**

## Ambiente

- Runtime de containers: **`podman compose`** (o Makefile detecta sozinho). Não há
  `docker` nesta máquina.
- Imagens no compose são **totalmente qualificadas** — o podman aqui não tem
  `unqualified-search-registries`, então nome curto não resolve.
- **MinIO vem do `quay.io`**, não do Docker Hub, que passou a negar a tag `latest`.
- De pé na última verificação: `formatador_postgres_1`, `formatador_libreoffice_1`,
  `formatador_jaeger_1`, `formatador_prometheus_1`. MinIO corrigido no arquivo mas
  **ainda não subido** — é preciso um `make up` e confirmar que o bucket
  `documentos` nasce sem acesso anônimo.
- **`golangci-lint` não é binário instalado**, é invocado sob demanda:
  `cd backend && go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...`
  É o portão de lint de toda tarefa; `build`/`vet`/`gofmt` sozinhos não bastam.
  A primeira execução baixa e compila o linter e demora alguns minutos.
  **Hoje ele não roda limpo no repositório inteiro**, porque `domain/job/*` e
  `infra/storage` não compilam (estado vermelho do TDD).

## Modo de operação do orquestrador

A sessão já morreu três vezes por limite de uso. Em vigor:
**um subagente por vez** (auditoria em sequência, não em paralelo), **uma tarefa
por vez**, e **este arquivo atualizado antes de começar a próxima**.

## Git

Nada commitado ainda. A árvore inteira está staged (`git add -A` foi rodado no fim
da F0). Não há branch além de `main`.

---

## Próximo passo concreto (atualizado 2026-09-18)

O bloqueio de dono/IDOR do `documento/service` já foi destravado (ver decisões
do usuário de 2026-09-13 acima) e `infra/storage` acabou de fechar verde. O que
resta hoje, sem dependência entre si:

1. **`domain/job/service`** — RED legado, spec já auditada
   (`docs/auditoria-specs-f1.md`). Retomar com investigador → testador → codador
   → auditoria em paralelo, igual ao ciclo que fechou `job/criacao`/`job/consulta`.
2. **`internal/data/contracts` + `internal/data/postgres`** — pacotes vazios,
   nenhuma linha ainda. Porta de acesso a dados para os serviços de domínio já
   prontos (`documento/service`, `job/criacao`, `job/consulta`).
3. Rodar `go test -tags integration ./internal/infra/storage/... -race -v` numa
   máquina com Docker/podman — não foi possível neste ambiente (sandbox sem
   `docker`); é o achado 2 da auditoria (operação/chave/bucket/validade efetivos
   da URL assinada) ainda sem prova de execução real.
4. Só então `infra/fila` → `infra/pdfconv` → `cmd/worker` real → rotas →
   `application/web` → frontend.

**Critério de pronto da F1:** subir um DOCX pela tela, o job de preview rodar, e
ver o PDF renderizado no navegador — com `make test` e `make lint` verdes e
cobertura de `internal/domain/` ≥ 80% (`backend/scripts/verificar_cobertura.sh`).
