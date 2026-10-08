# Retomada

Atualizado em 08/10/2026. Recorte `f3-gate-ruleset-inativo` **concluído**:
4 contratos, 15 critérios, V1/V2/V3 todos `passou`, integração EXECUTADA.
A árvore contém alterações de várias sessões e arquivos não rastreados;
preservar tudo. Nada commitado. A spec ativa também é não rastreada: não
assumir que versões anteriores sejam recuperáveis pelo Git.

## O que ficou pronto

`TipoJob.ValidarRulesetParaNovoJob(ativo *bool)` em `entity/tipo.go` e o gate
em `InserirOuObter` (`data/postgres/job.go`). Job NOVO que exige ruleset precisa
de perfil existente e ativo; ausente e inativo devolvem erro IDÊNTICO em
`ruleset_id`. Repetição de chave vence o gate. Ordem no código: `SET LOCAL
lock_timeout` → `FOR SHARE` em `documentos` → gate → INSERT → releitura.

A implementação ficou **mais barata que o contrato original**: o gate roda
primeiro e só consulta a chave quando reprova, então o caminho feliz não paga
query extra. Foi por isso que reescrever o contrato do adaptador como
INVARIANTES em vez de ordem de queries valeu a pena.

Ciclo: spec → pré-auditoria (2 rodadas, arquitetura e segurança) → RED
executado → código → V1/V2/V3 → revisão do delta (segurança APROVA;
arquitetura APROVA COM CORREÇÕES, 1 bloqueante) → correção → fechamento.

## Evidência de 08/10, reproduzida pelo principal

- build/vet/vet-integration/gofmt PASS; `golangci-lint run ./...` 0 issues;
  `go test ./... -race -count=1` exit 0, 34 pacotes, nenhum FAIL/race.
- Integração com Podman: `-race` exit 0 em 19.265s. O teste de modo de lock
  (`TestInserirOuObterGateDePerfilUsaForShare`) levou 0,23s, coerente com o
  deadline de 200 ms disparando — **a prova de lock executou**, não ficou só
  escrita.
- Shuffle: ok com a seed que ANTES falhava (1791499507274547732) e com seed
  aleatória. Comando de integração desta máquina:
  `env DOCKER_HOST=unix:///run/user/1000/podman/podman.sock MIGRACOES_REDE_CONTAINER=slirp4netns TESTCONTAINERS_RYUK_DISABLED=true go test ./internal/data/postgres -tags integration -race -count=1`

## Quatro erros deste recorte, para não repetir

1. **Justificativa factual errada aceita por leitura.** Escrevi que "inserir e
   validar depois" exigiria `SAVEPOINT`; falso, ele só serve para CONTINUAR a
   transação. Pior: exigi prova para a afirmação do `FOR SHARE` e aceitei essa
   por leitura no mesmo parágrafo. O motivo real é de lock, e o RED o provou.
