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
	// somente a nil. Payload diferente gera conflito; repetição devolve o mesmo
	// job no estado atual, inclusive terminal, sem sobrescrever ou reiniciar.
	// Nova chave permite novo job; documentos diferentes têm escopos distintos.
	// Erro ou conflito não pode deixar o candidato inserido. Sem retry ilimitado.
	InserirOuObter(ctx context.Context, solicitante vo.Dono, job entity.Job, chave uuid.UUID) (entity.Job, error)
}
