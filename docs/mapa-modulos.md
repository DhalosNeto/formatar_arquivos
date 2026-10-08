# Mapa de módulos — índice de busca

**Este arquivo é para GREP, não para leitura integral.** Cada módulo tem uma
linha `## MOD:` com palavras-chave. Ache o módulo, vá direto no arquivo.

```sh
grep -i "dono\|idor" docs/mapa-modulos.md      # acha o módulo
grep -rn "PodeAcessar" backend/internal/         # acha o uso real
```

Regra: **símbolo aqui é símbolo que existe**. Se não achar no código, o mapa
está desatualizado — corrija o mapa, não invente o código.

Legenda de estado: ✅ implementado com evidência no recorte · 🔴 testes RED · ⬜ pendente

Revisão de 22/09/2026: F1 funcional síncrona; F2 com CDM, análise via fila,
PATCH estrutura e GET jobs implementados. Suíte global com `-race` e integração
Postgres passaram. Isso não implica prontidão de produção.
Medições históricas e limites da evidência: `docs/estado-do-backend.md`.

---

## MOD: rulesets-seed ✅
**keywords:** ruleset, schema, YAML, seed, semear, checksum, imutabilidade, F3

- `backend/internal/domain/ruleset/definicao.go` — `Definicao.Validar`: regras técnicas de página e corpo, sem valores normativos presumidos.
- `backend/internal/infra/ruleset/carregar.go` — `Carregar`: YAML limitado, schema embutido de `backend/rulesets/_schema.json`, sem resolução externa.
- `backend/internal/domain/ruleset/repository/ruleset.go` — `RulesetRepo.Semear`; fachada `contracts.RulesetRepo` e `GerenciadorDados.Rulesets`.
- `backend/internal/data/postgres/ruleset.go` — `RepositorioRuleset.Semear`: lote atômico READ COMMITTED, timeout, repetição sem UPDATE, conflito por versão divergente.
- `backend/cmd/rulesetctl/main.go` — `validar`, `semear`: diretório confinado em `os.Root`, arquivos limitados, carregamento anterior à persistência.
- **Contrato de caminho:** `<slug>/v<versao>.yaml`, sem zeros à esquerda e coerente com os dados do arquivo. `.yml` é detectado e **recusado**. Subárvores com nome iniciado em `_` são puladas. Travessia limitada a 4096 entradas e profundidade 4; exige arquivo regular e recusa link simbólico. Diretório vazio devolve sucesso, para o CI rodar enquanto não há perfil normativo.
- `backend/internal/infra/config/config.go` — `CarregarPostgres`: configuração de banco sem exigir storage/LLM.
- `backend/internal/data/postgres/ruleset_integration_test.go` — integração de versões, rollback, concorrência e timeout. Medição registrada em `docs/estado-do-backend.md`.

## MOD: unidades-formatacao ✅
**keywords:** F3, twips, centimetros, pontos, meio-ponto, entrelinha, arredondamento

- `backend/internal/domain/vo/unidades.go` — `CentimetrosParaTwips`, `PontosParaTwips`, `PontosParaMeiosPontos`, `EntrelinhaParaUnidades`: conversões não negativas, arredondamento e teto técnico int32. Não define valores normativos nem substitui validação contextual de OOXML.
- `backend/internal/domain/vo/unidades_test.go` — fatores, empates, zeros, NaN/Inf, negativos e fronteiras de overflow. Testes com race e auditoria aprovados em 22/09. **Fonte normativa pendente e a ambiguidade de entrelinha da Geousp estão em `docs/plano-backend.md`, seção F3.**

## MOD: dono-autorizacao ✅
**keywords:** Dono, IDOR, autorizacao, ownership, sessao, usuario, terceiro, PodeAcessar, solicitante

- `backend/internal/domain/vo/dono.go` — `Dono`, `EspecieDono`, `NovoDonoSessao`, `NovoDonoUsuario`, `ParaDono`, `PodeAcessar`, `Igual`, `Vazio`, `Especie`, `ParaColunas`, `String`, `GoString`
- **`==` NÃO autoriza. Só `PodeAcessar(recurso)` autoriza.**
- `ParaColunas()` devolve `(usuarioID, sessaoID)` com exatamente um não-nulo → CHECK `documentos_dono_exclusivo`
- Filtro de dono vai no **WHERE do SQL**, nunca busca-e-compara: `backend/internal/data/postgres/documento.go`, `job.go`
- Terceiro recebe o **mesmo erro** de inexistente; filtros e testes não provam latência constante nem ausência de todo IDOR.

## MOD: documento-dominio ✅
**keywords:** Documento, ingestao, upload, nome de arquivo, CDM, status, transicao

- `backend/internal/domain/documento/entity/documento.go` — `Documento`, `NovoDocumento`, `HigienizarNomeArquivo`, `ValidarCDM`, `Validar`, `MIME`, `TemPreview`, `IniciarAnalise`, `ConcluirAnalise`, `IniciarFormatacao`, `ConcluirFormatacao`, `MarcarFalha`, `TamanhoMaximoNome`, `TamanhoMaximoCDMBytes`
- `entity/status.go` — `Status`, `Valido`, `PodeTransitarPara`
- `service/documento.go` — `Servico`, `DadosIngestao`, `ValidarIngestao`, `PrepararIngestao`, `Registrar`, `Obter`, `ListarDoDono`, `RegistrarPreviewPDF`, `TamanhoMaximoPadraoBytes`
- `processamento/servico.go` — `ServicoInterno` (**worker**, sem dono, com CAS): `IniciarAnalise`, `MarcarFalha`, `ConcluirAnalise`
- `repository/documento.go` — `DocumentoRepo` (público, com dono) e `DocumentoInternoRepo` (worker, CAS)

