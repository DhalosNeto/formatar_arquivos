// Este arquivo é o RED dos contratos C3 e C4 do recorte
// f3-integracao-plano-formatacao: (*Documento).AplicarPlano, a aplicação
// atômica de um formatador.Plano por cima dos mutadores já existentes.
//
// FASE VERMELHA: formatar.go ainda não existe. O pacote não compila porque
// AplicarPlano está indefinido — não por erro de sintaxe ou de import.
//
// Critérios cobertos aqui: A9, A10, A11, A12, A13 e A17 (C3); A14 e A15 (C4);
// e a parte de A16 que pertence a AplicarPlano.
//
// Nenhum montador de ZIP novo: montarZip, partesBaseDocx, docxSintetico,
// alinhamentoFixture, docxComRelacoesStyles, abrirParaSubstituicao,
// salvarSubstituicao, listarPartes, lerParte, lerFixture, extrairTextosWT,
// paginaSintetica, conferirPaginaSinteticaXML e propriedadesTipografiaDireta
// são os helpers já existentes do pacote.
package ooxml

import (
	"fmt"
	"sort"
	"testing"

	"github.com/daniel-halos/formatador/internal/domain/cdm"
	"github.com/daniel-halos/formatador/internal/domain/formatador"
	"github.com/daniel-halos/formatador/internal/domain/ruleset"
	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Fixtures e inspetores locais
// ---------------------------------------------------------------------------

// quatroParagrafos é o corpo literal usado pelos casos sintéticos. Ser um
// literal é o que permite afirmar que um parágrafo NÃO foi tocado.
const quatroParagrafos = `<w:p><w:r><w:t>zero</w:t></w:r></w:p><w:p><w:r><w:t>um</w:t></w:r></w:p><w:p><w:r><w:t>dois</w:t></w:r></w:p><w:p><w:r><w:t>tres</w:t></w:r></w:p>`

// sectPrDiferenteDoPlano declara medidas distintas das de paginaSintetica
// (11906x16838 e pgMar 1134/1134/1701/1134). Sem isso AplicarPagina não
// gravaria delta e os critérios A9, A10 e A12 ficariam vacuosos.
const sectPrDiferenteDoPlano = `<w:sectPr><w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/></w:sectPr>`

// complementosZerados são os atributos que AplicarPagina grava quando o
// sectPr do arquivo não os declara e MargensComplementares está zerada.
var complementosZerados = map[string]string{"header": "0", "footer": "0", "gutter": "0"}

func corpoPlanoSintetico() ruleset.Corpo {
	return ruleset.Corpo{
		Fonte:          "Times New Roman",
		TamanhoPT:      12,
		Entrelinha:     1.5,
		RecuoCM:        1.25,
		EspacoAntesPT:  0,
		EspacoDepoisPT: 6,
		Alinhamento:    "justificado",
	}
}

func planoSintetico(referencias []int) formatador.Plano {
	return formatador.Plano{
		Pagina:           paginaSintetica(),
		Corpo:            corpoPlanoSintetico(),
		ReferenciasCorpo: referencias,
	}
}

func fixtureQuatroParagrafos() []byte {
	return alinhamentoFixture(quatroParagrafos + sectPrDiferenteDoPlano)
}

// fixtureComNumPr devolve um corpo cujo parágrafo 1 tem w:numPr — recusado
// pelo preflight de AplicarRecuoPrimeiraLinha, que é o quarto mutador da
// ordem de C3, depois de a página já ter sido aplicada na cópia.
func fixtureComNumPr(texto string) []byte {
	return alinhamentoFixture(
		`<w:p><w:r><w:t>zero</w:t></w:r></w:p>` +
			`<w:p><w:pPr><w:numPr><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t>` + texto + `</w:t></w:r></w:p>` +
			sectPrDiferenteDoPlano)
}

// xmlDosBlocosDeTopo devolve o XML BRUTO de cada filho de w:body que é w:p ou
// w:tbl, na ordem do documento — os mesmos ordinais que ExtrairBlocos
// (blocos.go:109-117) e o adaptador (alinhamento.go:61-66) usam. Reusa o
// parser interno do pacote em vez de recortar string, para que a comparação
// seja do nó inteiro e não de um trecho.
func xmlDosBlocosDeTopo(t *testing.T, documentoXML []byte) []string {
	t.Helper()

	corpo, err := corpoXMLParagrafos(documentoXML)
	require.NoError(t, err, "xmlDosBlocosDeTopo: word/document.xml precisa ser analisável")

	var brutos []string
	for _, filho := range corpo.filhos {
		if _, bloco := tipoDoElemento(filho.nome); !bloco {
			continue
		}
		fim := filho.fechoFim
		if fim <= filho.inicio {
			fim = filho.fim
		}
		brutos = append(brutos, string(documentoXML[filho.inicio:fim]))
	}
	return brutos
}

// propriedadeParagrafo é um filho de w:pPr com seus atributos do namespace w.
type propriedadeParagrafo struct {
	local     string
	atributos map[string]string
}

// propriedadesDoParagrafo exige EXATAMENTE um w:pPr no parágrafo de ordinal
// informado e devolve seus filhos. É aqui que se prova que os mutadores
// reusaram o mesmo w:spacing em vez de criar um segundo.
func propriedadesDoParagrafo(t *testing.T, documentoXML []byte, ordinal int) []propriedadeParagrafo {
	t.Helper()

	corpo, err := corpoXMLParagrafos(documentoXML)
	require.NoError(t, err)

	var blocos []*noXMLPagina
	for _, filho := range corpo.filhos {
		if _, bloco := tipoDoElemento(filho.nome); bloco {
			blocos = append(blocos, filho)
		}
	}
	require.Less(t, ordinal, len(blocos), "ordinal %d fora do corpo", ordinal)
	require.True(t, ehElementoW(blocos[ordinal].nome, "p"), "ordinal %d não é um w:p", ordinal)

	pprs := filhosW(blocos[ordinal], "pPr")
	require.Len(t, pprs, 1, "A9: o parágrafo alvo tem exatamente um w:pPr")

	propriedades := make([]propriedadeParagrafo, 0, len(pprs[0].filhos))
	for _, filho := range pprs[0].filhos {
		atributos := map[string]string{}
		for _, atributo := range filho.atributos {
			if atributo.namespace == namespaceWord {
				atributos[atributo.local] = atributo.valor
			}
		}
		propriedades = append(propriedades, propriedadeParagrafo{local: filho.nome.Local, atributos: atributos})
	}
	return propriedades
}

// unicaPropriedade exige que o local apareça uma só vez no w:pPr e devolve
// seus atributos.
func unicaPropriedade(t *testing.T, propriedades []propriedadeParagrafo, local string) map[string]string {
	t.Helper()

	var encontradas []map[string]string
	for _, propriedade := range propriedades {
		if propriedade.local == local {
			encontradas = append(encontradas, propriedade.atributos)
		}
	}
	require.Len(t, encontradas, 1, "A9: o w:pPr precisa ter exatamente um w:%s", local)
	return encontradas[0]
}

// ---------------------------------------------------------------------------
// A9 — página do plano e as seis propriedades nos parágrafos selecionados
// ---------------------------------------------------------------------------

func TestAplicarPlanoEscrevePaginaEPropriedadesDosAlvos(t *testing.T) {
	fonte := fixtureQuatroParagrafos()
	pacote := docxSintetico(t, fonte)
	doc := abrirParaSubstituicao(t, pacote)

	require.NotContains(t, string(fonte), `w:w="11906"`,
		"pré-condição: a página do arquivo precisa DIFERIR da do plano, senão AplicarPagina não grava delta")

	require.NoError(t, doc.AplicarPlano(planoSintetico([]int{1, 3}), MargensComplementares{}))
	saida := lerParte(t, salvarSubstituicao(t, doc), "word/document.xml")

	// Página: medidas do plano, uma única pgSz e uma única pgMar.
	conferirPaginaSinteticaXML(t, saida, complementosZerados)

	blocos := xmlDosBlocosDeTopo(t, saida)
	require.Len(t, blocos, 4, "pré-condição: quatro parágrafos de topo")
	assert.Equal(t, `<w:p><w:r><w:t>zero</w:t></w:r></w:p>`, blocos[0], "A9: parágrafo 0 não é alvo e não ganha w:pPr")
	assert.Equal(t, `<w:p><w:r><w:t>dois</w:t></w:r></w:p>`, blocos[2], "A9: parágrafo 2 não é alvo e não ganha w:pPr")

	for _, ordinal := range []int{1, 3} {
		propriedades := propriedadesDoParagrafo(t, saida, ordinal)

		espacamento := unicaPropriedade(t, propriedades, "spacing")
		assert.Equal(t, map[string]string{"line": "360", "lineRule": "auto", "before": "0", "after": "120"}, espacamento,
			"A9: entrelinha e os dois espaçamentos carregam o MESMO w:spacing (parágrafo %d)", ordinal)

		recuo := unicaPropriedade(t, propriedades, "ind")
		assert.Equal(t, "709", recuo["firstLine"], "A9: w:ind/@w:firstLine do parágrafo %d", ordinal)

		alinhamento := unicaPropriedade(t, propriedades, "jc")
		assert.Equal(t, "both", alinhamento["val"], "A9: w:jc/@w:val do parágrafo %d", ordinal)
	}

	// Tipografia: só os runs dos alvos ganham rPr com rFonts, sz e szCs.
	tipografia := propriedadesTipografiaDireta(t, saida)
	require.Len(t, tipografia, 2, "A9: exatamente os dois parágrafos alvo têm run com rPr direto")
	for _, run := range tipografia {
		assert.Equal(t, "Times New Roman", run["rFonts.ascii"])
		assert.Equal(t, "Times New Roman", run["rFonts.hAnsi"])
		assert.Equal(t, "24", run["sz.val"])
		assert.Equal(t, "24", run["szCs.val"])
	}

	assert.Equal(t, extrairTextosWT(t, fonte), extrairTextosWT(t, saida), "C4: a sequência de runes dos w:t é idêntica")
}

// ---------------------------------------------------------------------------
// A10 — corpo vazio aplica somente a página
// ---------------------------------------------------------------------------

func TestAplicarPlanoSemReferenciasAlteraSomenteAPagina(t *testing.T) {
	fonte := fixtureQuatroParagrafos()
	doc := abrirParaSubstituicao(t, docxSintetico(t, fonte))

	require.NoError(t, doc.AplicarPlano(planoSintetico(nil), MargensComplementares{}))
	saida := lerParte(t, salvarSubstituicao(t, doc), "word/document.xml")

	conferirPaginaSinteticaXML(t, saida, complementosZerados)
	assert.Equal(t, xmlDosBlocosDeTopo(t, fonte), xmlDosBlocosDeTopo(t, saida),
		"A10: com ReferenciasCorpo vazio o XML dos parágrafos é idêntico ao original")
}

// ---------------------------------------------------------------------------
// A11 — receptor nulo e ordinal inexistente
// ---------------------------------------------------------------------------

func TestAplicarPlanoRejeitaReceptorNulo(t *testing.T) {
	err := (*Documento)(nil).AplicarPlano(planoSintetico([]int{0}), MargensComplementares{})

	var nulo *errors.ErroArgumentoNulo
	require.Error(t, err)
	require.True(t, errors.Como(err, &nulo), "C3: receptor nulo devolve *ErroArgumentoNulo; erro foi %T", err)
	assert.Equal(t, "documento", nulo.Argumento)
}

func TestAplicarPlanoRejeitaOrdinalInexistenteSemDeixarDelta(t *testing.T) {
	pacote := docxSintetico(t, fixtureQuatroParagrafos())
	require.Equal(t, pacote, salvarSubstituicao(t, abrirParaSubstituicao(t, pacote)),
		"pré-condição: sem delta algum, Salvar reproduz o pacote byte a byte")

	doc := abrirParaSubstituicao(t, pacote)
	err := doc.AplicarPlano(planoSintetico([]int{9}), MargensComplementares{})

	var validacao *errors.ErroValidacao
	require.Error(t, err, "A11: ordinal fora do corpo não pode ser aceito")
	require.True(t, errors.Como(err, &validacao), "A11: referência inválida é *ErroValidacao; erro foi %T", err)
	assert.Equal(t, []errors.CampoInvalido{{Campo: "referencias", Mensagem: "referências de parágrafos inválidas"}}, validacao.Campos)
	assert.Equal(t, pacote, salvarSubstituicao(t, doc), "A11: a falha não deixa parte substituída no receptor")
}

// ---------------------------------------------------------------------------
// A12 — atomicidade: nem o delta de página sobrevive à falha
// ---------------------------------------------------------------------------

func TestAplicarPlanoFalhaNaoDeixaNemODeltaDePagina(t *testing.T) {
	pacote := docxSintetico(t, fixtureComNumPr("um"))

	require.Equal(t, pacote, salvarSubstituicao(t, abrirParaSubstituicao(t, pacote)),
		"pré-condição: sem delta algum, Salvar reproduz o pacote byte a byte")

	// Pré-condição explícita de C3/A12: nesta MESMA fixture, uma AplicarPagina
	// isolada realmente substitui a parte. Sem isso o critério seria vacuoso.
	isolado := abrirParaSubstituicao(t, pacote)
	require.NoError(t, isolado.AplicarPagina(paginaSintetica(), MargensComplementares{}))
	require.NotEqual(t, pacote, salvarSubstituicao(t, isolado),
		"pré-condição: a página do plano produz delta nesta fixture")

	casos := []struct {
		nome        string
		referencias []int
	}{
		{"parágrafo com numPr recusado pelo recuo", []int{1}},
		{"referências acima do teto dos mutadores de corpo", make([]int, limiteNosPagina+1)},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			doc := abrirParaSubstituicao(t, pacote)

			require.Error(t, doc.AplicarPlano(planoSintetico(caso.referencias), MargensComplementares{}),
				"A12: o caso precisa alcançar a guarda que pretende provar")
			assert.Equal(t, pacote, salvarSubstituicao(t, doc),
				"A12: nem o delta de página fica no receptor; Salvar em destino novo reproduz o original")
		})
	}
}

