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

Revisão de 20/09/2026: F1 funcional síncrona fechada, sem prontidão de produção.
Build PASS; test/vet globais FAIL (`job/service` legado e CDM/OOXML WIP).
Medições e limites da evidência: `docs/estado-do-backend.md`.

---

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
- `deploy/docker-compose.yml` constrói o sidecar próprio e publica HTTP `2004:2004`; XML-RPC 2003 é interno ao sidecar.
- ⚠️ Porta 2004 publicada sem autenticação permite contornar a validação do VO na API; faltam limites explícitos de concorrência, prazo de processamento e recursos. Saída de rede ainda permitida.
- ⚠️ socket 2003 abre **antes** do LibreOffice subir → health check por porta aberta mente; use RPC real
- ⚠️ `UnoClient._connect` = 5× `sleep=10` (~50s). Nunca no `/saude`.
- ⚠️ watchdog precisa de `os._exit`, não `sys.exit`
- ✅ resolvido: `ModuleNotFoundError: No module named 'uno'` vinha de pip e apt em interpretadores diferentes. `debian:trixie-slim` + `--break-system-packages` no `python3` do sistema

## MOD: fila-worker ✅
**keywords:** River, fila, queue, worker, enfileirar, render_preview, consumo

- **Sem River** — ver `docs/adr/0002-fila-sem-river.md`. A fila é a própria tabela `jobs`.
- `backend/internal/infra/fila/laco.go` — `Laco`, `NovoLaco`, `Reivindicador`, `Executor`, `Finalizador`
- `backend/internal/infra/fila/executor.go` — `ExecutorDocumento`, `NovoExecutorDocumento`
- `internal/domain/job/repository/reivindicacao.go` — `ReivindicacaoJobRepo`, porta separada de propósito: reivindicar é o caso de uso de quem PROCURA trabalho; executar é de quem JÁ TEM um job
- `postgres.RepositorioJob.Reivindicar` — `FOR UPDATE SKIP LOCKED`, provado por `TestReivindicarConcorrenteNaoEntregaJobDuasVezes`
- ⚠️ desligamento usa `context.WithoutCancel`: cancelar o laço **não** pode abortar job em voo, senão a linha fica presa em `executando`
- ⬜ **a fila está ociosa**: nada enfileira jobs, o upload converte síncrono. Ligar exige porta nova para o worker gravar `chave_storage_pdf` (não existe hoje — decisão de arquitetura)
- ⬜ Se `Concluir`/`Falhar` não persistir, o laço apenas registra a falha: falta recuperação do job que permanece `executando`.

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
- Segredo só por env (regra 8). 💸 `config.Storage` sem `GoStringer` (regra 11, achado BAIXO).

## MOD: fiacao-bootstrap ✅
**keywords:** main, cmd/api, cmd/worker, wiring, fiacao, montar, Verificador, prontidao

- `cmd/api/main.go` monta Postgres, storage, conversor e serviço de documentos; registra saúde e documentos, com verificadores de Postgres, storage e `pdfconv`.
- `cmd/worker/main.go` monta gerenciador, storage, conversor, executor e finalizador; chama `fila.Laco.Executar`. O upload não enfileira jobs (ver `MOD: fila-worker`).
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

- `backend/internal/arquitetura/fronteira_test.go` — `TestDetectorDeFronteira`, `TestHTTPNaoDependeDoProcessamentoInterno`
- Fronteira HTTP → processamento interno: `TestHTTPNaoDependeDoProcessamentoInterno` usa `go list -deps`, incluindo dependências transitivas de API e rotas.
- Fronteira pgx: `TestPgxSoVazDentroDeInternalData` examina `.Imports` **diretos**. API → data/postgres → pgx é uma dependência transitiva legítima.

## MOD: ooxml ✅ (round-trip + extração de blocos)
**keywords:** ooxml, docx, XML, w:pPr, w:rPr, w:sectPr, w:body, w:tbl, w:pStyle, styles.xml, golden, extrair blocos, namespace

