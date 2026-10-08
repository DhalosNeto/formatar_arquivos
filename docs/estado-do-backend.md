# Estado do backend — resumo executivo

**Atualizado:** 2026-10-08 · Medições históricas preservadas abaixo

F3: seed imutável Postgres e CLI semear entregues; testes unitários com race,
integrações reais e revisão aprovados. Motor, perfis normativos e endpoints
ainda pendentes como fluxo completo. Página/margens, alinhamento, entrelinha,
recuo da primeira linha, espaçamento antes/depois e tipografia direta dos runs
entregues como componentes;
ponto de pausa e próxima tarefa em `docs/retomada.md`.

## Medição de 08/10 — leitura de ruleset por ID concluída

Os rulesets eram semeados e NUNCA lidos de volta: `RulesetRepo` só tinha
`Semear`, `data/postgres/ruleset.go` só escrevia, `cmd/rulesetctl` só semeava.
Sem leitura, `formatador.Planejar` não tem de onde obter a `Definicao`, e
nenhuma integração do motor é possível. Recorte `f3-leitura-ruleset-por-id`:
porta segregada `ConsultaRulesetRepo` com
`ObterPorID(ctx, uuid.UUID) (ruleset.Definicao, bool, error)`.

Leitura por **ID**, não por slug+versão: `jobs.ruleset_id REFERENCES
rulesets(id)` e `entity.Job.RulesetID` é `*uuid.UUID`. Slug+versão serve ao
catálogo, pendente em `docs/contrato-api.md`.

Pré-auditoria: arquitetura APROVOU COM CORREÇÕES; segurança REPROVOU a primeira
versão (ALTO) e APROVOU COM CORREÇÕES a revisada. Todas incorporadas.

### Implementado e validado contra PostgreSQL em 08/10

`ConsultaRulesetRepo` em `domain/ruleset/repository/ruleset.go`,
`contracts.ConsultaRulesetRepo` + `RulesetsConsulta()` em
`data/contracts/contratos.go` e `data/postgres/conexao.go`, e
`decodificarRuleset` + `ObterPorID` em `data/postgres/ruleset.go`.

`decodificarRuleset` é pura e testada sem banco: teto de tamanho →
`json.Unmarshal` → `Validar()`, tudo classificado como `*ErroAplicacao` (500),
nunca `*ErroValidacao`, e o `*ErroValidacao` original NÃO é encadeado — senão
`rotasutil.Classificar` acharia e responderia 400 com os nomes de campo em
`razoes`. A mensagem de `Validar` interpola só `Campos[0].Campo`.

`ObterPorID` lê `SELECT CASE WHEN octet_length(definicao::text) <= 65536 THEN
definicao END, slug, versao, ativo WHERE id=$1`. O teto vai no SQL porque é a
única guarda que protege a memória do worker: `Scan` materializa o jsonb antes
de qualquer verificação em Go. Acima do teto o servidor manda NULL e o `Scan`
materializa nada. Não filtra `ativo` — devolve o flag. Confere procedência
(coluna vs JSON). Falha de driver segue o idioma de `erroSeed`, que não
encadeia a causa, e NÃO `envolverPostgres`.

**Evidência em 08/10/2026 08:30, dir `backend`:** `go build && go vet &&
gofmt -l .` PASS; `go vet -tags=integration ./...` PASS (gate do projeto,
`Makefile:57`); `golangci-lint run ./...` 0 issues; `go test ./... -race
-count=1` exit 0, 34 pacotes ok, nenhum FAIL/panic/DATA RACE.

Revisões independentes do delta: segurança APROVOU (as seis correções que ela
exigiu foram conferidas no código, uma por uma); arquitetura APROVOU COM
CORREÇÕES, e as duas que ela marcou como "antes de fechar" foram feitas —
amarrar o teto da leitura a `rulesetinfra.TamanhoMaximoBytes` no teste (era
literal `65536`, e subir o teto da entrada quebraria a leitura em silêncio) e
remover a segunda avaliação de `definicao::text`, que destoastava linha TOASTed
duas vezes e deixava um termo de guarda inalcançável.

**Fechamento posterior em 08/10:** V3 executada pelo principal usando o Podman
5.7.0 já disponível e o procedimento de `backend/migrations/README.md`:

```sh
DOCKER_HOST=unix:///run/user/1000/podman/podman.sock \
MIGRACOES_REDE_CONTAINER=slirp4netns TESTCONTAINERS_RYUK_DISABLED=true \
go test ./internal/data/postgres -tags integration -run 'TestObterRulesetPorID' -race -count=1 -v
```

Resultado: exit 0, pacote Postgres 11.226s; os sete testes A8–A14 passaram,
incluindo quatro divergências de procedência e as fronteiras 65536/65537 bytes.
PostgreSQL descartável criado e removido pelo TestMain. Validador e segurança
aprovaram independentemente o delta corretivo final, por revisão estática.
Build/vet/gofmt passaram novamente; V1 e suíte global V2 mantêm a evidência
histórica acima, sem nova execução global neste fechamento documental.
A14 verifica a recusa por tamanho; não mede alocação nem tráfego do driver.