## MOD: job-dominio ✅

**Gate de ruleset inativo (08/10):** `entity/tipo.go` tem
`ValidarRulesetParaNovoJob(ativo *bool) error` — regra pura, `nil` = perfil
ausente CONFIRMADO, e ausente/inativo devolvem erro IDÊNTICO em `ruleset_id`
para o gate não virar oráculo de enumeração do catálogo. Aplicado dentro de
`data/postgres/job.go` `InserirOuObter`, na mesma transação, com `FOR SHARE` na
linha de `rulesets`. Repetição de chave VENCE o gate. É gate de CRIAÇÃO e **não
retira perfil de circulação** — takedown é débito do executor.
**keywords:** Job, fila, idempotencia, chave, retry, progresso, cancelar, CAS

- `entity/job.go` — `Job`, `NovoJob`, `Iniciar`, `DefinirProgresso`, `Concluir`, `Falhar`, `Cancelar`, `Reenfileirar`, `Terminal`, `Duracao`
- `entity/status.go` — `StatusJob`, `Terminal`, `PodeTransitarPara` · `entity/tipo.go` — `TipoJob`, `ExigeRuleset`
- `criacao/servico.go` — `Servico`, `DadosNovoJob`, `Criar` (idempotente, autoriza pelo documento)
- `consulta/servico.go` — `Servico`, `Obter`, `ListarDoDocumento`
- `execucao/servico.go` — `ServicoInterno` (**worker**, sem dono): `Iniciar`, `Concluir`, `Falhar`
- `repository/{criacao,consulta,execucao}.go` — `CriacaoJobRepo` (`InserirOuObter`), `ConsultaJobRepo`, `ExecucaoJobRepo` (`Salvar` com CAS)
- Histórico da **spec legada superada**, SHA `654be0fd…3e16dc`: recuperável pelo histórico do git, não duplicada em `docs/`. Quatro asserções compatíveis migradas para `job/execucao`. Não implementar `JobRepo` legado; medição global em `docs/estado-do-backend.md`.

## MOD: persistencia ✅
**keywords:** postgres, pgx, repositorio, SQL, transacao, isolamento, InserirOuObter, contracts

- `backend/internal/data/contracts/contratos.go` — `GerenciadorDados` (**fachada única**, regra 6)
- `backend/internal/data/postgres/conexao.go` — `Gerenciador`, `NovoGerenciador`, `Documentos`, `DocumentosInternos`, `JobsConsulta`, `JobsCriacao`, `JobsExecucao`, `Fechar`, **`Nome`/`Verificar`** (já é Verificador de prontidão)
- `documento.go` — `RepositorioDocumento`, `erroLinhaCorrompida` (linha corrompida = `ErroAplicacao`, **nunca** `ErroValidacao`/HTTP 400)
- `job.go` — `RepositorioJob`, `InserirOuObter` (`FOR SHARE` + `ON CONFLICT DO NOTHING RETURNING` + releitura)
- ⚠️ **`InserirOuObter` exige READ COMMITTED.** `pool.Begin(ctx)` herda o isolamento padrão, sem fixá-lo; sob REPEATABLE READ a releitura não vê a linha concorrente.
- Só `internal/data` pode importar pgx **diretamente** — teste em `internal/arquitetura/fronteira_test.go`

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
- ✅ `ConferirPacoteDocx(conteudo []byte)` — A3 fechado: `MaximoEntradasPacote` (512), `TamanhoDescomprimidoMaximoBytes` (250 MiB), `RazaoDescompressaoMaxima` (200×) acima de `PisoRazaoDescompressaoBytes` (1 MiB)
- ⚠️ **Não compare `UncompressedSize64` com `len(conteudo)`**: compressão faz o descomprimido ser maior que o pacote. Já foi falso positivo que reprovava DOCX legítimo.

## MOD: conversao-pdf ✅
**keywords:** pdfconv, PDF, LibreOffice, unoserver, sidecar, conversor, docx para pdf

