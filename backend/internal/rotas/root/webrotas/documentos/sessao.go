// Package documentos expõe os endpoints HTTP de ingestão e consulta de
// documentos: converte requisição em caso de uso da camada de aplicação e
// caso de uso em resposta HTTP. Nenhuma regra de negócio mora aqui.
package documentos

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/daniel-halos/formatador/internal/rotas"
)

// NomeCookieSessao é o nome do cookie que carrega o identificador da sessão
// anônima de quem usa o sistema sem conta.
const NomeCookieSessao = "sessao_id"

// mensagemSessaoAusente é fixa: o cookie de sessão nunca aparece em erro nem
// em log (CLAUDE.md, regra 7).
// O recurso declarado é "documento", não "sessão": o objetivo é que a resposta
// seja indistinguível da de um documento inexistente. Dizer "sessão" entregaria
// ao atacante que a falha foi de autenticação e não de existência — além de
// concordar errado em português ("sessão não encontrado").
const mensagemSessaoAusente = "documento"

// garantirSessao devolve o dono da sessão carregada pelo cookie da
// requisição. Quando o cookie está ausente ou é ilegível (não é um uuid, ou é
// o uuid nulo), uma sessão nova é criada e gravada num cookie httpOnly +
// Secure + SameSite=Strict (CLAUDE.md, regra 9) — cookie de lixo é tratado
// como ausente, nunca como erro do cliente.
func garantirSessao(requisicao rotas.Requisicao, resposta rotas.Resposta) (vo.Dono, error) {
	if dono, ok := lerDonoDoCookie(requisicao); ok {
		return dono, nil
	}

	sessaoID := uuid.New()
	dono, err := vo.NovoDonoSessao(sessaoID)
	if err != nil {
		return vo.Dono{}, errors.Envolver(err, "criar sessão anônima")
	}

	resposta.DefinirCookie(&http.Cookie{
		Name:     NomeCookieSessao,
		Value:    sessaoID.String(),
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
	return dono, nil
}

// sessaoExistente devolve o dono da sessão carregada pelo cookie da
// requisição, sem nunca criar ou gravar um cookie novo. Cookie ausente ou
// ilegível devolve o mesmo ErroNaoEncontrado de um recurso inexistente: um
// 401 aqui distinguiria "sem sessão" de "sessão de outro dono" e viraria
// oráculo de existência.
func sessaoExistente(requisicao rotas.Requisicao) (vo.Dono, error) {
	dono, ok := lerDonoDoCookie(requisicao)
	if !ok {
		return vo.Dono{}, errors.NovoErroNaoEncontrado(mensagemSessaoAusente)
	}
	return dono, nil
}

// lerDonoDoCookie tenta ler um dono de sessão válido do cookie da
// requisição. O segundo retorno é false para qualquer forma de cookie
// inutilizável: ausente, não-uuid ou uuid nulo.
func lerDonoDoCookie(requisicao rotas.Requisicao) (vo.Dono, bool) {
	valor := requisicao.Cookie(NomeCookieSessao)
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
