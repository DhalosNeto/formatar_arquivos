// Package vo reúne os objetos de valor do domínio: tipos imutáveis que se
// validam na construção e não dependem de nenhum outro pacote do sistema.
package vo

import (
	"archive/zip"
	"bytes"

	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// FormatoArquivo identifica o formato de um arquivo manipulado pelo sistema.
type FormatoArquivo string

const (
	// FormatoDocx é o pacote OOXML de texto do Word.
	FormatoDocx FormatoArquivo = "docx"
	// FormatoPDF é o documento portátil da Adobe.
	FormatoPDF FormatoArquivo = "pdf"
	// FormatoLaTeX é o fonte TeX/LaTeX.
	FormatoLaTeX FormatoArquivo = "tex"
)

// Tipos MIME correspondentes aos formatos suportados.
const (
	MIMEDocx  = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	MIMEPDF   = "application/pdf"
	MIMELaTeX = "application/x-tex"
)

// TamanhoPrefixoDeteccao é quanto do início do arquivo DetectarFormato exige
// para decidir. As assinaturas reconhecidas têm 4 bytes; os 8 são folga
// deliberada, para recusar upload truncado antes de classificá-lo.
const TamanhoPrefixoDeteccao = 8

// Partes obrigatórias de um pacote DOCX válido.
const (
	ParteContentTypes       = "[Content_Types].xml"
	ParteDocumentoPrincipal = "word/document.xml"
)

// Mensagens fixas: nunca carregam conteúdo do arquivo do usuário (CLAUDE.md, regra 7).
const (
	mensagemFormatoNaoSuportado = "formato de arquivo não suportado"
	mensagemPrefixoInsuficiente = "conteúdo insuficiente para identificar o formato"
	mensagemPacoteDocxInvalido  = "o arquivo não é um pacote DOCX válido"
	mensagemMIMENaoSuportado    = "tipo de conteúdo não suportado"
	mensagemPacoteDocxAbusivo   = "o pacote excede os limites de descompressão permitidos"
)

// Limites contra zip bomb na conferência de pacotes DOCX. Os valores vêm da
// central directory do ZIP, nunca de descompressão real.
const (
	// RazaoDescompressaoMaxima é o maior fator (descomprimido / comprimido)
	// tolerado por entrada, acima do PisoRazaoDescompressaoBytes.
	RazaoDescompressaoMaxima = 200
	// PisoRazaoDescompressaoBytes isenta entradas pequenas da checagem de
	// razão: arquivos legítimos e pequenos (ex.: XML repetitivo) podem
	// comprimir muito bem sem serem uma bomba.
	PisoRazaoDescompressaoBytes uint64 = 1 << 20
	// MaximoEntradasPacote é o teto de arquivos dentro do ZIP.
	MaximoEntradasPacote = 512
	// TamanhoDescomprimidoMaximoBytes é o teto da soma de todos os tamanhos
	// descomprimidos declarados no pacote.
	TamanhoDescomprimidoMaximoBytes uint64 = 250 << 20
)

// Assinaturas binárias reconhecidas no início do arquivo.
var (
	assinaturaZipLocal = []byte{0x50, 0x4B, 0x03, 0x04}
	assinaturaPDF      = []byte("%PDF")
)

// Valido informa se o formato é um dos suportados pelo sistema.
func (f FormatoArquivo) Valido() bool {
	switch f {
	case FormatoDocx, FormatoPDF, FormatoLaTeX:
		return true
	default:
		return false
	}
}

// String devolve a representação textual do formato, mesmo quando inválido.
func (f FormatoArquivo) String() string { return string(f) }

// MIME devolve o tipo de conteúdo do formato, ou vazio quando inválido.
func (f FormatoArquivo) MIME() string {
	switch f {
	case FormatoDocx:
		return MIMEDocx
	case FormatoPDF:
		return MIMEPDF
	case FormatoLaTeX:
		return MIMELaTeX
	default:
		return ""
	}
}

// Extensao devolve a extensão de arquivo do formato, ou vazio quando inválido.
func (f FormatoArquivo) Extensao() string {
	if !f.Valido() {
		return ""
	}
	return string(f)
}

// FormatoPorMIME converte um tipo de conteúdo declarado no formato correspondente.
func FormatoPorMIME(mime string) (FormatoArquivo, error) {
	switch mime {
	case MIMEDocx:
		return FormatoDocx, nil
	case MIMEPDF:
		return FormatoPDF, nil
	case MIMELaTeX:
		return FormatoLaTeX, nil
	default:
		return "", errors.NovoErroValidacao("mime", mensagemMIMENaoSuportado)
	}
}

// DetectarFormato identifica o formato pelos primeiros bytes do arquivo, sem
// confiar no MIME nem na extensão informados pelo cliente.
//
// O resultado FormatoDocx é PROVISÓRIO: a assinatura é a de um ZIP local file
// header, comum a xlsx, odt, jar e apk. Quem receber FormatoDocx tem que
// confirmar o pacote com ConferirPacoteDocx antes de tratá-lo como DOCX.
func DetectarFormato(prefixo []byte) (FormatoArquivo, error) {
	if len(prefixo) < TamanhoPrefixoDeteccao {
		return "", errors.NovoErroValidacao("arquivo", mensagemPrefixoInsuficiente)
	}

	switch {
	case bytes.HasPrefix(prefixo, assinaturaZipLocal):
		return FormatoDocx, nil
	case bytes.HasPrefix(prefixo, assinaturaPDF):
		return FormatoPDF, nil
	default:
		return "", errors.NovoErroValidacao("arquivo", mensagemFormatoNaoSuportado)
	}
}

// ConferirPacoteDocx abre o ZIP e valida que ele corresponde a um DOCX,
// recusando pacotes construídos para esgotar memória por descompressão (zip
// bomb). Os tamanhos usados vêm da central directory do ZIP — ou seja, são
// DECLARADOS pelo cabeçalho, sem nenhuma entrada ser de fato descomprimida.
func ConferirPacoteDocx(conteudo []byte) error {
	leitor, err := zip.NewReader(bytes.NewReader(conteudo), int64(len(conteudo)))
	if err != nil {
		return errors.NovoErroValidacao("arquivo", mensagemPacoteDocxInvalido)
	}

	if len(leitor.File) == 0 {
		return errors.NovoErroValidacao("arquivo", mensagemPacoteDocxInvalido)
	}
	if len(leitor.File) > MaximoEntradasPacote {
		return errors.NovoErroValidacao("arquivo", mensagemPacoteDocxAbusivo)
	}

	var total uint64
	var temContentTypes, temDocumentoPrincipal bool

	// ponytail: os tamanhos são DECLARADOS pela central directory e um ZIP
	// hostil pode mentir; este teto é a barreira barata de primeira linha.
	// Quem descomprimir de verdade (F2) precisa envolver cada f.Open() num
	// io.LimitReader.
	for _, f := range leitor.File {
		// Teto POR ENTRADA antes de somar. Serve a duas coisas: barra de uma vez
		// o ZIP64 que declara um tamanho absurdo, e mantém a soma abaixo de
		// MaximoEntradasPacote * TamanhoDescomprimidoMaximoBytes, que cabe
		// folgadamente em uint64 — então o acumulador não estoura.
		//
		// Não compare com len(conteudo): compressão existe exatamente para que
		// o descomprimido seja MAIOR que o pacote. 512 KiB de zeros viram ~600
		// bytes em deflate, e um teto assim reprovaria o caso legítimo.
		if f.UncompressedSize64 > TamanhoDescomprimidoMaximoBytes {
			return errors.NovoErroValidacao("arquivo", mensagemPacoteDocxAbusivo)
		}

		total += f.UncompressedSize64
		if total > TamanhoDescomprimidoMaximoBytes {
			return errors.NovoErroValidacao("arquivo", mensagemPacoteDocxAbusivo)
		}

		if f.CompressedSize64 == 0 && f.UncompressedSize64 > 0 {
			return errors.NovoErroValidacao("arquivo", mensagemPacoteDocxAbusivo)
		}

		if f.UncompressedSize64 > PisoRazaoDescompressaoBytes &&
			f.UncompressedSize64/f.CompressedSize64 > RazaoDescompressaoMaxima {
			return errors.NovoErroValidacao("arquivo", mensagemPacoteDocxAbusivo)
		}

		switch f.Name {
		case ParteContentTypes:
			temContentTypes = true
		case ParteDocumentoPrincipal:
			temDocumentoPrincipal = true
		}
	}

	if temContentTypes && temDocumentoPrincipal {
		return nil
	}
	return errors.NovoErroValidacao("arquivo", mensagemPacoteDocxInvalido)
}