- `backend/internal/infra/pdfconv/conversor.go` — `Cliente`, `NovoCliente`, `ConverterParaPDF`, `Verificador`, `NovoVerificador`, `CaminhoConversao` (`/converter`), `CaminhoSaude` (`/saude`), `TipoConteudoDocx`, `TamanhoMaximoPDFBytes` (64 MiB)
- Sidecar próprio: `deploy/Dockerfile.libreoffice` + `deploy/pdfconv-handler.py` (551 MB, Debian 13 / Python 3.13)
- Integração real histórica: conversão DOCX→PDF contra o sidecar, `ok 247s`; não repetida na revisão de 20/09. Unitário PASS, 94,2% nessa revisão.
- ⚠️ **execução FRIA reprova por starvation** (build do LibreOffice compete com os casos de timing de 5s). Builde a imagem ANTES de rodar a integração. Não afrouxe os timeouts.
- Contrato é **nosso**: corpo cru, **sem multipart**, sem campo de nome original no protocolo. Isso não substitui a regra de não registrar conteúdo do documento.
- `deploy/docker-compose.yml` constrói o sidecar próprio **sem publicar 2004 no host**; HTTP acessível por API/worker na rede `sem-saida` (`internal: true`). XML-RPC 2003 é interno ao sidecar.
- Limites configurados: `PDFCONV_MAXIMO_SIMULTANEAS=2`, `PDFCONV_PRAZO_SEGUNDOS=120`, `mem_limit: 2g`, `cpus: 2.0`, `pids_limit: 512`. Contenção de processos filhos ainda tem limites; ver `docs/estado-do-backend.md`.
- ⚠️ socket 2003 abre **antes** do LibreOffice subir → health check por porta aberta mente; use RPC real
- ⚠️ `UnoClient._connect` = 5× `sleep=10` (~50s). Nunca no `/saude`.
- ⚠️ watchdog precisa de `os._exit`, não `sys.exit`
- ✅ resolvido: `ModuleNotFoundError: No module named 'uno'` vinha de pip e apt em interpretadores diferentes. `debian:trixie-slim` + `--break-system-packages` no `python3` do sistema

## MOD: fila-worker ✅
**keywords:** River, fila, queue, worker, enfileirar, render_preview, consumo, analisar, retry, marcarFalha

- **Sem River** — ver `docs/adr/0002-fila-sem-river.md`. A fila é a própria tabela `jobs`.
- `backend/internal/infra/fila/laco.go` — `Laco`, `NovoLaco`, `Reivindicador`, `Executor`, `Finalizador`
- `backend/internal/infra/fila/executor.go` — `ExecutorDocumento`, `NovoExecutorDocumento`
- `internal/domain/job/repository/reivindicacao.go` — `ReivindicacaoJobRepo`, porta separada de propósito: reivindicar é o caso de uso de quem PROCURA trabalho; executar é de quem JÁ TEM um job
- `postgres.RepositorioJob.Reivindicar` — `WHERE status='pendente'` + `FOR UPDATE SKIP LOCKED`, provado por `TestReivindicarConcorrenteNaoEntregaJobDuasVezes`. Não seleciona `falhou`; incrementar `tentativas` não implementa retry automático. Reenfileiramento, backoff e teto de tentativas permanecem pendentes.
- ⚠️ desligamento usa `context.WithoutCancel`: cancelar o laço **não** pode abortar job em voo, senão a linha fica presa em `executando`
- ✅ `POST .../analisar` enfileira análise; `ExecutorDocumento.analisar` chama `ooxml.AnalisarEstrutura`, serializa o CDM e persiste via `ConcluirAnalise`. `jobs.resultado` contém só a contagem de blocos.
- ⬜ O upload mantém preview síncrono. Preview assíncrono exige porta interna para gravar `chave_storage_pdf` no documento; essa pendência não bloqueia a análise.
- `errors.ErroPersistirFalha` preserva as duas causas quando `MarcarFalha` também falha; mensagem e log fixos, logger explícito em `NovoExecutorDocumento`. O banco pode manter o documento `analisando`; se `Concluir`/`Falhar` não persistir, o job pode ficar `executando`. Recuperação e reconciliação continuam pendentes.

## MOD: sessao ✅
**keywords:** sessao, cookie, sessao_id, dono anonimo, httpOnly, SameSite, autenticacao

- `backend/internal/rotas/sessao/sessao.go` — `NomeCookie`, `Garantir`, `Existente`, `Opcional`
- 🔒 **É o ÚNICO lugar que conhece o nome do cookie.** Antes havia duas implementações (`webrotas/documentos` e `middleware/sessao`) com duas constantes `"sessao_id"`: trocar uma e esquecer a outra derrubava metade da API sem erro de compilação
- Três formas, e a diferença importa: `Garantir` cria sessão quando não há cookie (upload); `Existente` devolve `ErroNaoEncontrado` com o recurso do chamador (obter/preview/estrutura/job); `Opcional` devolve `false` sem erro (listagem — primeira visita não é falha)
- Cookie ausente, não-uuid ou uuid nulo é tratado como **ausente**, nunca erro do cliente
- Cookie gravado com httpOnly + Secure + SameSite=Strict + Path=/ (regra 9)
- O nome do recurso vem do chamador para a mensagem não citar "sessão": um 401 distinguiria "sem sessão" de "recurso de outro dono" e viraria oráculo de existência

## MOD: http-rotas ✅
**keywords:** rota, handler, echo, middleware, requisicao, resposta, erro HTTP, prontidao

