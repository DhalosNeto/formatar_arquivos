package vo

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/daniel-halos/formatador/internal/infra/errors"
)

const idFixoDeTeste = "6f1a2b3c-4d5e-4f60-8a1b-2c3d4e5f6071"

func TestNovaChaveOriginal(t *testing.T) {
	t.Parallel()

	idFixo := uuid.MustParse(idFixoDeTeste)

	chave, err := NovaChaveOriginal(idFixo, FormatoDocx)
	if err != nil {
		t.Fatalf("não esperava erro, obteve %v", err)
	}

	esperada := "documentos/" + idFixo.String() + "/original.docx"
	if chave.String() != esperada {
		t.Fatalf("chave = %q, esperava %q", chave.String(), esperada)
	}
	if chave.Vazia() {
		t.Fatal("a chave não deveria ser considerada vazia")
	}
}

func TestNovaChavePreviewPDF(t *testing.T) {
	t.Parallel()

	idFixo := uuid.MustParse(idFixoDeTeste)

	chave, err := NovaChavePreviewPDF(idFixo)
	if err != nil {
		t.Fatalf("não esperava erro, obteve %v", err)
	}

	esperada := "documentos/" + idFixo.String() + "/preview.pdf"
	if chave.String() != esperada {
		t.Fatalf("chave = %q, esperava %q", chave.String(), esperada)
	}
}

func TestConstrutoresDeChaveRejeitamEntradaInvalida(t *testing.T) {
	t.Parallel()

	t.Run("chave original com uuid nulo", func(t *testing.T) {
		t.Parallel()

		_, err := NovaChaveOriginal(uuid.Nil, FormatoDocx)
		exigirErroDeValidacao(t, err)
	})

	t.Run("chave de preview com uuid nulo", func(t *testing.T) {
		t.Parallel()

		_, err := NovaChavePreviewPDF(uuid.Nil)
		exigirErroDeValidacao(t, err)
	})

	t.Run("chave original com formato inválido", func(t *testing.T) {
		t.Parallel()

		_, err := NovaChaveOriginal(uuid.MustParse(idFixoDeTeste), FormatoArquivo("exe"))
		exigirErroDeValidacao(t, err)
	})
}

func TestChaveVaziaDetectaAusencia(t *testing.T) {
	t.Parallel()

	if !ChaveStorage("").Vazia() {
		t.Fatal("esperava que a chave vazia fosse detectada como vazia")
	}
	if ChaveStorage("documentos/x/original.docx").Vazia() {
		t.Fatal("esperava que uma chave preenchida não fosse vazia")
	}
}

func TestParaChaveStorageAceitaSaidaDosConstrutores(t *testing.T) {
	t.Parallel()

	idFixo := uuid.MustParse(idFixoDeTeste)

	original, err := NovaChaveOriginal(idFixo, FormatoDocx)
	if err != nil {
		t.Fatalf("não esperava erro ao montar a chave original, obteve %v", err)
	}
	preview, err := NovaChavePreviewPDF(idFixo)
	if err != nil {
		t.Fatalf("não esperava erro ao montar a chave de preview, obteve %v", err)
	}

	casos := []struct {
		nome  string
		chave ChaveStorage
	}{
		{nome: "round-trip da chave original", chave: original},
		{nome: "round-trip da chave de preview", chave: preview},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			convertida, err := ParaChaveStorage(caso.chave.String())
			if err != nil {
				t.Fatalf("não esperava erro, obteve %v", err)
			}
			if convertida != caso.chave {
				t.Fatalf("round-trip devolveu %q, esperava %q", convertida, caso.chave)
			}
		})
	}
}

func TestParaChaveStorageRejeitaChavesPerigosas(t *testing.T) {
	t.Parallel()

	idFixo := uuid.MustParse(idFixoDeTeste).String()

	casos := []struct {
		nome  string
		valor string
	}{
		{nome: "chave vazia", valor: ""},
		{nome: "travessia de diretório absoluta", valor: "../../etc/passwd"},
		{nome: "travessia de diretório embutida", valor: "documentos/../x/original.docx"},
		{nome: "barra inicial", valor: "/documentos/" + idFixo + "/original.docx"},
		{nome: "barra dupla", valor: "documentos//original.docx"},
		{nome: "separador do windows", valor: "documentos\\" + idFixo + "\\original.docx"},
		{nome: "byte nulo no fim", valor: "documentos/" + idFixo + "/original.docx\x00"},
		{nome: "prefixo de outro bucket", valor: "outrobucket/" + idFixo + "/original.docx"},
		{nome: "identificador que não é uuid", valor: "documentos/nao-e-uuid/original.docx"},
		{nome: "extensão fora do conjunto canônico", valor: "documentos/" + idFixo + "/original.exe"},
		{nome: "nome de arquivo fora do conjunto canônico", valor: "documentos/" + idFixo + "/relatorio.docx"},
		{nome: "só espaços", valor: "   "},
		{nome: "caractere de controle DEL", valor: "documentos/a\x7f.docx"},
		{nome: "quebra de linha embutida", valor: "doc\numento.docx"},
		{nome: "UTF-8 inválido", valor: "documentos/" + string([]byte{0xff, 0xfe}) + ".docx"},
		{nome: "segmento final de travessia", valor: "documentos/.."},
		{nome: "apenas dois pontos", valor: ".."},
		{nome: "um byte acima do tamanho máximo", valor: chaveComTamanho(TamanhoMaximoChave + 1)},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			_, err := ParaChaveStorage(caso.valor)
			if err == nil {
				t.Fatal("esperava erro de validação para chave perigosa")
			}

			var invalido *errors.ErroValidacao
			if !errors.Como(err, &invalido) {
				t.Fatalf("esperava *ErroValidacao, obteve %T", err)
			}
			exigirCampo(t, invalido, "chave_storage")
			if strings.Contains(err.Error(), "passwd") {
				t.Fatal("a mensagem de erro não pode repetir o caminho enviado pelo usuário")
			}
		})
	}
}