// ---------------------------------------------------------------------------
// A13 — parte já substituída pelo chamador não é afetada pela falha
// ---------------------------------------------------------------------------

func TestAplicarPlanoFalhaPreservaParteSubstituidaPeloChamador(t *testing.T) {
	// Conteúdo diferente do que está no ZIP, para que a origem dos bytes no
	// receptor seja distinguível.
	estilosDoChamador := []byte(`<w:styles xmlns:w="` + espacoNomesW + `"></w:styles>`)
	pacote := docxComRelacoesStyles(t, fixtureComNumPr("um"), []byte(relStylesCanonica), []byte(stylesSemRegras))
	require.NotEqual(t, stylesSemRegras, string(estilosDoChamador),
		"pré-condição: os bytes do chamador diferem dos bytes do pacote")

	referencia := abrirParaSubstituicao(t, pacote)
	require.NoError(t, referencia.SubstituirParte("word/styles.xml", estilosDoChamador))
	esperado := salvarSubstituicao(t, referencia)

	doc := abrirParaSubstituicao(t, pacote)
	require.NoError(t, doc.SubstituirParte("word/styles.xml", estilosDoChamador))

	require.Error(t, doc.AplicarPlano(planoSintetico([]int{1}), MargensComplementares{}),
		"A13: o caso precisa falhar no meio da sequência de mutadores")

	saida := salvarSubstituicao(t, doc)
	assert.Equal(t, estilosDoChamador, lerParte(t, saida, "word/styles.xml"),
		"A13: a falha na cópia não altera a fatia retida pelo receptor (aliasing da clonagem rasa)")
	assert.Equal(t, esperado, saida, "A13: o receptor fica só com o delta que o chamador gravou")
}

