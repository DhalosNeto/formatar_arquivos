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
	"log/slog"
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

// ---------------------------------------------------------------------------
// Recorte f3-cdm-classifica-citacao — RED
//
// Critérios cobertos neste arquivo: A1 e A11 (C1), A2 e A3 (C2), A4, A5 e
// A13 (C3).
//
// FASE VERMELHA por COMPILAÇÃO: BlocoBruto.RecuoEsquerdaTwips e
// limiarRecuoCitacaoTwips ainda não existem.
//
// A confiança própria do sinal de recuo (C3/C4) é afirmada pelo VALOR, não
// pelo nome da constante: C4 a fixa em 0,5 — abaixo de LimiteConsulta (0,7,
// em infra/config/jev.go:26, para o bloco continuar candidato ao fallback) e
// acima de confiancaSemEstilo (0,2). O nome do identificador em blocos.go é
// escolha do codador; o número é contrato.
const confiancaRecuoCitacaoEsperada = 0.5

// documentoXMLComCorpo monta um word/document.xml com o conteúdo informado
// dentro de w:body. Diferente de documentoXMLMinimo, não embrulha nada em
// w:p: os casos de w:pPr/w:ind precisam escrever o parágrafo inteiro.
func documentoXMLComCorpo(corpo string) []byte {
	return []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
		corpo + `</w:body></w:document>`)
}

// extrairBlocosSemPanicDe é extrairBlocosDe com a garantia explícita de que
// nenhuma entrada deste arquivo derruba o processo: A1 exercita o vetor de
// entrada NÃO CONFIÁVEL (negativo, medida universal, inteiro de 20 dígitos),
// e "sem erro e sem pânico" é asserção, não esperança.
func extrairBlocosSemPanicDe(t *testing.T, documentoXML []byte) []BlocoBruto {
	t.Helper()

	dados := docxSintetico(t, documentoXML)
	doc, err := Abrir(bytes.NewReader(dados), int64(len(dados)))
	require.NoError(t, err)
	require.NotNil(t, doc)

	blocos, err := abrirBlocosSemPanic(t, doc)
	require.NoError(t, err, "valor de w:ind inválido é limite do formato, não erro do pacote: a leitura precisa seguir")
	return blocos
}

