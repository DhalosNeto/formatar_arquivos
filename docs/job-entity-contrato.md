# F1 — job/entity

Recorte: entidade, tipos/status e testes. Sem fila, persistência, HTTP ou deps
novas. Auditoria herdada: `docs/auditoria-specs-f1.md`. Autorização por dono,
CAS persistente, idempotência e DTOs públicos pertencem ao próximo ciclo de
serviços/adaptadores; não declarar esses requisitos resolvidos pela entidade.

Reaproveitar assinaturas/campos dos testes existentes. NovoJob retorna Job{} e
ErroValidacao com campos acumulados para documento zero/tipo inválido/ruleset
indevido ou ausente. Copiar RulesetID. Criar UUID e data UTC, status pendente.

Transições: pendente→executando/cancelado; executando→concluído/falhou/cancelado;
falhou→pendente. Demais conflitos sem mutação, verificados antes de payload.
Iniciar incrementa tentativas. Progresso executando apenas, 0–100, monotônico.
Concluir fixa 100; falhar/cancelar preservam progresso. Datas UTC. Reenfileirar
limpa erro/resultado/progresso/datas da tentativa; preserva ID/criação/tentativas.
Falhou é terminal para a tentativa mas admite retry. Duração exige início e fim.

Concluir(nil) permite ausência de resultado. Não nil exige UTF-8 válido e único
objeto JSON, teto `TamanhoMaximoResultadoBytes = 64 << 10` sobre os bytes brutos,
antes de parse/cópia. Recusar vazio, branco, null, array, escalar, inválido,
excedente. Copiar resultado aceito; rejeições não alteram entidade.
Resultado é metadado INTERNO, não DTO público nem conteúdo para logs; JSON
estruturalmente válido não garante ausência de segredo ou conteúdo do usuário.

Falhar(string) permanece compatível: branco gera validação no campo erro;
qualquer outro motivo grava só `Não foi possível processar o documento.`.
Nunca persistir/interpolar/truncar motivo técnico; testes antigos de preservação
e truncagem são substituídos por prova de não vazamento. Mensagens fixas via
infra/errors, como no restante do projeto.

Preservar testes úteis; acrescentar bordas/imutabilidade/cópia de ponteiro,
resultado inválido e limites exatos. Executar RED antes de produção e GREEN
com race/cobertura. Não modificar specs de job/service neste recorte.
