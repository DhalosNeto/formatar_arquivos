// Package pdfconv testa o cliente HTTP do sidecar de conversão DOCX → PDF.
//
// O protocolo é nosso (não é API de terceiro): POST CaminhoConversao com o
// DOCX cru no corpo, sem multipart, e GET CaminhoSaude devolvendo 200 quando
// o sidecar está pronto.
package pdfconv

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/daniel-halos/formatador/internal/infra/config"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// endpointMorto aponta para uma porta reservada que jamais aceita conexão.
// Qualquer caso de teste que chegue à rede usando este endpoint trava até o
// timeout do contexto/cliente, o que denuncia validação feita tarde demais.
const endpointMorto = "http://127.0.0.1:1"

// limiteSemRede é o teto de tempo tolerado para uma chamada que deveria
// falhar na validação, antes de qualquer I/O.
const limiteSemRede = 200 * time.Millisecond

// assinaturaZIP são os quatro primeiros bytes de qualquer pacote OOXML válido.
var assinaturaZIP = []byte{'P', 'K', 0x03, 0x04}

func configValida() config.Conversor {
	return config.Conversor{URL: endpointMorto, TempoLimite: time.Minute}
}

// clienteMorto constrói um cliente apontado para um endpoint que não responde.
func clienteMorto(t *testing.T) *Cliente {
	t.Helper()

	cliente, err := NovoCliente(configValida())
	if err != nil {
		t.Fatalf("construir cliente não deveria falhar, obteve %v", err)
	}
	if cliente == nil {
		t.Fatal("esperava cliente não nulo")
	}
	return cliente
}

func docxSintetico(sufixo string) []byte {
	return append(append([]byte{}, assinaturaZIP...), []byte(sufixo)...)
}

func exigirErroValidacao(t *testing.T, err error, campos ...string) *errors.ErroValidacao {
	t.Helper()

	if err == nil {
		t.Fatal("esperava erro de validação, obteve nil")
	}
	var invalido *errors.ErroValidacao
	if !errors.Como(err, &invalido) {
		t.Fatalf("esperava *errors.ErroValidacao, obteve %T (%v)", err, err)
	}
	for _, campo := range campos {
		if !temCampo(invalido, campo) {
			t.Fatalf("esperava campo %q reprovado, obteve %v", campo, invalido.Campos)
		}
	}
	return invalido
}

func exigirErroAplicacao(t *testing.T, err error) *errors.ErroAplicacao {
	t.Helper()

	if err == nil {
		t.Fatal("esperava erro de aplicação, obteve nil")
	}
	var aplicacao *errors.ErroAplicacao
	if !errors.Como(err, &aplicacao) {
		t.Fatalf("esperava *errors.ErroAplicacao, obteve %T (%v)", err, err)
	}
	var validacao *errors.ErroValidacao
	if errors.Como(err, &validacao) {
		t.Fatalf("falha do lado do servidor não pode virar *errors.ErroValidacao: %v", err)
	}
	return aplicacao
}

func temCampo(err *errors.ErroValidacao, campo string) bool {
	for _, invalido := range err.Campos {
		if invalido.Campo == campo {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Construtor
// ---------------------------------------------------------------------------

func TestNovoClienteRejeitaConfiguracaoInvalida(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome   string
		ajuste func(*config.Conversor)
		campos []string
	}{
		{
			nome:   "url vazia",
			ajuste: func(c *config.Conversor) { c.URL = "" },
			campos: []string{"url"},
		},
		{
			nome:   "url sem esquema",
			ajuste: func(c *config.Conversor) { c.URL = "libreoffice:2004" },
			campos: []string{"url"},
		},
		{
			nome:   "url sintaticamente quebrada",
			ajuste: func(c *config.Conversor) { c.URL = "://quebrado" },
			campos: []string{"url"},
		},
		{
			nome:   "url com esquema errado",
			ajuste: func(c *config.Conversor) { c.URL = "ftp://libreoffice:2004" },
			campos: []string{"url"},
		},
		{
			nome:   "url com host vazio",
			ajuste: func(c *config.Conversor) { c.URL = "http:///converter" },
			campos: []string{"url"},
		},
		{
			nome:   "tempo limite zero",
			ajuste: func(c *config.Conversor) { c.TempoLimite = 0 },
			campos: []string{"tempoLimite"},
		},
		{
			nome:   "tempo limite negativo",
			ajuste: func(c *config.Conversor) { c.TempoLimite = -time.Second },
			campos: []string{"tempoLimite"},
		},
		{
			nome: "url vazia e tempo limite zero ao mesmo tempo",
			ajuste: func(c *config.Conversor) {
				c.URL = ""
				c.TempoLimite = 0
			},
			campos: []string{"url", "tempoLimite"},
		},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			cfg := configValida()
			caso.ajuste(&cfg)

			cliente, err := NovoCliente(cfg)
			if cliente != nil {
				t.Fatal("esperava cliente nulo quando a configuração é inválida")
			}
			exigirErroValidacao(t, err, caso.campos...)
		})
	}
}

