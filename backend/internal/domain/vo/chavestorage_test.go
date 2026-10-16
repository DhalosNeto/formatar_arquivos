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

// ---------------------------------------------------------------------------
// A1 e A2 (C1) — NovaChaveFormatado e o alargamento de segueGramaticaCanonica.
//
// RED por COMPILAÇÃO enquanto NovaChaveFormatado não existe.
// ---------------------------------------------------------------------------

// rulesetFixoDeTeste e outroRulesetFixoDeTeste são dois perfis DISTINTOS: a
// colisão que A1 mata é entre perfis, não entre documentos.
const (
	rulesetFixoDeTeste      = "9b7c1d2e-3f40-4a51-8b62-7c8d9e0f1a2b"
	outroRulesetFixoDeTeste = "1c2d3e4f-5061-4728-9a3b-4c5d6e7f8091"
)

func TestNovaChaveFormatadoEhDeterministicaEDistinguePerfil(t *testing.T) {
	t.Parallel()

	documento := uuid.MustParse(idFixoDeTeste)
	perfil := uuid.MustParse(rulesetFixoDeTeste)
	outroPerfil := uuid.MustParse(outroRulesetFixoDeTeste)

	chave, err := NovaChaveFormatado(documento, perfil)
	if err != nil {
		t.Fatalf("não esperava erro, obteve %v", err)
	}

	esperada := "documentos/" + documento.String() + "/formatado-" + perfil.String() + ".docx"
	if chave.String() != esperada {
		t.Fatalf("chave = %q, esperava %q", chave.String(), esperada)
	}

	// MESMO par -> MESMA chave: é o que faz retry e reexecução com o mesmo
	// perfil sobrescreverem o próprio objeto em vez de acumular lixo no bucket.
	repetida, err := NovaChaveFormatado(documento, perfil)
	if err != nil {
		t.Fatalf("não esperava erro na repetição, obteve %v", err)
	}
	if repetida != chave {
		t.Fatalf("o mesmo par devolveu %q e %q; o construtor precisa ser determinístico", chave, repetida)
	}

	// Perfil DIFERENTE -> chave DIFERENTE: sem isto o job do perfil Y
	// sobrescreveria os bytes do perfil X sob o mesmo endereço.
	deOutroPerfil, err := NovaChaveFormatado(documento, outroPerfil)
	if err != nil {
		t.Fatalf("não esperava erro para o segundo perfil, obteve %v", err)
	}
	if deOutroPerfil == chave {
		t.Fatalf("dois rulesets distintos produziram a mesma chave %q", chave)
	}

	// E distinta das outras famílias de objeto do mesmo documento.
	original, err := NovaChaveOriginal(documento, FormatoDocx)
	if err != nil {
		t.Fatalf("não esperava erro ao montar a chave original, obteve %v", err)
	}
	preview, err := NovaChavePreviewPDF(documento)
	if err != nil {
		t.Fatalf("não esperava erro ao montar a chave de preview, obteve %v", err)
	}
	if chave == original || chave == preview {
		t.Fatalf("a chave do formatado (%q) colide com original (%q) ou preview (%q)", chave, original, preview)
	}
}

func TestNovaChaveFormatadoRejeitaIdentificadorNulo(t *testing.T) {
	t.Parallel()

	// Um termo por caso: nulo no documento e nulo no ruleset são guardas
	// diferentes, e uma decisão composta exige caso por termo (regra 12).
	casos := []struct {
		nome                  string
		documento, rulesetUID uuid.UUID
	}{
		{nome: "documento nulo", documento: uuid.Nil, rulesetUID: uuid.MustParse(rulesetFixoDeTeste)},
		{nome: "ruleset nulo", documento: uuid.MustParse(idFixoDeTeste), rulesetUID: uuid.Nil},
		{nome: "os dois nulos", documento: uuid.Nil, rulesetUID: uuid.Nil},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			chave, err := NovaChaveFormatado(caso.documento, caso.rulesetUID)
			exigirErroDeValidacao(t, err)
			if !chave.Vazia() {
				t.Fatalf("recusa não pode devolver chave utilizável, obteve %q", chave)
			}
		})
	}
}

