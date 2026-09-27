// Package jobs expõe a consulta HTTP de jobs da sessão atual.
package jobs

import (
	"context"

	"github.com/google/uuid"

	"github.com/daniel-halos/formatador/internal/application/web/webservices"
	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/daniel-halos/formatador/internal/rotas"
	"github.com/daniel-halos/formatador/internal/rotas/rotasutil"
	"github.com/daniel-halos/formatador/internal/rotas/sessao"
)

// CaminhoItem é a rota de consulta de um job pelo id.
const CaminhoItem = "/jobs/:id"

const campoID = "id"

// Mensagens e nomes de recurso fixos: nunca ecoam id, cookie nem conteúdo do
// usuário (CLAUDE.md, regra 7).
const (
	mensagemIDInvalido = "identificador inválido"

	// recursoJob é o recurso declarado quando falta sessão. Cookie ausente
	// responde como job inexistente de propósito: um 401 aqui distinguiria
	// "sem sessão" de "job de outro dono" e viraria oráculo de existência.
	recursoJob = "job"
)

// Controlador atende a consulta HTTP de job.
type Controlador struct {
	servico *webservices.ServicoJob
}

// NovoControlador cria o controlador de jobs.
func NovoControlador(servico *webservices.ServicoJob) *Controlador {
	return &Controlador{servico: servico}
}

// TratarObtencao devolve o progresso de um job da sessão do solicitante.
//
// Job inexistente e job de outro dono devolvem o MESMO 404. O corpo é o DTO
// público, que não carrega o resultado interno do job.
func (c *Controlador) TratarObtencao(ctx context.Context, requisicao rotas.Requisicao, resposta rotas.Resposta) error {
	id, err := uuid.Parse(requisicao.Parametro(campoID))
	if err != nil || id == uuid.Nil {
		return rotasutil.TratarErro(ctx, resposta, errors.NovoErroValidacao(campoID, mensagemIDInvalido))
	}
	dono, err := sessao.Existente(requisicao, recursoJob)
	if err != nil {
		return rotasutil.TratarErro(ctx, resposta, err)
	}
	job, err := c.servico.Obter(ctx, dono, id)
	if err != nil {
		return rotasutil.TratarErro(ctx, resposta, err)
	}
	return resposta.Ok(job)
}