// A1 — leitura de w:ind sobre docxSintetico, um termo por caso. Cada caso
// prova UM termo de C1; remover qualquer pedaço da guarda quebra o caso dele
// e só o dele.
//
// Cada caso é pinado pelo VALOR exato. Não existe asserção de FAIXA contra
// limiarRecuoCitacaoTwips aqui, e não pode existir: "nenhum caso acima do
// limiar" é falso por construção — w:start="2268" e w:left="1134" estão acima
// de 567 de propósito, e A11 DEPENDE de 2268 estar acima para provar que o
// recuo classifica. O saneamento frouxo que a faixa pretendia pegar
// (negativo virando grande, overflow virando MaxInt) já quebra no
// assert.Equal do caso, que exige o número exato, não um número diferente.
func TestExtrairBlocosLeRecuoEsquerdaTwipsDoProprioParagrafo(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome    string
		corpo   string
		indice  int
		tipo    TipoBlocoBruto
		esperal int
		// esperaTexto, quando preenchido, afere o texto do bloco alvo. Só os
		// casos em que a guarda do recuo poderia engolir texto o usam.
		esperaTexto string
	}{
		{
			nome:    "w:start é lido: é o que o LibreOffice emite para fo:margin-left",
			corpo:   `<w:p><w:pPr><w:ind w:start="2268"/></w:pPr><w:r><w:t>recuado</w:t></w:r></w:p>`,
			esperal: 2268,
		},
		{
			nome:    "w:left é lido: é o que o Word emite",
			corpo:   `<w:p><w:pPr><w:ind w:left="1134"/></w:pPr><w:r><w:t>recuado</w:t></w:r></w:p>`,
			esperal: 1134,
		},
		{
			nome:    "w:start tem precedência sobre w:left quando os dois estão presentes",
			corpo:   `<w:p><w:pPr><w:ind w:left="1134" w:start="2268"/></w:pPr><w:r><w:t>recuado</w:t></w:r></w:p>`,
			esperal: 2268,
		},
		{
			// Achado BAIXO da segurança: a precedência de w:start vencia
			// ANTES do saneamento, então um w:start com medida universal
			// zerava o bloco e descartava o único valor utilizável do w:ind.
			// Precedência vale sobre valor que PARSEIA; sobre lixo, não.
			nome:    "w:start INVÁLIDO (\"4cm\") ao lado de w:left VÁLIDO cai para o w:left, não para 0",
			corpo:   `<w:p><w:pPr><w:ind w:start="4cm" w:left="2268"/></w:pPr><w:r><w:t>recuado</w:t></w:r></w:p>`,
			esperal: 2268,
		},
		{
			// A assimetria DECLARADA, que faltava caso: negativo PARSEIA,
			// então a precedência de w:start continua valendo e o saneamento
			// zera depois. Não é o caso de "não descartar o único valor
			// utilizável" — ali o w:start não parseia; aqui ele parseia e
			// vence, mesmo sendo recuo pendente.
			nome:    "w:start NEGATIVO ao lado de w:left válido dá 0: negativo parseia, logo a precedência vale e o saneamento zera",
			corpo:   `<w:p><w:pPr><w:ind w:start="-567" w:left="2268"/></w:pPr><w:r><w:t>pendente</w:t></w:r></w:p>`,
			esperal: 0,
		},
		{
			nome:    "w:leftChars sozinho dá 0: a unidade é centésimo de caractere, não twip",
			corpo:   `<w:p><w:pPr><w:ind w:leftChars="400"/></w:pPr><w:r><w:t>recuado</w:t></w:r></w:p>`,
			esperal: 0,
		},
		{
			nome:    "recuo NEGATIVO dá 0: ST_SignedTwipsMeasure aceita negativo, e recuo pendente não é citação",
			corpo:   `<w:p><w:pPr><w:ind w:left="-567"/></w:pPr><w:r><w:t>pendente</w:t></w:r></w:p>`,
			esperal: 0,
		},
		{
			nome:    "medida universal \"4cm\" dá 0: ST_SignedTwipsMeasure também admite unidade, e o campo é em twips",
			corpo:   `<w:p><w:pPr><w:ind w:left="4cm"/></w:pPr><w:r><w:t>recuado</w:t></w:r></w:p>`,
			esperal: 0,
		},
		{
			nome:    "inteiro de 20 dígitos dá 0 sem erro e sem pânico: overflow de strconv é entrada hostil, não falha do pacote",
			corpo:   `<w:p><w:pPr><w:ind w:left="99999999999999999999"/></w:pPr><w:r><w:t>hostil</w:t></w:r></w:p>`,
			esperal: 0,
		},
		{
			nome:    "w:ind repetido no mesmo w:pPr: o PRIMEIRO vence, igual já vale para w:pStyle",
			corpo:   `<w:p><w:pPr><w:ind w:left="1134"/><w:ind w:left="2268"/></w:pPr><w:r><w:t>repetido</w:t></w:r></w:p>`,
			esperal: 1134,
		},
		{
			nome: "w:ind dentro de w:pPrChange é IGNORADO: pPrChange guarda a propriedade REVOGADA por revisão rastreada",
			// Este é o único w:ind do parágrafo, de propósito: se o caso
			// tivesse um w:ind corrente antes, "o primeiro vence" já o
			// resolveria e o termo do pPrChange seria vacuoso — o teste
			// passaria sem que a guarda existisse.
			corpo:   `<w:p><w:pPr><w:pPrChange w:id="1" w:author="revisor"><w:pPr><w:ind w:left="2268"/></w:pPr></w:pPrChange></w:pPr><w:r><w:t>recuo revogado</w:t></w:r></w:p>`,
			esperal: 0,
			// A guarda pula a subárvore do w:pPrChange, e o comentário de
			// produção afirma que isso é seguro porque pPrChange não contém
			// w:t. A afirmação precisa de asserção: sem ela, um salto que
			// consumisse tokens além da subárvore engoliria texto do
			// parágrafo sem quebrar nenhum caso desta tabela.
			esperaTexto: "recuo revogado",
		},
		{
			nome:    "w:ind de parágrafo DENTRO de célula não contamina o BlocoBruto do w:tbl",
			corpo:   `<w:tbl><w:tr><w:tc><w:p><w:pPr><w:ind w:left="2268"/></w:pPr><w:r><w:t>celula</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`,
			tipo:    TipoBlocoBrutoTabela,
			esperal: 0,
		},
		{
			nome:    "w:pPr sem w:ind dá 0",
			corpo:   `<w:p><w:pPr><w:pStyle w:val="Normal"/></w:pPr><w:r><w:t>sem recuo</w:t></w:r></w:p>`,
			esperal: 0,
		},
		{
			nome:    "parágrafo sem w:pPr dá 0",
			corpo:   `<w:p><w:r><w:t>sem propriedades</w:t></w:r></w:p>`,
			esperal: 0,
		},
	}

	for _, caso := range casos {
		caso := caso
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			blocos := extrairBlocosSemPanicDe(t, documentoXMLComCorpo(caso.corpo))

			require.Len(t, blocos, 1, "pré-condição do caso: o corpo tem exatamente um bloco de nível superior")
			require.Equal(t, caso.tipo, blocos[caso.indice].Tipo, "pré-condição do caso: o bloco alvo precisa ser do tipo que o caso pretende provar")

			assert.Equal(t, caso.esperal, blocos[caso.indice].RecuoEsquerdaTwips)
			if caso.esperaTexto != "" {
				assert.Equal(t, caso.esperaTexto, blocos[caso.indice].Texto,
					"a leitura do recuo não pode consumir texto do parágrafo")
			}
		})
	}
}