func TestParaChaveStorageAceitaOFormatadoERecusaNearMisses(t *testing.T) {
	t.Parallel()

	documento := uuid.MustParse(idFixoDeTeste)
	perfil := uuid.MustParse(rulesetFixoDeTeste)
	chave, err := NovaChaveFormatado(documento, perfil)
	if err != nil {
		t.Fatalf("não esperava erro ao montar a chave formatada, obteve %v", err)
	}

	// Round-trip: sem isto o artefato é ilegível para quem relê a chave do
	// banco (data/postgres/documento.go:222).
	convertida, err := ParaChaveStorage(chave.String())
	if err != nil {
		t.Fatalf("round-trip do formatado recusado: %v", err)
	}
	if convertida != chave {
		t.Fatalf("round-trip devolveu %q, esperava %q", convertida, chave)
	}

	prefixoDoc := "documentos/" + documento.String() + "/"
	semHifen := strings.ReplaceAll(perfil.String(), "-", "")

	// Alargar segueGramaticaCanonica é afrouxar um validador de espaço de
	// chaves: provar que ele segue ESTRITO importa tanto quanto provar que a
	// forma nova passa.
	casos := []struct {
		nome  string
		valor string
	}{
		{nome: "ruleset em maiúsculas", valor: prefixoDoc + "formatado-" + strings.ToUpper(perfil.String()) + ".docx"},
		{nome: "ruleset sem hífen", valor: prefixoDoc + "formatado-" + semHifen + ".docx"},
		{nome: "ruleset em urn:uuid:", valor: prefixoDoc + "formatado-urn:uuid:" + perfil.String() + ".docx"},
		{nome: "ruleset entre chaves", valor: prefixoDoc + "formatado-{" + perfil.String() + "}.docx"},
		// uuid.Nil: endereço LEGÍVEL que nenhum construtor produz. Sem este
		// caso, a assimetria construtor/parser volta.
		{nome: "ruleset nulo", valor: prefixoDoc + "formatado-" + uuid.Nil.String() + ".docx"},
		{nome: "ruleset ausente", valor: prefixoDoc + "formatado-.docx"},
		{nome: "sem o separador do prefixo", valor: prefixoDoc + "formatado" + perfil.String() + ".docx"},
		{nome: "prefixo no feminino", valor: prefixoDoc + "formatada-" + perfil.String() + ".docx"},
		{nome: "prefixo com sublinhado", valor: prefixoDoc + "formatado_" + perfil.String() + ".docx"},
		{nome: "extensão pdf", valor: prefixoDoc + "formatado-" + perfil.String() + ".pdf"},
		{nome: "extensão tex", valor: prefixoDoc + "formatado-" + perfil.String() + ".tex"},
		{nome: "sem extensão", valor: prefixoDoc + "formatado-" + perfil.String()},
		{nome: "quatro segmentos com o formatado no fim", valor: prefixoDoc + "v2/formatado-" + perfil.String() + ".docx"},
		{nome: "quatro segmentos com o formatado no meio", valor: prefixoDoc + "formatado-" + perfil.String() + "/saida.docx"},
		{nome: "travessia no lugar do ruleset", valor: prefixoDoc + "../formatado-" + perfil.String() + ".docx"},
		{nome: "travessia no lugar do documento", valor: "documentos/../formatado-" + perfil.String() + ".docx"},
		{nome: "documento nulo com formatado válido", valor: "documentos/" + uuid.Nil.String() + "/formatado-" + perfil.String() + ".docx"},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			obtida, err := ParaChaveStorage(caso.valor)
			if err == nil {
				t.Fatalf("esperava recusa para %q", caso.nome)
			}
			var invalido *errors.ErroValidacao
			if !errors.Como(err, &invalido) {
				t.Fatalf("esperava *ErroValidacao, obteve %T", err)
			}
			exigirCampo(t, invalido, "chave_storage")
			if !obtida.Vazia() {
				t.Fatalf("recusa não pode devolver chave utilizável, obteve %q", obtida)
			}
		})
	}
}
