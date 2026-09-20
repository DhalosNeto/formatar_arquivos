package vo

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/daniel-halos/formatador/internal/infra/errors"
)

const idDaAuditoria = "3f2504e0-4f89-41d3-9a0c-0305e82c3301"

// TestParaChaveStorageExigeUUIDCanonico protege o espaço de chaves contra
// aliasing: uuid.Parse aceita maiúsculas, urn:uuid:, chaves e a forma sem
// hífen, mas montarChave só emite o texto de uuid.UUID.String(), que é
// minúsculo e com hífen. Se ParaChaveStorage aceitar as demais grafias,
// o mesmo documento passa a ter várias chaves válidas e distintas — e o
// storage de objetos compara bytes, não semântica de UUID.
func TestParaChaveStorageExigeUUIDCanonico(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome  string
		valor string
	}{
		{nome: "uuid em maiúsculas", valor: "documentos/3F2504E0-4F89-41D3-9A0C-0305E82C3301/original.docx"},
		{nome: "uuid em forma urn", valor: "documentos/urn:uuid:" + idDaAuditoria + "/original.docx"},
		{nome: "uuid entre chaves", valor: "documentos/{" + idDaAuditoria + "}/original.docx"},
		{nome: "uuid sem hífens", valor: "documentos/3f2504e04f8941d39a0c0305e82c3301/original.docx"},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			_, err := ParaChaveStorage(caso.valor)
			if err == nil {
				t.Fatalf("ParaChaveStorage(%q) aceitou grafia não canônica de UUID; "+
					"a chave resultante nunca é igual à emitida por NovaChaveOriginal", caso.valor)
			}
			var invalido *errors.ErroValidacao
			if !errors.Como(err, &invalido) {
				t.Fatalf("esperava *ErroValidacao, obteve %T", err)
			}
		})
	}
}

// TestChaveAceitaTemQueSerIdempotente verifica a propriedade que sustenta o
// espaço de chaves: toda chave aceita por ParaChaveStorage tem que ser a mesma
// que os construtores emitiriam para aquele documento.
func TestChaveAceitaTemQueSerIdempotente(t *testing.T) {
	t.Parallel()

	valores := []string{
		"documentos/" + idDaAuditoria + "/original.docx",
		"documentos/3F2504E0-4F89-41D3-9A0C-0305E82C3301/original.docx",
		"documentos/urn:uuid:" + idDaAuditoria + "/original.docx",
		"documentos/3f2504e04f8941d39a0c0305e82c3301/original.docx",
	}

	canonica, err := NovaChaveOriginal(uuid.MustParse(idDaAuditoria), FormatoDocx)
	if err != nil {
		t.Fatalf("não esperava erro ao montar a chave canônica, obteve %v", err)
	}

	for _, valor := range valores {
		chave, err := ParaChaveStorage(valor)
		if err != nil {
			continue // recusada: não há aliasing.
		}
		if chave != canonica {
			t.Fatalf("ParaChaveStorage(%q) = %q, mas a chave canônica do mesmo documento é %q: "+
				"o mesmo documento passou a ter duas chaves válidas e diferentes", valor, chave, canonica)
		}
	}
}

// TestMotivoChaveInseguraRejeitaControlesUnicode cobre os controles fora do
// ASCII. motivoPorCaractere só barra C0 (< 0x20) e DEL (0x7F); os controles C1
// (U+0080–U+009F), o separador de linha U+2028, o de parágrafo U+2029 e o
// zero-width U+FEFF passam. São caracteres invisíveis que viajam em cabeçalho
// HTTP, em log e em nome de objeto do storage.
func TestMotivoChaveInseguraRejeitaControlesUnicode(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome  string
		valor string
	}{
		{nome: "NEL U+0085", valor: "documentos/a\u0085b.docx"},
		{nome: "CSI U+009B", valor: "documentos/a\u009bb.docx"},
		{nome: "separador de linha U+2028", valor: "documentos/a\u2028b.docx"},
		{nome: "separador de parágrafo U+2029", valor: "documentos/a\u2029b.docx"},
		{nome: "zero-width no-break space U+FEFF", valor: "documentos/a\ufeffb.docx"},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			if motivo := MotivoChaveInsegura(caso.valor); motivo == "" {
				t.Fatalf("MotivoChaveInsegura aceitou caractere de controle invisível em %q", caso.nome)
			}
		})
	}
}

