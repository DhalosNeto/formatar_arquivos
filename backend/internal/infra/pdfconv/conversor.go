package pdfconv

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/daniel-halos/formatador/internal/infra/config"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// Contrato HTTP do sidecar de conversão, única fonte da verdade replicada em
// deploy/Dockerfile.libreoffice e no docstring de deploy/pdfconv-handler.py:
//
//	POST /converter  corpo = bytes crus do .docx (SEM multipart) -> 200 + bytes crus do .pdf
//	                 503 quando o sidecar está no teto de conversões simultâneas
//	                 504 quando a conversão estourou o prazo e foi morta
//	GET  /saude       -> 200 quando o sidecar respondeu de verdade a uma
//	                     chamada RPC ao unoserver interno; 503 caso contrário.
const (
	// CaminhoConversao é a rota do sidecar que recebe o DOCX cru e devolve o PDF.
	CaminhoConversao = "/converter"
	// CaminhoSaude é a rota de readiness do sidecar.
	CaminhoSaude = "/saude"
	// TipoConteudoDocx é o Content-Type enviado no corpo da conversão.
	TipoConteudoDocx = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	// TamanhoMaximoPDFBytes é o teto de tamanho aceito na resposta do
	// sidecar; acima disso a resposta é descartada, para um sidecar
	// comprometido não esgotar a memória do processo chamador.
	TamanhoMaximoPDFBytes = 64 * 1024 * 1024
)

// assinaturaPacoteZIP são os 4 primeiros bytes de qualquer pacote OOXML válido.
var assinaturaPacoteZIP = []byte{'P', 'K', 0x03, 0x04}

// assinaturaPDF são os bytes iniciais de qualquer arquivo PDF válido.
var assinaturaPDF = []byte("%PDF-")

// Cliente fala o protocolo HTTP do sidecar de conversão DOCX -> PDF.
type Cliente struct {
	baseURL    string
	httpClient *http.Client
}

// NovoCliente valida a configuração e monta o cliente. Não faz I/O: só
// analisa a URL e monta o *http.Client em memória.
func NovoCliente(cfg config.Conversor) (*Cliente, error) {
	invalidos := &errors.ErroValidacao{Mensagem: "configuração do conversor inválida"}

	if !urlValida(cfg.URL) {
		invalidos.Acrescentar("url", "precisa ser uma URL http(s) com host")
	}
	if cfg.TempoLimite <= 0 {
		invalidos.Acrescentar("tempoLimite", "precisa ser maior que zero")
	}
	if invalidos.TemCampos() {
		return nil, invalidos
	}

	return &Cliente{
		baseURL:    strings.TrimSuffix(cfg.URL, "/"),
		httpClient: &http.Client{Timeout: cfg.TempoLimite},
	}, nil
}

func urlValida(bruta string) bool {
	if strings.TrimSpace(bruta) == "" {
		return false
	}
	analisada, err := url.Parse(bruta)
	if err != nil {
		return false
	}
	if analisada.Scheme != "http" && analisada.Scheme != "https" {
		return false
	}
	return analisada.Host != ""
}

// ConverterParaPDF envia o DOCX cru ao sidecar e devolve os bytes do PDF
// resultante. A validação da entrada acontece inteiramente antes de qualquer
// I/O de rede.
func (c *Cliente) ConverterParaPDF(ctx context.Context, docx []byte) ([]byte, error) {
	if !bytes.HasPrefix(docx, assinaturaPacoteZIP) {
		return nil, errors.NovoErroValidacao("docx", "precisa ser um pacote OOXML válido")
	}

	requisicao, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+CaminhoConversao, bytes.NewReader(docx))
	if err != nil {
		return nil, errors.NovoErroAplicacao("montar requisição de conversão")
	}
	requisicao.Header.Set("Content-Type", TipoConteudoDocx)

	resposta, err := c.httpClient.Do(requisicao)
	if err != nil {
		return nil, errors.NovoErroAplicacao("conversor indisponível")
	}
	defer func() { _ = resposta.Body.Close() }()

	// 503 e 504 são condições TRANSITÓRIAS do sidecar (fila cheia, prazo
	// estourado), não defeito do documento. Todas continuam virando 500 para
	// quem chama a API — o cliente não errou nada —, mas a mensagem as separa
	// no log, que é o que permite decidir entre reenfileirar e desistir.
	switch resposta.StatusCode {
	case http.StatusOK:
	case http.StatusServiceUnavailable:
		return nil, errors.NovoErroAplicacao("conversor saturado")
	case http.StatusGatewayTimeout:
		return nil, errors.NovoErroAplicacao("conversão excedeu o prazo do conversor")
	default:
		return nil, errors.NovoErroAplicacao("conversão falhou")
	}

	// io.LimitReader corta a leitura em TamanhoMaximoPDFBytes+1: um sidecar
	// comprometido (ou um bug) não consegue fazer o cliente alocar memória
	// sem limite lendo uma resposta infinita.
	pdf, err := io.ReadAll(io.LimitReader(resposta.Body, TamanhoMaximoPDFBytes+1))
	if err != nil {
		return nil, errors.NovoErroAplicacao("ler resposta do conversor")
	}
	if len(pdf) > TamanhoMaximoPDFBytes {
		return nil, errors.NovoErroAplicacao("resposta do conversor excede o tamanho máximo")
	}
	if len(pdf) == 0 {
		return nil, errors.NovoErroAplicacao("conversor devolveu corpo vazio")
	}
	if !bytes.HasPrefix(pdf, assinaturaPDF) {
		return nil, errors.NovoErroAplicacao("conversor devolveu conteúdo que não é PDF")
	}

	return pdf, nil
}

// Verificador checa se o sidecar de conversão está pronto.
type Verificador struct{ cliente *Cliente }

// NovoVerificador cria o verificador de saúde do conversor.
func NovoVerificador(cliente *Cliente) *Verificador {
	return &Verificador{cliente: cliente}
}

// Nome identifica esta dependência no readiness.
func (v *Verificador) Nome() string { return "conversor" }

// Verificar satisfaz, por duck typing, a interface Verificador de
// internal/rotas/root/webrotas/saude (não importada aqui: infra não importa rotas).
// A mensagem de erro é deliberadamente genérica em todo caminho de falha:
// não deve vazar endereço, porta nem detalhe de transporte do sidecar.
func (v *Verificador) Verificar(ctx context.Context) error {
	requisicao, err := http.NewRequestWithContext(ctx, http.MethodGet, v.cliente.baseURL+CaminhoSaude, nil)
	if err != nil {
		return errors.NovoErroAplicacao("conversor indisponível")
	}

	resposta, err := v.cliente.httpClient.Do(requisicao)
	if err != nil {
		return errors.NovoErroAplicacao("conversor indisponível")
	}
	defer func() { _ = resposta.Body.Close() }()

	if resposta.StatusCode != http.StatusOK {
		return errors.NovoErroAplicacao("conversor indisponível")
	}
	return nil
}
