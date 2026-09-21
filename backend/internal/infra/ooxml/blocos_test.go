// Este arquivo testa a extração de blocos de word/document.xml e a
// classificação pela camada 1 (estilos nomeados do DOCX: Title, HeadingN,
// Normal) — o primeiro corte do CDM descrito em docs/plano-backend.md, F2.
//
// Decisão de desenho travada aqui: extrair para LER não pode virar extrair
// para ESCREVER. ExtrairBlocos parseia word/document.xml num caminho
// paralelo, somente leitura — nunca toca d.arquivos, que é o que Salvar
// recopia. TestExtrairBlocosNaoAlteraOPacote é a rede que garante que essa
// separação não regride: se a extração contaminar o caminho de escrita, esse
// teste quebra antes de qualquer outro.
//
// RefXML/Indice é o índice ORDINAL do bloco entre os filhos diretos de
// w:body (w:p e w:tbl), não um id injetado no XML: injetar w14:paraId ou
// atributo próprio mutaria o documento do usuário, o que o ADR 0001 proíbe.
//
// Reusa os helpers de pacote_test.go (mesmo pacote): lerFixture,
// docxSintetico, documentoXMLMinimo, abrirSemPanic.
package ooxml

import (
	"bytes"
	"crypto/sha256"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-halos/formatador/internal/domain/cdm"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// extrairBlocosDe monta um pacote docx sintético com o document.xml
// informado, abre e extrai os blocos. Falha o teste (via require) se
// qualquer passo falhar — é a pré-condição dos casos que exercitam o
// conteúdo dos blocos, não o que eles verificam.
func extrairBlocosDe(t *testing.T, documentoXML []byte) []BlocoBruto {
	t.Helper()

	dados := docxSintetico(t, documentoXML)
	doc, err := Abrir(bytes.NewReader(dados), int64(len(dados)))
	require.NoError(t, err)
	require.NotNil(t, doc)

	blocos, err := doc.ExtrairBlocos()
	require.NoError(t, err)
	return blocos
}

// ---------------------------------------------------------------------------
// ExtrairBlocos contra o artigo real — contagem, ordem, texto e w:tbl
// ---------------------------------------------------------------------------

func TestExtrairBlocosFixtureReal(t *testing.T) {
	t.Parallel()

	dados := lerFixture(t, "artigo-real-libreoffice.docx")
	doc, err := Abrir(bytes.NewReader(dados), int64(len(dados)))
	require.NoError(t, err)
	require.NotNil(t, doc)

	blocos, err := doc.ExtrairBlocos()
	require.NoError(t, err)

	// O fixture tem 57 <w:p> no XML inteiro, mas 18 deles vivem DENTRO da
	// célula da única tabela do documento. Blocos de nível superior são só os
	// filhos diretos de w:body: 39 parágrafos + 1 tabela = 40. Explodir a
	// tabela em parágrafos de célula individuais perderia a tabela como
	// unidade e é exatamente o que este teste proíbe.
	require.Len(t, blocos, 40,
		"39 parágrafos de nível superior + 1 tabela; parágrafos dentro de célula não contam como blocos próprios")

	casos := []struct {
		indice int
		tipo   TipoBlocoBruto
		estilo string
		texto  string
	}{
		{indice: 0, tipo: TipoBlocoBrutoParagrafo, estilo: "Title",
			texto: "A PERCEPÇÃO DE ESTUDANTES DE GRADUAÇÃO SOBRE A FORMATAÇÃO DE TRABALHOS ACADÊMICOS: UM ESTUDO EXPLORATÓRIO"},
		{indice: 6, tipo: TipoBlocoBrutoParagrafo, estilo: "Heading1", texto: "RESUMO"},
		{indice: 9, tipo: TipoBlocoBrutoParagrafo, estilo: "Heading1", texto: "ABSTRACT"},
		{indice: 12, tipo: TipoBlocoBrutoParagrafo, estilo: "Heading1", texto: "1 INTRODUÇÃO"},
		{indice: 16, tipo: TipoBlocoBrutoParagrafo, estilo: "Heading1", texto: "2 METODOLOGIA"},
		{indice: 18, tipo: TipoBlocoBrutoParagrafo, estilo: "Heading2", texto: "2.1 Instrumento de coleta"},
		{indice: 20, tipo: TipoBlocoBrutoParagrafo, estilo: "Heading2", texto: "2.2 Participantes"},
		{indice: 23, tipo: TipoBlocoBrutoTabela, estilo: "",
			texto: "Árean%Ciências Humanas5429,7Ciências Exatas4725,8Ciências da Saúde3921,4Ciências Sociais Aplicadas4223,1Total182100,0"},
		{indice: 25, tipo: TipoBlocoBrutoParagrafo, estilo: "Heading1", texto: "3 RESULTADOS E DISCUSSÃO"},
		{indice: 32, tipo: TipoBlocoBrutoParagrafo, estilo: "Heading1", texto: "4 CONSIDERAÇÕES FINAIS"},
		{indice: 35, tipo: TipoBlocoBrutoParagrafo, estilo: "Heading1", texto: "REFERÊNCIAS"},
		{indice: 39, tipo: TipoBlocoBrutoParagrafo, estilo: "Normal",
			texto: "MARCONI, Marina de Andrade; LAKATOS, Eva Maria. Fundamentos de metodologia científica. 8. ed. São Paulo: Atlas, 2017."},
	}

	for _, caso := range casos {
		caso := caso
		t.Run(fmt.Sprintf("bloco de índice %d", caso.indice), func(t *testing.T) {
			t.Parallel()

			require.Less(t, caso.indice, len(blocos))
			bloco := blocos[caso.indice]
			assert.Equal(t, caso.tipo, bloco.Tipo)
			assert.Equal(t, caso.estilo, bloco.EstiloNomeado)
			assert.Equal(t, caso.texto, bloco.Texto,
				"o texto do bloco concatena todos os w:t dele, na ordem, sem separador")
			assert.Equal(t, caso.indice, bloco.Indice,
				"Indice (que vira RefXML no CDM) precisa ser a posição ordinal real, não um contador paralelo")
		})
	}

	// A ordem do slice devolvido É a ordem do documento: cada bloco está na
	// posição igual ao seu próprio Indice.
	for i, bloco := range blocos {
		assert.Equal(t, i, bloco.Indice, "posição %d do slice tem Indice %d: a ordem tem que ser a do documento", i, bloco.Indice)
	}
}

// ---------------------------------------------------------------------------
// Parágrafo vazio, tabela isolada, xml:space e caractere fora do BMP —
// casos sintéticos, mínimos e determinísticos
// ---------------------------------------------------------------------------

func TestExtrairBlocosParagrafoVazioNaoEhDescartado(t *testing.T) {
	t.Parallel()

	documentoXML := []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>primeiro</w:t></w:r></w:p><w:p/><w:p><w:r><w:t>terceiro</w:t></w:r></w:p></w:body></w:document>`)

	blocos := extrairBlocosDe(t, documentoXML)

	require.Len(t, blocos, 3, "descartar o parágrafo vazio do meio desalinharia o Indice do terceiro parágrafo")
	assert.Equal(t, "primeiro", blocos[0].Texto)

	assert.Equal(t, TipoBlocoBrutoParagrafo, blocos[1].Tipo)
	assert.Equal(t, "", blocos[1].Texto, "parágrafo vazio existe em documento real e precisa virar bloco com texto vazio")
	assert.Equal(t, 1, blocos[1].Indice)

	assert.Equal(t, "terceiro", blocos[2].Texto)
	assert.Equal(t, 2, blocos[2].Indice, "descartar o vazio teria deixado este bloco no índice 1, não 2")
}

func TestExtrairBlocosTabelaViraBlocoProprioNaoSomeNemViraParagrafo(t *testing.T) {
	t.Parallel()

	documentoXML := []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>antes</w:t></w:r></w:p><w:tbl><w:tr><w:tc><w:p><w:r><w:t>celula</w:t></w:r></w:p></w:tc></w:tr></w:tbl><w:p><w:r><w:t>depois</w:t></w:r></w:p></w:body></w:document>`)

	blocos := extrairBlocosDe(t, documentoXML)

	require.Len(t, blocos, 3, "a tabela é UM bloco: não some (contagem 2) nem explode nos parágrafos de célula")
	assert.Equal(t, TipoBlocoBrutoParagrafo, blocos[0].Tipo)

	assert.Equal(t, TipoBlocoBrutoTabela, blocos[1].Tipo, "w:tbl precisa virar TipoBlocoBrutoTabela, não TipoBlocoBrutoParagrafo")
	assert.Equal(t, "celula", blocos[1].Texto, "o texto das células da tabela precisa aparecer no bloco, não sumir")
	assert.Equal(t, 1, blocos[1].Indice)

	assert.Equal(t, TipoBlocoBrutoParagrafo, blocos[2].Tipo)
	assert.Equal(t, "depois", blocos[2].Texto)
	assert.Equal(t, 2, blocos[2].Indice, "o parágrafo depois da tabela continua no índice seguinte ao dela, não pulado nem duplicado")
}

