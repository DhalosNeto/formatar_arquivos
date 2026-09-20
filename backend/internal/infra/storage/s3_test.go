package storage

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/config"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// endpointMorto aponta para uma porta reservada que jamais aceita conexão.
// Qualquer caso de teste que chegue à rede usando este endpoint vai travar até
// o timeout do contexto, o que denuncia validação feita tarde demais.
const endpointMorto = "http://127.0.0.1:1"

// limiteSemRede é o teto de tempo tolerado para uma chamada que deveria falhar
// na validação, antes de qualquer I/O.
const limiteSemRede = time.Second

func configValida() config.Storage {
	return config.Storage{
		Endpoint:  endpointMorto,
		Bucket:    "documentos",
		AccessKey: "chave-de-teste",
		SecretKey: "segredo-de-teste",
		Regiao:    "us-east-1",
	}
}

// clienteMorto constrói um cliente apontado para um endpoint que não responde.
func clienteMorto(t *testing.T) *ClienteS3 {
	t.Helper()

	cliente, err := NovoClienteS3(context.Background(), configValida())
	if err != nil {
		t.Fatalf("construir cliente não deveria falhar, obteve %v", err)
	}
	if cliente == nil {
		t.Fatal("esperava cliente não nulo")
	}
	return cliente
}

func exigirErroValidacao(t *testing.T, err error, campo string) {
	t.Helper()

	if err == nil {
		t.Fatal("esperava erro de validação, obteve nil")
	}
	var invalido *errors.ErroValidacao
	if !errors.Como(err, &invalido) {
		t.Fatalf("esperava *errors.ErroValidacao, obteve %T (%v)", err, err)
	}
	if campo == "" {
		return
	}
	if !temCampo(invalido, campo) {
		t.Fatalf("esperava campo %q reprovado, obteve %v", campo, invalido.Campos)
	}
}

func TestNovoClienteS3RejeitaConfiguracaoInvalida(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome   string
		ajuste func(*config.Storage)
		campo  string
	}{
		{
			nome:   "bucket vazio",
			ajuste: func(c *config.Storage) { c.Bucket = "" },
			campo:  "bucket",
		},
		{
			nome:   "endpoint sem esquema",
			ajuste: func(c *config.Storage) { c.Endpoint = "minio:9000" },
			campo:  "endpoint",
		},
		{
			nome:   "endpoint sintaticamente quebrado",
			ajuste: func(c *config.Storage) { c.Endpoint = "://quebrado" },
			campo:  "endpoint",
		},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			cfg := configValida()
			caso.ajuste(&cfg)

			cliente, err := NovoClienteS3(context.Background(), cfg)
			if cliente != nil {
				t.Fatal("esperava cliente nulo quando a configuração é inválida")
			}
			exigirErroValidacao(t, err, caso.campo)
		})
	}
}

func TestNovoClienteS3NaoFazIOAoConstruir(t *testing.T) {
	t.Parallel()

	ctx, cancelar := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelar()

	inicio := time.Now()
	cliente, err := NovoClienteS3(ctx, configValida())
	decorrido := time.Since(inicio)

	if err != nil {
		t.Fatalf("esperava sucesso ao construir cliente com endpoint inexistente, obteve %v", err)
	}
	if cliente == nil {
		t.Fatal("esperava cliente não nulo")
	}
	if decorrido > limiteSemRede {
		t.Fatalf("construir cliente levou %v: parece estar tocando a rede", decorrido)
	}
}

func TestURLPreAssinadaValidaAntesDeAssinar(t *testing.T) {
	t.Parallel()

	cliente := clienteMorto(t)

	casos := []struct {
		nome     string
		chave    vo.ChaveStorage
		validade time.Duration
		campo    string
	}{
		{
			nome:     "validade acima do máximo",
			chave:    "documentos/2026/abc.docx",
			validade: 2 * time.Hour,
			campo:    "validade",
		},
		{
			nome:     "validade negativa",
			chave:    "documentos/2026/abc.docx",
			validade: -time.Minute,
			campo:    "validade",
		},
		{
			nome:     "chave com travessia de diretório",
			chave:    "../fuga",
			validade: time.Minute,
			campo:    "chave",
		},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			ctx, cancelar := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancelar()

			inicio := time.Now()
			url, err := cliente.URLPreAssinada(ctx, caso.chave, caso.validade)
			decorrido := time.Since(inicio)

			exigirErroValidacao(t, err, caso.campo)
			if url != "" {
				t.Fatal("não pode devolver URL quando a entrada é inválida")
			}
			if decorrido > limiteSemRede {
				t.Fatalf("validação levou %v: a rede foi chamada antes de validar", decorrido)
			}
		})
	}
}

