// Package sessao é o ÚNICO lugar do projeto que conhece o cookie de sessão
// anônima: o nome dele, como lê-lo e como gravá-lo.
//
// Existir como pacote próprio não é organização por gosto. Antes disso a
// leitura do cookie estava implementada duas vezes — uma em webrotas/documentos
// e outra aqui — cada uma com sua própria constante "sessao_id". Duas
// constantes para o mesmo identificador de credencial significam que trocar
// uma e esquecer a outra derruba metade da API sem erro de compilação.
package sessao

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/daniel-halos/formatador/internal/rotas"
)

// NomeCookie é o nome do cookie que carrega o identificador da sessão anônima
// de quem usa o sistema sem conta.
const NomeCookie = "sessao_id"

// Garantir devolve o dono da sessão do cookie da requisição, criando uma
// sessão nova quando não há cookie utilizável.
//
// Cookie ausente, não-uuid ou com o uuid nulo é tratado como AUSENTE, nunca
// como erro do cliente: quem chega com cookie de lixo recebe uma sessão nova,
// não um 400. O cookie gravado é httpOnly + Secure + SameSite=Strict
// (CLAUDE.md, regra 9).
func Garantir(requisicao rotas.Requisicao, resposta rotas.Resposta) (vo.Dono, error) {
	if dono, ok := lerDono(requisicao); ok {
		return dono, nil
	}

	sessaoID := uuid.New()
	dono, err := vo.NovoDonoSessao(sessaoID)
	if err != nil {
		return vo.Dono{}, errors.Envolver(err, "criar sessão anônima")
	}

	resposta.DefinirCookie(&http.Cookie{
		Name:     NomeCookie,
		Value:    sessaoID.String(),
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
	return dono, nil
}

// Existente devolve o dono da sessão do cookie, sem nunca criar nem gravar
// cookie novo.
//
// Cookie ausente ou ilegível devolve o MESMO ErroNaoEncontrado de um recurso
// inexistente, com o nome de recurso que o chamador informa. Um 401 aqui
// distinguiria "sem sessão" de "recurso de outro dono" e viraria oráculo de
// existência. O recurso vem do chamador para a mensagem concordar em português
// e nunca citar "sessão" — o cookie não aparece em erro nem em log (regra 7).
func Existente(requisicao rotas.Requisicao, recurso string) (vo.Dono, error) {
	dono, ok := lerDono(requisicao)
	if !ok {
		return vo.Dono{}, errors.NovoErroNaoEncontrado(recurso)
	}
	return dono, nil
}

// Opcional devolve o dono da sessão quando há cookie utilizável, e false
// quando não há — sem erro e sem gravar cookie.
//
// Existe para a listagem: quem chega sem cookie é visitante novo, não falha.
// Responder 404 ali diria "você não tem documentos" com o status de "não
// existe", e responder 401 criaria um estado de erro para o caso mais comum
// de todos — a primeira visita.
func Opcional(requisicao rotas.Requisicao) (vo.Dono, bool) {
	return lerDono(requisicao)
}

// lerDono tenta ler um dono de sessão válido do cookie. O segundo retorno é
// false para qualquer forma de cookie inutilizável: ausente, não-uuid ou uuid
// nulo. É o único ponto que interpreta o valor do cookie.
func lerDono(requisicao rotas.Requisicao) (vo.Dono, bool) {
	valor := requisicao.Cookie(NomeCookie)
	if valor == "" {
		return vo.Dono{}, false
	}
	sessaoID, err := uuid.Parse(valor)
	if err != nil {
		return vo.Dono{}, false
	}
	dono, err := vo.NovoDonoSessao(sessaoID)
	if err != nil {
		return vo.Dono{}, false
	}
	return dono, true
}