- `backend/internal/rotas/contrato.go` — `Requisicao`, `Resposta`, `Manipulador`, `Middleware`, `Metodo`, `Roteador`, `Rota`
- Handler **nunca** conhece Echo: assinatura `(context.Context, rotas.Requisicao, rotas.Resposta) error`
- `rotasutil/rotasutil.go` — `TratarErro`, `Classificar` (erro→status)
- `middleware/middleware.go` — `IdentificarRequisicao`, `RegistrarAcesso`, `RecuperarDePanico`, `Medir`, `CabecalhoIDRequisicao`
- `root/webrotas/saude/` e `root/webrotas/documentos/` — conjuntos registrados hoje
- ⚠️ **Toda rota fica sob `/v1`** (`root.PrefixoAPI`). `/prontidao` dá 404; use `/v1/prontidao` e `/v1/saude`.
- ✅ achado MÉDIO FECHADO: `saude/controlador.go:94` devolve `motivoIndisponivel` (mensagem fixa); causa real só no `slog`.
- ✅ `webrotas/documentos`: `Controlador`, `Roteador`, `CaminhoColecao`/`CaminhoItem`/`CaminhoPreview`, `CampoArquivo`, `NomeCookieSessao`, `garantirSessao`, `sessaoExistente`
- ✅ `application/web`: `webmodel.DocumentoResposta`/`PreviewResposta`, `webservices.ServicoDocumento` (portas `ArmazenadorObjetos`/`ConversorPDF`)
- ✅ `application/web/webservices/analise.go` — `ServicoAnalise`, `NovoServicoAnalise`, `Analisar` (job idempotente por documento), `ObterEstrutura` (autorização por dono), `erroCDMCorrompido` (erro de banco vira 500, não 400).
- ✅ `webrotas/documentos`: `CaminhoAnalise`/`CaminhoEstrutura`, `TratarAnalise`/`TratarEstrutura`; POST de análise retorna 202 e GET de estrutura devolve o CDM. DTOs: `webmodel.JobResposta`, `EstruturaResposta`, `BlocoResposta`.
- ✅ `PATCH /v1/documentos/{id}/estrutura` e `GET /v1/jobs/{id}`. `domain/documento/service/estrutura.go` — `ServicoEstrutura.Corrigir`; `domain/documento/repository/estrutura.go` — `EstruturaRepo`; `data/postgres/estrutura.go` — `SalvarEstrutura`: dono e CAS do status/CDM. `application/web/webservices/jobs.go` — `ServicoJob`; `rotas/root/webrotas/jobs/controlador.go` — consulta autorizada; `rotas/sessao` — ver `MOD: sessao`.
- ✅ `rotas.LimitarCorpo`, `Requisicao.Cookie`, `Resposta.DefinirCookie`
- ⚠️ **`BodyLimit` global roda ANTES do middleware de rota.** `servidor.go` isenta o upload via `Skipper` (`ehUploadDeDocumento`); sem isso o teto real da API vira 1 MB e o `LimitarCorpo` nunca é alcançado. Já foi bug.
- ✅ `GET /v1/documentos` (`TratarListagem`): sem cookie devolve `[]` com **200**, não 404 — quem nunca enviou nada não tem sessão, e isso é normal. Não usa `sessaoExistente`.
- ⚠️ `CaminhoColecao` hospeda POST **e** GET; só o POST leva `LimitarCorpo`
- ⬜ vazios: `webrotas/jobs`, `webrotas/auth`

## MOD: erros ✅
**keywords:** erro, Envolver, ErroValidacao, ErroAplicacao, classificacao, status HTTP

- `backend/internal/infra/errors/erros.go` — `Envolver`, `NovoErroValidacao`, `NovoErroValidacaoCampos`, `NovoErroNaoEncontrado`, `NovoErroConflito`, `NovoErroNaoAutorizado`, `NovoErroProibido`, `NovoErroArgumentoNulo`, `NovoErroAplicacao`, `Como`, `E`, `Novo`, `CampoInvalido`
- ⚠️ **`Envolver` NÃO reclassifica.** Envolver um `ErroValidacao` mantém ele na cadeia → vira HTTP 400. Falha de servidor = `NovoErroAplicacao` direto. Foi bug real em `data/postgres`.

## MOD: config-observabilidade ✅
**keywords:** config, env, CONVERSOR_URL, POSTGRES_DSN, log, slog, metricas, tracing, OTel

- `backend/internal/infra/config/config.go` — `Config`, `Postgres`, `Storage`, `Conversor`, `Telemetria`, `LLM`, `Carregar`, `EhDesenvolvimento`, `AmbienteDesenvolvimento`
- `infra/log/log.go`, `infra/telemetry/{metricas,tracing}.go`
- Segredo só por env (regra 8). `Config`, `Postgres`, `Storage` e `LLM` têm `String()` e `GoString()` com redação de segredos; testes em `redacao_test.go`.

## MOD: fiacao-bootstrap ✅
**keywords:** main, cmd/api, cmd/worker, wiring, fiacao, montar, Verificador, prontidao

- `cmd/api/main.go` monta Postgres, storage, conversor, serviço de documentos, criação de jobs e `ServicoAnalise`; registra saúde e documentos (incluindo análise/estrutura), com verificadores de Postgres, storage e `pdfconv`.
- `cmd/worker/main.go` monta gerenciador, storage, conversor, executor e finalizador; chama `fila.Laco.Executar`. A análise enfileira jobs; o upload mantém preview síncrono (ver `MOD: fila-worker`).
- **Provado rodando**: `/v1/prontidao` devolve 200 com tudo de pé e 503 sem vazar nada com o MinIO derrubado
- ⚠️ Compose usa `http://minio:9000` também para assinar URLs: risco de host inacessível ao browser, inferido do código, sem teste E2E nesta revisão.

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
- `golangci-lint` não está no PATH. Use a mesma forma do `Makefile`:
  `cd backend && go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...` (acrescente `--build-tags=integration` quando o alvo tiver teste com essa tag)