// ---------------------------------------------------------------------------
// A14 — invariantes de C4 sobre o pacote real
// ---------------------------------------------------------------------------

// referenciasDeParagrafoDoPacoteReal percorre o caminho real do CDM:
// ExtrairBlocos + ClassificarPorEstiloDocx. Devolve o índice e as referências
// dos blocos classificados como cdm.Paragrafo.
func referenciasDeParagrafoDoPacoteReal(t *testing.T, doc *Documento) (cdm.Indice, []int) {
	t.Helper()

	brutos, err := doc.ExtrairBlocos()
	require.NoError(t, err)

	blocos := make([]cdm.Bloco, 0, len(brutos))
	var referencias []int
	var temTabela bool
	for _, bruto := range brutos {
		bloco := ClassificarPorEstiloDocx(bruto)
		blocos = append(blocos, bloco)
		if bloco.Papel == cdm.Paragrafo {
			referencias = append(referencias, bloco.RefXML)
		}
		if bloco.Papel == cdm.Tabela {
			temTabela = true
		}
	}
	require.True(t, temTabela, "pré-condição: o pacote real tem uma w:tbl intercalada entre os parágrafos")
	require.NotEmpty(t, referencias, "pré-condição: o pacote real tem parágrafos classificados")
	return cdm.NovoIndice(blocos), referencias
}

