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
//
// RECORTE f3-ruleset-campos-silentes: acrescenta A8, A9, A10, A12 e A13 (C4 e
// C5) — os cinco campos opcionais de ruleset.Corpo como ponteiro, e o mutador
// PULADO quando o campo é nil. Os casos herdados acima continuam valendo como
// o caminho "perfil que diz tudo" (corpoPlanoSintetico).
package ooxml

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/daniel-halos/formatador/internal/domain/cdm"
	"github.com/daniel-halos/formatador/internal/domain/formatador"
	"github.com/daniel-halos/formatador/internal/domain/ruleset"
	"github.com/daniel-halos/formatador/internal/infra/errors"
	infraruleset "github.com/daniel-halos/formatador/internal/infra/ruleset"
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

// corpoPlanoSintetico tem os CINCO campos opcionais PRESENTES: é o perfil
// institucional, que diz tudo. O perfil normativo silente é corpoSilente.
func corpoPlanoSintetico() ruleset.Corpo {
	return ruleset.Corpo{
		Fonte:          apontar("Times New Roman"),
		TamanhoPT:      12,
		Entrelinha:     1.5,
		RecuoCM:        apontar(1.25),
		EspacoAntesPT:  apontar(0.0),
		EspacoDepoisPT: apontar(6.0),
		Alinhamento:    apontar("justificado"),
	}
}

// corpoSilente é o corpo de um perfil que declara SÓ o que a norma define:
// tamanho (§5.1) e entrelinha (§5.2). Os cinco demais são nil — "a norma não
// diz, preserve o documento" (C1/C4).
func corpoSilente() ruleset.Corpo {
	return ruleset.Corpo{TamanhoPT: 12, Entrelinha: 1.5}
}

func planoSilencioso(referencias []int) formatador.Plano {
	return formatador.Plano{Pagina: paginaSintetica(), Corpo: corpoSilente(), ReferenciasCorpo: referencias}
}

// locaisDoPPr devolve os nomes locais dos filhos do w:pPr, na ordem do
// documento. É como se afere que um mutador PULADO não deixou rastro.
func locaisDoPPr(propriedades []propriedadeParagrafo) []string {
	locais := make([]string, 0, len(propriedades))
	for _, propriedade := range propriedades {
		locais = append(locais, propriedade.local)
	}
	return locais
}

// secaoUnica devolve os atributos do namespace w de um filho do único
// w:sectPr do corpo. Diferente de conferirPaginaSinteticaXML, aqui os números
// esperados são NOMEADOS pelo caso (A12).
func secaoUnica(t *testing.T, documentoXML []byte, local string) map[string]string {
	t.Helper()

	corpo, err := corpoXMLParagrafos(documentoXML)
	require.NoError(t, err)
	secoes := filhosW(corpo, "sectPr")
	require.Len(t, secoes, 1, "a fixture precisa ter exatamente um w:sectPr corrente")
	nos := filhosW(secoes[0], local)
	require.Len(t, nos, 1, "o w:sectPr precisa ter exatamente um w:%s", local)

	atributos := map[string]string{}
	for _, atributo := range nos[0].atributos {
		if atributo.namespace == namespaceWord {
			atributos[atributo.local] = atributo.valor
		}
	}
	return atributos
}

// propriedadesPorOrdinal indexa os filhos do w:pPr de cada ordinal informado.
func propriedadesPorOrdinal(t *testing.T, documentoXML []byte, ordinais []int) map[int][]propriedadeParagrafo {
	t.Helper()

	indexadas := make(map[int][]propriedadeParagrafo, len(ordinais))
	for _, ordinal := range ordinais {
		indexadas[ordinal] = propriedadesDoParagrafo(t, documentoXML, ordinal)
	}
	return indexadas
}

