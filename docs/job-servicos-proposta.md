# F1 — proposta para serviços de jobs

## Atualização — criação de domínio entregue em 2026-09-15

O recorte de criação foi formalizado em `job-criacao-contrato.md`, aprovado e
implementado em `job/criacao` com porta atômica, TDD/race 100% e revisores
aprovando. A proposta histórica abaixo não substitui esse contrato. SQL,
migration de chave e concorrência real continuam pendentes; nenhuma prova de
idempotência persistente foi produzida ainda. Consulta já entregue também.

## Revisão preventiva da criação — 2026-09-15

Agente de segurança revisou somente idempotência, sem código ou testes. A
proposta de escrita ainda NÃO está congelada. Pontos para fechar no próximo ciclo:

- Unicidade `(documento_id, chave)` arbitra concorrência no banco; não basta
  lookup seguido de insert. O perdedor compara o payload persistido e retorna
  o original ou conflito, sem sobrescrever. Autorizar também na operação atômica.
- Novos pedidos exigem UUID de chave não zero/não NULL; payload comparado é
  tipo + ruleset_id, tratando dois NULL como iguais e NULL/UUID como diferentes.
- A entidade já rejeita ruleset em preview/análise: manter essa regra, não
  ampliar aceitação por causa da constraint menos restritiva do schema atual.
- Repetições incluem jobs cancelados/concluídos/falhos, sem reativação. Sugestão
  mínima: devolver o mesmo job no estado ATUAL, não prometer resposta byte a
  byte idêntica à primeira criação. Formalizar isso em testes antes do código.
- Legado não tem chave: migration deve preservar cada job sem presumir
  duplicação por payload. Proposta: gerar chave nova por registro antigo,
  depois NOT NULL/CHECK não zero e UNIQUE, sem default para novos pedidos.
  Não alegar recuperação de chaves originais desconhecidas.
- Sem permissão ou documento inexistente: mesmo erro público, inclusive em
  reenvio ou payload conflitante; nunca revelar existência de job de terceiro.

Próximo entregável continua sendo contrato executável/testes da criação,
com validação de arquitetura e testes reais de concorrência na persistência.
Nenhuma implementação foi iniciada nesta revisão por margem de quota.

Investigação de 2026-09-14. **Proposta, não contrato aprovado nem implementação.**
Próximo recorte recomendado: somente leitura autorizada; auditar o delta de
spec antes de codar. Não repetir `docs/auditoria-specs-f1.md`.

## Primeiro subciclo

- `Obter(ctx context.Context, solicitante vo.Dono, id uuid.UUID) (entity.Job, error)`.
- `ListarDoDocumento(ctx context.Context, solicitante vo.Dono, documentoID uuid.UUID) ([]entity.Job, error)`.
- Porta de leitura com `ObterPorID` e `ListarPorDocumento`, também recebendo dono.
- Autorizar pelo documento associado, sem duplicar dono em Job. O adaptador
  futuro restringe por JOIN/EXISTS. Listagem verifica acesso ao documento antes
  de consultar jobs. Dono vazio falha antes de I/O; terceiro e inexistente
  retornam o mesmo erro público. Testar sessões/usuários com UUID coincidente,
  documento alheio e repositório adversarial, propagação de contexto/erros.
- Antes do RED, decidir retorno defensivo da porta para Obter: entidade Job
  não carrega dono, então checar também o documento associado via porta já
  existente, sem confiar apenas em uma convenção do repositório fake.
- Reaproveitar as specs úteis, preservando as operações futuras em arquivo
  separado se necessário. Não eliminar testes de escrita apenas para deixar
  toda a suite verde. Resultados internos nunca são DTO público ou log.

## Proposta posterior de escrita — ainda requer validação

Criar/Cancelar públicos recebem solicitante; processamento vai para pacote
interno sem dependência das rotas. Versão monotônica incrementada em TODA
escrita, inclusive progresso, para CAS. Worker também retém a tentativa obtida
ao iniciar: não pode adotar outra tentativa ao reler depois de um retry.
Status sozinho sofre ABA (executando→falhou→pendente→executando).

Chave de idempotência UUID por pedido: UNIQUE(documento_id, chave). Repetição
retorna job original mesmo encerrado; comparar tipo/ruleset incluindo NULL e
recusar payload divergente. Nova chave permite reprocessamento explícito.
Essas escolhas exigem testes e migration próprios, ainda não autorizados como
contrato congelado. Limite de retries/concorrência ainda precisa ser definido;
não inventar número nem implementar loop de retry ilimitado.

Nesta investigação nenhum código/teste/dependência foi alterado. Entidade Job
permanece na entrega aprovada anterior, sem Versao ou ChaveIdempotencia.