2. **Duas afirmações INFALSIFICÁVEIS** na spec ("análise e preview não pagam
   query alguma", "sem query ao catálogo"), repetindo um erro que a auditoria
   já tinha me corrigido em outro critério na rodada anterior.
3. **Terceiro comentário mentiroso, num comentário que eu dicteti.** Mandei
   dizer que "o contexto vence antes do lock_timeout" — verdade só no teste, que
   usa deadline de 200 ms de propósito; em produção não há prazo por requisição.
   Este recorte tinha DOIS contratos (C3, C4) existindo só para consertar
   comentários falsos, e produziu um terceiro.
4. **Teste dependente de ordem.** A pré-condição global de catálogo vazio
   passava por sorte alfabética. `-shuffle=on` derruba. Vale rodar shuffle ao
   fechar recorte que toque integração.

## Próximo recorte

Branch `TipoFormatar` no executor. Precisa:

1. Campo de resultado no `Documento` + migration (hoje só `ChaveStorage` e
   `ChaveStoragePDF`).
2. Branch em `fila/executor.go:93`, que hoje cai em "tipo de job não suportado",
   usando `RulesetsConsulta().ObterPorID` + `Planejar` + `AplicarPlano`.
3. **Takedown**, que este gate NÃO resolve: job pendente criado antes da
   desativação segue executável, `Reenfileirar` o devolve para pendente e
   `Reivindicar` não consulta `rulesets`. A porta já devolve o flag `ativo` e
   nenhum Go o consome.
4. **Aí sim** `context.Context` nos sete mutadores OOXML, com teste real de
   cancelamento, e o single-parse do `document.xml`.
5. Prazo por requisição no caminho HTTP — sem ele, `lock_timeout` de 3s é o teto
   e dez requisições presas esgotam o pool de 10.

Débitos completos em `docs/estado-do-backend.md`, seções de 08/10. Os que mais
importam: reprodutibilidade do ruleset SEM LASTRO até o checksum ser verificado;
SSRF em `Fonte`; `RulesetsConsulta()` sem consumidor de produção (código morto se
o dono não entregar o consumidor); e o buraco do gate de lint, que roda
`golangci-lint` sem a tag `integration`.

F3 NÃO está concluída. `Planejar`, `AplicarPlano` e `ObterPorID` seguem sem
chamador de produção, e nenhuma rota cria `TipoFormatar`.

Nenhum agente ou comando ativo.

## Pausa posterior em 08/10 — investigação do gate de criação

Usuário autorizou prosseguir com o gate de rulesets inativos. Git e instruções
reconciliados; nenhum delta prévio em domain/job nem data/postgres/job.go.
Árvore suja preservada. Nenhum código ou teste alterado nesta rodada.
A spec ativa permanece f3-leitura-ruleset-por-id, concluída; ainda NÃO foi
substituída por spec do gate. Evidência focal histórica da leitura preservada
também em docs/estado-do-backend.md antes da futura substituição.

Fatos conferidos: CriacaoJobRepo.InserirOuObter mantém assinatura; o adaptador
devolve o job existente mesmo com payload divergente, e Servico.Criar decide
o conflito. O teste TestInserirOuObterPayloadDivergenteDevolveJobExistente
documenta essa divisão, embora o comentário da porta pareça prometer conflito
no adaptador. Não mover a regra incidentalmente. Testes de integração estão
em data/postgres/postgres_integration_test.go, não job_integration_test.go.

Proposta em investigação, NÃO aprovada: serializar criação por documento,
reautorizar dono com lock, consultar a chave existente antes de consultar
ativo, e apenas para job novo ler ruleset sob FOR SHARE até o commit.
Regra pura no domínio; adaptador coleta fatos e mantém a transação. Avaliar
FOR UPDATE no documento e READ COMMITTED explícito, custo de serialização e
testes determinísticos das duas ordens de desativação/criação. Preservar
repetição terminal, conflito de payload e erros iguais para ausente/inativo.
Sem novo framework de transação, executor, frontend ou alteração OOXML.

Referência consultada: documentação PostgreSQL 16, explicit-locking.html
(FOR SHARE bloqueia UPDATE não-chave; FOR KEY SHARE não basta) e
transaction-iso.html. Isso é fundamento de desenho, não teste executado.

Gate desta rodada, backend: go build ./... && go vet ./... && gofmt -l .
exit 0, sem saída, em 08/10. Nenhuma suíte executada nesta rodada.
Quota passou de 69% para 93% na janela de cinco horas (41% semanal);
pausa exigida pelo AGENTS.md. Nenhum reset consumido.

Investigador concluiu a síntese parcial e foi encerrado. Propôs
TipoJob.ValidarRulesetParaNovoJob(existe, ativo bool) error em entity/tipo.go:
para tipo válido que exige ruleset, ausência/inatividade devolvem a mesma
validação em ruleset_id; análise/preview passam. Ainda faltam fechar os
testes determinísticos dos locks, erros SQL/contexto e destino do atual
ON CONFLICT/releitura. Proposta não auditada; nenhuma nova spec gravada.
Nenhum agente ou comando permanece ativo.

Próximo passo: consultar quota, concluir os pontos pendentes da proposta,
gravar nova spec apenas depois de preservar a anterior, pré-auditar com
validador/segurança; então RED → codador → testes/revisões → fechamento.
Próximo comando: git status --short; ler este checkpoint e a spec ativa.
Não tratar esta investigação como implementação ou aprovação de contrato.

Atualizado em 08/10/2026. Recorte f3-leitura-ruleset-por-id concluído:
14 critérios, V1/V2 históricos aprovados e V3 executada neste fechamento.
A árvore contém alterações de várias sessões e arquivos não rastreados;
preservar tudo. Nada commitado ou enviado. A spec ativa também está não
rastreada: não assumir que suas versões anteriores são recuperáveis pelo Git.

## Reconciliação e trabalho desta retomada

O Docker continua ausente, mas o Podman 5.7.0 está instalado e seu socket local
está disponível. A alternativa já estava documentada em
backend/migrations/README.md. Não foi preciso instalar software ou alterar
produção/testes. O acesso ao socket e a execução foram autorizados pelo
mecanismo de escalonamento do ambiente.

Corrigidas divergências documentais: C3 da spec ainda descrevia cinco colunas,
embora a correção final use quatro; A11 agora descreve slug inválido, A12 os
quatro casos de procedência e A14 distingue recusa observada de inferência
sobre transporte. Estado/mapa atualizados para retirar o bloqueio antigo.

## Evidência real de 08/10/2026

Responsável pela V3: principal. Diretório: backend.

~~~sh
DOCKER_HOST=unix:///run/user/1000/podman/podman.sock \
MIGRACOES_REDE_CONTAINER=slirp4netns TESTCONTAINERS_RYUK_DISABLED=true \
go test ./internal/data/postgres -tags integration -run 'TestObterRulesetPorID' -race -count=1 -v
~~~

Exit 0; pacote postgres 11.226s. Sete testes principais PASS: perfil semeado,
ID ausente/zero, flag inativo, definição inválida, quatro divergências de
slug/versão, contexto cancelado e tamanho exatamente 65536/65537 bytes.
Testcontainers usou a imagem PostgreSQL fixada por digest no teste. Container
0dd2892f8b7e criado, parado e removido pelo TestMain. Ryuk desativado apenas
nesta execução local, com cleanup explícito; não usar prune global.

go build ./... && go vet ./... && gofmt -l .: exit 0, sem saída.
Nenhuma alteração de produção nesta retomada. V1 e V2 continuam com as
medições anteriores da spec (V2 de 08/10 às 08:30, 34 pacotes com -race);
a suíte global não foi repetida. A14 não mede alocação nem tráfego do driver.

Validador e segurança reauditaram independentemente o delta corretivo final,
somente leitura, e ambos APROVARAM sem bloqueios: SELECT/Scan com quatro
colunas, NULL acima do teto, teste ligado ao limite do carregador e mensagens.
Revisão estática não substituiu a execução de V3 acima.

## Contratos entregues

- ConsultaRulesetRepo.ObterPorID(ctx, UUID) devolve Definicao, ativo e erro;
  porta segregada, exposta por data/contracts e Gerenciador.RulesetsConsulta.
- decodificarRuleset aplica teto, unmarshal e Validar; corrupção vira
  ErroAplicacao sem encadear JSON/ErroValidacao ou valores armazenados.
- SELECT usa CASE/octet_length para devolver NULL acima de 65536 bytes;
  quatro colunas: payload, slug, versao e ativo. Scan em json.RawMessage;
  len(dados)==0 recusa o NULL. Confere slug/versao das colunas com o JSON;
  devolve o flag ativo sem filtrá-lo. Driver sanitizado e contexto propagado.

## Próximo recorte

Preparar spec do gate de ruleset na CRIAÇÃO de job formatar, antes de ligar o
executor. Conferido nesta retomada:

- domain/job/criacao/servico.go: Criar autoriza o documento e chama
  InserirOuObter; não consulta ativo.
- data/postgres/job.go: InserirOuObter reautoriza o dono em transação com
  FOR SHARE no documento; INSERT ON CONFLICT e releitura asseguram idempotência.

O novo contrato deve decidir a validação de ativo para job NOVO preservando
repetição idempotente de job existente, conflito de payload e autorização.
Uma consulta simples antes de InserirOuObter pode recusar uma repetição após
inativação; consulta e INSERT separados também permitem desativação concorrente.
São riscos de desenho a resolver na spec e nos critérios de integração,
sem implantar regras de negócio na camada de dados por conveniência.

Próxima leitura: rg -n 'job-dominio|job.go|ruleset' docs/mapa-modulos.md,
serviço de criação, porta CriacaoJobRepo e testes de idempotência. Investigador
usa spec-verificavel; validador/segurança auditam a nova spec antes do RED.
Não substituir a spec concluída sem preservar suas evidências/pendências.

Depois do gate: fluxo de resultado, armazenamento e branch TipoFormatar;
propagação de context nos mutadores ao integrá-los. F3 segue sem fluxo completo.
Não presumir necessidade de migration antes de definir se o resultado pertence
ao documento ou ao job; a decisão anterior ainda não foi contratada.

## Débitos preservados

Constam em docs/estado-do-backend.md: checksum não conferido, sanitização dos
demais erros Postgres, deadlines/cancelamento do executor, lint com tag
integration e validação de Fonte caso venha a ser buscada pelo servidor.
Trocar o teto concatenado por placeholder é sugestão de estilo, sem bloqueio.
A mensagem da função pura agrupa vazio/excesso; revisar se ganhar outro uso.
Não afirmar conformidade normativa nem reprodutibilidade contra alterações
arbitrárias no banco. Nenhum LLM/Jev foi chamado.

## Agentes, comandos e quota

Dois revisores desta retomada concluídos e encerrados. Nenhum escritor de
produção delegado; nenhum comando de teste permanece ativo.
Última quota consultada: 42% da janela de cinco horas e 33% semanal; nenhum
reset consumido. Consultar novamente antes da próxima delegação.