// conferirSomenteEntrelinhaMudouNoPPr exige que, em cada ordinal, o w:pPr saia
// IGUAL ao original exceto w:line e w:lineRule dentro de w:spacing. w:before,
// w:after, w:jc e qualquer outro filho ficam inalterados em valor.
func conferirSomenteEntrelinhaMudouNoPPr(t *testing.T, antes, depois map[int][]propriedadeParagrafo) {
	t.Helper()

	require.Equal(t, len(antes), len(depois))
	for ordinal, originais := range antes {
		novas := depois[ordinal]
		require.Equal(t, locaisDoPPr(originais), locaisDoPPr(novas),
			"os filhos do w:pPr do parágrafo %d não podem aparecer nem desaparecer", ordinal)
		for indice, original := range originais {
			esperados := map[string]string{}
			for chave, valor := range original.atributos {
				esperados[chave] = valor
			}
			if original.local == "spacing" {
				delete(esperados, "line")
				delete(esperados, "lineRule")
			}
			obtidos := map[string]string{}
			for chave, valor := range novas[indice].atributos {
				obtidos[chave] = valor
			}
			if original.local == "spacing" {
				assert.Equal(t, "360", novas[indice].atributos["line"], "entrelinha 1,5 no parágrafo %d", ordinal)
				assert.Equal(t, "auto", novas[indice].atributos["lineRule"], "lineRule no parágrafo %d", ordinal)
				delete(obtidos, "line")
				delete(obtidos, "lineRule")
			}
			assert.Equal(t, esperados, obtidos,
				"parágrafo %d: w:%s só pode diferir em w:line e w:lineRule", ordinal, original.local)
		}
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

// ---------------------------------------------------------------------------
// A8 — cinco campos nil sobre o pacote real: só página e entrelinha mudam
//
// Medido em artigo-real-libreoffice.docx: w:jc 58, w:spacing 57, w:before 57,
// w:after 57, w:ind 0, w:rFonts 0. Por isso a asserção NÃO é "nenhum w:jc" —
// o arquivo tem 58 — e sim "o w:pPr sai igual EXCETO w:line e w:lineRule".
// ---------------------------------------------------------------------------

func TestAplicarPlanoComCamposSilentesSoAplicaPaginaETipografiaDeTamanho(t *testing.T) {
	original := lerFixture(t, "artigo-real-libreoffice.docx")
	documentoOriginal := lerParte(t, original, "word/document.xml")

	require.Empty(t, tagsRFonts(documentoOriginal), "pré-condição medida: a fixture tem ZERO w:rFonts")
	require.NotContains(t, string(documentoOriginal), `w:left="1701"`,
		"pré-condição: a página do plano DIFERE da medida do arquivo")

	doc := abrirParaSubstituicao(t, original)
	_, referencias := referenciasDeParagrafoDoPacoteReal(t, doc)
	antes := propriedadesPorOrdinal(t, documentoOriginal, referencias)

	require.NoError(t, doc.AplicarPlano(planoSilencioso(referencias), MargensComplementares{}))
	saida := salvarSubstituicao(t, doc)
	documentoNovo := lerParte(t, saida, "word/document.xml")
	require.NotEqual(t, documentoOriginal, documentoNovo, "pré-condição: o caso precisa produzir delta")

	// Página do plano aplicada.
	conferirPaginaSinteticaXML(t, documentoNovo, nil)

	// Nos parágrafos alvo, só a entrelinha muda.
	conferirSomenteEntrelinhaMudouNoPPr(t, antes, propriedadesPorOrdinal(t, documentoNovo, referencias))

	// Nenhum w:ind criado: RecuoCM nil não chama AplicarRecuoPrimeiraLinha.
	require.Equal(t, 0, strings.Count(string(documentoOriginal), "<w:ind"), "pré-condição medida: w:ind 0 antes")
	assert.Equal(t, 0, strings.Count(string(documentoNovo), "<w:ind"), "A8: w:ind segue com contagem 0 depois")

	// Nenhum w:rFonts CRIADO: é o erro que C3 pode introduzir ao montar
	// valores["rFonts"] com fonte vazia e gravar ascii="".
	assert.Empty(t, tagsRFonts(documentoNovo), "A8: w:rFonts segue com contagem 0 depois")

	// O tamanho é normativo e continua sendo gravado.
	blocos := xmlDosBlocosDeTopo(t, documentoNovo)
	for _, ordinal := range referencias {
		assert.Contains(t, blocos[ordinal], `<w:sz w:val="24"/>`, "A8: w:sz no parágrafo %d", ordinal)
	}

	assert.Equal(t, extrairTextosWT(t, documentoOriginal), extrairTextosWT(t, documentoNovo))
	for _, nome := range listarPartes(t, original) {
		if nome != "word/document.xml" {
			assert.Equal(t, lerParte(t, original, nome), lerParte(t, saida, nome), "parte não alvo %s", nome)
		}
	}
}

// ---------------------------------------------------------------------------
// A9 — cada campo presente, um por vez, com os outros quatro nil
//
// Cinco casos, um TERMO cada: remover qualquer uma das cinco guardas de nil
// quebra exatamente um caso.
// ---------------------------------------------------------------------------

func TestAplicarPlanoRodaSomenteOMutadorDoCampoPresente(t *testing.T) {
	casos := []struct {
		nome     string
		definir  func(*ruleset.Corpo)
		conferir func(t *testing.T, documentoXML []byte, propriedades []propriedadeParagrafo)
	}{
		{"fonte", func(c *ruleset.Corpo) { c.Fonte = apontar("Times New Roman") },
			func(t *testing.T, documentoXML []byte, propriedades []propriedadeParagrafo) {
				tipografia := propriedadesTipografiaDireta(t, documentoXML)
				require.Len(t, tipografia, 1, "A9: só o run do parágrafo alvo ganha rPr direto")
				assert.Equal(t, map[string]string{
					"rFonts.ascii": "Times New Roman", "rFonts.hAnsi": "Times New Roman",
					"rFonts.eastAsia": "Times New Roman", "rFonts.cs": "Times New Roman",
					"sz.val": "24", "szCs.val": "24",
				}, tipografia[0], "A9: o mutador de tipografia rodou com família")
				require.Len(t, tagsRFonts(documentoXML), 1, "A9: um único w:rFonts no documento")
				assert.Equal(t, []string{"spacing"}, locaisDoPPr(propriedades))
			}},
		{"alinhamento", func(c *ruleset.Corpo) { c.Alinhamento = apontar("justificado") },
			func(t *testing.T, documentoXML []byte, propriedades []propriedadeParagrafo) {
				assert.Equal(t, "both", unicaPropriedade(t, propriedades, "jc")["val"])
				assert.ElementsMatch(t, []string{"spacing", "jc"}, locaisDoPPr(propriedades))
			}},
		{"recuo", func(c *ruleset.Corpo) { c.RecuoCM = apontar(1.25) },
			func(t *testing.T, documentoXML []byte, propriedades []propriedadeParagrafo) {
				assert.Equal(t, "709", unicaPropriedade(t, propriedades, "ind")["firstLine"])
				assert.ElementsMatch(t, []string{"spacing", "ind"}, locaisDoPPr(propriedades))
			}},
		{"espaco antes", func(c *ruleset.Corpo) { c.EspacoAntesPT = apontar(6.0) },
			func(t *testing.T, documentoXML []byte, propriedades []propriedadeParagrafo) {
				espacamento := unicaPropriedade(t, propriedades, "spacing")
				assert.Equal(t, "120", espacamento["before"])
				assert.NotContains(t, espacamento, "after", "A9: EspacoDepoisPT nil não grava w:after")
				assert.Equal(t, []string{"spacing"}, locaisDoPPr(propriedades))
			}},
		{"espaco depois", func(c *ruleset.Corpo) { c.EspacoDepoisPT = apontar(6.0) },
			func(t *testing.T, documentoXML []byte, propriedades []propriedadeParagrafo) {
				espacamento := unicaPropriedade(t, propriedades, "spacing")
				assert.Equal(t, "120", espacamento["after"])
				assert.NotContains(t, espacamento, "before", "A9: EspacoAntesPT nil não grava w:before")
				assert.Equal(t, []string{"spacing"}, locaisDoPPr(propriedades))
			}},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			plano := planoSilencioso([]int{1})
			caso.definir(&plano.Corpo)

			fonte := fixtureQuatroParagrafos()
			require.NotContains(t, string(fonte), "<w:pPr>", "pré-condição: os parágrafos da fixture não têm w:pPr")

			doc := abrirParaSubstituicao(t, docxSintetico(t, fonte))
			require.NoError(t, doc.AplicarPlano(plano, MargensComplementares{}))
			documentoNovo := lerParte(t, salvarSubstituicao(t, doc), "word/document.xml")

			propriedades := propriedadesDoParagrafo(t, documentoNovo, 1)
			// Entrelinha é normativa e roda sempre.
			espacamento := unicaPropriedade(t, propriedades, "spacing")
			assert.Equal(t, "360", espacamento["line"])
			assert.Equal(t, "auto", espacamento["lineRule"])
			// Tamanho é normativo e roda sempre; família só com Fonte presente.
			assert.Contains(t, string(documentoNovo), `<w:sz w:val="24"/>`)
			if caso.nome != "fonte" {
				assert.Empty(t, tagsRFonts(documentoNovo), "A9: Fonte nil não cria w:rFonts")
			}
			caso.conferir(t, documentoNovo, propriedades)

			assert.Equal(t, `<w:p><w:r><w:t>zero</w:t></w:r></w:p>`, xmlDosBlocosDeTopo(t, documentoNovo)[0],
				"A9: parágrafo fora da seleção não é tocado")
		})
	}
}

