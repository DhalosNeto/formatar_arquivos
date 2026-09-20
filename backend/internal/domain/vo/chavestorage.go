package vo

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// ChaveStorage é o caminho canônico de um objeto no armazenamento de arquivos.
type ChaveStorage string

// TamanhoMaximoChave é o teto de bytes aceito para uma chave de storage.
const TamanhoMaximoChave = 1024

const (
	prefixoDocumentos = "documentos"
	arquivoPreviewPDF = "preview.pdf"
	separadorChave    = "/"
)

// Nomes de objeto aceitos pelo espaço de chaves. São expressões constantes
// porque FormatoArquivo é um tipo string com constantes.
const (
	arquivoOriginalDocx  = "original." + string(FormatoDocx)
	arquivoOriginalPDF   = "original." + string(FormatoPDF)
	arquivoOriginalLaTeX = "original." + string(FormatoLaTeX)
)

// Separadores de linha Unicode: não são controles nem formatação para o
// unicode, mas quebram linha em log e em arquivo de texto.
const (
	separadorLinhaUnicode     = '\u2028'
	separadorParagrafoUnicode = '\u2029'
)

// Mensagens fixas: nunca ecoam a chave recebida do usuário (CLAUDE.md, regra 7).
const (
	mensagemChaveInvalida      = "chave de storage inválida"
	mensagemChaveVazia         = "a chave não pode ser vazia"
	mensagemChaveLonga         = "a chave excede o tamanho máximo permitido"
	mensagemChaveCodificacao   = "a chave precisa ser texto UTF-8 válido"
	mensagemChaveEspacoBorda   = "a chave não pode começar nem terminar com espaço"
	mensagemChaveControle      = "a chave não pode conter caracteres de controle"
	mensagemChaveContrabarra   = "a chave não pode conter contrabarra"
	mensagemChaveSegmentoVazio = "a chave não pode conter segmento vazio"
	mensagemChaveTravessia     = "a chave não pode conter segmento de travessia de diretório"
	mensagemDocumentoInvalido  = "identificador do documento é obrigatório"
)

// NovaChaveOriginal monta a chave do arquivo enviado pelo usuário.
func NovaChaveOriginal(documentoID uuid.UUID, formato FormatoArquivo) (ChaveStorage, error) {
	invalidos := &errors.ErroValidacao{Mensagem: mensagemChaveInvalida}
	if documentoID == uuid.Nil {
		invalidos.Acrescentar("documento_id", mensagemDocumentoInvalido)
	}
	if !formato.Valido() {
		invalidos.Acrescentar("formato", mensagemFormatoNaoSuportado)
	}
	if invalidos.TemCampos() {
		return "", invalidos
	}

	return montarChave(documentoID, "original."+formato.Extensao()), nil
}

// NovaChavePreviewPDF monta a chave do PDF de pré-visualização do documento.
func NovaChavePreviewPDF(documentoID uuid.UUID) (ChaveStorage, error) {
	if documentoID == uuid.Nil {
		return "", errors.NovoErroValidacaoCampos(
			mensagemChaveInvalida,
			errors.CampoInvalido{Campo: "documento_id", Mensagem: mensagemDocumentoInvalido},
		)
	}
	return montarChave(documentoID, arquivoPreviewPDF), nil
}

func montarChave(documentoID uuid.UUID, arquivo string) ChaveStorage {
	return ChaveStorage(prefixoDocumentos + separadorChave + documentoID.String() + separadorChave + arquivo)
}

// ParaChaveStorage converte um valor externo em chave, exigindo a gramática
// canônica. O motivo exato da recusa é descartado de propósito: devolvê-lo
// daria a quem sondasse a validação um oráculo sobre a regra reprovada.
func ParaChaveStorage(valor string) (ChaveStorage, error) {
	if MotivoChaveInsegura(valor) != "" || !segueGramaticaCanonica(valor) {
		return "", errors.NovoErroValidacao("chave_storage", mensagemChaveInvalida)
	}
	return ChaveStorage(valor), nil
}

// segueGramaticaCanonica exige documentos/<uuid>/<arquivo>, com o UUID escrito
// exatamente como uuid.UUID.String() o emite. uuid.Parse é permissivo e aceita
// maiúsculas, a forma sem hífen, urn:uuid: e chaves; aceitar essas grafias daria
// vários endereços distintos ao mesmo documento, e o storage compara bytes.
func segueGramaticaCanonica(valor string) bool {
	segmentos := strings.Split(valor, separadorChave)
	if len(segmentos) != 3 || segmentos[0] != prefixoDocumentos {
		return false
	}

	documentoID, err := uuid.Parse(segmentos[1])
	if err != nil || documentoID == uuid.Nil || segmentos[1] != documentoID.String() {
		return false
	}

	switch segmentos[2] {
	case arquivoOriginalDocx, arquivoOriginalPDF, arquivoOriginalLaTeX, arquivoPreviewPDF:
		return true
	default:
		return false
	}
}

// MotivoChaveInsegura aplica a regra de segurança do espaço de chaves, sem
// exigir namespace. Devolve vazio quando a chave é aceita ou uma mensagem fixa
// de recusa, que jamais repete trecho do valor recebido.
//
// O valor recebido tem que ser o texto JÁ DECODIFICADO da chave. Nenhum chamador
// pode aplicar url.PathUnescape, normalização Unicode ou qualquer outra
// transformação DEPOIS de validar: validar antes de normalizar deixaria passar
// travessia e caractere de controle escondidos na forma codificada.
//
// Espaço nas bordas do valor é recusado: é invisível em log e em console e
// produz dois objetos confusáveis no storage. Espaço no interior do nome é
// legítimo em S3 e continua aceito.
func MotivoChaveInsegura(valor string) string {
	if strings.TrimSpace(valor) == "" {
		return mensagemChaveVazia
	}
	if len(valor) > TamanhoMaximoChave {
		return mensagemChaveLonga
	}
	if !utf8.ValidString(valor) {
		return mensagemChaveCodificacao
	}
	if strings.TrimSpace(valor) != valor {
		return mensagemChaveEspacoBorda
	}
	if motivo := motivoPorCaractere(valor); motivo != "" {
		return motivo
	}
	return motivoPorEstruturaDeCaminho(valor)
}

// motivoPorCaractere barra o que é invisível em log e em nome de objeto:
// controles C0 e C1 (unicode.Cc), formatação como BOM, zero-width e override de
// direção (unicode.Cf) e os separadores de linha e de parágrafo do Unicode.
// Letra acentuada é categoria L e continua aceita.
func motivoPorCaractere(valor string) string {
	for _, caractere := range valor {
		switch {
		case unicode.IsControl(caractere),
			unicode.Is(unicode.Cf, caractere),
			caractere == separadorLinhaUnicode,
			caractere == separadorParagrafoUnicode:
			return mensagemChaveControle
		case caractere == '\\':
			return mensagemChaveContrabarra
		}
	}
	return ""
}

// motivoPorEstruturaDeCaminho recusa segmento vazio — regra única que cobre
// barra inicial, barra dupla e barra final, esta última criadora de marcador de
// diretório no S3 — e segmento de travessia.
func motivoPorEstruturaDeCaminho(valor string) string {
	for _, segmento := range strings.Split(valor, separadorChave) {
		switch segmento {
		case "":
			return mensagemChaveSegmentoVazio
		case ".", "..":
			return mensagemChaveTravessia
		}
	}
	return ""
}

// String devolve o caminho textual da chave.
func (c ChaveStorage) String() string { return string(c) }

// Vazia informa se a chave não foi preenchida.
func (c ChaveStorage) Vazia() bool { return c == "" }
