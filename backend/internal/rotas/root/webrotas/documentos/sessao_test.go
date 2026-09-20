package documentos

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

func TestGarantirSessaoCriaQuandoCookieAusente(t *testing.T) {
	t.Parallel()
	requisicao := &requisicaoFake{}
	resposta := &respostaFake{}

	dono, err := garantirSessao(requisicao, resposta)
	exigirSemErroDocumentos(t, err)
	if dono.Vazio() {
		t.Fatal("esperava dono não vazio")
	}
	if len(resposta.cookies) != 1 {
		t.Fatalf("esperava 1 cookie definido, obteve %d", len(resposta.cookies))
	}

	cookie := resposta.cookies[0]
	if cookie.Name != NomeCookieSessao {
		t.Fatalf("nome do cookie divergente: %q", cookie.Name)
	}
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" {
		t.Fatalf("atributos de segurança do cookie incorretos: %+v", cookie)
	}
	id, err := uuid.Parse(cookie.Value)
	if err != nil {
		t.Fatalf("cookie não carrega um uuid válido: %v", err)
	}

	esperado, err := vo.NovoDonoSessao(id)
	exigirSemErroDocumentos(t, err)
	if !dono.Igual(esperado) {
		t.Fatal("dono devolvido não corresponde ao uuid gravado no cookie")
	}
}

func TestGarantirSessaoReusaCookieValidoSemRegravar(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	requisicao := &requisicaoFake{cookie: id.String()}
	resposta := &respostaFake{}

	dono, err := garantirSessao(requisicao, resposta)
	exigirSemErroDocumentos(t, err)
	if len(resposta.cookies) != 0 {
		t.Fatalf("não deveria regravar cookie já válido, obteve %d gravações", len(resposta.cookies))
	}

	esperado, err := vo.NovoDonoSessao(id)
	exigirSemErroDocumentos(t, err)
	if !dono.Igual(esperado) {
		t.Fatal("dono não corresponde ao cookie existente")
	}
}

func TestGarantirSessaoTrataCookieDeLixoComoAusente(t *testing.T) {
	t.Parallel()
	for _, caso := range []struct{ nome, cookie string }{
		{"não é uuid", "isto-nao-e-um-uuid"},
		{"uuid nil", uuid.Nil.String()},
		{"vazio", ""},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			requisicao := &requisicaoFake{cookie: caso.cookie}
			resposta := &respostaFake{}
			dono, err := garantirSessao(requisicao, resposta)
			exigirSemErroDocumentos(t, err)
			if dono.Vazio() {
				t.Fatal("esperava sessão nova mesmo com cookie inválido")
			}
			if len(resposta.cookies) != 1 {
				t.Fatalf("esperava novo cookie gravado, obteve %d", len(resposta.cookies))
			}
		})
	}
}

func TestSessaoExistenteNuncaGravaCookieEDevolveNaoEncontradoQuandoAusenteOuIlegivel(t *testing.T) {
	t.Parallel()
	for _, caso := range []struct{ nome, cookie string }{
		{"ausente", ""},
		{"lixo", "nao-e-um-uuid"},
		{"uuid nil", uuid.Nil.String()},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			requisicao := &requisicaoFake{cookie: caso.cookie}

			_, err := sessaoExistente(requisicao)
			var naoEncontrado *errors.ErroNaoEncontrado
			if !errors.Como(err, &naoEncontrado) {
				t.Fatalf("esperava ErroNaoEncontrado, obteve %T: %v", err, err)
			}
		})
	}
}

func TestSessaoExistenteReusaCookieValido(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	requisicao := &requisicaoFake{cookie: id.String()}

	dono, err := sessaoExistente(requisicao)
	exigirSemErroDocumentos(t, err)

	esperado, err := vo.NovoDonoSessao(id)
	exigirSemErroDocumentos(t, err)
	if !dono.Igual(esperado) {
		t.Fatal("dono devolvido não corresponde ao uuid do cookie")
	}
}
