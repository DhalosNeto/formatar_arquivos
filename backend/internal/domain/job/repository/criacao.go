package repository

import (
	"context"

	"github.com/google/uuid"

	"github.com/daniel-halos/formatador/internal/domain/job/entity"
	"github.com/daniel-halos/formatador/internal/domain/vo"
)

// CriacaoJobRepo exige autorização e decisão de idempotência atômicas no adaptador.
type CriacaoJobRepo interface {
	// InserirOuObter revalida o acesso ao documento, respeitando a espécie do dono,
	// inclusive antes de devolver repetição ou conflito. Documento ausente, dono
	// revogado e terceiro resultam no mesmo erro de não encontrado.
	// A chave é obrigatória e não zero; a unicidade é (documento_id, chave).
	// Tipo e ruleset_id compõem o payload, comparado por valor e com nil igual
	// somente a nil. O ADAPTADOR NÃO DECIDE CONFLITO: chave ocupada devolve o
	// job existente no estado atual, inclusive terminal, sem sobrescrever nem
	// reiniciar; quem compara o payload e devolve conflito é o serviço de
	// criação (criacao/servico.go:68). A divisão é intencional e está testada
	// em data/postgres/postgres_integration_test.go:622.
	// Nova chave permite novo job; documentos diferentes têm escopos distintos.
	// Erro ou conflito não pode deixar o candidato inserido. Sem retry ilimitado.
	//
	// GATE DE PERFIL: job NOVO cujo tipo exige ruleset só é criado se o perfil
	// existir e estiver ativo; perfil ausente e perfil inativo devolvem o MESMO
	// erro de validação em ruleset_id, indistinguíveis por requisito de
	// segurança. Repetição de chave vence o gate: devolve o job existente
	// INDEPENDENTE do estado atual do perfil.
	//
	// O gate é de CRIAÇÃO e NÃO retira perfil de circulação: job pendente criado
	// antes da desativação continua executável, Job.Reenfileirar
	// (entity/job.go:165) o devolve para pendente e Reivindicar
	// (data/postgres/job.go) não consulta rulesets. Aprovar este gate não
	// significa que o operador já consegue tirar uma versão do ar.
	InserirOuObter(ctx context.Context, solicitante vo.Dono, job entity.Job, chave uuid.UUID) (entity.Job, error)
}
