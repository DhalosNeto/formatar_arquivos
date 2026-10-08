package llm

import (
	"context"
	"encoding/json"
	"github.com/daniel-halos/formatador/internal/domain/cdm"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type transporteFunc func(*http.Request) (*http.Response, error)

func (f transporteFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func blocoTeste() cdm.Bloco {
	return cdm.Bloco{Papel: cdm.Paragrafo, TextoResumo: strings.Repeat("á", 600), Confianca: 0.2, Origem: cdm.OrigemHeuristica, RefXML: 7}
}
func respostaTeste(criterios map[string]any) map[string]any {
	probabilidades := map[string]float64{}
	for chave := range criterios {
		probabilidades[chave] = 0
	}
	probabilidades["titulo"] = 1
	return map[string]any{"type": "choice", "choice": "titulo", "confidence": 0.99, "probabilities": probabilidades}
}
func clienteTeste(t *testing.T, alterar func(map[string]any), status int) *ClienteJev {
	t.Helper()
	cliente, err := NovoClienteJev("chave-secreta", "jev-latest")
	if err != nil {
		t.Fatal(err)
	}
	cliente.cliente.Transport = transporteFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://api.typesafe.ai/v1/systemone" || r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer chave-secreta" {
			t.Error("contrato HTTP incorreto")
		}
		var pedido struct {
			State []struct {
				Texto string `json:"texto"`
			}
			Questions map[string]struct {
				Type     string
				Criteria map[string]any
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&pedido); err != nil {
			t.Fatal(err)
		}
		if len(pedido.State) != 1 || len([]rune(pedido.State[0].Texto)) != 500 {
			t.Error("trecho não limitado")
		}
		respostas := map[string]any{}
		for id, pergunta := range pedido.Questions {
			if pergunta.Type != "choice" || pergunta.Criteria["sem_correspondencia"] == nil {
				t.Error("falta escolha fechada")
			}
			respostas[id] = respostaTeste(pergunta.Criteria)
		}
		envelope := map[string]any{"answers": respostas}
		if alterar != nil {
			alterar(envelope)
		}
		dados, _ := json.Marshal(envelope)
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(dados))), Request: r}, nil
	})
	return cliente
}
func TestJevContratoELimites(t *testing.T) {
	cliente := clienteTeste(t, nil, 200)
	julgamentos, err := cliente.Classificar(context.Background(), []cdm.Bloco{blocoTeste()})
	if err != nil {
		t.Fatal(err)
	}
	if len(julgamentos) != 1 || julgamentos[0].Papel != cdm.Titulo || julgamentos[0].RefXML != 7 || julgamentos[0].Confianca != 0.99 {
		t.Fatal("julgamento incorreto")
	}
	if _, err := cliente.Classificar(context.Background(), make([]cdm.Bloco, 33)); err == nil {
		t.Fatal("limite ausente")
	}
}
func TestJevRecusaRespostasInvalidas(t *testing.T) {
	for _, caso := range []struct {
		nome    string
		alterar func(map[string]any)
	}{
		{"sem_respostas", func(e map[string]any) { delete(e, "answers") }},
		{"sem_confianca", func(e map[string]any) {
			for _, a := range e["answers"].(map[string]any) {
				delete(a.(map[string]any), "confidence")
			}
		}},
		{"papel_inventado", func(e map[string]any) {
			for _, a := range e["answers"].(map[string]any) {
				a.(map[string]any)["choice"] = "roubar"
			}
		}},
		{"probabilidade_invalida", func(e map[string]any) {
			for _, a := range e["answers"].(map[string]any) {
				a.(map[string]any)["probabilities"] = map[string]float64{"titulo": 2}
			}
		}},
		{"confianca_fora", func(e map[string]any) {
			for _, a := range e["answers"].(map[string]any) {
				a.(map[string]any)["confidence"] = 1.1
			}
		}},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			_, err := clienteTeste(t, caso.alterar, 200).Classificar(context.Background(), []cdm.Bloco{blocoTeste()})
			if err == nil {
				t.Fatal("resposta aceita")
			}
			if strings.Contains(err.Error(), "roubar") || strings.Contains(err.Error(), "chave-secreta") {
				t.Fatal("erro vazou conteúdo")
			}
		})
	}
}
func TestJevFalhasTransporteSemVazar(t *testing.T) {
	cliente, _ := NovoClienteJev("chave-secreta", "jev-latest")
	chamadas := 0
	cliente.cliente.Transport = transporteFunc(func(r *http.Request) (*http.Response, error) {
		chamadas++
		return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"https://outro.example/segredo"}}, Body: io.NopCloser(strings.NewReader("chave-secreta")), Request: r}, nil
	})
	if _, err := cliente.Classificar(context.Background(), []cdm.Bloco{blocoTeste()}); err == nil || strings.Contains(err.Error(), "segredo") {
		t.Fatal("redirect ou vazamento")
	}
	if chamadas != 1 {
		t.Fatal("redirect seguido")
	}
	cliente.cliente.Transport = transporteFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(strings.Repeat("x", 300<<10))), Request: r}, nil
	})
	if _, err := cliente.Classificar(context.Background(), []cdm.Bloco{blocoTeste()}); err == nil {
		t.Fatal("resposta grande aceita")
	}
	ctx, cancelar := context.WithCancel(context.Background())
	cancelar()
	if _, err := cliente.Classificar(ctx, []cdm.Bloco{blocoTeste()}); err == nil {
		t.Fatal("cancelamento ignorado")
	}
	cliente.cliente.Timeout = time.Millisecond
	cliente.cliente.Transport = transporteFunc(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })
	if _, err := cliente.Classificar(context.Background(), []cdm.Bloco{blocoTeste()}); err == nil {
		t.Fatal("timeout ignorado")
	}
}