// TestMotivoChaveInseguraAceitaEspacoNoInterior fixa a distinção: espaço no
// INTERIOR do valor é legítimo em chave S3 e tem que ser aceito. Espaço nas
// BORDAS é recusado (ver TestMotivoChaveInseguraRejeitaEspacoNasBordas) porque
// é invisível em log e em console. Não "conserte" este teste aceitando bordas.
func TestMotivoChaveInseguraAceitaEspacoNoInterior(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome  string
		valor string
	}{
		{nome: "espaço no meio do nome", valor: "documentos/meu arquivo.docx"},
		{nome: "espaço no início do nome do arquivo", valor: "documentos/ arquivo.docx"},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			if motivo := MotivoChaveInsegura(caso.valor); motivo != "" {
				t.Fatalf("MotivoChaveInsegura(%q) = %q, esperava aceite", caso.valor, motivo)
			}
		})
	}
}

// TestMotivoChaveInseguraRejeitaEspacoNasBordas exige a recusa de espaço no
// começo ou no fim do valor inteiro (strings.TrimSpace(valor) != valor). Chave
// com borda em branco é invisível em log e em console e cria dois objetos
// confusáveis no storage — a mesma classe de problema do aliasing de UUID.
func TestMotivoChaveInseguraRejeitaEspacoNasBordas(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome  string
		valor string
	}{
		{nome: "espaço no fim do nome", valor: "documentos/arquivo.docx "},
		{nome: "dois espaços à esquerda", valor: "  documentos/a.docx"},
		{nome: "dois espaços à direita", valor: "documentos/a.docx  "},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			if motivo := MotivoChaveInsegura(caso.valor); motivo == "" {
				t.Fatalf("MotivoChaveInsegura(%q) aceitou espaço na borda do valor", caso.valor)
			}
		})
	}
}

// TestParaChaveStorageAceitaDemaisArquivosCanonicos exercita os braços do
// switch de segueGramaticaCanonica que o round-trip dos construtores não cobre.
func TestParaChaveStorageAceitaDemaisArquivosCanonicos(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome  string
		valor string
	}{
		{nome: "original em pdf", valor: "documentos/" + idDaAuditoria + "/original.pdf"},
		{nome: "original em latex", valor: "documentos/" + idDaAuditoria + "/original.tex"},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			chave, err := ParaChaveStorage(caso.valor)
			if err != nil {
				t.Fatalf("ParaChaveStorage(%q) recusou chave canônica: %v", caso.valor, err)
			}
			if chave.String() != caso.valor {
				t.Fatalf("ParaChaveStorage(%q) = %q, esperava o mesmo valor", caso.valor, chave.String())
			}
		})
	}
}

// TestDetectarFormatoNoLimiteDoPrefixo exercita a fronteira exata de
// TamanhoPrefixoDeteccao: assinatura seguida de lixo binário e prefixo maior
// que o mínimo. A decisão é pela assinatura, nunca pelo resto do conteúdo.
func TestDetectarFormatoNoLimiteDoPrefixo(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome     string
		prefixo  []byte
		esperado FormatoArquivo
	}{
		{
			nome:     "pdf com exatamente o tamanho mínimo e lixo depois da assinatura",
			prefixo:  []byte{'%', 'P', 'D', 'F', 0xDE, 0xAD, 0xBE, 0xEF},
			esperado: FormatoPDF,
		},
		{
			nome:     "pdf com um byte a mais que o mínimo",
			prefixo:  []byte{'%', 'P', 'D', 'F', 0x00, 0x00, 0x00, 0x00, 0x01},
			esperado: FormatoPDF,
		},
		{
			nome:     "docx com prefixo bem maior que o mínimo",
			prefixo:  append([]byte{0x50, 0x4B, 0x03, 0x04}, make([]byte, 4096)...),
			esperado: FormatoDocx,
		},
		{
			nome:     "assinatura pdf com um byte a menos que o mínimo é recusada",
			prefixo:  []byte{'%', 'P', 'D', 'F', 0x00, 0x00, 0x00},
			esperado: "",
		},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			formato, err := DetectarFormato(caso.prefixo)
			if caso.esperado == "" {
				if err == nil {
					t.Fatalf("esperava erro, obteve formato %q", formato)
				}
				var invalido *errors.ErroValidacao
				if !errors.Como(err, &invalido) {
					t.Fatalf("esperava *ErroValidacao, obteve %T", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("não esperava erro, obteve %v", err)
			}
			if formato != caso.esperado {
				t.Fatalf("DetectarFormato() = %q, esperava %q", formato, caso.esperado)
			}
		})
	}
}

