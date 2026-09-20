# Mapa de módulos — índice de busca

**Este arquivo é para GREP, não para leitura integral.** Cada módulo tem uma
linha `## MOD:` com palavras-chave. Ache o módulo, vá direto no arquivo.

```sh
grep -i "dono\|idor" docs/mapa-modulos.md      # acha o módulo
grep -rn "PodeAcessar" backend/internal/         # acha o uso real
```

Regra: **símbolo aqui é símbolo que existe**. Se não achar no código, o mapa
está desatualizado — corrija o mapa, não invente o código.

Legenda de estado: ✅ verde com prova · 🔴 TDD vermelho proposital · ⬜ vazio

---

## MOD: dono-autorizacao ✅
**keywords:** Dono, IDOR, autorizacao, ownership, sessao, usuario, terceiro, PodeAcessar, solicitante

- `backend/internal/domain/vo/dono.go` — `Dono`, `EspecieDono`, `NovoDonoSessao`, `NovoDonoUsuario`, `ParaDono`, `PodeAcessar`, `Igual`, `Vazio`, `Especie`, `ParaColunas`, `String`, `GoString`
- **`==` NÃO autoriza. Só `PodeAcessar(recurso)` autoriza.**
- `ParaColunas()` devolve `(usuarioID, sessaoID)` com exatamente um não-nulo → CHECK `documentos_dono_exclusivo`
- Filtro de dono vai no **WHERE do SQL**, nunca busca-e-compara: `backend/internal/data/postgres/documento.go`, `job.go`
- Terceiro recebe o **mesmo erro** de inexistente, sem diferença de latência

## MOD: documento-dominio ✅
**keywords:** Documento, ingestao, upload, nome de arquivo, CDM, status, transicao

- `backend/internal/domain/documento/entity/documento.go` — `Documento`, `NovoDocumento`, `HigienizarNomeArquivo`, `ValidarCDM`, `Validar`, `MIME`, `TemPreview`, `IniciarAnalise`, `ConcluirAnalise`, `IniciarFormatacao`, `ConcluirFormatacao`, `MarcarFalha`, `TamanhoMaximoNome`, `TamanhoMaximoCDMBytes`
- `entity/status.go` — `Status`, `Valido`, `PodeTransitarPara`
- `service/documento.go` — `Servico`, `DadosIngestao`, `ValidarIngestao`, `PrepararIngestao`, `Registrar`, `Obter`, `ListarDoDono`, `RegistrarPreviewPDF`, `TamanhoMaximoPadraoBytes`
- `processamento/servico.go` — `ServicoInterno` (**worker**, sem dono, com CAS): `IniciarAnalise`, `MarcarFalha`, `ConcluirAnalise`
- `repository/documento.go` — `DocumentoRepo` (público, com dono) e `DocumentoInternoRepo` (worker, CAS)

## MOD: job-dominio ✅
**keywords:** Job, fila, idempotencia, chave, retry, progresso, cancelar, CAS

- `entity/job.go` — `Job`, `NovoJob`, `Iniciar`, `DefinirProgresso`, `Concluir`, `Falhar`, `Cancelar`, `Reenfileirar`, `Terminal`, `Duracao`
- `entity/status.go` — `StatusJob`, `Terminal`, `PodeTransitarPara` · `entity/tipo.go` — `TipoJob`, `ExigeRuleset`
- `criacao/servico.go` — `Servico`, `DadosNovoJob`, `Criar` (idempotente, autoriza pelo documento)
- `consulta/servico.go` — `Servico`, `Obter`, `ListarDoDocumento`
- `execucao/servico.go` — `ServicoInterno` (**worker**, sem dono): `Iniciar`, `Concluir`, `Falhar`
- `repository/{criacao,consulta,execucao}.go` — `CriacaoJobRepo` (`InserirOuObter`), `ConsultaJobRepo`, `ExecucaoJobRepo` (`Salvar` com CAS)
- 🔴 `job/service/job_test.go` — **spec legada superada**, SHA `654be0fd…3e16dc`. Não apagar, não criar `JobRepo` só para compilar.