func TestNovoClienteNaoFazIOAoConstruir(t *testing.T) {
	t.Parallel()

	inicio := time.Now()
	cliente, err := NovoCliente(configValida())
	decorrido := time.Since(inicio)

	if err != nil {
		t.Fatalf("esperava sucesso ao construir cliente com endpoint morto, obteve %v", err)
	}
	if cliente == nil {
		t.Fatal("esperava cliente não nulo")
	}
	if decorrido > limiteSemRede {
		t.Fatalf("construir cliente levou %v: parece estar tocando a rede", decorrido)
	}
}

// ---------------------------------------------------------------------------
// Validação antes da rede
// ---------------------------------------------------------------------------

func TestConverterParaPDFValidaAntesDeQualquerRede(t *testing.T) {
	t.Parallel()

	cliente := clienteMorto(t)

	casos := []struct {
		nome string
		docx []byte
	}{
		{nome: "docx nulo", docx: nil},
		{nome: "docx vazio", docx: []byte{}},
		{nome: "docx sem assinatura zip", docx: []byte("nao sou um docx")},
		{nome: "assinatura zip truncada", docx: []byte{'P', 'K'}},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			concluido := make(chan struct{})
			var pdf []byte
			var err error
			go func() {
				defer close(concluido)
				pdf, err = cliente.ConverterParaPDF(context.Background(), caso.docx)
			}()

			select {
			case <-concluido:
			case <-time.After(limiteSemRede):
				t.Fatal("validação não terminou a tempo: parece estar tocando a rede")
			}

			exigirErroValidacao(t, err, "docx")
			if pdf != nil {
				t.Fatal("não pode devolver PDF quando a entrada é inválida")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Classificação de erro
// ---------------------------------------------------------------------------

func TestConverterParaPDFEndpointMortoDevolveErroAplicacao(t *testing.T) {
	t.Parallel()

	cfg := configValida()
	cfg.TempoLimite = 2 * time.Second
	cliente, err := NovoCliente(cfg)
	if err != nil {
		t.Fatalf("construir cliente não deveria falhar, obteve %v", err)
	}

	pdf, err := cliente.ConverterParaPDF(context.Background(), docxSintetico("lixo"))
	if pdf != nil {
		t.Fatal("não pode devolver PDF quando o endpoint está morto")
	}
	exigirErroAplicacao(t, err)
}

func TestConverterParaPDFStatusNaoOkDevolveErroAplicacao(t *testing.T) {
	t.Parallel()

	const corpoSentinela = "detalhe-interno-do-sidecar /tmp/entrada-xyz.docx"

	statusCasos := []int{http.StatusBadRequest, http.StatusNotFound, http.StatusInternalServerError, http.StatusServiceUnavailable}

	for _, status := range statusCasos {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			t.Parallel()

			servidor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = io.WriteString(w, corpoSentinela)
			}))
			defer servidor.Close()

			cliente, err := NovoCliente(config.Conversor{URL: servidor.URL, TempoLimite: 5 * time.Second})
			if err != nil {
				t.Fatalf("construir cliente não deveria falhar, obteve %v", err)
			}

			pdf, err := cliente.ConverterParaPDF(context.Background(), docxSintetico("lixo"))
			if pdf != nil {
				t.Fatal("não pode devolver PDF quando o status não é 200")
			}
			exigirErroAplicacao(t, err)
			if strings.Contains(err.Error(), "detalhe-interno-do-sidecar") {
				t.Fatalf("mensagem de erro vazou corpo do sidecar: %v", err)
			}
			if strings.Contains(err.Error(), "/tmp/") {
				t.Fatalf("mensagem de erro vazou caminho interno do sidecar: %v", err)
			}
		})
	}
}

func TestConverterParaPDFRecusaRespostaAcimaDoTeto(t *testing.T) {
	t.Parallel()

	servidor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		bloco := bytes.Repeat([]byte{'X'}, 1<<20)
		copy(bloco, []byte("%PDF-"))

		var escrito int64
		for escrito < TamanhoMaximoPDFBytes+1 {
			restante := TamanhoMaximoPDFBytes + 1 - escrito
			if restante < int64(len(bloco)) {
				bloco = bloco[:restante]
			}
			n, err := w.Write(bloco)
			if err != nil {
				return
			}
			escrito += int64(n)
		}
	}))
	defer servidor.Close()

	cliente, err := NovoCliente(config.Conversor{URL: servidor.URL, TempoLimite: 30 * time.Second})
	if err != nil {
		t.Fatalf("construir cliente não deveria falhar, obteve %v", err)
	}

	pdf, err := cliente.ConverterParaPDF(context.Background(), docxSintetico("lixo"))
	if pdf != nil {
		t.Fatal("não pode devolver PDF acima do teto de tamanho")
	}
	exigirErroAplicacao(t, err)
}

