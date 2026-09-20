package documentos

import (
	"context"
	"net/http"
	"testing"

	"github.com/daniel-halos/formatador/internal/rotas"
)

// TestRoteadorRegistraListagem prova que GET /documentos está fiado ao
// roteador de documentos. Sem isso, TratarListagem pode existir no
// controlador e nunca ser alcançável pela API.
func TestRoteadorRegistraListagem(t *testing.T) {
	t.Parallel()
	controlador, _ := controladorDeTeste(t)
	roteador := Roteador(controlador, 1<<20)

	var encontrada *rotas.Rota
	for _, rota := range roteador.Rotas("") {
		if rota.Metodo == rotas.Get && rota.Caminho == CaminhoColecao {
			r := rota
			encontrada = &r
		}
	}
	if encontrada == nil {
		t.Fatal("GET " + CaminhoColecao + " não está registrado no roteador de documentos")
	}

	requisicao := &requisicaoFake{metodo: http.MethodGet, caminho: CaminhoColecao}
	resposta := &respostaFake{}
	if err := encontrada.Aplicar()(context.Background(), requisicao, resposta); err != nil {
		t.Fatalf("manipulador registrado falhou: %v", err)
	}
	if resposta.status != http.StatusOK {
		t.Fatalf("esperava 200 ao chamar o manipulador registrado, obteve %d", resposta.status)
	}
}