## MOD: persistencia ✅
**keywords:** postgres, pgx, repositorio, SQL, transacao, isolamento, InserirOuObter, contracts

- `backend/internal/data/contracts/contratos.go` — `GerenciadorDados` (**fachada única**, regra 6)
- `backend/internal/data/postgres/conexao.go` — `Gerenciador`, `NovoGerenciador`, `Documentos`, `DocumentosInternos`, `JobsConsulta`, `JobsCriacao`, `JobsExecucao`, `Fechar`, **`Nome`/`Verificar`** (já é Verificador de prontidão)
- `documento.go` — `RepositorioDocumento`, `erroLinhaCorrompida` (linha corrompida = `ErroAplicacao`, **nunca** `ErroValidacao`/HTTP 400)
- `job.go` — `RepositorioJob`, `InserirOuObter` (`FOR SHARE` + `ON CONFLICT DO NOTHING RETURNING` + releitura)
- ⚠️ **`InserirOuObter` exige READ COMMITTED.** Sob REPEATABLE READ a releitura não vê a linha concorrente e a idempotência vira erro.
- Só `internal/data` pode importar pgx — teste em `internal/arquitetura/fronteira_test.go`

## MOD: storage ✅
**keywords:** S3, MinIO, bucket, URL pre-assinada, presigned, chave, upload, download

- `backend/internal/infra/storage/s3.go` — `ClienteS3`, `NovoClienteS3`, `URLPreAssinada`, `Salvar`, `Obter`, `Verificador`, `NovoVerificador`, `ValidadePadraoURL` (15m), `ValidadeMaximaURL` (1h)
- URL assinada é **só GET** (`PresignGetObject`). Não existe caminho para assinar PUT.
- `Salvar` usa `manager.Uploader` (corpo de rede é **não-seekable**; `PutObject` cru falha no hash do SigV4). `io.LimitReader` é controle de segurança, não gordura.
- `ContentLength` **não** é enviado: o SDK o deriva dos bytes lidos.
- Chave revalidada com `vo.MotivoChaveInsegura`, **nunca** `vo.ParaChaveStorage`
- 💸 dívida: `feature/s3/manager` deprecado; `transfermanager` é pré-1.0 (v0.4.x)

## MOD: chave-storage ✅
**keywords:** ChaveStorage, path traversal, chave seguro, caminho

- `backend/internal/domain/vo/chavestorage.go` — `ChaveStorage`, `NovaChaveOriginal`, `NovaChavePreviewPDF`, `ParaChaveStorage`, `MotivoChaveInsegura`, `TamanhoMaximoChave`
- `ParaChaveStorage` exige gramática canônica `documentos/<uuid>/<arquivo>`; `MotivoChaveInsegura` é só a regra de segurança. Não confundir.

## MOD: formato-arquivo ✅
**keywords:** MIME, magic bytes, docx, deteccao, ConferirPacoteDocx, A3

- `backend/internal/domain/vo/formatoarquivo.go` — `FormatoArquivo`, `MIME`, `Extensao`, `FormatoPorMIME`, `DetectarFormato`, `ConferirPacoteDocx`, `TamanhoPrefixoDeteccao`
- ⚠️ `ConferirPacoteDocx(partes []string)` **só confere nomes de partes** — não mede descompressão nem valida XML. Achado A3 segue aberto.

## MOD: conversao-pdf ✅
**keywords:** pdfconv, PDF, LibreOffice, unoserver, sidecar, conversor, docx para pdf

