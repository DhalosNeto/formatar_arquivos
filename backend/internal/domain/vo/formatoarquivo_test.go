package vo

import (
	"strings"
	"testing"

	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// prefixoDocx é a assinatura de um pacote ZIP local file header (PK\x03\x04).
var prefixoDocx = []byte{0x50, 0x4B, 0x03, 0x04, 0x14, 0x00, 0x06, 0x00}

func TestFormatoArquivoValido(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome     string
		formato  FormatoArquivo
		esperado bool
	}{
		{nome: "docx é válido", formato: FormatoDocx, esperado: true},
		{nome: "pdf é válido", formato: FormatoPDF, esperado: true},
		{nome: "tex é válido", formato: FormatoLaTeX, esperado: true},
		{nome: "exe é inválido", formato: FormatoArquivo("exe"), esperado: false},
		{nome: "vazio é inválido", formato: FormatoArquivo(""), esperado: false},
		{nome: "docx em maiúsculas é inválido", formato: FormatoArquivo("DOCX"), esperado: false},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			if obtido := caso.formato.Valido(); obtido != caso.esperado {
				t.Fatalf("Valido() = %v, esperava %v", obtido, caso.esperado)
			}
		})
	}
}

func TestFormatoArquivoMIMEEExtensao(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome            string
		formato         FormatoArquivo
		mimeEsperado    string
		extensaEsperada string
		textoEsperado   string
	}{
		{
			nome:            "docx",
			formato:         FormatoDocx,
			mimeEsperado:    MIMEDocx,
			extensaEsperada: "docx",
			textoEsperado:   "docx",
		},
		{
			nome:            "pdf",
			formato:         FormatoPDF,
			mimeEsperado:    MIMEPDF,
			extensaEsperada: "pdf",
			textoEsperado:   "pdf",
		},
		{
			nome:            "latex",
			formato:         FormatoLaTeX,
			mimeEsperado:    MIMELaTeX,
			extensaEsperada: "tex",
			textoEsperado:   "tex",
		},
		{
			nome:            "formato inválido não tem mime nem extensão",
			formato:         FormatoArquivo("exe"),
			mimeEsperado:    "",
			extensaEsperada: "",
			textoEsperado:   "exe",
		},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			if obtido := caso.formato.MIME(); obtido != caso.mimeEsperado {
				t.Fatalf("MIME() = %q, esperava %q", obtido, caso.mimeEsperado)
			}
			if obtido := caso.formato.Extensao(); obtido != caso.extensaEsperada {
				t.Fatalf("Extensao() = %q, esperava %q", obtido, caso.extensaEsperada)
			}
			if obtido := caso.formato.String(); obtido != caso.textoEsperado {
				t.Fatalf("String() = %q, esperava %q", obtido, caso.textoEsperado)
			}
		})
	}
}

func TestFormatoPorMIMEFazRoundTrip(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome     string
		mime     string
		esperado FormatoArquivo
	}{
		{nome: "mime de docx", mime: MIMEDocx, esperado: FormatoDocx},
		{nome: "mime de pdf", mime: MIMEPDF, esperado: FormatoPDF},
		{nome: "mime de latex", mime: MIMELaTeX, esperado: FormatoLaTeX},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			formato, err := FormatoPorMIME(caso.mime)
			if err != nil {
				t.Fatalf("não esperava erro, obteve %v", err)
			}
			if formato != caso.esperado {
				t.Fatalf("FormatoPorMIME(%q) = %q, esperava %q", caso.mime, formato, caso.esperado)
			}
			if formato.MIME() != caso.mime {
				t.Fatalf("round-trip falhou: %q -> %q -> %q", caso.mime, formato, formato.MIME())
			}
		})
	}
}

func TestFormatoPorMIMERejeitaDesconhecido(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome string
		mime string
	}{
		{nome: "mime desconhecido", mime: "application/x-msdownload"},
		{nome: "mime vazio", mime: ""},
		{nome: "mime de texto puro", mime: "text/plain"},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			_, err := FormatoPorMIME(caso.mime)
			if err == nil {
				t.Fatal("esperava erro de validação")
			}

			var invalido *errors.ErroValidacao
			if !errors.Como(err, &invalido) {
				t.Fatalf("esperava *ErroValidacao, obteve %T", err)
			}
			exigirCampo(t, invalido, "mime")
		})
	}
}

func TestDetectarFormatoReconhecePacotesSuportados(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome     string
		prefixo  []byte
		esperado FormatoArquivo
	}{
		{nome: "docx pela assinatura PK", prefixo: prefixoDocx, esperado: FormatoDocx},
		{nome: "pdf pela assinatura %PDF", prefixo: []byte("%PDF-1.7"), esperado: FormatoPDF},
		{nome: "pdf com prefixo maior que o mínimo", prefixo: []byte("%PDF-1.4\n%âãÏÓ"), esperado: FormatoPDF},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			formato, err := DetectarFormato(caso.prefixo)
			if err != nil {
				t.Fatalf("não esperava erro, obteve %v", err)
			}
			if formato != caso.esperado {
				t.Fatalf("DetectarFormato() = %q, esperava %q", formato, caso.esperado)
			}
		})
	}
}