func TestAplicarPlanoNoPacoteRealPreservaTextoEPartesNaoAlvo(t *testing.T) {
	original := lerFixture(t, "artigo-real-libreoffice.docx")
	doc := abrirParaSubstituicao(t, original)
	_, referencias := referenciasDeParagrafoDoPacoteReal(t, doc)

	documentoOriginal := lerParte(t, original, "word/document.xml")
	require.NotContains(t, string(documentoOriginal), `w:left="1701"`,
		"pré-condição: a página do plano DIFERE da medida do arquivo")

	require.NoError(t, doc.AplicarPlano(planoSintetico(referencias), MargensComplementares{}))
	saida := salvarSubstituicao(t, doc)
	documentoNovo := lerParte(t, saida, "word/document.xml")

	require.NotEqual(t, documentoOriginal, documentoNovo, "pré-condição: o caso precisa produzir delta")

	assert.Equal(t, extrairTextosWT(t, documentoOriginal), extrairTextosWT(t, documentoNovo),
		"C4: a sequência de runes dos w:t é idêntica à original")
	assert.Equal(t, listarPartes(t, original), listarPartes(t, saida),
		"C4: as entradas do ZIP saem na mesma ordem")
	for _, nome := range listarPartes(t, original) {
		if nome == "word/document.xml" {
			continue
		}
		assert.Equal(t, lerParte(t, original, nome), lerParte(t, saida, nome),
			"C4: a única parte substituída é word/document.xml; %s precisa sair byte a byte igual", nome)
	}
}

