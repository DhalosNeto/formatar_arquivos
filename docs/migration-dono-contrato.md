# F1 — contrato da migration de dono

Escopo: `00002_documento_dono.sql`, teste de integração em PostgreSQL 16
descartável e documentação de execução. Não aplicar em banco do usuário.

Up transacional: ALTER TABLE adiciona `sessao_id uuid` sem default. Preencher
somente documentos sem usuário com `gen_random_uuid()` por linha, antes dos
CHECKs. Exigir exatamente um dono (`num_nonnulls = 1`) e recusar UUID zero nas
duas colunas, inclusive se houver usuário legado de ID zero. Nesse último caso,
falhar e reverter toda a migration, sem atribuir outro dono automaticamente.
Índice parcial não único `(sessao_id, criado_em DESC) WHERE sessao_id IS NOT NULL`.
Preservar tabela, dados, índices existentes e FKs. Não alterar a migration 00001.

Down remove apenas índice, constraints novas e coluna. Não usar CASCADE nem
recriar tabelas. **Down perde os identificadores das sessões**; órfãos legados
também não recuperam sua sessão original no Up. Um novo Up gera novas identidades.
Desfazer exige backup prévio e avaliação operacional; o teste só usa dados fictícios.

Teste primeiro: integração com testcontainers-go, sem mock, versão disponível
v0.44.0. Executar migrations reais com Goose v3.28.0 se disponível, em transações.
Casos: comparação de colunas antigas/OIDs/FKs e dependentes; usuários preservados;
backfill distinto/não zero; INSERT/UPDATE com ambos/nenhum/zero recusados; dono
válido aceito e mesma sessão permitida em vários documentos; índice parcial;
Up/Down/Up; falha de validação reverte inclusive a adição de coluna. Sem skip
silencioso se container indisponível. Não modificar testes herdados job/storage.
