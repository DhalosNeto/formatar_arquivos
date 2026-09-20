# F1 — criação autorizada de jobs

Recorte de 2026-09-15, derivado da investigação e revisão preventiva em
`job-servicos-proposta.md`. Somente domínio e porta; sem adaptador SQL,
migration, fila, cancelamento, worker ou HTTP. Não prova idempotência durável
ou concorrência no banco: isso exige integração posterior com PostgreSQL.

Contrato aprovado preventivamente por agente revisor de arquitetura/segurança
em 2026-09-15; liberado para TDD deste recorte, não para publicação HTTP.

## API do recorte

- `repository.CriacaoJobRepo.InserirOuObter(ctx context.Context, solicitante vo.Dono, job entity.Job, chave uuid.UUID) (entity.Job, error)`.
- Pacote `job/criacao`: `DadosNovoJob` com `DocumentoID uuid.UUID`,
  `Tipo entity.TipoJob`, `RulesetID *uuid.UUID`, `ChaveIdempotencia uuid.UUID`.
- `NovoServico(jobs repository.CriacaoJobRepo, documentos *documentoservice.Servico) (*Servico, error)` rejeita dependências nil, como consulta.
- `(*Servico).Criar(ctx context.Context, solicitante vo.Dono, dados DadosNovoJob) (entity.Job, error)`.

## Fluxo e erros

1. Dono vazio e chave zero são erros de validação antes de qualquer I/O.
   Reusar `entity.NovoJob` para validar documento, tipo e ruleset, também
   antes de I/O; não duplicar essas regras.
2. Autorizar documento pelo serviço público REAL `documento/service.Obter`;
   nunca confiar só em UUID/igualdade de dono nem fazer bypass de sistema.
3. Chamar uma única operação `InserirOuObter`, propagando contexto, dono,
   candidato validado e chave. Não implementar lookup seguido de insert.
4. Retorno defensivo: ID zero ou DocumentoID divergente produz não encontrado.
   Tipo/ruleset divergentes produzem conflito fixo, sem expor payload. Comparar
   UUIDs por valor; nil equivale só a nil. Retornar Job zero em todo erro.
5. Ausências tipadas vindas de documento ou job são normalizadas SEM wrapper
   para `NovoErroNaoEncontrado("job")`: mensagem exata `job não encontrado`.
   Outros erros mantêm tipo/causa via `errors.Envolver`. Sem conteúdo/token em
   mensagens novas. Entidade retornada é interna, nunca DTO/log público.

## Obrigação atômica da porta (adaptador futuro)

Revalidar autorização do documento dentro da operação persistente, inclusive
antes de decidir retorno repetido/conflito. Dono revogado/terceiro e documento
ausente devem resultar no mesmo não encontrado. Respeitar espécie do dono.
Arbitrar pela unicidade `(documento_id, chave)`; chave obrigatória, não zero.
Payload é tipo + ruleset_id, com comparação NULL-safe. Repetição devolve o
mesmo job no estado ATUAL, inclusive terminal, sem sobrescrever nem reiniciar.
Payload diferente gera conflito. Nova chave permite novo job; documentos
diferentes têm escopos de chave diferentes. Autorizar e decidir atomicamente,
sem deixar candidato inserido quando houver erro/conflito. Sem retry ilimitado.

Job não precisa carregar a chave: parâmetro separado da porta. Não adicionar
versão/tentativa neste recorte de criação. Migração futura preservará registros
legados com chaves novas individuais; não pressupor chaves originais conhecidas.

## TDD e limites da entrega

Cobrir dependências, entradas inválidas sem I/O, contexto/ordem de autorização,
sessão/usuário com mesmo UUID, terceiro/ausente indistinguíveis, falhas tipadas,
resposta adversarial da porta e repetição com jobs terminais sem reinício.
Fake exercita fluxo/contrato, NÃO demonstra atomicidade SQL. Testes antigos de
`job/service` permanecem inalterados e vermelhos. Sem dependências novas.
Testador só testes; codador só produção; revisores somente leitura.
