package rotas_test

import (
	"context"
	"testing"

	"github.com/daniel-halos/formatador/internal/rotas"
)

func manipuladorVazio(context.Context, rotas.Requisicao, rotas.Resposta) error { return nil }

func TestRotasPrefixaCaminhosDosFilhos(t *testing.T) {
	documentos := rotas.NovoRoteador()
	documentos.Adicionar(rotas.Get, "/:id", manipuladorVazio)
	documentos.Adicionar(rotas.Post, "", manipuladorVazio)

	web := rotas.NovoRoteador()
	web.Registrar(documentos, "/documentos")

	raiz := rotas.NovoRoteador()
	raiz.Registrar(web, "/web")

	resolvidas := raiz.Rotas("/v1")

	esperados := []string{"/v1/web/documentos/:id", "/v1/web/documentos"}
	if len(resolvidas) != len(esperados) {
		t.Fatalf("esperava %d rotas, obteve %d", len(esperados), len(resolvidas))
	}
	for i, esperado := range esperados {
		if resolvidas[i].Caminho != esperado {
			t.Fatalf("rota %d: esperava %q, obteve %q", i, esperado, resolvidas[i].Caminho)
		}
	}
}

func TestOrdemDasRotasEDeterministica(t *testing.T) {
	montar := func() []rotas.Rota {
		raiz := rotas.NovoRoteador()
		for _, prefixo := range []string{"/a", "/b", "/c", "/d", "/e"} {
			filho := rotas.NovoRoteador()
			filho.Adicionar(rotas.Get, "", manipuladorVazio)
			raiz.Registrar(filho, prefixo)
		}
		return raiz.Rotas("")
	}

	primeira := montar()
	for range 20 {
		outra := montar()
		for i := range primeira {
			if primeira[i].Caminho != outra[i].Caminho {
				t.Fatalf("ordem instável na posição %d: %q vs %q", i, primeira[i].Caminho, outra[i].Caminho)
			}
		}
	}
}

func TestMiddlewaresAcumulamDoExternoParaOInterno(t *testing.T) {
	var execucao []string

	marcar := func(nome string) rotas.Middleware {
		return func(proximo rotas.Manipulador) rotas.Manipulador {
			return func(ctx context.Context, req rotas.Requisicao, resp rotas.Resposta) error {
				execucao = append(execucao, nome)
				return proximo(ctx, req, resp)
			}
		}
	}

	filho := rotas.NovoRoteador()
	filho.Adicionar(rotas.Get, "/x", func(context.Context, rotas.Requisicao, rotas.Resposta) error {
		execucao = append(execucao, "manipulador")
		return nil
	}, marcar("rota"))

	raiz := rotas.NovoRoteador()
	raiz.Registrar(filho, "/filho", marcar("filho"))

	resolvidas := raiz.Rotas("", marcar("raiz"))
	if len(resolvidas) != 1 {
		t.Fatalf("esperava 1 rota, obteve %d", len(resolvidas))
	}

	if err := resolvidas[0].Aplicar()(context.Background(), nil, nil); err != nil {
		t.Fatalf("não esperava erro, obteve %v", err)
	}

	esperada := []string{"raiz", "filho", "rota", "manipulador"}
	if len(execucao) != len(esperada) {
		t.Fatalf("esperava %v, obteve %v", esperada, execucao)
	}
	for i, nome := range esperada {
		if execucao[i] != nome {
			t.Fatalf("esperava %v, obteve %v", esperada, execucao)
		}
	}
}

func TestMiddlewaresDeIrmaosNaoVazam(t *testing.T) {
	nenhum := func(proximo rotas.Manipulador) rotas.Manipulador { return proximo }

	primeiro := rotas.NovoRoteador()
	primeiro.Adicionar(rotas.Get, "/p", manipuladorVazio)
	segundo := rotas.NovoRoteador()
	segundo.Adicionar(rotas.Get, "/s", manipuladorVazio)

	raiz := rotas.NovoRoteador()
	raiz.Registrar(primeiro, "/um", nenhum, nenhum)
	raiz.Registrar(segundo, "/dois")

	resolvidas := raiz.Rotas("", nenhum)

	if quantidade := len(resolvidas[0].Middlewares); quantidade != 3 {
		t.Fatalf("esperava 3 middlewares no primeiro filho, obteve %d", quantidade)
	}
	if quantidade := len(resolvidas[1].Middlewares); quantidade != 1 {
		t.Fatalf("esperava 1 middleware no segundo filho, obteve %d", quantidade)
	}
}

func TestRoteadorVazioNaoDevolveRotas(t *testing.T) {
	if rotasResolvidas := rotas.NovoRoteador().Rotas("/v1"); len(rotasResolvidas) != 0 {
		t.Fatalf("esperava nenhuma rota, obteve %d", len(rotasResolvidas))
	}
}
