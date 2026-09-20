# F1 — leitura autorizada de jobs

Recorte da proposta `job-servicos-proposta.md`, antes do código. Somente
`internal/domain/job/consulta` e porta `job/repository/consulta.go`; preservar
integralmente os testes herdados de `job/service` (escritas continuam pendentes).

`repository.ConsultaJobRepo`:

```go
ObterPorID(ctx context.Context, solicitante vo.Dono, id uuid.UUID) (entity.Job, error)
ListarPorDocumento(ctx context.Context, solicitante vo.Dono, documentoID uuid.UUID) ([]entity.Job, error)
```

`consulta.NovoServico(jobs repository.ConsultaJobRepo, documentos *documentoservice.Servico) (*Servico, error)`
recusa dependências nil. Reusar o serviço público de documento já testado para
autorizar e verificar dono/ID retornados pelo repositório; não duplicar sua regra.
`consulta.Servico` expõe somente:

```go
Obter(ctx context.Context, solicitante vo.Dono, id uuid.UUID) (entity.Job, error)
ListarDoDocumento(ctx context.Context, solicitante vo.Dono, documentoID uuid.UUID) ([]entity.Job, error)
```

Dono vazio ou ID vazio: validação antes de qualquer I/O. Obter consulta porta
escopada, exige ID retornado igual ao pedido e DocumentoID não zero, depois
autoriza documento associado via documentos.Obter. Nenhum Job é retornado
antes disso. Listar autoriza documento ANTES da porta de jobs, inclusive para
lista vazia; exige cada Job.ID não zero e DocumentoID igual ao solicitado.
Lista adversarial é rejeitada integralmente, sem resultado parcial.

Toda ausência/terceiro, inclusive erro de não encontrado embrulhado por qualquer
porta, é normalizada para NovoErroNaoEncontrado("job"), texto idêntico. Erros
demais preservam tipo via infra/errors. Não fazer log/ecoar IDs ou conteúdo.
Propagar contexto e dono em todas consultas. Porta futura deve aplicar JOIN/
EXISTS por dono no banco; isso ainda não é adaptador persistente implementado.

Testes novos em consulta/servico_test.go: happy path, vazio, dono/ID inválidos
sem I/O, sessões diferentes e usuário/sessão mesmo UUID, documento adversarial,
job de outro documento ou ID divergente, not-found byte a byte, falhas tipadas,
ordem das consultas e contexto. Usar serviço real de documento com repo fake.
Sem deps novas, migrations, HTTP, fila ou escrita. Retorno é interno; futuro DTO
deve omitir Resultado e conteúdo técnico, e listagem HTTP precisará paginação.