// TestConferirPacoteDocxComListaSuja garante que duplicata e entrada vazia na
// lista de partes do ZIP não confundem a conferência.
func TestConferirPacoteDocxComListaSuja(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome    string
		partes  []string
		temErro bool
	}{
		{
			nome:    "partes obrigatórias duplicadas",
			partes:  []string{ParteContentTypes, ParteContentTypes, ParteDocumentoPrincipal, ParteDocumentoPrincipal},
			temErro: false,
		},
		{
			nome:    "entradas vazias no meio da lista",
			partes:  []string{"", ParteContentTypes, "", ParteDocumentoPrincipal, ""},
			temErro: false,
		},
		{
			nome:    "lista só de entradas vazias",
			partes:  []string{"", "", ""},
			temErro: true,
		},
		{
			nome:    "documento principal com prefixo de diretório errado",
			partes:  []string{ParteContentTypes, "xl/word/document.xml"},
			temErro: true,
		},
		{
			nome:    "partes com caixa alta não valem",
			partes:  []string{"[CONTENT_TYPES].XML", "WORD/DOCUMENT.XML"},
			temErro: true,
		},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			err := ConferirPacoteDocx(caso.partes)
			if caso.temErro {
				if err == nil {
					t.Fatalf("esperava erro para %v", caso.partes)
				}
				var invalido *errors.ErroValidacao
				if !errors.Como(err, &invalido) {
					t.Fatalf("esperava *ErroValidacao, obteve %T", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("não esperava erro, obteve %v", err)
			}
		})
	}
}

// TestMensagensDeErroNaoEcoamEntrada é a verificação executável da regra 7 do
// CLAUDE.md: nenhuma mensagem pode repetir trecho do valor recebido.
func TestMensagensDeErroNaoEcoamEntrada(t *testing.T) {
	t.Parallel()

	const marcador = "SEGREDO-DO-USUARIO-9137"

	entradas := []struct {
		nome string
		agir func() error
	}{
		{
			nome: "ParaChaveStorage com valor perigoso",
			agir: func() error {
				_, err := ParaChaveStorage("../" + marcador + "/../etc/passwd")
				return err
			},
		},
		{
			nome: "ParaChaveStorage com prefixo alheio",
			agir: func() error {
				_, err := ParaChaveStorage(marcador + "/" + idDaAuditoria + "/original.docx")
				return err
			},
		},
		{
			nome: "FormatoPorMIME com mime desconhecido",
			agir: func() error {
				_, err := FormatoPorMIME("application/" + marcador)
				return err
			},
		},
		{
			nome: "DetectarFormato com conteúdo não suportado",
			agir: func() error {
				_, err := DetectarFormato([]byte(marcador + marcador))
				return err
			},
		},
		{
			nome: "DetectarFormato com prefixo insuficiente",
			agir: func() error {
				_, err := DetectarFormato([]byte("ABC"))
				return err
			},
		},
		{
			nome: "ConferirPacoteDocx com pacote alheio",
			agir: func() error {
				return ConferirPacoteDocx([]string{marcador + ".xml", "mimetype"})
			},
		},
		{
			nome: "NovaChaveOriginal com formato inválido",
			agir: func() error {
				_, err := NovaChaveOriginal(uuid.MustParse(idDaAuditoria), FormatoArquivo(marcador))
				return err
			},
		},
	}

	for _, entrada := range entradas {
		t.Run(entrada.nome, func(t *testing.T) {
			t.Parallel()

			err := entrada.agir()
			if err == nil {
				t.Fatalf("esperava erro para montar a mensagem")
			}
			if strings.Contains(err.Error(), marcador) {
				t.Fatalf("mensagem de erro ecoou entrada do usuário: %q", err.Error())
			}
		})
	}
}
