# Auditoria das specs herdadas da F1

Refeita no Codex em 2026-09-13 por agentes separados de segurança e validação,
somente leitura. Ambos reprovaram os contratos de `job/service` e `infra/storage`.
Esta é uma auditoria preventiva de testes: não demonstra exploração HTTP atual,
pois os endpoints e adaptadores correspondentes ainda não existem.

## Correções exigidas antes de implementar

- Jobs: consulta, listagem e cancelamento devem exigir `vo.Dono`, com terceiro
  recebendo a mesma resposta pública de recurso inexistente.
- Separar operações públicas das transições internas do processamento; impedir
  dependência das rotas sobre o pacote interno com teste de arquitetura.
- Progresso e transições precisam de comparação de estado/versão na escrita;
  testar atualização obsoleta após conclusão ou cancelamento.
- Definir idempotência de criação e reentrega. A unicidade no banco deve tratar
  `ruleset_id` nulo e não impedir reprocessamentos legítimos já encerrados.
- Erros técnicos e resultados não podem ser expostos como JSON livre na API:
  usar códigos/mensagens públicos e validar tamanho e estrutura dos resultados.
- Storage: URL assinada exclusivamente GET, bucket configurado e expiração
  positiva limitada; testar operação, chave, bucket e validade efetivos.
- Tamanho persistido deve ser medido do conteúdo limitado, não confiado ao
  número declarado pelo cliente (A2).
- `ChaveStorage` é conversível de string: revalidar na fronteira usando a regra
  existente do VO, sem criar outro validador divergente.
- Testar bucket privado e URL assinada contra MinIO; autenticar/autorizar antes
  de assinar a URL, pois o adaptador de storage não identifica o solicitante.
- A3 permanece bloqueante: conferir pacote real e limites ZIP antes de preview.
  `ConferirPacoteDocx(partes []string)` sozinho só confere nomes de partes;
  não mede descompressão nem valida conteúdo XML.

Referências principais: `job/service/job_test.go` (obter/listar/cancelar,
progresso, criação), `job/entity/job_test.go` (erro e resultado),
`infra/storage/s3_test.go` (URL, salvar, chaves e validade).

## Ordem de implementação

Fechar o Ciclo B de documentos; adaptar as specs de job e storage; implementar
os respectivos componentes e seus testes de integração; seguir para fila,
conversor, worker, rotas e preview. Os testes de tipo/status de job podem ser
reaproveitados, explicitando que falha encerra uma tentativa e admite retry.
