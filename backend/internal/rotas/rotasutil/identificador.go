package rotasutil

import (
	"github.com/google/uuid"

	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/daniel-halos/formatador/internal/rotas"
)

// MensagemIDInvalido é fixa: nunca ecoa o valor recebido (CLAUDE.md, regra 7).
// Devolver de volta o que o cliente mandou é como um id virar refletor de
// conteúdo arbitrário na resposta.
const MensagemIDInvalido = "identificador inválido"

// IDDaRota lê um UUID de parâmetro de rota.
//
// Existe para acabar com uma assimetria que uma auditoria independente
// encontrou: o controlador de jobs recusava o UUID nulo e os cinco handlers de
// documento não. Na prática nenhum dos dois caminhos era explorável — nenhuma
// linha é gravada com id nulo e o WHERE filtra por dono de qualquer forma —,
// mas duas validações diferentes para a mesma coisa fazem quem lê as duas se
// perguntar qual está certa, e a resposta vira "depende do arquivo".
//
// O UUID nulo é recusado aqui porque ele nunca identifica recurso nenhum:
// aceitá-lo gastaria uma consulta para descobrir o que já se sabe.
func IDDaRota(requisicao rotas.Requisicao, campo string) (uuid.UUID, error) {
	id, err := uuid.Parse(requisicao.Parametro(campo))
	if err != nil || id == uuid.Nil {
		return uuid.Nil, errors.NovoErroValidacao(campo, MensagemIDInvalido)
	}
	return id, nil
}