func TestSalvarValidaAntesDeQualquerRede(t *testing.T) {
	t.Parallel()

	cliente := clienteMorto(t)
	conteudoValido := strings.NewReader("conteúdo")

	casos := []struct {
		nome        string
		chave       vo.ChaveStorage
		conteudo    io.Reader
		tamanho     int64
		contentType string
		campo       string
	}{
		{
			nome:        "tamanho zero",
			chave:       "documentos/a.docx",
			conteudo:    conteudoValido,
			tamanho:     0,
			contentType: "application/octet-stream",
			campo:       "tamanho",
		},
		{
			nome:        "tamanho negativo",
			chave:       "documentos/a.docx",
			conteudo:    conteudoValido,
			tamanho:     -1,
			contentType: "application/octet-stream",
			campo:       "tamanho",
		},
		{
			nome:        "conteúdo nulo",
			chave:       "documentos/a.docx",
			conteudo:    nil,
			tamanho:     8,
			contentType: "application/octet-stream",
			campo:       "conteudo",
		},
		{
			nome:        "content type vazio",
			chave:       "documentos/a.docx",
			conteudo:    conteudoValido,
			tamanho:     8,
			contentType: "",
			campo:       "contentType",
		},
		{
			nome:        "chave com travessia de diretório",
			chave:       "../fuga",
			conteudo:    conteudoValido,
			tamanho:     8,
			contentType: "application/octet-stream",
			campo:       "chave",
		},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			ctx, cancelar := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancelar()

			inicio := time.Now()
			err := cliente.Salvar(ctx, caso.chave, caso.conteudo, caso.tamanho, caso.contentType)
			decorrido := time.Since(inicio)

			exigirErroValidacao(t, err, caso.campo)
			if decorrido > limiteSemRede {
				t.Fatalf("validação levou %v: a rede foi chamada antes de validar", decorrido)
			}
		})
	}
}

func TestObterValidaAntesDeQualquerRede(t *testing.T) {
	t.Parallel()

	cliente := clienteMorto(t)

	casos := []struct {
		nome  string
		chave vo.ChaveStorage
	}{
		{nome: "chave vazia", chave: ""},
		{nome: "travessia de diretório", chave: "../fuga"},
		{nome: "barra inicial", chave: "/documentos/a.docx"},
		{nome: "byte nulo", chave: "documentos/a\x00.docx"},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			ctx, cancelar := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancelar()

			inicio := time.Now()
			corpo, err := cliente.Obter(ctx, caso.chave)
			decorrido := time.Since(inicio)

			exigirErroValidacao(t, err, "chave")
			if corpo != nil {
				t.Fatal("não pode devolver corpo quando a chave é inválida")
			}
			if decorrido > limiteSemRede {
				t.Fatalf("validação levou %v: a rede foi chamada antes de validar", decorrido)
			}
		})
	}
}

func TestVerificadorSeIdentificaComoStorage(t *testing.T) {
	t.Parallel()

	verificador := NovoVerificador(clienteMorto(t))
	if verificador == nil {
		t.Fatal("esperava verificador não nulo")
	}
	if verificador.Nome() != "storage" {
		t.Fatalf("esperava nome %q, obteve %q", "storage", verificador.Nome())
	}
}

func TestConstantesDoPacoteTemOsValoresDoContrato(t *testing.T) {
	t.Parallel()

	if ValidadePadraoURL.String() != "15m0s" {
		t.Fatalf("esperava ValidadePadraoURL de 15m, obteve %v", ValidadePadraoURL)
	}
	if ValidadeMaximaURL.String() != "1h0m0s" {
		t.Fatalf("esperava ValidadeMaximaURL de 1h, obteve %v", ValidadeMaximaURL)
	}
	if ValidadePadraoURL > ValidadeMaximaURL {
		t.Fatal("a validade padrão não pode ultrapassar a máxima")
	}
}

func temCampo(err *errors.ErroValidacao, campo string) bool {
	for _, invalido := range err.Campos {
		if invalido.Campo == campo {
			return true
		}
	}
	return false
}