// ---------------------------------------------------------------------------
// A15 — repetição: nil nas duas chamadas E bytes idênticos
// ---------------------------------------------------------------------------

func TestAplicarPlanoRepetidoEhNilDuasVezesEByteIdempotente(t *testing.T) {
	fonte := fixtureQuatroParagrafos()
	doc := abrirParaSubstituicao(t, docxSintetico(t, fonte))
	plano := planoSintetico([]int{1, 3})

	// Exigir nil nas DUAS chamadas é o que torna o caso não vacuoso quanto à
	// atomicidade: uma segunda chamada que FALHASSE também deixaria os bytes
	// iguais.
	require.NoError(t, doc.AplicarPlano(plano, MargensComplementares{}), "A15: primeira chamada")
	primeira := salvarSubstituicao(t, doc)
	documentoPrimeira := lerParte(t, primeira, "word/document.xml")

	// E exigir delta REAL na primeira é o que impede um AplicarPlano no-op de
	// satisfazer o critério: sem isto, "bytes iguais" seria verdade à toa.
	require.NotEqual(t, fonte, documentoPrimeira, "A15: a primeira chamada precisa produzir delta")
	require.Equal(t, "both", unicaPropriedade(t, propriedadesDoParagrafo(t, documentoPrimeira, 1), "jc")["val"],
		"A15: a primeira chamada precisa ter formatado o parágrafo alvo")

	require.NoError(t, doc.AplicarPlano(plano, MargensComplementares{}), "A15: segunda chamada também devolve nil")
	assert.Equal(t, primeira, salvarSubstituicao(t, doc), "A15: bytes idênticos depois da segunda chamada")
}

// ---------------------------------------------------------------------------
// A16 (parte de AplicarPlano) — texto do documento nunca entra no erro
// ---------------------------------------------------------------------------

const marcaVazamentoOOXML = "ZZMARCA-TEXTO-DO-USUARIO-7f3a9b-OOXML"