- Harnesses de integração fixam imagens por **digest**; isso não vale para todas as imagens do compose, que usa também tags. MinIO vem do **quay.io**.

## MOD: arquitetura-fronteiras ✅
**keywords:** fronteira, hexagonal, dependencia, import proibido, camada

- `backend/internal/arquitetura/fronteira_test.go` — `TestHTTPNaoDependeDoProcessamentoInterno`, `TestPgxSoVazDentroDeInternalData`
- `backend/internal/arquitetura/dominio_test.go` — `TestDominioNaoImportaInfra` ⭐ **a regra nº 1**
- 🔒 **Domínio → infra:** `TestDominioNaoImportaInfra` varre `.Imports` de `./internal/domain/...` e reprova qualquer `internal/infra/*` que não seja `internal/infra/errors`, a **única** exceção autorizada. Guarda contra verificar zero pacotes por erro de filtro
- Fronteira HTTP → processamento interno: usa `go list -deps`, incluindo dependências transitivas de API e rotas.
- Fronteira pgx: examina `.Imports` **diretos**. API → data/postgres → pgx é uma dependência transitiva legítima.
- Cada teste de fronteira tem um teste do PRÓPRIO DETECTOR antes dele: detector quebrado faz a fronteira passar sempre, que é a falha mais silenciosa possível.

## MOD: ooxml ✅ (round-trip + extração de blocos + substituição de partes)
**keywords:** ooxml, docx, XML, w:pPr, w:rPr, w:sectPr, w:body, w:tbl, w:pStyle, styles.xml, golden, extrair blocos, namespace

- `backend/internal/infra/ooxml/pacote.go` — `Documento`, `Abrir(io.ReaderAt, int64)`, `Salvar(io.Writer)`
- `backend/internal/infra/ooxml/partes.go` — `SubstituirParte`: alvo existente/único/seguro, bytes defensivos e teto agregado de deltas de 250 MiB. Nil/vazio substituem por zero bytes, sem excluir parte. Contrato na seção F3 do plano.
- `backend/internal/infra/ooxml/blocos.go` — `BlocoBruto`, `TipoBlocoBruto{Paragrafo,Tabela}`, `(*Documento).ExtrairBlocos()`, `ClassificarPorEstiloDocx`, `TamanhoMaximoTextoResumo`
- Pacote medido em 20/09 (pós-CDM): **PASS, 92,3%**, `-race`
- **Round-trip é byte a byte**: SHA256 do ZIP de saída idêntico ao de entrada nos dois fixtures. É o teste que sustenta o ADR 0001
- **Salvar não reserializa XML.** Partes intactas usam `Copy`; alteradas usam `CreateHeader` preservando método e timestamps, com CRC/tamanhos recalculados e ZIP64 antigo removido. `ExtrairBlocos` lê o delta aceito ou a origem. No-op integral continua testado; casos com delta conferem partes não-alvo.
- Medição 29/09: suíte global com race PASS, OOXML 19,067s; testes focais novos com race PASS na rodada anterior. Mutador de página ainda não implementado.
- **Bloco = filho direto de `w:body`** (`w:p` ou `w:tbl`). Parágrafo dentro de célula **não** é bloco próprio; a tabela é um bloco só. `Indice` é a posição ordinal e vira `RefXML` no CDM — nunca um id injetado no XML (injetar mutaria o documento do usuário)
- Elementos são reconhecidos pelo **namespace** (`espacoNomesW`), não pelo prefixo literal `w:`: o prefixo é escolha de quem gerou o arquivo
- Texto concatena todos os `w:t` do bloco **sem separador**, respeitando `xml:space="preserve"`; parágrafo vazio vira bloco de texto vazio para não desalinhar os índices
- XML malformado devolve `ErroValidacao` (HTTP 400) com mensagem **fixa** — a do `encoding/xml` cita o trecho que falhou, e trecho é conteúdo do usuário (regra 7). XXE coberto por teste
- ⚠️ Zip bomb **não** é conferido: `Copy` não descomprime e a extração lê uma parte só; o upload já chama `vo.ConferirPacoteDocx`. Reavaliar ao descomprimir mais partes
- Fixtures: `testdata/artigo-real-libreoffice.docx` (10 partes, LibreOffice, 40 blocos) e `artigo-desformatado.docx` (4 partes, sintético)
- `backend/internal/infra/ooxml/analisar_estrutura.go` — `AnalisarEstrutura`: abre DOCX, extrai blocos e aplica camadas 1 e 2. O worker serializa e persiste o CDM; POST análise, GET/PATCH estrutura e GET jobs implementados; ver `docs/plano-backend.md`, F2.

## MOD: ruleset-leitura ✅ (leitura por ID; integração executada)

**keywords:** ruleset, perfil, revista, ObterPorID, ConsultaRulesetRepo, decodificarRuleset, ativo, procedencia, teto de tamanho, jsonb

