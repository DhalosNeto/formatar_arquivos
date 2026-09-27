package sessao_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/rotas"
	"github.com/daniel-halos/formatador/internal/rotas/sessao"
)

// respostaCookie captura os cookies gravados. Embute a interface nula e só
// implementa o método usado — mesmo padrão de requisicaoCookie: um dublê que
// implementasse Resposta inteira esconderia o que o teste de fato exercita.
type respostaCookie struct {
	rotas.Resposta
	cookies []*http.Cookie
}

func (r *respostaCookie) DefinirCookie(cookie *http.Cookie) {
	r.cookies = append(r.cookies, cookie)
}

// TestGarantirCriaSessaoQuandoNaoHaCookie confere, junto com a criação, os
// quatro atributos de segurança do cookie. Eles são a regra 9 do CLAUDE.md, e
// perder qualquer um deles não quebra nenhum fluxo — só expõe o token.
func TestGarantirCriaSessaoQuandoNaoHaCookie(t *testing.T) {
	t.Parallel()

	requisicao := &requisicaoCookie{}
	resposta := &respostaCookie{}

	dono, err := sessao.Garantir(requisicao, resposta)

	require.NoError(t, err)
	assert.False(t, dono.Vazio())
	require.Len(t, resposta.cookies, 1)

	cookie := resposta.cookies[0]
	assert.Equal(t, sessao.NomeCookie, cookie.Name)
	assert.True(t, cookie.HttpOnly, "token de sessão legível por script é a regra 9 violada")
	assert.True(t, cookie.Secure)
	assert.Equal(t, http.SameSiteStrictMode, cookie.SameSite)
	assert.Equal(t, "/", cookie.Path)

	id, err := uuid.Parse(cookie.Value)
	require.NoError(t, err, "o cookie precisa carregar um uuid")

	esperado, err := vo.NovoDonoSessao(id)
	require.NoError(t, err)
	assert.True(t, dono.Igual(esperado), "o dono devolvido tem que ser o do uuid gravado no cookie")
}

// TestGarantirReusaCookieValidoSemRegravar: regravar a cada requisição
// renovaria a validade do cookie sem motivo e sujaria toda resposta com
// Set-Cookie.
func TestGarantirReusaCookieValidoSemRegravar(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	requisicao := &requisicaoCookie{valor: id.String()}
	resposta := &respostaCookie{}

	dono, err := sessao.Garantir(requisicao, resposta)

	require.NoError(t, err)
	assert.Empty(t, resposta.cookies, "cookie já válido não pode ser regravado")

	esperado, err := vo.NovoDonoSessao(id)
	require.NoError(t, err)
	assert.True(t, dono.Igual(esperado))
}

// TestGarantirTrataCookieDeLixoComoAusente: cookie corrompido é sessão nova,
// não erro do cliente. Devolver 400 aqui deixaria quem tem cookie estragado
// travado fora do sistema, sem caminho de recuperação.
func TestGarantirTrataCookieDeLixoComoAusente(t *testing.T) {
	t.Parallel()

	casos := []struct{ nome, cookie string }{
		{"não é uuid", "isto-nao-e-um-uuid"},
		{"uuid nulo", uuid.Nil.String()},
		{"vazio", ""},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			requisicao := &requisicaoCookie{valor: caso.cookie}
			resposta := &respostaCookie{}

			dono, err := sessao.Garantir(requisicao, resposta)

			require.NoError(t, err)
			assert.False(t, dono.Vazio(), "esperava sessão nova mesmo com cookie inválido")
			assert.Len(t, resposta.cookies, 1, "esperava um cookie novo gravado")
		})
	}
}

// TestOpcionalNaoGravaCookieENaoErraQuandoAusente trava o contrato da
// listagem: primeira visita não é falha nem sessão nova.
func TestOpcionalNaoGravaCookieENaoErraQuandoAusente(t *testing.T) {
	t.Parallel()

	_, ok := sessao.Opcional(&requisicaoCookie{})
	assert.False(t, ok, "sem cookie, Opcional informa ausência em vez de criar sessão")

	id := uuid.New()
	dono, ok := sessao.Opcional(&requisicaoCookie{valor: id.String()})
	require.True(t, ok)

	esperado, err := vo.NovoDonoSessao(id)
	require.NoError(t, err)
	assert.True(t, dono.Igual(esperado))
}