func TestExtrairBlocosRespeitaXMLSpacePreserve(t *testing.T) {
	t.Parallel()

	documentoXML := []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t xml:space="preserve">  espaço nas duas pontas  </w:t></w:r></w:p></w:body></w:document>`)

	blocos := extrairBlocosDe(t, documentoXML)

	require.Len(t, blocos, 1)
	assert.Equal(t, "  espaço nas duas pontas  ", blocos[0].Texto,
		"perder xml:space=\"preserve\" na extração come o espaço do texto do usuário, igual perderia na escrita")
}

func TestExtrairBlocosPreservaCaractereForaDoBMP(t *testing.T) {
	t.Parallel()

	// Mesmo caso de pacote_test.go: 😀 é U+1F600, fora do BMP; 你好世界 é CJK.
	// Nenhum dos dois é contagem de rune = contagem de byte = contagem de
	// unidade UTF-16 (o modelo interno do Word).
	textoOriginal := "emoji 😀 e CJK 你好世界"
	var escapado bytes.Buffer
	require.NoError(t, xml.EscapeText(&escapado, []byte(textoOriginal)))
	documentoXML := documentoXMLMinimo(escapado.String())

	blocos := extrairBlocosDe(t, documentoXML)

	require.Len(t, blocos, 1)
	assert.Equal(t, []rune(textoOriginal), []rune(blocos[0].Texto),
		"caractere fora do BMP sobrevive à extração: comparação em runas, não em bytes nem em unidades UTF-16")
}

// ---------------------------------------------------------------------------
// A decisão de desenho central: extrair para ler não pode virar extrair
// para escrever. Round-trip continua byte a byte idêntico depois de extrair.
// ---------------------------------------------------------------------------

func TestExtrairBlocosNaoAlteraOPacote(t *testing.T) {
	t.Parallel()

	for _, fixture := range []string{"artigo-real-libreoffice.docx", "artigo-desformatado.docx"} {
		fixture := fixture
		t.Run(fixture, func(t *testing.T) {
			t.Parallel()

			original := lerFixture(t, fixture)
			doc, err := Abrir(bytes.NewReader(original), int64(len(original)))
			require.NoError(t, err)
			require.NotNil(t, doc)

			_, err = doc.ExtrairBlocos()
			require.NoError(t, err)

			var saida bytes.Buffer
			require.NoError(t, doc.Salvar(&saida))

			shaOriginal := sha256.Sum256(original)
			shaSaida := sha256.Sum256(saida.Bytes())
			assert.Equal(t, shaOriginal, shaSaida,
				"extrair blocos não pode contaminar o caminho de escrita: Abrir -> extrair -> Salvar precisa produzir o mesmo ZIP byte a byte")
		})
	}
}

// TestExtrairBlocosNaoResolveEntidadeExternaXXE: ExtrairBlocos é a primeira
// vez que este pacote decodifica o CONTEÚDO de word/document.xml (Abrir e
// Salvar nunca desserializam). É superfície nova de XXE e precisa da mesma
// garantia que TestAbrirNaoResolveEntidadeExternaXXE já trava para o pacote.
func TestExtrairBlocosNaoResolveEntidadeExternaXXE(t *testing.T) {
	t.Parallel()

	segredo := "SEGREDO-XXE-EXTRACAO-nao-pode-vazar-7c2e91"
	arquivoSegredo := filepath.Join(t.TempDir(), "segredo.txt")
	require.NoError(t, os.WriteFile(arquivoSegredo, []byte(segredo), 0o600))

	documentoXML := []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<!DOCTYPE w:document [<!ENTITY xxe SYSTEM "file://%s">]>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>&xxe;</w:t></w:r></w:p></w:body></w:document>`, arquivoSegredo))

	dados := docxSintetico(t, documentoXML)
	doc, err := Abrir(bytes.NewReader(dados), int64(len(dados)))
	require.NoError(t, err)

	blocos, err := abrirBlocosSemPanic(t, doc)
	if err != nil {
		assert.NotContains(t, err.Error(), segredo, "o conteúdo do arquivo local não pode aparecer nem na mensagem de erro")
		return
	}
	for _, bloco := range blocos {
		assert.NotContains(t, bloco.Texto, segredo,
			"entidade externa foi resolvida durante a extração: conteúdo de arquivo local vazou para dentro do CDM")
	}
}