- `backend/internal/infra/ooxml/pacote.go` — `Documento`, `Abrir(io.ReaderAt, int64)`, `Salvar(io.Writer)`
- `backend/internal/infra/ooxml/blocos.go` — `BlocoBruto`, `TipoBlocoBruto{Paragrafo,Tabela}`, `(*Documento).ExtrairBlocos()`, `ClassificarPorEstiloDocx`, `TamanhoMaximoTextoResumo`
- Pacote medido em 20/09 (pós-CDM): **PASS, 92,3%**, `-race`
- **Round-trip é byte a byte**: SHA256 do ZIP de saída idêntico ao de entrada nos dois fixtures. É o teste que sustenta o ADR 0001
- ⚠️ **Salvar continua sem desserializar.** `zip.Writer.Copy` recopia cada entrada sem descomprimir. `ExtrairBlocos` parseia num caminho **paralelo e somente leitura**, reabrindo a entrada do ZIP; não toca `d.arquivos`. `TestExtrairBlocosNaoAlteraOPacote` é a rede que impede essa separação de regredir
- **Bloco = filho direto de `w:body`** (`w:p` ou `w:tbl`). Parágrafo dentro de célula **não** é bloco próprio; a tabela é um bloco só. `Indice` é a posição ordinal e vira `RefXML` no CDM — nunca um id injetado no XML (injetar mutaria o documento do usuário)
- Elementos são reconhecidos pelo **namespace** (`espacoNomesW`), não pelo prefixo literal `w:`: o prefixo é escolha de quem gerou o arquivo
- Texto concatena todos os `w:t` do bloco **sem separador**, respeitando `xml:space="preserve"`; parágrafo vazio vira bloco de texto vazio para não desalinhar os índices
- XML malformado devolve `ErroValidacao` (HTTP 400) com mensagem **fixa** — a do `encoding/xml` cita o trecho que falhou, e trecho é conteúdo do usuário (regra 7). XXE coberto por teste
- ⚠️ Zip bomb **não** é conferido: `Copy` não descomprime e a extração lê uma parte só; o upload já chama `vo.ConferirPacoteDocx`. Reavaliar ao descomprimir mais partes
- Fixtures: `testdata/artigo-real-libreoffice.docx` (10 partes, LibreOffice, 40 blocos) e `artigo-desformatado.docx` (4 partes, sintético)
- ⬜ faltam heurística estrutural (camada 2), persistência do CDM e rotas de análise. Ver `docs/plano-backend.md`, F2

## MOD: cdm ✅ (entidade pura)
**keywords:** cdm, bloco, papel, secao, origem, confianca, reclassificar, RefXML, TextoResumo, heuristica, llm, correção do usuário

- `backend/internal/domain/cdm/bloco.go` — `Papel` (VO comparável, campos privados), `Secao(nivel)`, `Origem`, `Bloco`, `NovoBloco`, `(Bloco).Reclassificar`
- `backend/internal/domain/cdm/heuristica.go` — `AplicarHeuristica([]Bloco) ([]Bloco, error)`, a **camada 2**
- Cobertura 20/09: **PASS, 95,5%**, `-race`
- `Papel` sem nível: Titulo, ListaAutores, Resumo, PalavrasChave, Paragrafo, Citacao, ItemLista, Tabela, Figura, Legenda, Equacao, Referencia, NotaRodape. `Secao(n)` é o único com hierarquia, válido em **1..6**
- `Origem` ∈ `estilo-docx` | `heuristica` | `llm` | `usuario`, case sensitive
- 🔒 **Correção do usuário nunca é sobrescrita**: `Reclassificar` com origem automática sobre um bloco `OrigemUsuario` é **no-op sem erro** (rodar a heurística de novo é fluxo normal, não falha). Só outra correção do próprio usuário sobrescreve
- `NovoBloco` **acumula** todos os campos reprovados num só `ErroValidacao` (papel, confianca, origem, ref_xml)
- `TextoResumo` é truncado em **200 runas** (`ooxml.TamanhoMaximoTextoResumo`), cortado por runa e não por byte. O CDM é índice, não cópia: o texto íntegro fica no pacote, alcançável por `RefXML`
- `RefXML` zero é válido (primeiro bloco do corpo); negativo é recusado

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

## MOD: instrucoes-agentes
**keywords:** agentes, instrucoes, sincronizar, Claude, Codex, AGENTS, fonte, geracao

- Fontes: `CLAUDE.md` e `.claude/agents/*.md`. Cópias geradas: `AGENTS.md` e `.codex/agents/*.toml`.
- `scripts/sincronizar_instrucoes.py` — `sincronizar`, `gerar_agente`; `--write` regenera, `--check` detecta divergência sem escrever.
- `scripts/test_sincronizar_instrucoes.py` — testes de geração, TOML e detecção de drift.
- Não editar cópias nem copiar modelos/ferramentas de outra IDE; comandos da aplicação permanecem os do código.
