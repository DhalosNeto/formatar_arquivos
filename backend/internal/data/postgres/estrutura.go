package postgres

import (
	"context"
	"encoding/json"
	"github.com/daniel-halos/formatador/internal/domain/documento/entity"
	"github.com/daniel-halos/formatador/internal/domain/documento/repository"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/google/uuid"
)

// SalvarEstrutura preserva o status e compara o CDM como jsonb.
func (r *RepositorioDocumento) SalvarEstrutura(ctx context.Context, dono vo.Dono, id uuid.UUID, anterior, novo json.RawMessage, statusAtual entity.Status) error {
	if dono.Vazio() {
		return errors.NovoErroValidacao("dono", "o dono do documento é obrigatório")
	}
	if id == uuid.Nil {
		return errors.NovoErroValidacao("id", "identificador do documento é obrigatório")
	}
	if !statusAtual.Valido() {
		return errors.NovoErroValidacao("status", "status inválido")
	}
	if err := entity.ValidarCDM(anterior); err != nil {
		return err
	}
	if err := entity.ValidarCDM(novo); err != nil {
		return err
	}
	usuarioID, sessaoID := dono.ParaColunas()
	marca, err := r.pool.Exec(ctx, `UPDATE documentos SET cdm = $5::jsonb, atualizado_em = now()
 WHERE id = $1 AND (usuario_id = $2 OR sessao_id = $3) AND cdm = $4::jsonb AND status = $6`, id, usuarioID, sessaoID, anterior, novo, string(statusAtual))
	if err != nil {
		return envolverPostgres(err, "salvar estrutura do documento")
	}
	if marca.RowsAffected() == 0 {
		if _, err := r.ObterPorID(ctx, dono, id); err != nil {
			return errors.Envolver(err, "consultar documento após conflito de estrutura")
		}
		return errors.NovoErroConflito("a estrutura ou o status do documento foi alterado")
	}
	return nil
}

var _ repository.EstruturaRepo = (*RepositorioDocumento)(nil)
