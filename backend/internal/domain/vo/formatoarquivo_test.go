package vo

import (
	"archive/zip"
	"bytes"
	"fmt"
	"math/rand"
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

// entradaZip é um par nome/conteúdo usado para montar um ZIP determinístico
// em memória. A ordem da lista é a ordem de escrita: necessária para o caso
// que exercita as partes obrigatórias fora de ordem.
type entradaZip struct {
	nome     string
	conteudo []byte
	metodo   uint16
}

// entradaDeflate monta uma entradaZip comprimida com DEFLATE — o método real
// usado por pacotes OOXML. O método é sempre explícito porque o valor zero de
// uint16 coincide com zip.Store: deixar implícito trocaria o método sem
// avisar.
func entradaDeflate(nome string, conteudo []byte) entradaZip {
	return entradaZip{nome: nome, conteudo: conteudo, metodo: zip.Deflate}
}

// entradaVazia monta uma entrada sem conteúdo (típica de diretório dentro de
// um ZIP), gravada com zip.Store para que também o tamanho comprimido saia
// zero — é o caso que exercita divisão por zero na razão de descompressão.
func entradaVazia(nome string) entradaZip {
	return entradaZip{nome: nome, metodo: zip.Store}
}

// montarZip escreve um ZIP em memória a partir de uma lista ordenada de
// entradas e devolve os bytes do pacote. Falha o teste se o ZIP em si não
// puder ser montado — isso não faz parte do que TestConferirPacoteDocx cobre.
func montarZip(t *testing.T, entradas []entradaZip) []byte {
	t.Helper()

	var buf bytes.Buffer
	escritor := zip.NewWriter(&buf)
	for _, entrada := range entradas {
		saida, err := escritor.CreateHeader(&zip.FileHeader{Name: entrada.nome, Method: entrada.metodo})
		if err != nil {
			t.Fatalf("montarZip: CreateHeader(%q): %v", entrada.nome, err)
		}
		if _, err := saida.Write(entrada.conteudo); err != nil {
			t.Fatalf("montarZip: Write(%q): %v", entrada.nome, err)
		}
	}
	if err := escritor.Close(); err != nil {
		t.Fatalf("montarZip: Close: %v", err)
	}
	return buf.Bytes()
}

// Conteúdo mínimo das duas partes obrigatórias de um pacote DOCX. O conteúdo
// em si é irrelevante para ConferirPacoteDocx: só o nome e o tamanho
// descomprimido de cada entrada importam.
var (
	conteudoContentTypesMinimo = []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`)
	conteudoDocumentoMinimo    = []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body/></w:document>`)
)

// docxPartesObrigatorias devolve as duas entradas exigidas por um DOCX
// válido, na ordem canônica ([Content_Types].xml, depois word/document.xml).
func docxPartesObrigatorias() []entradaZip {
	return []entradaZip{
		entradaDeflate(ParteContentTypes, conteudoContentTypesMinimo),
		entradaDeflate(ParteDocumentoPrincipal, conteudoDocumentoMinimo),
	}
}

// entradasDePreenchimento gera n entradas pequenas e distintas, só para
// inflar a contagem total de entradas do pacote nos testes de limite.
func entradasDePreenchimento(n int) []entradaZip {
	entradas := make([]entradaZip, n)
	for i := range entradas {
		entradas[i] = entradaDeflate(fmt.Sprintf("word/preenchimento%d.xml", i), []byte("x"))
	}
	return entradas
}

// blocoPseudoAleatorio gera um bloco determinístico e pouco compressível
// (dados quase incompressíveis isoladamente), para servir de unidade repetida
// em conteúdos com razão de descompressão controlada: repetir o MESMO bloco
// várias vezes deixa o DEFLATE encontrar referências de volta muito baratas,
// então a razão cresce de forma previsível com o número de repetições, sem
// disparar para milhares como acontece com bytes.Repeat de zeros.
func blocoPseudoAleatorio(tamanho int) []byte {
	fonte := rand.New(rand.NewSource(42))
	bloco := make([]byte, tamanho)
	_, _ = fonte.Read(bloco)
	return bloco
}

