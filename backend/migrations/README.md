# Teste de migrations

`documento_dono_test.go` usa PostgreSQL 16 descartável, Testcontainers e o
provider real do Goose. Não lê `POSTGRES_DSN` nem aplica mudanças no banco do
projeto. A porta do container fica restrita a `127.0.0.1`; os dados são fictícios.

Em `backend`, com Docker/Testcontainers configurado:

```sh
go test -tags=integration ./migrations -race -count=1 -v
```

Para Podman local, configure `DOCKER_HOST` para um socket ativo. Nesta máquina,
a rede bridge rootless falhou ao limpar o namespace; a alternativa abaixo
passou, incluindo a remoção dos containers e volumes:

```sh
MIGRACOES_REDE_CONTAINER=slirp4netns TESTCONTAINERS_RYUK_DISABLED=true \
  go test -tags=integration ./migrations -race -count=1 -v
```

`MIGRACOES_REDE_CONTAINER` é opção deste teste, não configuração de produção.
Vazia, mantém a rede padrão. Ryuk só foi desativado nesta execução local: o
teste registra cleanup explícito e falha se não conseguir limpar. Se o processo
for interrompido abruptamente, conferir os containers/volumes identificados
pela sessão Testcontainers antes de removê-los; não usar prune global.
Referência de configuração: [Testcontainers com Podman](https://golang.testcontainers.org/system_requirements/using_podman/).

## Cuidados operacionais da 00002

Up preserva a tabela e seus vínculos. Documentos legados sem usuário ganham
sessões novas e distintas; isso **não recupera** seus donos originais. UUID zero
em proprietário legado reprova a migration inteira, sem reassociação silenciosa.

**Down descarta os identificadores de sessão.** Não é reversível do ponto de
vista da identidade, embora preserve documentos e dependentes. Fazer backup e
avaliar a perda de acesso antes de qualquer rollback fora do banco descartável.
Um novo Up gera outras sessões, não restaura as anteriores.

## Idempotência de jobs — 00003

`job_idempotencia_test.go` verifica backfill preservando dados e dependentes,
chave UUID obrigatória sem default, não zero, unicidade por documento/chave e
Up/Down/Up. Mesma chave em outro documento é permitida. Executa pelo comando
de integração acima, usando o mesmo PostgreSQL descartável.

**Down perde as chaves de idempotência.** Um novo Up gera outras, não recupera
as anteriores. Coordenar implantação com produtores parados até que todos os
INSERTs informem chave; não há compatibilidade com escritores antigos que a
omitem. Esta migration não implementa o adaptador atômico InserirOuObter.