// A2 — a lista MEDIDA de estilos de citação. Um caso por nome: remover um
// item da lista de C2 quebra exatamente o caso dele.
func TestClassificarPorEstiloDocxEstiloDeCitacaoViraPapelCitacao(t *testing.T) {
	t.Parallel()

	// A lista é a medida de soffice, não a intuição: o LibreOffice descarta
	// os caracteres não-ASCII do styleId, então "Citação" sai como "Citao" e
	// "Bloco de citação" como "Blocodecitao". Quote/IntenseQuote são os
	// styleIds latent-English do Word.
	for _, estilo := range []string{"Citao", "Blocodecitao", "Quote", "IntenseQuote"} {
		estilo := estilo
		t.Run(estilo, func(t *testing.T) {
			t.Parallel()

			bruto := BlocoBruto{Tipo: TipoBlocoBrutoParagrafo, EstiloNomeado: estilo, Texto: "Trecho citado de outra obra.", Indice: 7}
			bloco := ClassificarPorEstiloDocx(bruto)

			assert.Equal(t, cdm.Citacao, bloco.Papel)
			assert.Equal(t, confiancaEstiloConhecido, bloco.Confianca,
				"estilo nomeado de citação é evidência DECLARADA pelo documento: mesma confiança de Title/Normal/HeadingN")
			assert.Equal(t, cdm.OrigemEstiloDocx, bloco.Origem)
			assert.Equal(t, bruto.Indice, bloco.RefXML)
		})
	}
}

// A3 — near-misses INEQUÍVOCOS. "Citacao" e "Citação" estão aqui de
// propósito: são o que a intuição diria, e são exatamente as formas que o
// LibreOffice NÃO emite. O caso documenta a medição.
func TestClassificarPorEstiloDocxNearMissDeEstiloDeCitacaoNaoViraCitacao(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome   string
		estilo string
	}{
		{nome: "Citacao sem cedilha-til: o LO descarta o não-ASCII, nunca translitera", estilo: "Citacao"},
		{nome: "Citação acentuado: idem, não chega a sair do LO", estilo: "Citação"},
		{nome: "Quote2 não é Quote", estilo: "Quote2"},
		{nome: "BlocoDeCitacao não é Blocodecitao", estilo: "BlocoDeCitacao"},
		{nome: "citao em caixa baixa: a comparação é sensível a caixa, como Title/Normal/Heading já são", estilo: "citao"},
		{nome: "BlockQuote: nenhuma ferramenta conhecida o emite", estilo: "BlockQuote"},
	}

	for _, caso := range casos {
		caso := caso
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			bruto := BlocoBruto{Tipo: TipoBlocoBrutoParagrafo, EstiloNomeado: caso.estilo, Texto: "Trecho qualquer.", Indice: 2}
			bloco := ClassificarPorEstiloDocx(bruto)

			assert.NotEqual(t, cdm.Citacao, bloco.Papel,
				"estilo fora da lista medida não pode virar Citacao: sem recuo, o bloco cai em Paragrafo e a camada 2 decide")
			assert.Equal(t, cdm.Paragrafo, bloco.Papel)
			assert.Equal(t, confiancaSemEstilo, bloco.Confianca)
		})
	}
}