- `backend/internal/domain/ruleset/repository/ruleset.go` — `RulesetRepo` (só `Semear`) e `ConsultaRulesetRepo` (`ObterPorID(ctx, uuid) (Definicao, bool, error)`; o `bool` é o flag `ativo`). Portas segregadas por caso de uso.
- `backend/internal/data/postgres/ruleset.go` — `decodificarRuleset` (pura: teto → unmarshal → `Validar`, tudo `*ErroAplicacao`), `ObterPorID` (sem filtro de `ativo`, confere procedência coluna vs JSON, teto no SQL), `erroConsultaRuleset` (idioma de `erroSeed`, não encadeia driver).
- A8–A14 executados em 08/10/2026 via Testcontainers/Podman contra PostgreSQL descartável: PASS com `-race`, 11.226s. Sem chamador de produção; o gate de `ativo` na criação de job segue pendente.

## MOD: formatador ✅ (plano de formatação, domínio puro)

**keywords:** plano, planejar, formatacao, selecao de blocos, referencias de corpo, ordinal, RefXML, paragrafo generico, porta de validacao

- `backend/internal/domain/formatador/plano.go` — `Plano{Pagina, Corpo, ReferenciasCorpo}`, `Planejar(cdm.Indice, ruleset.Definicao) (Plano, error)`. Puro: sem ctx, sem I/O, sem log. Importa só `domain/cdm`, `domain/ruleset`. É a porta de validação: o adaptador não revalida.
- Executor do plano: `backend/internal/infra/ooxml/formatar.go` — `(*Documento).AplicarPlano(formatador.Plano, MargensComplementares) error`, atômico sobre cópia com `partesSubstituidas` clonado; reusa `AplicarPagina`, `AplicarAlinhamento`, `AplicarEntrelinha`, `AplicarRecuoPrimeiraLinha`, `AplicarEspacamentoAntes/Depois`, `AplicarTipografia`. **Sem caller em produção.**

## MOD: cdm ✅ (entidade, heurística e serialização)

### Fallback Jev experimental (29/09/2026)

- `backend/internal/domain/cdm/fallback.go` — `ClassificadorEstrutura`,
  `NovoFallback`, `PoliticaConfianca`, `RevisaoEstrutura`: consulta limitada,
  aplicar/confirmar/revisar, preservação da origem usuário.
- `backend/internal/infra/llm/jev.go` — `NovoClienteJev`: API Choice TypeSafe,
  trechos limitados, validação HTTP e probabilidades, mensagens fixas.
- `backend/internal/infra/config/jev.go` — flag `JEV_HABILITADO`, chave
  `TYPESAFE_API_KEY`, modelo e limites. Worker conecta; off por padrão.
- `Indice.Revisoes` persiste no CDM v1 e aparece no GET/PATCH estrutura.
  Correção manual remove revisão do alvo. Calibração e teto monetário pendentes.
**keywords:** cdm, bloco, papel, secao, origem, confianca, reclassificar, RefXML, TextoResumo, heuristica, llm, correção do usuário

- `backend/internal/domain/cdm/bloco.go` — `Papel` (VO comparável, campos privados), `Secao(nivel)`, `Origem`, `Bloco`, `NovoBloco`, `(Bloco).Reclassificar`
- `backend/internal/domain/cdm/heuristica.go` — `AplicarHeuristica([]Bloco) ([]Bloco, error)`, a **camada 2**
- `backend/internal/domain/cdm/serializacao.go` — `(Indice).Validar() error`: versão → RefXML duplicado (vence, devolve `ErroRefXMLDuplicado` cru) → blocos inválidos (máximo um `CampoInvalido` por termo). NÃO confere `Confianca` nem `Revisoes`
- Coberturas históricas de 20/09/2026: camada 2 **PASS, 95,5%**; após serialização **PASS, 96,7%**, ambas com `-race`. Não são medição global atual.
- `Papel` sem nível: Titulo, ListaAutores, Resumo, PalavrasChave, Paragrafo, Citacao, ItemLista, Tabela, Figura, Legenda, Equacao, Referencia, NotaRodape. `Secao(n)` é o único com hierarquia, válido em **1..6**
- `Origem` ∈ `estilo-docx` | `heuristica` | `llm` | `usuario`, case sensitive
- 🔒 **Correção do usuário nunca é sobrescrita**: `Reclassificar` com origem automática sobre um bloco `OrigemUsuario` é **no-op sem erro** (rodar a heurística de novo é fluxo normal, não falha). Só outra correção do próprio usuário sobrescreve
- `NovoBloco` **acumula** todos os campos reprovados num só `ErroValidacao` (papel, confianca, origem, ref_xml)
- `TextoResumo` é truncado em **200 runas** (`ooxml.TamanhoMaximoTextoResumo`), cortado por runa e não por byte. O texto íntegro fica no pacote, alcançável por `RefXML`
- ⚠️ O teto **não** garante CDM menor que o texto: medido no fixture real, 8411 bytes de CDM contra 6376 de texto (132%), porque os blocos são curtos. O que ele garante é que o CDM **não cresce com o tamanho do bloco** e não guarda formatação
- `RefXML` zero é válido (primeiro bloco do corpo); negativo é recusado