func exigirErroDeValidacao(t *testing.T, err error) {
	t.Helper()

	if err == nil {
		t.Fatal("esperava erro de validação")
	}

	var invalido *errors.ErroValidacao
	if !errors.Como(err, &invalido) {
		t.Fatalf("esperava *ErroValidacao, obteve %T", err)
	}
	if !invalido.TemCampos() {
		t.Fatal("esperava ao menos um campo reprovado")
	}
}

// chaveComTamanho monta um valor com exatamente o número de bytes pedido,
// usando apenas caracteres que a regra de segurança aceita.
func chaveComTamanho(bytes int) string {
	const prefixo = "documentos/"
	return prefixo + strings.Repeat("a", bytes-len(prefixo))
}

func TestMotivoChaveInseguraAceitaChavesLegitimas(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome  string
		valor string
	}{
		{nome: "chave canônica", valor: "documentos/" + idFixoDeTeste + "/original.docx"},
		{nome: "acento é legítimo e não pode ser rejeitado com o lixo", valor: "documentos/ação-2026.docx"},
		{nome: "dois pontos no meio do segmento não é travessia", valor: "documentos/a..b/c.docx"},
		{nome: "dois pontos como prefixo de nome não é travessia", valor: "..oculto/a.docx"},
		{nome: "chave com exatamente o tamanho máximo", valor: chaveComTamanho(TamanhoMaximoChave)},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			if len(caso.valor) > TamanhoMaximoChave {
				t.Fatalf("pré-condição: o caso tem %d bytes, acima do teto", len(caso.valor))
			}
			if motivo := MotivoChaveInsegura(caso.valor); motivo != "" {
				t.Fatalf("esperava chave aceita, obteve motivo %q", motivo)
			}
		})
	}
}

func TestMotivoChaveInseguraRejeitaChavesPerigosas(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome  string
		valor string
	}{
		{nome: "chave vazia", valor: ""},
		{nome: "chave só com espaços", valor: "   "},
		{nome: "barra inicial", valor: "/documentos/a.docx"},
		{nome: "travessia para fora da raiz", valor: "../etc/passwd"},
		{nome: "travessia no meio do caminho", valor: "documentos/../../etc/passwd"},
		{nome: "segmento final de travessia", valor: "documentos/.."},
		{nome: "apenas dois pontos", valor: ".."},
		{nome: "barra dupla", valor: "documentos//a.docx"},
		{nome: "contrabarra do Windows", valor: `documentos\a.docx`},
		{nome: "byte nulo embutido", valor: "doc\x00umento.docx"},
		{nome: "quebra de linha embutida", valor: "doc\numento.docx"},
		{nome: "caractere de controle DEL", valor: "documentos/a\x7f.docx"},
		{nome: "UTF-8 inválido", valor: "documentos/" + string([]byte{0xff, 0xfe}) + ".docx"},
		{nome: "um byte acima do tamanho máximo", valor: chaveComTamanho(TamanhoMaximoChave + 1)},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			motivo := MotivoChaveInsegura(caso.valor)
			if motivo == "" {
				t.Fatal("esperava um motivo de recusa, obteve string vazia")
			}
			// O motivo vira mensagem de erro: não pode carregar trecho da chave do usuário.
			for _, vazamento := range []string{"passwd", "umento", "ocumentos/a"} {
				if strings.Contains(motivo, vazamento) {
					t.Fatalf("o motivo não pode repetir trecho da chave recebida (%q)", vazamento)
				}
			}
		})
	}
}

func TestTamanhoMaximoChaveTemOValorDoContrato(t *testing.T) {
	t.Parallel()

	if TamanhoMaximoChave != 1024 {
		t.Fatalf("esperava TamanhoMaximoChave 1024, obteve %d", TamanhoMaximoChave)
	}
}