func TestConverterParaPDFRecusaCorpoVazio(t *testing.T) {
	t.Parallel()

	servidor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer servidor.Close()

	cliente, err := NovoCliente(config.Conversor{URL: servidor.URL, TempoLimite: 5 * time.Second})
	if err != nil {
		t.Fatalf("construir cliente não deveria falhar, obteve %v", err)
	}

	pdf, err := cliente.ConverterParaPDF(context.Background(), docxSintetico("lixo"))
	if pdf != nil {
		t.Fatal("não pode devolver PDF quando o corpo veio vazio")
	}
	exigirErroAplicacao(t, err)
}

func TestConverterParaPDFRecusaCorpoQueNaoEhPDF(t *testing.T) {
	t.Parallel()

	servidor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "<html>erro</html>")
	}))
	defer servidor.Close()

	cliente, err := NovoCliente(config.Conversor{URL: servidor.URL, TempoLimite: 5 * time.Second})
	if err != nil {
		t.Fatalf("construir cliente não deveria falhar, obteve %v", err)
	}

	pdf, err := cliente.ConverterParaPDF(context.Background(), docxSintetico("lixo"))
	if pdf != nil {
		t.Fatal("não pode devolver PDF quando o corpo não é PDF")
	}
	exigirErroAplicacao(t, err)
}

// ---------------------------------------------------------------------------
// Caminho feliz e protocolo
// ---------------------------------------------------------------------------

func TestConverterParaPDFSucesso(t *testing.T) {
	t.Parallel()

	docx := docxSintetico("conteudo-do-documento-de-teste")
	pdfEsperado := []byte("%PDF-1.7\n...conteudo...\n%%EOF")

	servidor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("esperava método POST, obteve %s", r.Method)
		}
		if r.URL.Path != CaminhoConversao {
			t.Errorf("esperava caminho %q, obteve %q", CaminhoConversao, r.URL.Path)
		}
		corpo, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("ler corpo da requisição: %v", err)
		}
		if !bytes.Equal(corpo, docx) {
			t.Errorf("corpo da requisição divergiu do docx enviado: recebeu %d bytes, esperava %d", len(corpo), len(docx))
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(pdfEsperado)
	}))
	defer servidor.Close()

	cliente, err := NovoCliente(config.Conversor{URL: servidor.URL, TempoLimite: 5 * time.Second})
	if err != nil {
		t.Fatalf("construir cliente não deveria falhar, obteve %v", err)
	}

	pdf, err := cliente.ConverterParaPDF(context.Background(), docx)
	if err != nil {
		t.Fatalf("esperava sucesso, obteve %v", err)
	}
	if !bytes.Equal(pdf, pdfEsperado) {
		t.Fatalf("esperava %q, obteve %q", pdfEsperado, pdf)
	}
}