Evidência focal preservada antes de substituir a spec concluída: em
08/10/2026 às 08:16, `go test ./internal/data/postgres -race -count=1`
passou (1.053s), incluindo os seis `TestDecodificarRuleset*` e
`TestSemearValidaLoteAntesDeAcessarPool`; repetido às 08:30 após as correções,
também PASS. O RED original foi por símbolos ausentes. A validação focal,
os gates globais acima e a integração posterior pertencem a
`f3-leitura-ruleset-por-id`; não são evidências do próximo gate de criação.

### Planejamento de 08/10 — gate de ruleset inativo na criação de job (em execução)

Recorte `f3-gate-ruleset-inativo`: `TipoJob.ValidarRulesetParaNovoJob(ativo *bool)`
em `domain/job/entity/tipo.go` (regra pura; `nil` = ausente CONFIRMADO, nunca
"não foi possível saber"), aplicada dentro de `InserirOuObter`
(`data/postgres/job.go`) na mesma transação, com `FOR SHARE` na linha de
`rulesets`. Ausente e inativo devolvem erro IDÊNTICO em `ruleset_id`, por
exigência de segurança: distinguir viraria oráculo de enumeração do catálogo.

Ordem normativa: **autorização do documento → gate → INSERT → releitura**.
Repetição de chave VENCE o gate — sem isso, repetir chave depois de o perfil ser
desativado passaria a falhar e quebraria a repetição terminal que a porta
promete, além do teste `postgres_integration_test.go:622`.

Por que não "inserir e validar depois": a checagem de FK do INSERT toma
automaticamente `FOR KEY SHARE`, que NÃO conflita com o `FOR NO KEY UPDATE` do
`UPDATE ativo=false` — validar depois do INSERT nunca obteria o lock
necessário. (A justificativa de `SAVEPOINT` registrada antes estava ERRADA:
ele só seria preciso para continuar a transação, e o caminho reprovado é
terminal.) Fato de apoio já provado pela suíte: `ON CONFLICT DO NOTHING` não
dispara a checagem de FK (`postgres_integration_test.go:639-648`).

Pré-auditoria: duas rodadas de arquitetura e segurança, ambas APROVA COM
CORREÇÕES na segunda. Correções incorporadas incluem trocar a assinatura para
`*bool` (a versão com dois `bool` adjacentes tinha troca behavioralmente
indetectável), reescrever o contrato do adaptador como invariantes em vez de
ordem de queries, e remover duas afirmações INOBSERVÁVEIS que nenhum teste
poderia derrubar.

### Implementado e verificado em 08/10, integração inclusa

`TipoJob.ValidarRulesetParaNovoJob(ativo *bool)` em `entity/tipo.go` e o gate
dentro de `InserirOuObter` (`data/postgres/job.go`). Ordem no código:
`SET LOCAL lock_timeout = '3s'` → `FOR SHARE` em `documentos` → gate → INSERT →
releitura. O gate só roda sob `ExigeRuleset()`; lê `SELECT ativo FROM rulesets
WHERE id=$1 FOR SHARE` na mesma transação; `pgx.ErrNoRows` → nil (ausente
confirmado), outro erro → `envolverPostgres`, nunca `*ErroValidacao`.
`RulesetID` nil não consulta e não desreferencia.

**A implementação ficou mais barata que o contrato original.** Em vez do SELECT
extra em toda criação, o gate roda primeiro e só consulta a chave **quando
reprova**: caminho feliz não paga query a mais, e o custo fica no caminho de
rejeição, que é raro. Round-trips: antes 4; agora 5 para análise/preview
(`SET LOCAL`) e 6 para formatar. O helper `obterJobDaChave` devolve
`(job, achou, err)` e cada chamador aplica o seu significado para zero linhas —
`ErroAplicacao` na releitura pós-conflito, erro do gate no caminho reprovado.

**Evidência em 08/10/2026 19:51, dir `backend`:** build/vet/vet-integration/
gofmt PASS; `golangci-lint run ./...` 0 issues; `go test ./... -race -count=1`
exit 0, 34 pacotes. Integração com Podman: `-race` exit 0 em 19.265s, com
`TestInserirOuObterGateDePerfilUsaForShare` em 0,23s — a prova do modo de lock
executou de fato.

**O RED provou por execução os fundamentos de lock**, que antes eram só
raciocínio: hoje o job É criado contra perfil inativo; ausente dá
`jobs_ruleset_id_fkey` 23503 (500, não 400); e o `FOR KEY SHARE` da checagem de
FK NÃO conflita com o `FOR NO KEY UPDATE` do `UPDATE ativo=false`, o que
justifica o `FOR SHARE` explícito. Há também o CHECK
`jobs_ruleset_obrigatorio_para_formatar` (`migrations/00001:69`), que já recusava
formatar sem ruleset, mas como erro de driver.

### Defeito de teste encontrado no fechamento, e a lição

`TestInserirOuObterAnalisarComCatalogoVazio` era **dependente de ordem**: a
pré-condição `SELECT count(*) FROM rulesets == 0` passava só porque `go test`
roda arquivos em ordem alfabética (`postgres_` antes de `ruleset_`), e
`ruleset_integration_test.go` semeia em 19 pontos **sem nenhum cleanup**.
Provado por `-shuffle=on`, que derrubou o teste. Corrigido tornando-o hermético
(cortada a contagem global, mantida a parte observável) e renomeado para
`TestInserirOuObterAnalisarSemRuleset`, porque o nome antigo anunciava uma
pré-condição que ele não tem. Reverificado com a seed exata que falhava.
**Lição:** pré-condição sobre estado GLOBAL do banco num pacote de integração
com container compartilhado é dependência de ordem disfarçada, e a mensagem de
falha aponta para a fixture errada. Vale rodar `-shuffle=on` ao fechar recorte
que toque testes de integração.