func TestDetectarFormatoRejeitaNaoSuportados(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome    string
		prefixo []byte
	}{
		{nome: "binário ELF", prefixo: []byte{0x7F, 0x45, 0x4C, 0x46, 0x02, 0x01, 0x01, 0x00}},
		{nome: "documento RTF", prefixo: []byte(`{\rtf1\ansi`)},
		{nome: "zip vazio", prefixo: []byte{0x50, 0x4B, 0x05, 0x06, 0x00, 0x00, 0x00, 0x00}},
		{nome: "zip segmentado", prefixo: []byte{0x50, 0x4B, 0x07, 0x08, 0x00, 0x00, 0x00, 0x00}},
		{nome: "prefixo curto de três bytes", prefixo: []byte{0x50, 0x4B, 0x03}},
		{nome: "prefixo nulo", prefixo: nil},
		{nome: "prefixo vazio", prefixo: []byte{}},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			_, err := DetectarFormato(caso.prefixo)
			if err == nil {
				t.Fatal("esperava erro de validação")
			}

			var invalido *errors.ErroValidacao
			if !errors.Como(err, &invalido) {
				t.Fatalf("esperava *ErroValidacao, obteve %T", err)
			}
			exigirCampo(t, invalido, "arquivo")
			exigirMensagemSemBytes(t, err.Error(), caso.prefixo)
		})
	}
}

func TestDetectarFormatoExigeTamanhoMinimoDePrefixo(t *testing.T) {
	t.Parallel()

	if TamanhoPrefixoDeteccao != 8 {
		t.Fatalf("TamanhoPrefixoDeteccao = %d, esperava 8", TamanhoPrefixoDeteccao)
	}

	curto := make([]byte, TamanhoPrefixoDeteccao-1)
	copy(curto, prefixoDocx)

	if _, err := DetectarFormato(curto); err == nil {
		t.Fatal("esperava erro para prefixo menor que o mínimo, mesmo com assinatura válida")
	}
}

func TestConferirPacoteDocx(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome      string
		partes    []string
		esperaErr bool
	}{
		{
			nome:      "pacote docx completo",
			partes:    []string{ParteContentTypes, "_rels/.rels", ParteDocumentoPrincipal, "word/styles.xml"},
			esperaErr: false,
		},
		{
			nome:      "pacote docx em ordem invertida",
			partes:    []string{ParteDocumentoPrincipal, ParteContentTypes, "_rels/.rels"},
			esperaErr: false,
		},
		{
			nome:      "pacote odt",
			partes:    []string{"mimetype", "content.xml", "styles.xml"},
			esperaErr: true,
		},
		{
			nome:      "pacote xlsx",
			partes:    []string{ParteContentTypes, "xl/workbook.xml"},
			esperaErr: true,
		},
		{
			nome:      "lista vazia",
			partes:    []string{},
			esperaErr: true,
		},
		{
			nome:      "lista nula",
			partes:    nil,
			esperaErr: true,
		},
		{
			nome:      "sem content types",
			partes:    []string{"_rels/.rels", ParteDocumentoPrincipal},
			esperaErr: true,
		},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			err := ConferirPacoteDocx(caso.partes)
			if !caso.esperaErr {
				if err != nil {
					t.Fatalf("não esperava erro, obteve %v", err)
				}
				return
			}

			if err == nil {
				t.Fatal("esperava erro de validação")
			}

			var invalido *errors.ErroValidacao
			if !errors.Como(err, &invalido) {
				t.Fatalf("esperava *ErroValidacao, obteve %T", err)
			}
			exigirCampo(t, invalido, "arquivo")
		})
	}
}

// exigirCampo falha o teste quando o erro de validação não reprova o campo esperado.
// Assere sobre o nome do campo, nunca sobre o texto completo, que pode carregar dado do usuário.
func exigirCampo(t *testing.T, erro *errors.ErroValidacao, campo string) {
	t.Helper()

	for _, invalido := range erro.Campos {
		if invalido.Campo == campo {
			return
		}
	}

	nomes := make([]string, 0, len(erro.Campos))
	for _, invalido := range erro.Campos {
		nomes = append(nomes, invalido.Campo)
	}
	t.Fatalf("esperava o campo %q entre %v", campo, nomes)
}

// exigirMensagemSemBytes garante que a mensagem de erro não vaza o conteúdo do arquivo do usuário.
func exigirMensagemSemBytes(t *testing.T, mensagem string, prefixo []byte) {
	t.Helper()

	if len(prefixo) == 0 {
		return
	}
	if strings.Contains(mensagem, string(prefixo)) {
		t.Fatal("a mensagem de erro não pode conter os bytes recebidos do usuário")
	}
	for _, octeto := range prefixo {
		hexa := hexDeOcteto(octeto)
		if octeto != 0x00 && strings.Contains(strings.ToLower(mensagem), hexa) {
			t.Fatalf("a mensagem de erro não pode conter o byte %s do arquivo do usuário", hexa)
		}
	}
}

func hexDeOcteto(octeto byte) string {
	digitos := "0123456789abcdef"
	return string([]byte{digitos[octeto>>4], digitos[octeto&0x0F]})
}