// A4 — o sinal de recuo dispara nos estilos que NÃO determinam outro papel.
// O caso de "Normal" é o que importa: 47 dos 57 parágrafos de
// artigo-real-libreoffice.docx o usam, e um portão que só aceitasse estilo
// vazio ou desconhecido nunca dispararia no produtor alvo.
func TestClassificarPorEstiloDocxRecuoAcimaDoLimiarViraCitacao(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome   string
		estilo string
	}{
		{nome: "estilo vazio (parágrafo sem w:pStyle)", estilo: ""},
		{nome: "Normal: estilo CONHECIDO que devolve o papel-default Paragrafo", estilo: "Normal"},
		{nome: "BodyText: estilo desconhecido pelo switch", estilo: "BodyText"},
	}

	for _, caso := range casos {
		caso := caso
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			bruto := BlocoBruto{
				Tipo:               TipoBlocoBrutoParagrafo,
				EstiloNomeado:      caso.estilo,
				Texto:              "Trecho longo apresentado em bloco recuado, sem aspas.",
				RecuoEsquerdaTwips: limiarRecuoCitacaoTwips + 1,
				Indice:             11,
			}
			bloco := ClassificarPorEstiloDocx(bruto)

			assert.Equal(t, cdm.Citacao, bloco.Papel)
			assert.Equal(t, confiancaRecuoCitacaoEsperada, bloco.Confianca,
				"recuo é evidência mais fraca que estilo nomeado, e precisa ficar ABAIXO de LimiteConsulta (0,7) para o bloco seguir candidato ao fallback")
			assert.Equal(t, cdm.OrigemEstiloDocx, bloco.Origem)
		})
	}
}

// A5 — as recusas do sinal de recuo, um termo por caso.
func TestClassificarPorEstiloDocxRecuoNaoViraCitacaoNessesCasos(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome              string
		estilo            string
		recuo             int
		papelEsperado     cdm.Papel
		confiancaEsperada float64
	}{
		{
			nome:              "recuo EXATAMENTE no limiar não classifica: a comparação é estrita",
			recuo:             limiarRecuoCitacaoTwips,
			estilo:            "Normal",
			papelEsperado:     cdm.Paragrafo,
			confiancaEsperada: confiancaEstiloConhecido,
		},
		{
			nome:              "recuo um twip abaixo do limiar não classifica",
			recuo:             limiarRecuoCitacaoTwips - 1,
			estilo:            "Normal",
			papelEsperado:     cdm.Paragrafo,
			confiancaEsperada: confiancaEstiloConhecido,
		},
		{
			nome:              "recuo zero não classifica",
			recuo:             0,
			estilo:            "",
			papelEsperado:     cdm.Paragrafo,
			confiancaEsperada: confiancaSemEstilo,
		},
		{
			nome:              "Title bloqueia o sinal: o papel do ESTILO vence",
			recuo:             limiarRecuoCitacaoTwips + 1,
			estilo:            "Title",
			papelEsperado:     cdm.Titulo,
			confiancaEsperada: confiancaEstiloConhecido,
		},
		{
			nome:              "Heading1 bloqueia o sinal: o papel do ESTILO vence",
			recuo:             limiarRecuoCitacaoTwips + 1,
			estilo:            "Heading1",
			papelEsperado:     cdm.Secao(1),
			confiancaEsperada: confiancaEstiloConhecido,
		},
		{
			nome:              "estilo da lista de C2 com recuo alto sai Citacao por ESTILO, não por recuo: a confiança é a de estilo nomeado",
			recuo:             limiarRecuoCitacaoTwips + 1,
			estilo:            "Quote",
			papelEsperado:     cdm.Citacao,
			confiancaEsperada: confiancaEstiloConhecido,
		},
	}

	for _, caso := range casos {
		caso := caso
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			bloco := ClassificarPorEstiloDocx(BlocoBruto{
				Tipo:               TipoBlocoBrutoParagrafo,
				EstiloNomeado:      caso.estilo,
				Texto:              "Trecho qualquer do documento.",
				RecuoEsquerdaTwips: caso.recuo,
				Indice:             3,
			})

			assert.Equal(t, caso.papelEsperado, bloco.Papel)
			assert.Equal(t, caso.confiancaEsperada, bloco.Confianca,
				"o sinal de recuo não pode nem classificar nem rebaixar a confiança nestes casos")
			assert.NotEqual(t, confiancaRecuoCitacaoEsperada, bloco.Confianca,
				"a confiança do sinal de recuo é a marca observável de que ele disparou: nenhum caso de A5 pode carregá-la")
		})
	}
}