### Débitos e limites registrados por este recorte

- **BLOQUEANTE do recorte do executor — o gate é de CRIAÇÃO e NÃO retira perfil
  de circulação.** Job pendente criado antes da desativação continua
  executável; `Job.Reenfileirar` (`entity/job.go:165`) o devolve para pendente;
  `Reivindicar` (`job.go:200`) não consulta `rulesets`. O lugar do limite de
  takedown é o EXECUTOR, e a porta necessária já existe e já devolve o flag:
  `postgres/ruleset.go` `ObterPorID` retorna `(definicao, ativo, error)` e
  nenhum Go consome esse bool. Débito: executor recusa job de formatação cujo
  ruleset está inativo no momento da reivindicação. **Não ler a aprovação deste
  recorte como "o operador já consegue retirar um perfil" — isso é falso.**
- **Contexto que torna os riscos LATENTES:** `criacao.Servico` tem UM consumidor
  hoje, `webservices/analise.go:55`, que cria `TipoAnalisar`. Nenhuma rota cria
  `TipoFormatar` e o executor não tem branch de formatar. O gate é defesa em
  profundidade na porta de dados, não gate exposto por HTTP; oráculo, DoS de
  lock e amplificação só se ativam com o recorte da rota de formatação.
- **Pool de 10 sem prazo de statement.** `POSTGRES_MAX_CONEXOES` default 10
  (`config.go:77`), sem `statement_timeout`, sem `lock_timeout` e sem prazo por
  requisição (`cmd/api/main.go:121` é desligamento). O gate acrescenta um
  SEGUNDO ponto de espera por lock, e cada chamada presa retém uma das 10. O
  recorte declara `SET LOCAL lock_timeout = '3s'` na transação como mitigação,
  mas **ela NÃO é provada por critério**: provar exigiria esperar mais que o
  próprio `lock_timeout`, isto é, um teste lento. Mitigação declarada e não
  verificada. **E o teto efetivo é esse valor, não o prazo de quem chama:** não
  existe deadline por requisição no caminho HTTP — os únicos `WithTimeout` do
  projeto são o do health check (`saude/controlador.go:91`) e o do desligamento
  (`cmd/api/main.go:121`). Como `lock_timeout` é POR AQUISIÇÃO, `documentos` mais
  `rulesets` somam até ~6s retendo a mesma conexão, e dez requisições
  concorrentes esgotam o pool por esse intervalo se um operador deixar transação
  aberta em `rulesets`. Fechar isso exige prazo por requisição: recorte próprio.
- **O `FOR SHARE` não bloqueia o operador indefinidamente**, ao contrário do que
  se poderia supor: o Postgres enfileira o *tuple lock*, então os `FOR SHARE`
  que chegam depois ficam ATRÁS do `UPDATE` que espera. O operador aguarda só as
  transações em voo. E no caminho reprovado o lock só é tomado quando o perfil
  EXISTE e está inativo — no caso ausente não há tupla, logo não há lock.
- **`RulesetsConsulta()` segue sem consumidor de produção** (só testes de
  integração). É porta sem cliente: se o recorte dono não entregar o consumidor,
  é código morto a deletar. O gate NÃO a consome, por precisar da mesma
  transação e do `FOR SHARE`.
- **Teto do `FOR SHARE`:** compra pouco — um job criado contra perfil desativado
  milissegundos antes é janela que permanece aberta de qualquer forma. Mantido
  porque é uma palavra de SQL, locks shared não contendem entre si, e sem ele
  "desativar" fica sem ponto de corte definível. Vários lockers shared na mesma
  linha quente usam multixact; irrelevante nesta escala, revisitar se criação de
  job de formatação virar caminho de alta concorrência.
- **`ExigeRuleset` é fail-open** (`entity/tipo.go:24` é `tipo == TipoFormatar`):
  um tipo futuro que exija norma escapa do gate em silêncio. Registrado no
  comentário de C1, não corrigido.
- **Duas afirmações removidas por serem INOBSERVÁVEIS**, e vale lembrar por quê:
  "análise e preview não pagam query alguma" e "sem query ao catálogo". `NovoJob`
  recusa `RulesetID` não-nil para tipo que não exige (`job.go:58-59`), então para
  `TipoAnalisar` o `RulesetID` é sempre nil e uma query incondicional viraria
  `WHERE id IS NULL` — sem linha, sem lock, resultado idêntico. Nenhum teste
  poderia derrubar a afirmação.
- **Sem rate limit na criação de job** (middleware tem só identificação, log,
  pânico e métrica). Preexistente, não piorado por este recorte.

## Fechamento de 08/10 — teto por placeholder

