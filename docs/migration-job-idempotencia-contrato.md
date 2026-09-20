# F1 — contrato da migration de idempotência de jobs

Recorte preparado e implementado em 2026-09-15. Testes reais PostgreSQL com
`-race` passaram em 10.695s, incluindo regressão 00002 e Up/Down/Up da 00003.
Revisão estática aprovada. Lint integration ainda aponta contextcheck no helper
preexistente `documento_dono_test.go:169`; não resolvido neste recorte.
Complementa `job-criacao-contrato.md`; não altera entidade ou serviço Go.
Revisão preventiva por agente concluída: APROVADO para iniciar os testes de
migration, sem bloqueantes. Revisão documental, não validação executável.
Próxima migration prevista: `00003_job_idempotencia.sql` (confirmar numeração
livre ao retomar). Usar Goose transacional, como as migrations existentes.

## Up

1. Alterar `jobs`, sem recriar tabela: adicionar `chave_idempotencia uuid`
   inicialmente nullable e SEM default.
2. Preencher cada registro antigo com `gen_random_uuid()`. Não deduplicar
   por documento, tipo, ruleset ou status; jobs semelhantes podem ser pedidos
   legítimos distintos. Não se recuperam chaves históricas desconhecidas.
3. Tornar a coluna NOT NULL e adicionar CHECK de UUID diferente de zero.
4. Adicionar UNIQUE `(documento_id, chave_idempotencia)`. Não incluir tipo,
   ruleset ou status na unicidade: payload divergente deve ser conflito e
   job encerrado continua ocupando sua chave.

Manter SEM default após Up: todo novo pedido deve informar a chave. Não
modificar colunas existentes, FKs, índices anteriores ou registros dependentes
em `artefatos` e `relatorios_mudanca`. Não fortalecer outras constraints neste
recorte (o schema legado aceita ruleset em análise/preview, embora domínio não).
Uma colisão improvável no backfill deve falhar/rollback, nunca apagar um job.

## Down e implantação

Remover somente a coluna e constraints criadas aqui, usando nomes explícitos;
preservar jobs e dependentes. **Down perde todas as chaves de idempotência.**
Up → Down → Up gera chaves novas; não prometer continuidade das antigas.
Não executar Down em produção sem decisão operacional sobre reenvios pendentes.

Esta migration quebra INSERTs que omitem chave. Aplicar com produtores de jobs
parados e retomar apenas com adaptador compatível; não alegar rolling upgrade
sem interrupção. A implantação coordenada fica para quando existir adaptador.
Não aplicar mudanças a banco de usuário nesta preparação.

## Testes de integração antes da implementação

Reusar o harness PostgreSQL/Goose de `backend/migrations/documento_dono_test.go`
e orientações do README, sem novo framework ou dependência.

- Aplicar 00001/00002; inserir jobs de todos os status, inclusive dois de
  mesmo documento/tipo/ruleset, com artefatos e relatórios ligados aos IDs.
- Aplicar 00003: quantidade, campos anteriores, IDs e dependentes intactos;
  todas as chaves presentes/não zero e distintas no mesmo documento.
- Verificar coluna UUID, NOT NULL, ausência de default; NULL, omissão e UUID
  zero recusados. Não depender de mensagem textual PostgreSQL para erro;
  conferir SQLSTATE ou constraint correspondente.
- Mesmo documento/chave recusado mesmo com payload/status diferente.
  Mesma chave em outro documento aceita; nova chave no mesmo documento aceita.
- Down preserva dados/FKs/índices anteriores e remove somente o delta;
  Up de novo funciona com chaves válidas novas. Conferir versão Goose.
- Fixtures isoladas; nenhum skip para simular sucesso, cleanup de recursos
  próprios. Rodar com build tag integration e registrar saída real.

Esses testes provam schema, não a operação `InserirOuObter`. O adaptador ainda
precisa de testes concorrentes, comparação NULL-safe, autorização transacional
e retorno do job atual; ficam em ciclo posterior, com contrato próprio.

## Retomada

Após revisão preventiva deste contrato: testador escreve RED de integração;
codador escreve somente migration; testador confirma GREEN; validador e
segurança revisam delta; principal registra comandos e resultados.
Não iniciar adaptação SQL contra testes legados inseguros de `job/service`.
