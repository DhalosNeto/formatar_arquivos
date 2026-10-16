# Retomada

Atualizado em 15/10/2026. Recorte `f3-ruleset-campos-silentes` **CONCLUÍDO**.
A **engenharia da F3 está completa** e existe o primeiro perfil normativo real.

## O que ficou pronto

`ruleset.Corpo` tem cinco campos opcionais (ponteiro, `omitempty` em json e
yaml): `Fonte`, `Alinhamento`, `RecuoCM`, `EspacoAntesPT`, `EspacoDepoisPT`.
`nil` = a norma é silente, preserve o documento. `AplicarPlano` pula o mutador
correspondente; `AplicarTipografia` recebe `*string` e com `nil` grava só
`w:sz`/`w:szCs`, preservando o `w:rFonts`.

`backend/rulesets/abnt-14724/v1.yaml` — primeiro perfil normativo, da ABNT NBR
14724:2024 que o usuário forneceu em PDF. Cada valor com seção citada: A4
21 × 29,7 e margens 3/2/3/2 de §5.1, corpo 12 de §5.1, entrelinha 1,5 de §5.2.
As cinco chaves silentes **ausentes**, com o motivo em comentário.

**Evidência em 15/10 21:52:** gates PASS, lint 0 issues, suíte global `-race`
exit 0 com 34 pacotes, integração com Podman ok (`data/postgres` 18.375s,
`infra/fila` 18.394s). Reverificado depois das duas correções de comentário.

## O que falta para a F3 fechar DE FATO

A engenharia está pronta, mas o perfil cobre só **§5.1 e §5.2** no que o motor
sabe aplicar — margens, corpo e entrelinha. Faltam, por ordem de valor:

1. **Exceções de espaçamento simples da §5.2** (citação longa, nota, referência,
   legenda): dependem de o CDM classificar esses papéis e de **mutadores por
   papel**, que não existem. É o maior buraco de conformidade real.
2. **NBR 10520** (citações) e **NBR 6023** (referências), a que a 14724 remete
   em §5.5 e §5.4. As tabelas na skill `normas-abnt` seguem **vazias** — exigem
   a fonte oficial de cada uma, como a 14724 exigiu.
3. **Numeração de página** (§5.3): exige mutar cabeçalho, que o motor não toca.
4. **Filete de 5 cm e fonte menor da nota de rodapé** (§5.2.1).

## Erro de processo registrado, para não repetir

Este recorte foi pré-auditado só pela arquitetura, com a justificativa de que
não havia superfície HTTP nova. A segurança apontou que isso é errado: **"sem
superfície HTTP nova" não equivale a "sem mudança no conjunto de entradas
aceitas"**. O recorte alterou a alcançabilidade de um validador de fronteira
(`validarAlvoTipografia`) e um contrato de serialização amarrado a teste de
imutabilidade. O resultado é sólido, mas por sorte de execução.
**Regra daqui em diante: mudança em validador de fronteira ou em contrato de
serialização chama segurança, mesmo sem rota nova.**

## Débitos abertos

Completos em `docs/estado-do-backend.md`, seção "Medição de 15/10". Os que mais
importam:

- **Mutação que sobrevive:** `definicao.go:224-231` reporta campo `"corpo"` para
  `EspacoAntesPT` e `EspacoDepoisPT`, não `corpo.espaco_antes`/`_depois`, e A1
  codifica isso — **trocar os dois campos entre si não seria detectado por teste
  algum**. Corrigir exige mudar produção e um critério que hoje passa.
- Perda permanente do par (documento, perfil) após qualquer falha do worker;
  recuperação exige caso de uso de reenfileiramento **escopado ao dono**.
- Job em `executando` sem lease nem reclaim.
- Procedência do ruleset modelada como URL única, o que não serve para norma
  técnica — a F6 vai precisar de procedência estruturada.
- `TamanhoPT` obrigatório embora §5.1 diga "Recomenda-se".

## Estado do Git

Branch `f3-motor-e-gate-de-ruleset`, com push em `bddbd9d`. **TRÊS recortes
concluídos e verdes NÃO estão commitados** — gate de ruleset inativo, endpoint e
download, e campos silentes mais o perfil normativo. São o delta não rastreado
sobre esse commit. `main` intocada em `5d106fb`.

A revisão de segurança delimitou que sua aprovação cobre só os seis arquivos do
delta nomeado; `fila/executor.go`, `rotas/.../controlador.go`,
`vo/chavestorage.go`, `webservices/formatacao.go` e `webmodel/documento.go` são
de recortes anteriores e não foram auditados nesta rodada.

Nenhum agente ativo.

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