A última concatenação de VALOR em SQL deste pacote foi trocada por parâmetro: a
query de `ObterPorID` é agora uma `const` com `octet_length(definicao::text) <=
$2` e o teto vai como argumento. `strconv` deixou de ser importado. Verificado
contra Postgres real: o subcaso `TetoDeTamanhoNoSQL/exatamente_no_teto_e_lido`
e `um_byte_acima_do_teto_recusado` são exatamente o que prova o binding do `$2`,
e os dois passam. Gates finais em 08/10 18:55: build/vet/vet-integration/gofmt
PASS, `golangci-lint` 0 issues, suíte global `-race` exit 0 com 34 pacotes.

### Buraco no gate de lint, não deste recorte

`make lint` roda `go vet -tags=integration ./...` mas o `golangci-lint` **sem**
a tag, então arquivos `//go:build integration` nunca passam pelo linter
estrito. Rodando com a tag aparecem 2 issues preexistentes:
`f2_http_integration_test.go:57` (noctx) e `postgres_integration_test.go:855`
(staticcheck QF1002).

### Débitos e bloqueios registrados por este recorte

- **BLOQUEANTE do recorte do executor — não existe gate de `ativo`.**
  `job/criacao/servico.go:53` aceita `RulesetID` do cliente e só a FK
  `ON DELETE RESTRICT` confere existência; o novo repositório lê `ativo`, mas
  a criação de job ainda não o consulta. Como arquivo
  semeado é imutável e não há DELETE, `ativo=false` é o ÚNICO recurso do
  operador para retirar um perfil de circulação — perfil com diretriz errada ou
  sob takedown continua aplicável a jobs NOVOS, e o operador acredita tê-lo
  retirado. Este recorte DEVOLVE o flag para que o gate custe zero query extra.
  Construir o gate é bloqueante do recorte que criar a branch `TipoFormatar`.
  **Ao construí-lo:** a resposta HTTP não deve distinguir "inativo" de "não
  encontrado" — mensagem genérica em `ruleset_id`, senão o gate vira oráculo de
  enumeração do catálogo.
- **Reprodutibilidade SEM LASTRO — o checksum nunca é verificado.** O risco não
  é adulteração via SQL (quem tem SQL reescreve o checksum também, então contra
  adversário privilegiado o campo vale zero). O risco residual é ALTERAÇÃO
  SEMANTICAMENTE VÁLIDA: trocar `margens.esquerda_cm` de 3,0 para 2,0 passa por
  `Definicao.Validar()` e pela conferência de procedência slug/versao, e nada
  detecta. A afirmação de reprodutibilidade de `definicao.go:17-19` e do
  CLAUDE.md fica sem lastro até a verificação existir. Pré-requisito: medir a
  estabilidade do round-trip `jsonb`, que normaliza ordem de chaves e formato
  numérico (`21.0` vs `21` em `float64` é o caso duvidoso). **Não alegar
  reprodutibilidade garantida antes dessa medição.**
- **SSRF em `Fonte`, fora deste recorte e importante.** `definicao.go:84-87`
  exige só host não vazio e esquema http/https, aceitando
  `http://169.254.169.254/...` e `http://localhost:5432`. Nada busca `Fonte`
  hoje. No dia em que algo no servidor baixar a diretriz, é SSRF para metadata
  de cloud e rede interna. A correção pertence à validação.
- **`envolverPostgres` pode expor dados do driver em log.** Seu comentário
  foi corrigido neste recorte; o corpo continua sendo
  `errors.Envolver`, cujo `Error()` interpola `%v` do original. A metade sobre
  `PgError.Detail` se sustenta em pgx v5.11.0, mas `ConnectError` e
  `perDialConnectError` emitem usuário, banco, endereço e hostname, que
  `rotasutil.go:22` grava em log nível Error. O corpo HTTP está protegido.
  Este recorte NÃO usa a função no caminho novo (usa o idioma de `erroSeed`,
  que deliberadamente não encadeia) e corrige o comentário que mente; os
  chamadores existentes seguem expostos. Consertar a função exige recorte
  próprio.
- **Sem statement timeout.** O pool tem só `ConnectTimeout`
  (`conexao.go:56`) e `Semear` embrulha 2 min próprios (`ruleset.go:69`).
  `ObterPorID` chamada com ctx de worker de vida longa pode prender conexão do
  pool e estagnar a fila. Mesma exposição das leituras de job existentes; vira
  requisito do recorte do executor.
- **A coluna `nome` fica fora da conferência de procedência**, deliberadamente:
  `nome` só é lida por `inserirRuleset` na idempotência do seed, e divergência
  coluna/jsonb nela é cosmética. `slug` e `versao` são conferidos porque
  carregam a `UNIQUE (slug,versao)` e definem procedência.
- **Porta sem consumidor em produção:** a leitura nasce com zero chamador — a
  branch `TipoFormatar` está fora de escopo. O `var _` segura a compilação, mas
  a assinatura só é validada contra uso real no recorte seguinte.
- **Bloqueio de V3 resolvido em 08/10:** o binário Docker continua ausente,
  mas Testcontainers executou os testes com o socket do Podman. A8–A14 têm
  resultado PASS contra banco real, conforme medição acima.
- **Sugestões mantidas fora do fechamento:** usar `$2` para o teto é uma
  preferência de estilo; a concatenação atual usa somente constante inteira.
  A mensagem da função pura agrupa vazio e excesso como limite de tamanho.
  Se surgir outro chamador, revisar se essa distinção precisa ser pública.
  Cancelamento ainda pode ser classificado como 500/log Error pelo adaptador
  HTTP; definir tratamento e prazo do job no recorte do executor.