// abrirBlocosSemPanic chama ExtrairBlocos capturando qualquer panic e
// convertendo em falha de teste explícita — mesmo espírito de abrirSemPanic
// em pacote_test.go: "documento malicioso não pode derrubar o processo" é
// uma asserção, não uma esperança.
func abrirBlocosSemPanic(t *testing.T, doc *Documento) (blocos []BlocoBruto, err error) {
	t.Helper()
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("ExtrairBlocos entrou em panic em vez de devolver erro: %v", rec)
		}
	}()
	return doc.ExtrairBlocos()
}

// ---------------------------------------------------------------------------
// XML malformado dentro de word/document.xml: Abrir não desserializa (só
// confere nomes de parte), então a estrutura só se prova malformada aqui,
// dentro de ExtrairBlocos.
// ---------------------------------------------------------------------------

func TestExtrairBlocosDevolveErroValidacaoParaXMLMalformado(t *testing.T) {
	t.Parallel()

	documentoXML := []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>sem fechamento`)

	dados := docxSintetico(t, documentoXML)
	doc, err := Abrir(bytes.NewReader(dados), int64(len(dados)))
	require.NoError(t, err, "Abrir não desserializa o conteúdo; só a estrutura do ZIP e os nomes de parte")
	require.NotNil(t, doc)

	blocos, err := abrirBlocosSemPanic(t, doc)

	require.Error(t, err)
	assert.Nil(t, blocos)

	var validacao *errors.ErroValidacao
	assert.True(t, errors.Como(err, &validacao),
		"document.xml malformado é culpa do arquivo do cliente (HTTP 400), não do servidor; obteve %T (%v)", err, err)
}

// ---------------------------------------------------------------------------
// Classificação pela camada 1: estilos nomeados que o próprio DOCX declara.
// A mais confiável das três camadas (F2) — heurística estrutural e LLM
// entram em recortes seguintes.
// ---------------------------------------------------------------------------

const confiancaAltaMinima = 0.8
const confiancaBaixaMaxima = 0.3

func TestClassificarPorEstiloDocxEstilosConhecidos(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome          string
		estilo        string
		papelEsperado cdm.Papel
	}{
		{nome: "Title vira Titulo", estilo: "Title", papelEsperado: cdm.Titulo},
		{nome: "Heading1 vira Secao de nível 1", estilo: "Heading1", papelEsperado: cdm.Secao(1)},
		{nome: "Heading2 vira Secao de nível 2", estilo: "Heading2", papelEsperado: cdm.Secao(2)},
		{nome: "Normal vira Paragrafo", estilo: "Normal", papelEsperado: cdm.Paragrafo},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			bruto := BlocoBruto{Tipo: TipoBlocoBrutoParagrafo, EstiloNomeado: caso.estilo, Texto: "conteúdo do bloco", Indice: 4}
			bloco := ClassificarPorEstiloDocx(bruto)

			assert.Equal(t, caso.papelEsperado, bloco.Papel)
			assert.Equal(t, cdm.OrigemEstiloDocx, bloco.Origem)
			assert.GreaterOrEqual(t, bloco.Confianca, confiancaAltaMinima,
				"estilo nomeado do DOCX é a camada mais confiável das três: confiança precisa ser alta")
			assert.LessOrEqual(t, bloco.Confianca, 1.0)
			assert.Equal(t, bruto.Indice, bloco.RefXML)
			assert.Equal(t, bruto.Texto, bloco.TextoResumo,
				"texto curto cabe inteiro no resumo, sem truncamento")
		})
	}
}

// TestClassificarPorEstiloDocxTruncaTextoResumo trava o "Resumo" do nome do
// campo. O CDM é um ÍNDICE semântico, não uma cópia do documento — está escrito
// em docs/adr/0001-docx-in-place.md e no godoc de
// entity.TamanhoMaximoCDMBytes. Guardar o texto integral de cada bloco
// duplicaria o documento inteiro dentro de cdm_jsonb, e o texto completo nunca
// se perde: RefXML aponta para o nó de origem no pacote.
//
// O teto de 200 runas vem do que a F5 manda enviar ao LLM (docs/plano-backend.md):
// os primeiros ~200 caracteres de cada bloco candidato.
func TestClassificarPorEstiloDocxTruncaTextoResumo(t *testing.T) {
	t.Parallel()

	longo := strings.Repeat("á", TamanhoMaximoTextoResumo+50)
	bloco := ClassificarPorEstiloDocx(BlocoBruto{
		Tipo: TipoBlocoBrutoParagrafo, EstiloNomeado: "Normal", Texto: longo, Indice: 0,
	})

	assert.Equal(t, TamanhoMaximoTextoResumo, len([]rune(bloco.TextoResumo)),
		"o resumo é cortado em runas, não em bytes: cortar em byte parte caractere multibyte ao meio")
	assert.True(t, strings.HasPrefix(longo, bloco.TextoResumo),
		"o resumo é o começo do texto, não uma reescrita dele")
}

// TestClassificarPorEstiloDocxNaoParteRunaAoTruncar prova que o corte não gera
// byte inválido em UTF-8 — o caractere fora do BMP ocupa 4 bytes e um corte
// ingênuo por índice de byte o partiria ao meio.
func TestClassificarPorEstiloDocxNaoParteRunaAoTruncar(t *testing.T) {
	t.Parallel()

	longo := strings.Repeat("𝕏", TamanhoMaximoTextoResumo+10)
	bloco := ClassificarPorEstiloDocx(BlocoBruto{
		Tipo: TipoBlocoBrutoParagrafo, EstiloNomeado: "Normal", Texto: longo, Indice: 0,
	})

	assert.True(t, utf8.ValidString(bloco.TextoResumo), "resumo truncado tem que continuar UTF-8 válido")
	assert.Equal(t, TamanhoMaximoTextoResumo, len([]rune(bloco.TextoResumo)))
}

func TestClassificarPorEstiloDocxTabelaViraPapelTabela(t *testing.T) {
	t.Parallel()

	// A forma do bloco (tabela) já é evidência estrutural direta: não
	// depende de nome de estilo para ter confiança alta.
	bruto := BlocoBruto{Tipo: TipoBlocoBrutoTabela, Texto: "conteúdo da tabela", Indice: 9}
	bloco := ClassificarPorEstiloDocx(bruto)

	assert.Equal(t, cdm.Tabela, bloco.Papel)
	assert.Equal(t, cdm.OrigemEstiloDocx, bloco.Origem)
	assert.GreaterOrEqual(t, bloco.Confianca, confiancaAltaMinima)
	assert.Equal(t, 9, bloco.RefXML)
}

func TestClassificarPorEstiloDocxEstiloDesconhecidoCaiNoFallback(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome   string
		estilo string
	}{
		{nome: "estilo customizado da revista", estilo: "MinhaRevistaTitulo"},
		{nome: "estilo ausente (parágrafo sem w:pStyle)", estilo: ""},
		{nome: "Heading7 está fora da faixa suportada por cdm.Secao (1..6)", estilo: "Heading7"},
		{nome: "HeadingX não é um nível numérico", estilo: "HeadingX"},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			bruto := BlocoBruto{Tipo: TipoBlocoBrutoParagrafo, EstiloNomeado: caso.estilo, Texto: "conteúdo", Indice: 2}
			bloco := ClassificarPorEstiloDocx(bruto)

			assert.Equal(t, cdm.Paragrafo, bloco.Papel, "estilo não reconhecido não pode inventar um papel semântico")
			assert.Equal(t, cdm.OrigemEstiloDocx, bloco.Origem)
			assert.LessOrEqual(t, bloco.Confianca, confiancaBaixaMaxima,
				"fallback precisa ter confiança baixa, sinalizando para a heurística da camada 2")
			assert.GreaterOrEqual(t, bloco.Confianca, 0.0)
		})
	}
}

func TestClassificarPorEstiloDocxDocumentoSemEstiloNomeadoNaoQuebra(t *testing.T) {
	t.Parallel()

	documentoXML := []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>primeiro parágrafo sem estilo</w:t></w:r></w:p><w:p><w:r><w:t>segundo parágrafo sem estilo</w:t></w:r></w:p></w:body></w:document>`)

	brutos := extrairBlocosDe(t, documentoXML)
	require.Len(t, brutos, 2)

	for _, bruto := range brutos {
		bloco := ClassificarPorEstiloDocx(bruto)
		assert.Equal(t, cdm.Paragrafo, bloco.Papel, "documento sem NENHUM estilo nomeado não pode inventar papel nem travar")
		assert.Equal(t, cdm.OrigemEstiloDocx, bloco.Origem)
		assert.LessOrEqual(t, bloco.Confianca, confiancaBaixaMaxima)
	}
}