func TestConferirPacoteDocx(t *testing.T) {
	t.Parallel()

	obrigatorias := docxPartesObrigatorias()
	completo := montarZip(t, docxPartesObrigatorias())
	completoTruncado := completo[:len(completo)-10]

	// Bloco de 4096 bytes repetido 300x: ~1,17 MiB descomprimido, razão ~104:1
	// — acima do piso de 1 MiB e comfortavelmente abaixo do teto de 200:1.
	blocoDentroDoLimite := bytes.Repeat(blocoPseudoAleatorio(4096), 300)

	// Mesmo bloco repetido 14080x: ~55 MiB por entrada, razão ~169:1 — ainda
	// dentro do limite por entrada, mas cinco cópias somam ~275 MiB, acima do
	// teto de 250 MiB. Prova que é a SOMA que é checada, não só a razão local.
	blocoParaSoma := bytes.Repeat(blocoPseudoAleatorio(4096), 14080)
	entradasDeSoma := make([]entradaZip, 0, 5)
	for i := 0; i < 5; i++ {
		entradasDeSoma = append(entradasDeSoma, entradaDeflate(fmt.Sprintf("word/bomba%d.xml", i), blocoParaSoma))
	}

	casos := []struct {
		nome            string
		conteudo        []byte
		esperaErr       bool
		mensagemAbusiva bool
	}{
		{
			nome:     "pacote docx mínimo",
			conteudo: montarZip(t, docxPartesObrigatorias()),
		},
		{
			nome: "pacote docx completo",
			conteudo: montarZip(t, []entradaZip{
				obrigatorias[0],
				entradaDeflate("_rels/.rels", []byte(`<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"/>`)),
				obrigatorias[1],
				entradaDeflate("word/styles.xml", []byte(`<w:styles/>`)),
				entradaDeflate("word/media/img.png", []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}),
			}),
		},
		{
			nome:     "ordem das entradas invertida",
			conteudo: montarZip(t, []entradaZip{obrigatorias[1], obrigatorias[0]}),
		},
		{
			nome: "partes obrigatórias duplicadas",
			conteudo: montarZip(t, []entradaZip{
				obrigatorias[0], obrigatorias[0], obrigatorias[1], obrigatorias[1],
			}),
		},
		{
			nome:      "faltando content types",
			conteudo:  montarZip(t, []entradaZip{obrigatorias[1]}),
			esperaErr: true,
		},
		{
			nome:      "faltando word/document.xml",
			conteudo:  montarZip(t, []entradaZip{obrigatorias[0]}),
			esperaErr: true,
		},
		{
			nome: "pacote odt",
			conteudo: montarZip(t, []entradaZip{
				entradaDeflate("mimetype", []byte("application/vnd.oasis.opendocument.text")),
				entradaDeflate("content.xml", []byte("<office:document-content/>")),
				entradaDeflate("styles.xml", []byte("<office:document-styles/>")),
			}),
			esperaErr: true,
		},
		{
			nome: "pacote xlsx",
			conteudo: montarZip(t, []entradaZip{
				obrigatorias[0],
				entradaDeflate("xl/workbook.xml", []byte("<workbook/>")),
			}),
			esperaErr: true,
		},
		{
			nome: "nomes em caixa alta",
			conteudo: montarZip(t, []entradaZip{
				entradaDeflate("[CONTENT_TYPES].XML", conteudoContentTypesMinimo),
				entradaDeflate("WORD/DOCUMENT.XML", conteudoDocumentoMinimo),
			}),
			esperaErr: true,
		},
		{
			nome: "prefixo de diretório errado",
			conteudo: montarZip(t, []entradaZip{
				obrigatorias[0],
				entradaDeflate("xl/word/document.xml", conteudoDocumentoMinimo),
			}),
			esperaErr: true,
		},
		{
			nome:      "zip sem entrada alguma",
			conteudo:  montarZip(t, nil),
			esperaErr: true,
		},
		{
			nome:      "não é um zip",
			conteudo:  []byte("não sou zip"),
			esperaErr: true,
		},
		{
			nome:      "conteúdo nulo",
			conteudo:  nil,
			esperaErr: true,
		},
		{
			nome:      "conteúdo vazio",
			conteudo:  []byte{},
			esperaErr: true,
		},
		{
			nome:      "zip válido truncado",
			conteudo:  completoTruncado,
			esperaErr: true,
		},
		{
			nome: "razão de descompressão abusiva",
			conteudo: montarZip(t, []entradaZip{
				obrigatorias[0], obrigatorias[1],
				entradaDeflate("word/bomba.xml", bytes.Repeat([]byte{0}, 4<<20)),
			}),
			esperaErr:       true,
			mensagemAbusiva: true,
		},
		{
			nome: "razão alta mas abaixo do piso de descompressão",
			conteudo: montarZip(t, []entradaZip{
				obrigatorias[0], obrigatorias[1],
				entradaDeflate("word/quase.xml", bytes.Repeat([]byte{0}, 512<<10)),
			}),
		},
		{
			nome: "razão alta mas dentro do limite permitido",
			conteudo: montarZip(t, []entradaZip{
				obrigatorias[0], obrigatorias[1],
				entradaDeflate("word/moderado.xml", blocoDentroDoLimite),
			}),
		},
		{
			nome: "entrada com tamanho comprimido e descomprimido zero",
			conteudo: montarZip(t, []entradaZip{
				obrigatorias[0], obrigatorias[1],
				entradaVazia("word/"),
			}),
		},
		{
			nome: "exatamente o máximo de entradas permitido",
			conteudo: montarZip(t, append(
				append([]entradaZip{}, obrigatorias...),
				entradasDePreenchimento(MaximoEntradasPacote-2)...,
			)),
		},
		{
			nome: "uma entrada a mais que o máximo permitido",
			conteudo: montarZip(t, append(
				append([]entradaZip{}, obrigatorias...),
				entradasDePreenchimento(MaximoEntradasPacote-1)...,
			)),
			esperaErr: true,
		},
		{
			nome: "soma total descomprimida acima do teto",
			conteudo: montarZip(t, append(
				append([]entradaZip{}, obrigatorias...),
				entradasDeSoma...,
			)),
			esperaErr:       true,
			mensagemAbusiva: true,
		},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			err := ConferirPacoteDocx(caso.conteudo)
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

			if caso.mensagemAbusiva {
				exigirMensagemDeCampo(t, invalido, mensagemPacoteDocxAbusivo)
			}
		})
	}
}

// exigirMensagemDeCampo falha o teste quando nenhum campo reprovado carrega a
// mensagem esperada. Usado para distinguir o pacote recusado por abuso de
// descompressão do pacote recusado por estrutura inválida — as duas viram
// *errors.ErroValidacao no campo "arquivo", mas com mensagens diferentes.
func exigirMensagemDeCampo(t *testing.T, erro *errors.ErroValidacao, mensagem string) {
	t.Helper()

	for _, invalido := range erro.Campos {
		if invalido.Mensagem == mensagem {
			return
		}
	}
	t.Fatalf("esperava a mensagem %q entre os campos reprovados", mensagem)
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