## Medição de 07/10 — plano de formatação e adaptador agregador

`formatador.Planejar(cdm.Indice, ruleset.Definicao) (Plano, error)` em
`internal/domain/formatador/plano.go` é a porta de validação do fluxo: delega
os invariantes do índice a `cdm.Indice.Validar()`, chama `Definicao.Validar()`,
propaga os dois erros SEM reclassificar e seleciona só os blocos
`cdm.Paragrafo`, em ordem crescente. Índice sem blocos é válido.
`(*ooxml.Documento).AplicarPlano` em `internal/infra/ooxml/formatar.go` executa
o plano: página nas seções correntes e as seis propriedades de corpo nos
ordinais selecionados, reusando os mutadores já existentes.

`cdm.Indice.Validar()` nasceu neste recorte em `domain/cdm/serializacao.go`,
não em `formatador`: o invariante já morava em `cdm`, que define
`ErroRefXMLDuplicado`, e `Desserializar` NÃO checa duplicata (só
`validarRevisoes`, e apenas quando há revisões). Ordem das guardas: versão →
RefXML duplicado → blocos inválidos. Duplicata VENCE bloco inválido e devolve
`ErroRefXMLDuplicado` cru, porque é corrupção de servidor e não pode virar 400
culpando o cliente. O acúmulo registra no máximo um `CampoInvalido` por termo,
porque `CampoInvalido.String()` é copiado para o `razoes` da resposta HTTP
(`rotas/rotasutil/rotasutil.go:36`) e 50 blocos ruins não podem render 50
razões idênticas. `Validar` NÃO confere `Confianca` nem `Revisoes`, e a
assimetria está documentada no código como deliberada.

Atomicidade: `AplicarPlano` roda os sete mutadores sobre uma cópia rasa do
`Documento` com `partesSubstituidas` clonado, e troca o mapa do receptor só
depois que todos devolvem nil. Se qualquer um recusar, nem o delta da página
fica. É seguro com cópia rasa porque `Documento` tem só dois campos e toda
escrita retida passa por `SubstituirParte`, que guarda `bytes.Clone`.

Classificação de `RefXML` negativo, fechada em 08/10: a varredura de duplicata
ignora ordinais negativos. Dois blocos com `RefXML: -1` são ENTRADA INVÁLIDA
(`*ErroValidacao` em `ref_xml`, 400), não corrupção — dois `-1` não são dois
blocos apontando para o mesmo nó, são dois ordinais inválidos. Isso também
alinhou `Validar` com `Fallback.Aplicar` (`fallback.go:78-83`), que classificava
o mesmo índice como 400 enquanto `Validar` devolvia 500. Duplicata continua
vencendo papel/origem inválidos quando os RefXML são válidos e repetidos.

**Evidência em 08/10/2026 07:13, dir `backend`:** `go build && go vet &&
gofmt -l .` PASS; `golangci-lint run ./...` 0 issues; `go test ./... -race
-count=1` exit 0, 34 pacotes ok, nenhum FAIL, panic ou DATA RACE
(`domain/cdm` 1.046s, `domain/formatador` 1.027s, `infra/ooxml` 58.750s,
`internal/arquitetura` 1.269s). Cobertura de statements com suíte verde:
`domain/formatador` 100%, `domain/cdm` 95,0%, mínimo do domínio 93,8%. O lint reprovou na PRIMEIRA execução com 6 issues `errorlint` no helper
anti-vazamento dos testes; o helper varre todos os elos da cadeia de `Unwrap`
e `errors.As` pararia no primeiro match, então foram anotadas 6
`//nolint:errorlint` estreitas, em vez de excluir `errorlint` dos testes.

**O que isto NÃO é.** Não há caller em produção: nenhuma rota, fila ou storage
chama `Planejar`/`AplicarPlano`. A superfície é inerte até ser integrada, e as
revisões de arquitetura e segurança deste recorte não cobrem essa integração.
Não há perfil normativo: a validação usou ruleset sintético, e nada aqui
autoriza afirmar conformidade ABNT/Geousp. F3 segue aberta.

### Débitos declarados neste recorte, a pagar ANTES de ligar o worker

- **Regra 3 — sem `context.Context`:** `AplicarPlano` faz I/O sem ctx,
  consistente com os sete mutadores, que também não o têm. Não é regressão; é
  débito do pacote. Pôr ctx só no agregador daria cancelamento entre etapas
  sem propagar para dentro delas, e mudaria assinatura fixada por teste.
  Corrigir junto com os sete.
- **Amplificação de leitura:** 7 leituras de `word/document.xml` por chamada
  (uma por mutador, cada uma com `LimitReader` de 32 MiB) e 14 parses (2 por
  passe, comportamento preexistente de cada mutador). Dentro do teto, nenhum
  guarda contornado. Ler e parsear uma vez, reusando a árvore, é o upgrade.
- **Teto de `Campos` não testado no caminho HTTP:** A19 prova o limite dentro
  do domínio; que `rotasutil.go:36` não amplifique `razoes` segue sem teste,
  porque não existe chamador HTTP de `Classificar` neste recorte.