### Serialização — `serializacao.go`
**keywords:** cdm_jsonb, serializar, desserializar, MarshalJSON, envelope, versao, persistir CDM

- `VersaoFormatoCDM`, `Indice{Versao,Blocos}`, `NovoIndice`, `(Indice).Serializar`, `Desserializar`, `Papel.MarshalJSON`/`UnmarshalJSON`
- `Papel` **precisa** de `MarshalJSON`: campos privados sem ele viram `{}` e o papel de todo bloco se perderia em silêncio na ida ao banco
- Forma: `{"versao":1,"blocos":[{"papel":{"nome":"secao","nivel":2},"texto_resumo":…,"confianca":…,"origem":…,"ref_xml":…}]}`. `nivel` omitido quando zero
- **Envelope é obrigatório**: `entity.ValidarCDM` recusa o que não começa com `{`, então array cru de blocos não persiste
- `blocoJSON` é tipo separado de propósito — força a volta a passar por `NovoBloco`. Tags direto em `Bloco` deixariam o `encoding/json` preencher o struct e contornar a validação
- `versao` diferente é recusada **antes** de percorrer blocos: interpretar formato desconhecido é pior que falhar
- 🔒 Mensagens de erro são **fixas**, nunca interpolam o JSON recebido — `erroLinhaCorrompida` embute `err.Error()` no log (regra 7). Coberto por teste que reintroduz a interpolação e vê 3 casos quebrarem
- ⚠️ `Desserializar` devolve `ErroValidacao`; quem lê do **banco** precisa reclassificar, senão linha corrompida vira HTTP 400 culpando o cliente

### Camada 2 — `AplicarHeuristica`

Passada única na ordem do documento; fatia nova, entrada nunca mutada. Cinco regras:

1. **Rótulo de região** — texto igual a `RESUMO`/`ABSTRACT` → os blocos seguintes viram `Resumo`; `REFERÊNCIAS`/`REFERENCIAS`/`REFERENCES` → `Referencia`. O rótulo em si **não** é reclassificado (é cabeçalho, não conteúdo). Região termina no próximo cabeçalho ou noutro rótulo
2. **Palavras-chave** — prefixo `Palavras-chave`/`Palavras chave`/`Keywords`, sem diferenciar caixa. Vence a região
3. **Legenda** — prefixo `Tabela N`/`Figura N`/`Quadro N` (número obrigatório logo após: `Tabelas 1 e 2` no plural **não** bate) ou `Fonte:`. Não encerra região
4. **Seção numerada** — `^\d+(\.\d+)* ` com espaço obrigatório: `1 INTRODUÇÃO` → `Secao(1)`, `2.1 Algo` → `Secao(2)`. Nível > 6 **não** reclassifica (`Secao(7)` seria recusado por `NovoBloco`). Encerra região
5. Precedência: palavras-chave > legenda > seção numerada > região

- 🔒 **Concordância não enfraquece**: só reclassifica quando o papel DIFERE do atual. `Heading1` + texto `1 INTRODUÇÃO` mantém `OrigemEstiloDocx`/0,95 em vez de cair para `OrigemHeuristica`/0,8
- ⬜ `ListaAutores` **não** é identificada: nenhuma evidência textual confiável a separa de parágrafo comum. Fica para a F5/correção manual
- Confiança: regra textual 0,8; inferência por região 0,6. Sempre abaixo do estilo nomeado (0,95)
- Sem dependência nova: variantes acentuadas são entradas literais no mapa, não normalização Unicode (`golang.org/x/text` continua indireta)
- Ponta a ponta: `internal/infra/ooxml/heuristica_integrada_test.go` confere os 40 blocos do artigo real, um por um

---

## Onde está o estado e o histórico

- `docs/estado-do-backend.md` — **estado atual**: quadro das fases e o que já existe
- `docs/plano-backend.md` — **plano vigente do backend (F2→F7)**
- `docs/contrato-api.md` — contrato da API, para quem faz o frontend
- `docs/plano.md` — histórico: visão de produto e stack (fases desatualizadas)
- `docs/adr/0001-docx-in-place.md` · `docs/adr/0002-fila-sem-river.md`
- Contratos fechados: `docs/{ciclo-b,job-entity,job-criacao,job-consulta,migration-dono,migration-job-idempotencia}-contrato.md`

## MOD: pagina-margens-ooxml

**keywords:** pagina, margens, sectPr, pgSz, pgMar, namespaces, atomicidade

- `backend/internal/infra/ooxml/pagina.go` — `Documento.AplicarPagina`, `MargensComplementares`: dimensões/margens cm, auxiliares explícitos em twips; preserva existentes.
- `backend/internal/infra/ooxml/pagina_xml.go` — edição lexical, escopos de namespace, seções correntes, ordem pgSz/pgMar, limites de recursos e gravação atômica.
- `backend/internal/infra/ooxml/pagina_test.go`, `pagina_bordas_test.go`, `pagina_auditoria_test.go` — preservação, Unicode, histórico, idempotência, atomicidade e entradas adversariais.
- `backend/internal/domain/ruleset/definicao.go` — `Pagina.Validar`: valida apenas página, sem exigir o ruleset completo.

## MOD: alinhamento-paragrafos-ooxml