func TestAplicarPlanoNaoVazaTextoDoDocumentoNoErro(t *testing.T) {
	fonte := fixtureComNumPr(marcaVazamentoOOXML + " parágrafo confidencial")
	require.Contains(t, string(fonte), marcaVazamentoOOXML, "pré-condição: o texto do documento carrega a marca")

	doc := abrirParaSubstituicao(t, docxSintetico(t, fonte))
	err := doc.AplicarPlano(planoSintetico([]int{1}), MargensComplementares{})
	require.Error(t, err, "A16: o caso precisa falhar para haver erro a inspecionar")

	assert.NotContains(t, err.Error(), marcaVazamentoOOXML, "regra 7: Error() não cita conteúdo do documento")
	for _, formato := range []string{"%v", "%+v", "%#v"} {
		assert.NotContains(t, fmt.Sprintf(formato, err), marcaVazamentoOOXML,
			"regra 7: %s sobre o erro não pode expor conteúdo do documento", formato)
	}

	pendentes := []error{err}
	for len(pendentes) > 0 {
		atual := pendentes[len(pendentes)-1]
		pendentes = pendentes[:len(pendentes)-1]
		if atual == nil {
			continue
		}
		assert.NotContains(t, atual.Error(), marcaVazamentoOOXML, "regra 7: elo da cadeia de Unwrap cita conteúdo")
		if validacao, ok := atual.(*errors.ErroValidacao); ok { //nolint:errorlint // atual já é um elo desembrulhado da cadeia; errors.As pararia no primeiro match e deixaria elos sem conferir, que é justamente o que A16 precisa varrer.
			assert.NotContains(t, validacao.Mensagem, marcaVazamentoOOXML)
			for _, campo := range validacao.Campos {
				assert.NotContains(t, campo.Campo, marcaVazamentoOOXML, "regra 7: CampoInvalido.Campo cita conteúdo")
				assert.NotContains(t, campo.Mensagem, marcaVazamentoOOXML, "regra 7: CampoInvalido.Mensagem cita conteúdo")
				assert.NotContains(t, campo.String(), marcaVazamentoOOXML,
					"regra 7: CampoInvalido.String() é o que vai para razoes da resposta HTTP")
			}
		}
		switch desembrulhavel := atual.(type) { //nolint:errorlint // o type switch É o mecanismo de desembrulho desta varredura, não uma checagem de erro específico.
		case interface{ Unwrap() error }:
			pendentes = append(pendentes, desembrulhavel.Unwrap())
		case interface{ Unwrap() []error }:
			pendentes = append(pendentes, desembrulhavel.Unwrap()...)
		}
	}
}

// ---------------------------------------------------------------------------
// A17 — alinhamento de ordinais entre CDM e adaptador, ponta a ponta
// ---------------------------------------------------------------------------

func TestAplicarPlanoAlinhaOrdinaisDoCDMComOAdaptadorNoPacoteReal(t *testing.T) {
	original := lerFixture(t, "artigo-real-libreoffice.docx")
	doc := abrirParaSubstituicao(t, original)

	indice, referencias := referenciasDeParagrafoDoPacoteReal(t, doc)
	require.NoError(t, indice.Validar(), "pré-condição: o índice real precisa ser válido")

	definicao := definicaoDoPlanoSintetico()
	require.NoError(t, definicao.Validar(), "pré-condição: a definição sintética precisa ser válida")

	plano, err := formatador.Planejar(indice, definicao)
	require.NoError(t, err)
	require.Equal(t, referencias, plano.ReferenciasCorpo,
		"pré-condição: Planejar seleciona os mesmos ordinais que o CDM classificou como parágrafo")

	antes := xmlDosBlocosDeTopo(t, lerParte(t, original, "word/document.xml"))
	require.NoError(t, doc.AplicarPlano(plano, MargensComplementares{}))
	depois := xmlDosBlocosDeTopo(t, lerParte(t, salvarSubstituicao(t, doc), "word/document.xml"))

	require.Equal(t, len(antes), len(depois), "A17: nenhum bloco de topo aparece nem desaparece")

	var alterados []int
	for ordinal := range antes {
		if antes[ordinal] != depois[ordinal] {
			alterados = append(alterados, ordinal)
		}
	}
	sort.Ints(alterados)
	assert.Equal(t, plano.ReferenciasCorpo, alterados,
		"A17: os blocos alterados são exatamente os classificados como cdm.Paragrafo; a w:tbl intercalada não deslocou o alvo")
}

// definicaoDoPlanoSintetico espelha planoSintetico como ruleset.Definicao, para
// que A17 passe pela porta real (Planejar) em vez de montar o plano por literal.
func definicaoDoPlanoSintetico() ruleset.Definicao {
	return ruleset.Definicao{
		Slug:   "revista-sintetica",
		Versao: 1,
		Nome:   "Revista Sintética de Teste",
		Fonte:  "https://exemplo.test/diretrizes",
		Pagina: paginaSintetica(),
		Corpo:  corpoPlanoSintetico(),
	}
}