- **Preexistente, fora do delta:** `validarRevisoes`
  (`cdm/serializacao.go:193`) só detecta RefXML duplicado quando há revisões, e
  `Serializar` não chama `Indice.Validar` — um índice com RefXML repetido e sem
  revisões ainda pode ser persistido. Não alcançável por `Planejar`. Pertence
  ao recorte de fallback.
- **Classificação crua vira 500 se o índice passar a vir do cliente**
  (segurança, BAIXO, capacidade futura): `cdm/serializacao.go` devolve
  `ErroRefXMLDuplicado` cru, que vira 500 + log nível Error. Hoje correto,
  porque o índice é montado pelo servidor e `Planejar` não tem chamador. Se um
  recorte futuro (editor de estrutura, import de CDM) passar índice DO CLIENTE
  por `Validar`/`Planejar`, duplicata trivial virará 500 e log Error por
  requisição: ruído de alerta e inversão de culpa, não vazamento. Conserto
  nesse momento: o chamador de borda envolve em `*ErroValidacao`; o sentinela
  cru fica só para leitura do banco.
- **A20 não prova fonte única, só texto igual** (validador): um teste em
  runtime não distingue constante de literal com o mesmo texto. A20 é
  regressão de texto. A prova de fonte única seria estática.
- **Quarta cópia de literal:** `documento/service/estrutura.go:43` repete o
  texto de `mensagemPapelDesconhecido` para o MESMO predicado
  (`papel.Valido()`), e as constantes de `cdm` são não exportadas: editar a
  constante deixa esse caminho HTTP divergente em silêncio, e o teste de
  regressão não vê. Conserto proposto: exportar `cdm.ValidarPapel(Papel) error`
  devolvendo o erro canônico, seguindo o idioma que o projeto já usa em
  `ruleset.ValidarAlinhamento`/`ValidarEntrelinha`/`ValidarRecuoCM`. É outro
  recorte — `estrutura.go` não estava autorizado aqui, e a quarta cópia é
  ANTERIOR a este recorte. Dentro de `cdm`, `bloco.go` e `serializacao.go` já
  compartilham as constantes.

## Medição de 07/10 — fonte e tamanho diretos nos runs

`Documento.AplicarTipografia` grava rFonts (ascii/hAnsi/eastAsia/cs) e sz/szCs
nos runs de texto diretos dos parágrafos selecionados. `Corpo.Fonte` e
`Corpo.TamanhoPT` usam validadores isolados e a conversão existente de pontos
para meios-pontos. A mutação preserva texto/histórico/partes não alvo e recusa
atributos de tema concorrentes, homônimos sem namespace e parágrafos com texto
em wrappers inline ainda não suportados. Não publica valores ABNT/Geousp nem
garante fonte instalada ou aparência visual. Spec `f3-tipografia-direta-runs`
concluída como componente.

Teste focal com `-race` inicialmente encontrou aceitação indevida de NBSP em
`w:rPr` e nos elementos rFonts/sz/szCs; os quatro casos foram corrigidos e o
focal final passou (ruleset 1.023s, OOXML 1.526s). Primeiro gate global parou
em errorlint no teste, também corrigido. Fechamento final: build/vet/gofmt,
`golangci-lint` 0 issues e suíte global com `-race` PASS (OOXML 90.211s).
Revisões finais estáticas de arquitetura e segurança aprovaram o delta após
as correções. Sem integração ponta a ponta ou validação visual.

## Medição de 07/10 — espaçamento direto antes/depois

`Documento.AplicarEspacamentoAntes/Depois` grava somente `w:before` ou
`w:after` no parágrafo selecionado, com conversão de pontos para twips pelo VO
existente. Preserva o lado oposto, entrelinha, propriedades de espaçamento
automático, estilos, histórico e partes ZIP não alvo. É gravação direta: herança
e `contextualSpacing` podem mudar o espaço visual. Spec
`f3-espacamento-direto-paragrafo` concluída. Teste focal com `-race` PASS
(OOXML 1.121s); build/vet/gofmt, lint sem issues e suíte global com `-race`
PASS (OOXML 49.375s). Revisões estáticas de arquitetura e segurança aprovadas.
Não houve RED: o primeiro teste foi criado após o código. Sem fluxo de
formatação ponta a ponta ou validação visual.

## Medição de 07/10 — recuo da primeira linha

`Documento.AplicarRecuoPrimeiraLinha` e `ruleset.ValidarRecuoCM` concluídos
como componente; preflight de propriedades diretas, `docDefaults` e cadeia de
estilos ativa. Teste focal com `-race` e suíte global com `-race` PASS; build,
vet, gofmt e lint limpos. Evidências exatas e revisões em `docs/retomada.md`.

## Planejamento de 05/10 — recuo com verificação de herança

Spec `f3-recuo-primeira-linha-preflight` pronta para TDD, com pré-auditoria de
arquitetura/segurança e reauditorias aprovadas. Ainda não implementada:
nenhum novo teste/RED ou mutador de recuo nesta rodada.
Contrato examina propriedades e estilos ativos, rejeita conflitos sem alterar
estilos compartilhados e restringe leitura de relacionamentos OPC, sem rede.
Testes planejados separam decisões por rota; detalhes na spec e plano seção05/10.