func TestConverterParaPDFNaoVazaConteudoDoDocumentoEmErro(t *testing.T) {
	t.Parallel()

	const marcador = "MARCADOR-SECRETO-DO-USUARIO"
	docx := docxSintetico(marcador + "-mais-conteudo-de-recheio-para-o-teste")

	servidor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "erro interno genérico")
	}))
	defer servidor.Close()

	cliente, err := NovoCliente(config.Conversor{URL: servidor.URL, TempoLimite: 5 * time.Second})
	if err != nil {
		t.Fatalf("construir cliente não deveria falhar, obteve %v", err)
	}

	_, err = cliente.ConverterParaPDF(context.Background(), docx)
	exigirErroAplicacao(t, err)

	mensagem := err.Error()
	if strings.Contains(mensagem, marcador) {
		t.Fatalf("mensagem de erro vazou marcador do documento do usuário: %v", err)
	}
	if strings.Contains(mensagem, string(assinaturaZIP)) {
		t.Fatalf("mensagem de erro vazou a assinatura zip do documento: %v", err)
	}
	for inicio := 0; inicio+16 <= len(docx); inicio++ {
		janela := string(docx[inicio : inicio+16])
		if strings.Contains(mensagem, janela) {
			t.Fatalf("mensagem de erro vazou um trecho de 16 bytes do documento original (offset %d): %v", inicio, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Tempo
// ---------------------------------------------------------------------------

func TestConverterParaPDFAplicaTempoLimiteProprio(t *testing.T) {
	t.Parallel()

	servidor := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer servidor.Close()

	cliente, err := NovoCliente(config.Conversor{URL: servidor.URL, TempoLimite: 150 * time.Millisecond})
	if err != nil {
		t.Fatalf("construir cliente não deveria falhar, obteve %v", err)
	}

	inicio := time.Now()
	pdf, err := cliente.ConverterParaPDF(context.Background(), docxSintetico("lixo"))
	decorrido := time.Since(inicio)

	if pdf != nil {
		t.Fatal("não pode devolver PDF quando o tempo limite estourou")
	}
	exigirErroAplicacao(t, err)
	if decorrido >= 2*time.Second {
		t.Fatalf("esperava que o tempo limite próprio abortasse antes de 2s, levou %v", decorrido)
	}
}

func TestConverterParaPDFHonraCancelamentoDoChamador(t *testing.T) {
	t.Parallel()

	servidor := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer servidor.Close()

	cliente, err := NovoCliente(config.Conversor{URL: servidor.URL, TempoLimite: time.Minute})
	if err != nil {
		t.Fatalf("construir cliente não deveria falhar, obteve %v", err)
	}

	ctx, cancelar := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancelar)
	defer cancelar()

	inicio := time.Now()
	pdf, err := cliente.ConverterParaPDF(ctx, docxSintetico("lixo"))
	decorrido := time.Since(inicio)

	if pdf != nil {
		t.Fatal("não pode devolver PDF quando o chamador cancelou")
	}
	exigirErroAplicacao(t, err)
	if decorrido >= 2*time.Second {
		t.Fatalf("esperava que o cancelamento do chamador abortasse antes de 2s, levou %v", decorrido)
	}
}

// ---------------------------------------------------------------------------
// Verificador
// ---------------------------------------------------------------------------

func TestVerificadorSeIdentificaComoConversor(t *testing.T) {
	t.Parallel()

	verificador := NovoVerificador(clienteMorto(t))
	if verificador == nil {
		t.Fatal("esperava verificador não nulo")
	}
	if verificador.Nome() != "conversor" {
		t.Fatalf("esperava nome %q, obteve %q", "conversor", verificador.Nome())
	}
}

func TestVerificadorExigeStatus200Exato(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome     string
		status   int
		esperaOK bool
	}{
		{nome: "200 esta vivo", status: http.StatusOK, esperaOK: true},
		{nome: "404 nao esta vivo", status: http.StatusNotFound, esperaOK: false},
		{nome: "503 nao esta vivo", status: http.StatusServiceUnavailable, esperaOK: false},
		{nome: "500 nao esta vivo", status: http.StatusInternalServerError, esperaOK: false},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			servidor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != CaminhoSaude {
					t.Errorf("esperava caminho %q, obteve %q", CaminhoSaude, r.URL.Path)
				}
				w.WriteHeader(caso.status)
			}))
			defer servidor.Close()

			cliente, err := NovoCliente(config.Conversor{URL: servidor.URL, TempoLimite: 5 * time.Second})
			if err != nil {
				t.Fatalf("construir cliente não deveria falhar, obteve %v", err)
			}

			err = NovoVerificador(cliente).Verificar(context.Background())
			if caso.esperaOK {
				if err != nil {
					t.Fatalf("esperava sucesso, obteve %v", err)
				}
				return
			}
			exigirErroAplicacao(t, err)
		})
	}
}

func TestVerificadorEndpointMortoNaoVazaTopologia(t *testing.T) {
	t.Parallel()

	err := NovoVerificador(clienteMorto(t)).Verificar(context.Background())
	aplicacao := exigirErroAplicacao(t, err)

	const mensagemEsperada = "conversor indisponível"
	if aplicacao.Error() != mensagemEsperada {
		t.Fatalf("esperava mensagem exata %q, obteve %q", mensagemEsperada, aplicacao.Error())
	}
	proibidos := []string{"127.0.0.1", "dial", "connection refused"}
	for _, proibido := range proibidos {
		if strings.Contains(aplicacao.Error(), proibido) {
			t.Fatalf("mensagem do verificador vazou topologia (%q): %v", proibido, aplicacao.Error())
		}
	}
}

// ---------------------------------------------------------------------------
// Contrato de constantes
// ---------------------------------------------------------------------------

func TestConstantesDoPacoteTemOsValoresDoContrato(t *testing.T) {
	t.Parallel()

	if CaminhoConversao != "/converter" {
		t.Fatalf("esperava CaminhoConversao %q, obteve %q", "/converter", CaminhoConversao)
	}
	if CaminhoSaude != "/saude" {
		t.Fatalf("esperava CaminhoSaude %q, obteve %q", "/saude", CaminhoSaude)
	}
	if TamanhoMaximoPDFBytes != 67108864 {
		t.Fatalf("esperava TamanhoMaximoPDFBytes 67108864, obteve %d", TamanhoMaximoPDFBytes)
	}
}