// A11 — o caminho completo sobre docxSintetico: ExtrairBlocos -> camada 1 ->
// AplicarHeuristica. Os textos são escolhidos para não casar com NENHUMA
// regra textual da camada 2 e para não abrir região, de modo que o que o
// teste observa é só a classificação de citação atravessando as três etapas.
func TestPipelineSinteticoClassificaCitacaoPorRecuoEPorEstilo(t *testing.T) {
	t.Parallel()

	documentoXML := documentoXMLComCorpo(
		`<w:p><w:pPr><w:pStyle w:val="Normal"/><w:ind w:start="2268"/></w:pPr><w:r><w:t>Trecho apresentado em bloco recuado de quatro centímetros.</w:t></w:r></w:p>` +
			`<w:p><w:pPr><w:pStyle w:val="Quote"/></w:pPr><w:r><w:t>Trecho apresentado com o estilo nomeado de citação.</w:t></w:r></w:p>`)

	brutos := extrairBlocosSemPanicDe(t, documentoXML)
	require.Len(t, brutos, 2)
	require.Greater(t, brutos[0].RecuoEsquerdaTwips, limiarRecuoCitacaoTwips,
		"pré-condição: o primeiro bloco precisa ter chegado da extração com recuo ACIMA do limiar")
	require.Equal(t, "Quote", brutos[1].EstiloNomeado, "pré-condição: o segundo bloco precisa ter chegado com o estilo de citação")

	classificados := make([]cdm.Bloco, len(brutos))
	for i, bruto := range brutos {
		classificados[i] = ClassificarPorEstiloDocx(bruto)
	}

	saida, err := cdm.AplicarHeuristica(classificados)
	require.NoError(t, err)
	require.Len(t, saida, 2)

	assert.Equal(t, cdm.Citacao, saida[0].Papel, "citação por RECUO precisa sobreviver às três etapas")
	assert.Equal(t, confiancaRecuoCitacaoEsperada, saida[0].Confianca)
	assert.Equal(t, cdm.Citacao, saida[1].Papel, "citação por ESTILO precisa sobreviver às três etapas")
	assert.Equal(t, confiancaEstiloConhecido, saida[1].Confianca)
}

// A13 — regra 7 no caminho novo de classificação. NASCE VERDE de propósito:
// ClassificarPorEstiloDocx não devolve erro e não loga, e ler w:pStyle e
// w:ind não toca o texto do bloco (diferente da heurística de aspas, que foi
// retirada do recorte). O teste existe para derrubar quem acrescentar log de
// conteúdo depois.
//
// Não é paralelo: troca o logger padrão do slog, que é estado de processo.
func TestClassificarCitacaoNaoVazaConteudoDoDocumentoParaErroOuLog(t *testing.T) {
	marca := "MARCA-UNICA-DE-CONTEUDO-nao-pode-vazar-4f91ab"

	var registro bytes.Buffer
	anterior := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&registro, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(anterior) })

	documentoXML := documentoXMLComCorpo(
		`<w:p><w:pPr><w:pStyle w:val="Quote"/><w:ind w:start="2268"/></w:pPr><w:r><w:t>` + marca + `</w:t></w:r></w:p>`)

	dados := docxSintetico(t, documentoXML)
	doc, err := Abrir(bytes.NewReader(dados), int64(len(dados)))
	require.NoError(t, err)

	brutos, err := doc.ExtrairBlocos()
	require.NoError(t, err, "pré-condição: o caminho novo não pode falhar neste documento")
	require.Len(t, brutos, 1)
	require.Contains(t, brutos[0].Texto, marca, "pré-condição: a marca precisa estar no texto para o teste valer algo")

	bloco := ClassificarPorEstiloDocx(brutos[0])
	saida, err := cdm.AplicarHeuristica([]cdm.Bloco{bloco})
	require.NoError(t, err)
	require.Len(t, saida, 1)
	require.Equal(t, cdm.Citacao, saida[0].Papel, "pré-condição: o caminho novo de classificação precisa ter sido exercitado")

	assert.NotContains(t, registro.String(), marca,
		"conteúdo de documento do usuário não pode ir para log, nem pelo caminho novo de classificação de citação (regra 7)")
}