Verificador estrutural, sincronização de instruções e diffcheck limpos;
build/vet/gofmt do backend limpos. Suíte global não repetida nesta rodada documental.
Última medição funcional continua a de04/10 abaixo. F3 permanece em andamento.

## Medição de 04/10 — entrelinha direta

`Documento.AplicarEntrelinha` altera somente line/lineRule=auto dos alvos;
validação contextual compartilhada no domínio, caminho lexical compartilhado
com alinhamento. Mantém espaço antes/depois, histórico, texto e partes não-alvo.
Sem recuos, fonte, estilos, perfis ou integração HTTP/worker de formatação.

- RED por APIs ausentes; focal race final PASS25.601s OOXML/1.041s ruleset.
  Principal corrigiu expectativa incorreta da fixture de tabela e assumiu execução
  dos testes deixados pelo agente, sem inferir aprovação não recebida.
- Build/vet/gofmt limpos; lint0issues; global race PASS (OOXML88.636s).
  Ruleset com suíte completa:98.2% de cobertura.
- Pré-auditoria e revisão de código aprovadas. Segurança confirmou risco médio
  herdado de buffers agregados com aliases longos; guarda incremental corrigida
  e reauditoria aprovada. Perfil focal prova ramo de rejeição executado1vez.
- Não medido RSS total nem visual em Word/LibreOffice; sem novos containers/E2E.
  Grade de documento e herança integral continuam fora do contrato.
  Skills orientaram preservação e reuso; nenhuma consulta Jev foi necessária.

## Medição de 03/10 — alinhamento direto de parágrafos

`Documento.AplicarAlinhamento` concluído: quatro valores validados no domínio,
seleção por ordinais compatíveis com ExtrairBlocos, edição localizada de jc,
preservação de texto/histórico/partes não-alvo e rejeição atômica de ambiguidades.
Não inclui entrelinha, recuos, fonte, styles.xml ou integração ao fluxo HTTP/worker.

- RED por APIs ausentes confirmado; focal race GREEN pelo testador.
- Global `go test ./... -race -count=1` PASS (OOXML71.692s, VO10.195s).
  Após delta somente de teste, regressão de namespace entre irmãos PASS1.028s.
- Build/vet/gofmt limpos e lint0issues, repetidos após o último delta de teste.
  Suíte completa de ruleset: cobertura98.0%.
- Pré-auditoria da spec e revisões finais de código/segurança aprovadas.
  Limite de expansão é calculado antes do buffer final. Teste de isolamento de
  namespace fecha a sugestão baixa registrada no componente de página/margens.
- Sem containers/E2E/inspeção visual. Sem nova consulta Jev, regra determinística.
  Skills de spec, OOXML e normas delimitaram contratos/preservação; simplificação
  manteve parser/editor compartilhados, sem dependências novas.

## Medição de 03/10 — página/margens OOXML

`Documento.AplicarPagina` concluído com preservação lexical/partes não-alvo,
seções correntes, histórico intocado, atomicidade e idempotência. Sem norma
publicada, endpoint, worker de formatação ou PDF novo. Skills locais orientaram
spec, invariantes OOXML e proibição de inventar valores normativos.

- Build/vet/gofmt globais limpos; lint0issues; global `go test ./... -race -count=1`
  PASS no snapshot final (OOXML62.032s). Ruleset cobertura97.9%.
- Validador aprovou estática. Segurança detectou amplificação por namespaces,
  corrigida com escopos compartilhados e limites de nós/atributos/declarações;
  regressão RED→GREEN e reauditoria aprovados. Detalhes técnicos no plano.
- Sem medição visual ou integrações com containers neste recorte.
  Não é conformidade normativa nem validação XSD integral.
  Sugestão baixa de isolamento de namespaces entre irmãos atendida pelo recorte
  de alinhamento acima, usando o mesmo parser.

## Medição de 29/09 — fallback Jev experimental

Porta no CDM, política de confiança, adaptador HTTP TypeSafe e ligação no worker.
Flag `JEV_HABILITADO` desligada por padrão. Alta confiança aplica; média sugere
confirmação; baixa/sem correspondência/falha pede revisão, preservando o papel.
Sugestões persistem no CDM v1 e saem no GET/PATCH; correção manual remove a
pendência do alvo com o controle de dono e concorrência já existente.

- RED inicial confirmado em domínio, DTO, adaptador e configuração por símbolos
  ausentes. Regressão adicional RED: probabilidade JSON null era aceita como
  zero; corrigida com validação de presença, focal GREEN.
- `go test ./... -race -count=1` PASS global (OOXML 37,460s, VO 20,747s).
  Após a correção null, focal llm PASS em 1,089s, cobertura 90,8%.
  Config focal race PASS em 1,042s, cobertura 91,6%.
- Build/vet/gofmt globais PASS; lint final após arquivos estabilizados: 0 issues.
  Teste independente CDM race: 1,069s, cobertura 94,0%. Segurança aprovou o
  snapshot sem críticos/altos. Riscos médios: privacidade dos trechos enviados
  e dívida anterior de recuperação se o contexto do worker for cancelado antes
  de MarcarFalha. Timeout HTTP sozinho degrada para revisão com contexto pai vivo.