// ---------------------------------------------------------------------------
// A10 — três campos nil e dois presentes, um deles recusando por XML inválido
//
// "Todos os cinco presentes" não percorreria NENHUMA guarda de nil e portanto
// não poderia falhar por causa de C4.
// ---------------------------------------------------------------------------

func TestAplicarPlanoComCamposNilMantemAtomicidadeNaRecusa(t *testing.T) {
	pacote := docxSintetico(t, fixtureComNumPr("um"))
	require.Equal(t, pacote, salvarSubstituicao(t, abrirParaSubstituicao(t, pacote)),
		"pré-condição: sem delta algum, Salvar reproduz o pacote byte a byte")

	isolado := abrirParaSubstituicao(t, pacote)
	require.NoError(t, isolado.AplicarPagina(paginaSintetica(), MargensComplementares{}))
	require.NotEqual(t, pacote, salvarSubstituicao(t, isolado),
		"pré-condição: a página do plano produz delta nesta fixture")

	plano := planoSilencioso([]int{1})
	// DOIS presentes: o alinhamento aplica, o recuo RECUSA (w:numPr no alvo).
	plano.Corpo.Alinhamento = apontar("justificado")
	plano.Corpo.RecuoCM = apontar(1.25)
	require.Nil(t, plano.Corpo.Fonte, "pré-condição: três campos seguem nil")
	require.Nil(t, plano.Corpo.EspacoAntesPT)
	require.Nil(t, plano.Corpo.EspacoDepoisPT)

	doc := abrirParaSubstituicao(t, pacote)
	err := doc.AplicarPlano(plano, MargensComplementares{})

	var validacao *errors.ErroValidacao
	require.Error(t, err, "A10: o recuo precisa alcançar a guarda de w:numPr")
	require.True(t, errors.Como(err, &validacao), "A10: a recusa é *ErroValidacao; erro foi %T", err)
	assert.Equal(t, pacote, salvarSubstituicao(t, doc),
		"A10: nenhuma parte substituída no receptor; Salvar em destino novo reproduz o original byte a byte")
}