**keywords:** alinhamento, paragrafo, jc, pPr, RefXML, ordinal, esquerda, direita, centralizado, justificado

- `backend/internal/infra/ooxml/alinhamento.go` — `Documento.AplicarAlinhamento`: referências de parágrafos diretos, edição lexical de jc, validação de ordem CT_PPr e substituição atômica. Reutiliza `pagina_xml.go`.
- `backend/internal/domain/ruleset/definicao.go` — `ValidarAlinhamento`: whitelist compartilhada com a validação de Corpo.
- `backend/internal/infra/ooxml/alinhamento_test.go` — ordinais, preservação, namespaces entre irmãos, idempotência, rejeições e expansão acima do limite.
- `backend/internal/domain/ruleset/alinhamento_test.go` — valores exatos e erros tipados.

## MOD: entrelinha-ooxml

**keywords:** entrelinha, spacing, lineRule, auto, multiplo, 240, preservacao

- `backend/internal/infra/ooxml/entrelinha.go` — `Documento.AplicarEntrelinha`: grava line/lineRule auto, sem alterar before/after ou estilos.
- `backend/internal/infra/ooxml/alinhamento.go` — caminho compartilhado `aplicarPropriedadeParagrafos` / `prepararPropriedadeParagrafo`: seleção, ordem CT_PPr, limites de buffers e substituição atômica para alinhamento e entrelinha.
- `backend/internal/domain/ruleset/definicao.go` — `ValidarEntrelinha`, compartilhada com Corpo.validar; conversor em `domain/vo/unidades.go`.
- `backend/internal/infra/ooxml/entrelinha_test.go`, `backend/internal/domain/ruleset/entrelinha_test.go` — critérios da spec f3-entrelinha-direta.

## MOD: recuo-primeira-linha-ooxml

**keywords:** recuo, primeira linha, firstLine, hanging, estilos, docDefaults, preflight

- `backend/internal/infra/ooxml/recuo.go` — `Documento.AplicarRecuoPrimeiraLinha`, preflight de conflitos diretos e herdados, resolução da relação canônica de estilos.
- `backend/internal/domain/ruleset/definicao.go` — `ValidarRecuoCM`; conversão em `domain/vo/unidades.go`.
- `backend/internal/infra/ooxml/recuo_test.go`, `backend/internal/domain/ruleset/recuo_test.go` — cobertura focal e rejeições atômicas.

## MOD: espacamento-direto-paragrafos-ooxml

**keywords:** espacamento, antes, depois, before, after, spacing, twips, paragrafo

- `backend/internal/infra/ooxml/espacamento.go` — `Documento.AplicarEspacamentoAntes/Depois`: grava atributos diretos `w:before` ou `w:after`, sem garantir distância visual efetiva.
- `backend/internal/infra/ooxml/alinhamento.go` — seleção, edição lexical e substituição atômica compartilhadas.
- `backend/internal/domain/vo/unidades.go` — `PontosParaTwips`; medidas do ruleset em `domain/ruleset/definicao.go`.
- `backend/internal/infra/ooxml/espacamento_test.go` — conversão, preservação, atomicidade e idempotência.

## MOD: tipografia-direta-runs-ooxml

**keywords:** fonte, tamanho, tipografia, rPr, rFonts, sz, szCs, meio-ponto, run, XML

- `backend/internal/infra/ooxml/tipografia.go` — `Documento.AplicarTipografia`: fonte e tamanho diretos em runs de texto, seleção ordinal, CT_RPr, rejeição de wrappers/temas/estrutura ambígua, edição lexical atômica.
- `backend/internal/domain/ruleset/definicao.go` — `ValidarFonteCorpo` e `ValidarTamanhoCorpoPT`, usados por `Corpo.validar`; `domain/vo/unidades.go` converte pontos para meios-pontos.
- `backend/internal/infra/ooxml/tipografia_test.go`, `backend/internal/domain/ruleset/tipografia_test.go` — validação, XML adversarial, preservação, atomicidade e idempotência.

## MOD: instrucoes-agentes
**keywords:** agentes, instrucoes, sincronizar, Claude, Codex, AGENTS, fonte, geracao

- Fontes: `CLAUDE.md` e `.claude/agents/*.md`. Cópias geradas: `AGENTS.md` e `.codex/agents/*.toml`.
- `scripts/sincronizar_instrucoes.py` — `sincronizar`, `gerar_agente`; `--write` regenera, `--check` detecta divergência sem escrever.
- `scripts/test_sincronizar_instrucoes.py` — testes de geração, TOML e detecção de drift.
- Não editar cópias nem copiar modelos/ferramentas de outra IDE; comandos da aplicação permanecem os do código.
- `CLAUDE.md` — contexto e compactação: leitura por recorte, quota, limite de paralelismo, ownership e evidências. Checkpoint único em `docs/retomada.md`.
- `.agents/skills/spec-verificavel/SKILL.md` — gerar/autoverificar a única `docs/spec-ativa.json`; independente das revisões de código.
- `.agents/skills/spec-verificavel/scripts/verificar_spec.py` — `verificar_spec`: estrutura, caminhos, contratos/critérios e evidências; nunca executa comandos. Testes: `scripts/test_verificar_spec.py`.