func TestJevSecaoSemCorrespondenciaEVazio(t *testing.T) {
	for _, escolha := range []string{"secao_3", "sem_correspondencia"} {
		t.Run(escolha, func(t *testing.T) {
			cliente := clienteTeste(t, func(envelope map[string]any) {
				for _, valor := range envelope["answers"].(map[string]any) {
					resposta := valor.(map[string]any)
					resposta["choice"] = escolha
					probabilidades := resposta["probabilities"].(map[string]float64)
					probabilidades["titulo"] = 0
					probabilidades[escolha] = 1
				}
			}, 200)
			resultado, err := cliente.Classificar(context.Background(), []cdm.Bloco{blocoTeste()})
			if err != nil {
				t.Fatal(err)
			}
			if escolha == "secao_3" && resultado[0].Papel != cdm.Secao(3) {
				t.Fatal("nível incorreto")
			}
			if escolha == "sem_correspondencia" && !resultado[0].SemCorrespondencia {
				t.Fatal("ausência não preservada")
			}
		})
	}
	cliente, _ := NovoClienteJev("fake", "jev-latest")
	cliente.cliente.Transport = transporteFunc(func(*http.Request) (*http.Response, error) { t.Fatal("chamada desnecessária"); return nil, nil })
	if resultado, err := cliente.Classificar(context.Background(), nil); err != nil || len(resultado) != 0 {
		t.Fatal("vazio incorreto")
	}
	for _, args := range [][2]string{{"", "jev-latest"}, {"fake\r\nkey", "jev-latest"}, {"fake", ""}} {
		if _, err := NovoClienteJev(args[0], args[1]); err == nil {
			t.Fatal("construtor aceitou argumento inválido")
		}
	}
}

func TestJevErroExternoEJSONInvalido(t *testing.T) {
	for _, corpo := range []string{"segredo-documento", "{}", "{\"answers\":null}", "{} {}"} {
		t.Run(corpo, func(t *testing.T) {
			cliente, _ := NovoClienteJev("fake", "jev-latest")
			cliente.cliente.Transport = transporteFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(corpo)), Request: r}, nil
			})
			if _, err := cliente.Classificar(context.Background(), []cdm.Bloco{blocoTeste()}); err == nil || strings.Contains(err.Error(), "segredo-documento") {
				t.Fatal("erro inválido")
			}
		})
	}
	for _, status := range []int{401, 429, 500, 529} {
		if _, err := clienteTeste(t, nil, status).Classificar(context.Background(), []cdm.Bloco{blocoTeste()}); err == nil {
			t.Fatal("status aceito", status)
		}
	}
}

func TestJevRecusaProbabilidadeNula(t *testing.T) {
	cliente := clienteTeste(t, func(envelope map[string]any) {
		for _, valor := range envelope["answers"].(map[string]any) {
			resposta := valor.(map[string]any)
			probabilidades := make(map[string]any)
			for chave, probabilidade := range resposta["probabilities"].(map[string]float64) {
				probabilidades[chave] = probabilidade
			}
			probabilidades["paragrafo"] = nil
			resposta["probabilities"] = probabilidades
		}
	}, http.StatusOK)
	if _, err := cliente.Classificar(context.Background(), []cdm.Bloco{blocoTeste()}); err == nil {
		t.Fatal("null não é probabilidade zero")
	}
}