- `backend/internal/infra/pdfconv/conversor.go` — `Cliente`, `NovoCliente`, `ConverterParaPDF`, `Verificador`, `NovoVerificador`, `CaminhoConversao` (`/converter`), `CaminhoSaude` (`/saude`), `TipoConteudoDocx`, `TamanhoMaximoPDFBytes` (64 MiB)
- Sidecar próprio: `deploy/Dockerfile.libreoffice` + `deploy/pdfconv-handler.py` (551 MB, Debian 13 / Python 3.13)
- Integração real verde: conversão DOCX→PDF contra o sidecar, `ok 247s`, unitário 94,2%
- ⚠️ **execução FRIA reprova por starvation** (build do LibreOffice compete com os casos de timing de 5s). Builde a imagem ANTES de rodar a integração. Não afrouxe os timeouts.
- Contrato é **nosso**: corpo cru, **sem multipart** (sem multipart não existe filename → regra 7 vira impossível por forma, não por disciplina)
- ⚠️ `compose` aponta para imagem que expõe **só 2003 (XML-RPC)**, mas config usa `:2004` — `make up` não converte nada hoje
- ⚠️ socket 2003 abre **antes** do LibreOffice subir → health check por porta aberta mente; use RPC real
- ⚠️ `UnoClient._connect` = 5× `sleep=10` (~50s). Nunca no `/saude`.
- ⚠️ watchdog precisa de `os._exit`, não `sys.exit`
- ✅ resolvido: `ModuleNotFoundError: No module named 'uno'` vinha de pip e apt em interpretadores diferentes. `debian:trixie-slim` + `--break-system-packages` no `python3` do sistema

## MOD: fila-worker ⬜ ← PRÓXIMO RECORTE
**keywords:** River, fila, queue, worker, enfileirar, render_preview, consumo

- ⬜ `backend/internal/infra/fila/` vazia · `backend/cmd/worker/main.go` é stub
- River **não** está no `go.mod`
- ❓ decisão pendente: River migra sozinho **ou** dentro de uma migration goose `00004`? Afeta `make migrar` e os snapshots de 00002/00003.

## MOD: http-rotas ✅
**keywords:** rota, handler, echo, middleware, requisicao, resposta, erro HTTP, prontidao

- `backend/internal/rotas/contrato.go` — `Requisicao`, `Resposta`, `Manipulador`, `Middleware`, `Metodo`, `Roteador`, `Rota`
- Handler **nunca** conhece Echo: assinatura `(context.Context, rotas.Requisicao, rotas.Resposta) error`
- `rotasutil/rotasutil.go` — `TratarErro`, `Classificar` (erro→status)
- `middleware/middleware.go` — `IdentificarRequisicao`, `RegistrarAcesso`, `RecuperarDePanico`, `Medir`, `CabecalhoIDRequisicao`
- `root/webrotas/saude/` — único conjunto registrado hoje
- ⚠️ **Toda rota fica sob `/v1`** (`root.PrefixoAPI`). `/prontidao` dá 404; use `/v1/prontidao` e `/v1/saude`.
- ✅ achado MÉDIO FECHADO: `saude/controlador.go:94` devolve `motivoIndisponivel` (mensagem fixa); causa real só no `slog`.
- ⬜ vazios: `webrotas/documentos`, `webrotas/jobs`, `webrotas/auth`, `internal/application/web`

## MOD: erros ✅
**keywords:** erro, Envolver, ErroValidacao, ErroAplicacao, classificacao, status HTTP

- `backend/internal/infra/errors/erros.go` — `Envolver`, `NovoErroValidacao`, `NovoErroValidacaoCampos`, `NovoErroNaoEncontrado`, `NovoErroConflito`, `NovoErroNaoAutorizado`, `NovoErroProibido`, `NovoErroArgumentoNulo`, `NovoErroAplicacao`, `Como`, `E`, `Novo`, `CampoInvalido`
- ⚠️ **`Envolver` NÃO reclassifica.** Envolver um `ErroValidacao` mantém ele na cadeia → vira HTTP 400. Falha de servidor = `NovoErroAplicacao` direto. Foi bug real em `data/postgres`.

## MOD: config-observabilidade ✅
**keywords:** config, env, CONVERSOR_URL, POSTGRES_DSN, log, slog, metricas, tracing, OTel

- `backend/internal/infra/config/config.go` — `Config`, `Postgres`, `Storage`, `Conversor`, `Telemetria`, `LLM`, `Carregar`, `EhDesenvolvimento`, `AmbienteDesenvolvimento`
- `infra/log/log.go`, `infra/telemetry/{metricas,tracing}.go`
- Segredo só por env (regra 8). 💸 `config.Storage` sem `GoStringer` (regra 11, achado BAIXO).