// ---------------------------------------------------------------------------
// A12 — ponta a ponta com o perfil abnt-14724, por Planejar + AplicarPlano
//
// Os números são NOMEADOS: "pgSz de A4" não é asserção e passaria com
// qualquer página.
// ---------------------------------------------------------------------------

func definicaoABNT14724(t *testing.T) ruleset.Definicao {
	t.Helper()

	caminho := filepath.Join("..", "..", "..", "rulesets", "abnt-14724", "v1.yaml")
	conteudo, err := os.ReadFile(caminho)
	require.NoError(t, err, "A12: o perfil %s precisa existir", caminho)

	definicao, err := infraruleset.Carregar(conteudo)
	require.NoError(t, err, "A12: o perfil normativo precisa carregar")
	require.Nil(t, definicao.Corpo.Fonte, "pré-condição: o perfil normativo é silente em fonte")
	require.Nil(t, definicao.Corpo.Alinhamento)
	require.Nil(t, definicao.Corpo.RecuoCM)
	require.Nil(t, definicao.Corpo.EspacoAntesPT)
	require.Nil(t, definicao.Corpo.EspacoDepoisPT)
	return definicao
}

func TestAplicarPlanoComPerfilABNT14724NoPacoteReal(t *testing.T) {
	original := lerFixture(t, "artigo-real-libreoffice.docx")
	documentoOriginal := lerParte(t, original, "word/document.xml")
	require.NotContains(t, string(documentoOriginal), `w:left="1701"`,
		"pré-condição: a página do perfil DIFERE da medida do arquivo")

	doc := abrirParaSubstituicao(t, original)
	indice, referencias := referenciasDeParagrafoDoPacoteReal(t, doc)
	require.NoError(t, indice.Validar())
	antes := propriedadesPorOrdinal(t, documentoOriginal, referencias)

	plano, err := formatador.Planejar(indice, definicaoABNT14724(t))
	require.NoError(t, err)
	require.Equal(t, referencias, plano.ReferenciasCorpo)

	require.NoError(t, doc.AplicarPlano(plano, MargensComplementares{}))
	documentoNovo := lerParte(t, salvarSubstituicao(t, doc), "word/document.xml")

	// §5.1 — A4 e margens do anverso, em twips por math.Round.
	assert.Equal(t, "11906", secaoUnica(t, documentoNovo, "pgSz")["w"], "A12: 21,0 cm em twips")
	assert.Equal(t, "16838", secaoUnica(t, documentoNovo, "pgSz")["h"], "A12: 29,7 cm em twips")
	margens := secaoUnica(t, documentoNovo, "pgMar")
	assert.Equal(t, "1701", margens["top"], "A12: 3 cm")
	assert.Equal(t, "1701", margens["left"], "A12: 3 cm")
	assert.Equal(t, "1134", margens["bottom"], "A12: 2 cm")
	assert.Equal(t, "1134", margens["right"], "A12: 2 cm")

	// §5.2 — entrelinha 1,5; §5.1 — corpo 12 em meios-pontos.
	blocos := xmlDosBlocosDeTopo(t, documentoNovo)
	for _, ordinal := range referencias {
		espacamento := unicaPropriedade(t, propriedadesDoParagrafo(t, documentoNovo, ordinal), "spacing")
		assert.Equal(t, "360", espacamento["line"])
		assert.Equal(t, "auto", espacamento["lineRule"])
		assert.Contains(t, blocos[ordinal], `<w:sz w:val="24"/>`, "A12: w:sz no parágrafo %d", ordinal)
	}

	// O que a norma NÃO diz continua como estava.
	assert.Equal(t, 0, strings.Count(string(documentoNovo), "<w:ind"), "A12: w:ind contagem 0 antes e depois")
	assert.Empty(t, tagsRFonts(documentoNovo), "A12: w:rFonts contagem 0 antes e depois")
	conferirSomenteEntrelinhaMudouNoPPr(t, antes, propriedadesPorOrdinal(t, documentoNovo, referencias))

	assert.Equal(t, extrairTextosWT(t, documentoOriginal), extrairTextosWT(t, documentoNovo))
}