- Sem inferência externa real, sem benchmark de precisão e sem novos E2E com
  containers. Limiares são experimentais. F5 completa ainda exige calibração,
  cache, métrica de tokens e teto monetário; detalhe no plano.
- Backend apenas: dados de confirmação disponíveis, tela ainda não implementada.

## Medição de 29/09 — suporte a partes alteradas

Implementado `Documento.SubstituirParte`, salvamento do delta e extração coerente.
No-op integral preservado; novo contrato na seção F3 de `plano-backend.md`.
Ainda não é o mutador de página nem fluxo de formatação ponta a ponta.

- RED confirmado por método ausente antes da implementação.
- `go test ./... -race -count=1`: PASS global; OOXML 19,067s, VO 11,082s.
- Após último ajuste nominal no teste: build/vet/gofmt limpos, lint `0 issues` e
  teste focal de falha de escrita/close com race PASS (1,055s).
- Segurança aprovou o delta por revisão estática. Validador não achou defeito
  funcional; pediu nomes expressivos no helper de teste e remoção de campos ZIP
  deprecated. Ambos corrigidos; lint/focal conferidos após correção.
- Integrações com containers e inspeção Word/LibreOffice não repetidas neste
  recorte de bytes. O limite de deltas não substitui validação geral do pacote.
- Testes de arquitetura corrigidos para analisar stdout de `go list` separado
  de avisos em stderr, evitando falso negativo com cache frio.

## Refatoração de 27/09 — auditoria do backend inteiro

Auditoria de funções, duplicação, fronteiras e documentação. Suíte verde entre
cada bloco de mudança. **Nenhuma alteração de comportamento observável pela API.**

Medição real ao fechar:

```
go build ./...                     PASS
go vet ./...                       limpo
go vet -tags=integration ./...     limpo
gofmt -l .                         limpo
golangci-lint run ./...            0 issues (conjunto estrito)
go test ./... -race -count=1       PASS, 31 pacotes, 0 falhas
```

**Integrações executadas de verdade em 27/09**, contra Postgres, MinIO e o
sidecar LibreOffice reais via Podman, com `-race`:

| Suíte | Resultado |
|---|---|
| `internal/data/postgres` | PASS, 12,234s |
| `internal/infra/fila` | PASS, 12,655s |
| `internal/infra/storage` | PASS, 18,688s |
| `migrations` | PASS, 18,757s |
| `internal/infra/pdfconv` (conversão DOCX→PDF real) | PASS, 200,170s |

As cinco suítes com `//go:build integration` foram executadas; nenhuma ficou
de fora. A `pdfconv` constrói o sidecar do Dockerfile, o que explica os 200s.

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

### Auditoria independente — 27/09

Os commits de 27/09 foram escritos e revisados pelo mesmo agente, o que o
`CLAUDE.md` desaconselha. `validador` e `seguranca` rodaram depois, somente
leitura, sobre `e373525..d994107` — 76 arquivos, incluindo o trabalho F2/F3
herdado de outra IDE que **nunca havia passado por revisão de ninguém**.

```
validador   APROVADO · 0 bloqueantes
seguranca   APROVADO · 0 críticos · 0 altos · 0 médios · 2 baixos
```

O que eles confirmaram, verificando em vez de aceitar a afirmação: a
consolidação da sessão não trocou a semântica de nenhum handler (mapeamento 1:1
dos chamadores contra o comportamento antigo); a reclassificação de CDM
corrompido está correta nos três chamadores, porque todos leem do banco e
nenhum recebe entrada HTTP direta; os `nil` nos testes do controlador não
alcançam caminho de produção; a ordem de inicialização em `montarDependencias`
é idêntica à anterior; e `config.lista` replica o comportamento de lista vazia
do antigo `origensCORS`.

Foram além do que a revisão interna havia olhado: conferiram que o CAS de
`SalvarEstrutura` compara status **e** CDM atomicamente, validaram os fatores
de conversão de unidade numericamente, e verificaram que o 404 de IDOR é **byte
a byte igual** entre "não existe" e "é de outro dono" — com o corpo do job
nunca carregando o campo `resultado` interno.

**Os dois achados BAIXO foram corrigidos:**

| Achado | Correção |
|---|---|
| Assimetria de validação: `jobs` recusava `uuid.Nil`, os cinco handlers de documento não | `rotasutil.IDDaRota` unifica os seis pontos. Não era explorável — nenhuma linha tem id nulo e o WHERE filtra por dono —, mas duas validações para a mesma coisa fazem a resposta certa depender do arquivo. De quebra removeu 6 cópias do mesmo bloco |
| `mapa-modulos.md` citava `rotas/middleware/sessao`, apagado na própria refatoração, e `ServicoJobs`, renomeado | Corrigido, e passou a haver varredura: os **42** caminhos citados no mapa existem no código |

**O que os auditores não conseguiram verificar:** `golangci-lint` (o binário não
está instalado no ambiente deles; roda via `go run`, como no `Makefile`), as
cinco suítes de integração (exigem containers de pé) e o `sandbox` do iframe em
navegador real. As três seguem como limitação declarada, não como aprovação.

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
| — | O **frontend está atrás da API**: não consome `POST .../analisar`, `GET/PATCH .../estrutura` nem `GET /v1/jobs/{id}`. Esperado — os endpoints são novos — mas significa que nenhum deles tem exercício por navegador. |
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