## MOD: fiacao-bootstrap ✅
**keywords:** main, cmd/api, cmd/worker, wiring, fiacao, montar, Verificador, prontidao

- `cmd/api/main.go` monta `postgres.NovoGerenciador` + `storage.NovoClienteS3` e registra os dois Verificadores no controlador de saúde
- `cmd/worker/main.go` monta o gerenciador; consumo de fila continua fora (ver `MOD: fila-worker`)
- **Provado rodando**: `/v1/prontidao` devolve 200 com tudo de pé e 503 sem vazar nada com o MinIO derrubado
- ⬜ falta ligar o `pdfconv.Verificador` (já existe) e as rotas de documento

## MOD: migrations ✅
**keywords:** migration, goose, schema, 00001, 00002, 00003, sessao_id, chave_idempotencia

- `backend/migrations/00001_esquema_inicial.sql` — `usuarios`, `rulesets`, `documentos`, `jobs`, `artefatos`, `relatorios_mudanca`
- `00002_documento_dono.sql` — `sessao_id`, CHECK `documentos_dono_exclusivo` (`num_nonnulls = 1`), índice `documentos_por_sessao`
- `00003_job_idempotencia.sql` — `chave_idempotencia`, UNIQUE `(documento_id, chave_idempotencia)`
- ⚠️ Down das duas **perde identidade/chaves**; novo Up gera outras
- `migrations/README.md` tem os cuidados operacionais

## MOD: testes-integracao ✅
**keywords:** testcontainers, integration, podman, harness, MinIO, postgres descartavel, fixture

- Build tag: `//go:build integration`
- `backend/internal/data/postgres/postgres_integration_test.go` — harness de referência: `executarComPostgresDescartavel`, `subirPostgres`, `criarUsuario`, `inserirDocumento`, `inserirJobFixtureSQL`, `inserirJobFixtureComStatusSQL`
- `backend/internal/infra/storage/s3_integration_test.go` — `novoClienteMinIO`
- `backend/migrations/{documento_dono,job_idempotencia}_test.go`
- Comando nesta máquina:
  ```sh
  cd backend && DOCKER_HOST=unix:///run/user/1000/podman/podman.sock \
    MIGRACOES_REDE_CONTAINER=slirp4netns TESTCONTAINERS_RYUK_DISABLED=true \
    go test -tags=integration ./<pacote>/... -race -count=1
  ```
- `golangci-lint` **não está no PATH**: `/home/daniel-halos/.cache/go-build/a7/a7b195705bae821695a7b931e2f355e7c4037848574f7c96ebf04fed6923c4e7-d/golangci-lint` (rodar também com `--build-tags=integration`)
- Imagens fixadas por **digest**. MinIO vem do **quay.io** (Docker Hub nega pull anônimo).

## MOD: arquitetura-fronteiras ✅
**keywords:** fronteira, hexagonal, dependencia, import proibido, camada

- `backend/internal/arquitetura/fronteira_test.go` — `TestDetectorDeFronteira`, `TestHTTPNaoDependeDoProcessamentoInterno`
- Usa `go list -deps`. Escrito sobre o caminho **direto** de propósito: `cmd/api` vai importar `data/postgres` legitimamente e um teste ingênuo nunca falharia.

## MOD: ooxml ⬜ (F2, não bloqueia F1)
**keywords:** ooxml, docx, XML, w:pPr, w:rPr, w:sectPr, styles.xml, golden

- ⬜ `backend/internal/infra/ooxml/` vazia. Ver skill `ooxml-referencia` e `docs/adr/0001-docx-in-place.md`.

---

## Onde está o estado e o histórico

- `docs/retomada-f1.md` — **checkpoint do topo é o estado atual**; abaixo é histórico
- `docs/estado-do-projeto.md` — mesmo conteúdo, visão por entrega
- `docs/auditoria-specs-f1.md` — auditoria das specs herdadas (regra 13 já satisfeita)
- `docs/plano.md` — plano geral F0→F7 · `docs/adr/0001-docx-in-place.md` — decisão do motor
- Contratos fechados: `docs/{ciclo-b,job-entity,job-criacao,job-consulta,migration-dono,migration-job-idempotencia}-contrato.md`