// ---------------------------------------------------------------------------
// A13 — ponteiro-para-0 versus nil, no XML
//
// artigo-desformatado.docx tem w:before contagem 0 (medido): é o que torna
// visível a gravação de before="0" explícito. É aqui que ponteiro-para-0 e nil
// se tornam DISTINGUÍVEIS — em Validar() os dois devolvem nil (A2).
// ---------------------------------------------------------------------------

func TestAplicarPlanoDistingueZeroExplicitoDeCampoSilente(t *testing.T) {
	original := lerFixture(t, "artigo-desformatado.docx")
	documentoOriginal := lerParte(t, original, "word/document.xml")

	require.Equal(t, 0, strings.Count(string(documentoOriginal), "w:before="),
		"pré-condição medida: a fixture não tem w:before")
	require.Equal(t, 0, strings.Count(string(documentoOriginal), "<w:ind"),
		"pré-condição medida: a fixture não tem w:ind")

	referencias := referenciasDeParagrafoDaFixture(t, original, ordinaisDesformatadoComRPrForaDeOrdem)
	antes := propriedadesPorOrdinal(t, documentoOriginal, referencias)

	t.Run("ponteiro para zero grava o zero", func(t *testing.T) {
		plano := planoSilencioso(referencias)
		plano.Corpo.EspacoAntesPT = apontar(0.0)
		plano.Corpo.RecuoCM = apontar(0.0)

		doc := abrirParaSubstituicao(t, original)
		require.NoError(t, doc.AplicarPlano(plano, MargensComplementares{}))
		documentoNovo := lerParte(t, salvarSubstituicao(t, doc), "word/document.xml")

		for _, ordinal := range referencias {
			propriedades := propriedadesDoParagrafo(t, documentoNovo, ordinal)
			assert.Equal(t, "0", unicaPropriedade(t, propriedades, "spacing")["before"],
				"A13: w:before=\"0\" é GRAVADO no parágrafo %d", ordinal)
			assert.Equal(t, "0", unicaPropriedade(t, propriedades, "ind")["firstLine"],
				"A13: w:ind/@w:firstLine=\"0\" é GRAVADO no parágrafo %d", ordinal)
		}
	})

	t.Run("nil nao cria nenhum dos dois", func(t *testing.T) {
		doc := abrirParaSubstituicao(t, original)
		require.NoError(t, doc.AplicarPlano(planoSilencioso(referencias), MargensComplementares{}))
		documentoNovo := lerParte(t, salvarSubstituicao(t, doc), "word/document.xml")

		require.NotEqual(t, documentoOriginal, documentoNovo, "pré-condição: o caso precisa produzir delta")
		assert.Equal(t, 0, strings.Count(string(documentoNovo), "w:before="),
			"A13: com EspacoAntesPT nil nenhum w:before é criado")
		assert.Equal(t, 0, strings.Count(string(documentoNovo), "<w:ind"),
			"A13: com RecuoCM nil nenhum w:ind é criado")
		conferirSomenteEntrelinhaMudouNoPPr(t, antes, propriedadesPorOrdinal(t, documentoNovo, referencias))
	})
}
