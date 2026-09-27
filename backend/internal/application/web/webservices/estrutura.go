package webservices

import (
	"context"
	"github.com/daniel-halos/formatador/internal/application/web/webmodel"
	"github.com/daniel-halos/formatador/internal/domain/cdm"
	dominioservice "github.com/daniel-halos/formatador/internal/domain/documento/service"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/google/uuid"
)

// ServicoEstrutura atende a correção manual do papel de um bloco do CDM.
//
// Separado de ServicoAnalise porque a correção escreve, e escrever exige a
// porta escopada ao dono do documento — a análise lê pela porta interna do
// worker, que tem outra fronteira de acesso.
type ServicoEstrutura struct {
	dominio *dominioservice.ServicoEstrutura
}

// NovoServicoEstrutura monta o serviço de correção de estrutura.
func NovoServicoEstrutura(dominio *dominioservice.ServicoEstrutura) (*ServicoEstrutura, error) {
	if dominio == nil {
		return nil, errors.NovoErroArgumentoNulo("dominio")
	}
	return &ServicoEstrutura{dominio: dominio}, nil
}

// Corrigir traduz o papel recebido na requisição e delega a correção ao
// domínio, devolvendo a estrutura já atualizada.
//
// A tradução do nome do papel acontece ANTES de tocar o documento: papel
// desconhecido é erro de validação do cliente, e descobri-lo depois de ler o
// CDM gastaria uma consulta para recusar a requisição de qualquer forma.
//
// Devolver a estrutura inteira em vez de só o bloco alterado é deliberado —
// poupa o cliente de um segundo GET para saber o estado resultante.
func (servico *ServicoEstrutura) Corrigir(ctx context.Context, dono vo.Dono, id uuid.UUID, refXML int, nomePapel string, nivel int) (webmodel.EstruturaResposta, error) {
	papel, err := cdm.ParaPapel(nomePapel, nivel)
	if err != nil {
		return webmodel.EstruturaResposta{}, err
	}
	indice, err := servico.dominio.Corrigir(ctx, dono, id, refXML, papel)
	if err != nil {
		return webmodel.EstruturaResposta{}, err
	}
	return paraEstruturaResposta(indice), nil
}