// TestClassificarPorEstiloDocxProduzBlocoValidoParaCDM roda a fixture real
// inteira pela extração + classificação e garante que todo cdm.Bloco
// produzido passa em cdm.NovoBloco. É o teste de fronteira entre os dois
// pacotes: a classificação nunca pode devolver Papel ou Confiança que o
// domínio recusaria — por exemplo, cdm.Secao(7) se o fallback de heading
// fora de faixa não estiver bem guardado.
func TestClassificarPorEstiloDocxProduzBlocoValidoParaCDM(t *testing.T) {
	t.Parallel()

	dados := lerFixture(t, "artigo-real-libreoffice.docx")
	doc, err := Abrir(bytes.NewReader(dados), int64(len(dados)))
	require.NoError(t, err)

	brutos, err := doc.ExtrairBlocos()
	require.NoError(t, err)
	require.NotEmpty(t, brutos)

	for _, bruto := range brutos {
		bloco := ClassificarPorEstiloDocx(bruto)
		_, err := cdm.NovoBloco(bloco.Papel, bloco.TextoResumo, bloco.Confianca, bloco.Origem, bloco.RefXML)
		assert.NoError(t, err, "classificação da camada 1 produziu um cdm.Bloco (índice %d, estilo %q) que o domínio recusaria",
			bruto.Indice, bruto.EstiloNomeado)
	}
}
