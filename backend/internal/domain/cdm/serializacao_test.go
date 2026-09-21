// Este arquivo testa a serialização do CDM (F2): o envelope que vai para a
// coluna cdm_jsonb — Indice{Versao, Blocos} — e o round-trip
// Serializar/Desserializar que precisa devolver exatamente os blocos que
// entraram, campos privados de Papel inclusive.
//
// FASE VERMELHA: serializacao.go ainda não existe. Este arquivo prova que o
// pacote não compila por VersaoFormatoCDM, Indice, NovoIndice, Serializar e
// Desserializar estarem indefinidos — não por erro de sintaxe ou de import.
//
// Decisões de desenho tomadas aqui (o contrato do investigador deixou em
// aberto, "Escolha os nomes de campo e documente a escolha"):
//
//  1. Papel.UnmarshalJSON VALIDA a si mesmo: nome desconhecido e nível de
//     Secao fora de 1..NivelSecaoMaximo são recusados ALI, devolvendo
//     *errors.ErroValidacao com campo "papel" — não só quando o Papel está
//     dentro de um Bloco. Isso satisfaz a regra 1 do contrato como uma
//     garantia independente de Papel, testável isoladamente, e continua
//     verdadeiro (redundantemente, sem custo) quando o Papel está aninhado
//     num bloco do envelope.
//  2. Origem NÃO tem MarshalJSON/UnmarshalJSON próprios (a lista de símbolos
//     do contrato não os pede): uma origem desconhecida decodifica sem erro
//     estrutural e só é recusada quando Desserializar chama cdm.NovoBloco
//     para cada bloco (regra 3, "a invariante central do recorte"). Por
//     isso o campo devolvido para origem inválida é exatamente "origem" — o
//     MESMO nome que NovoBloco já usa, reaproveitado, não reinventado.
//  3. Confiança fora de [0,1] e RefXML negativo também só são pegos por essa
//     mesma chamada a NovoBloco, com os campos "confianca" e "ref_xml" que
//     NovoBloco já produz.
//  4. Nenhum campo de bloco é prefixado com o índice na lista
//     ("blocos[2].papel"): Desserializar para no primeiro bloco inválido
//     (mesma postura de falha rápida do resto do domínio — nenhum outro
//     lugar do projeto indexa elemento de lista em erro, ver
//     erroLinhaCorrompida em internal/data/postgres/documento.go) e reusa o
//     VOCABULÁRIO de campo que o chamador já conhece de NovoBloco, em vez de
//     inventar um dialeto próprio só para este caminho.
//  5. JSON estruturalmente malformado no ENVELOPE (sintaxe inválida, ou um
//     valor JSON válido mas de forma errada — array, string, número no lugar
//     de objeto — inclusive um campo de bloco com TIPO errado, como
//     confianca como string) usa o campo "indice": é o nome do próprio tipo
//     que Desserializar tenta produzir, e não há um campo mais específico e
//     de bom senso para apontar quando a estrutura inteira não bate.
//  6. versao ausente ou diferente de VersaoFormatoCDM usa o campo "versao" e
//     é verificada ANTES de percorrer os blocos: ler um formato futuro (ou
//     um objeto qualquer que só por acaso tem a chave "blocos") como se
//     fosse o atual é pior que falhar cedo.
package cdm

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// serializarEDesserializar roda o par completo e falha o teste (via require)
// se qualquer etapa devolver erro — usado pelos casos que testam o
// CONTEÚDO do round-trip, não a falha dele.
func serializarEDesserializar(t *testing.T, blocos []Bloco) Indice {
	t.Helper()

	entrada := NovoIndice(blocos)
	dados, err := entrada.Serializar()
	require.NoError(t, err)

	saida, err := Desserializar(dados)
	require.NoError(t, err)
	return saida
}

// decodificarEnvelopeGenerico decodifica o resultado de Serializar sem
// depender de nenhum tipo do pacote: prova o FORMATO em bruto (as chaves
// JSON snake_case), não o que Desserializar entende delas.
func decodificarEnvelopeGenerico(t *testing.T, dados []byte) (versao float64, blocos []map[string]any) {
	t.Helper()

	var bruto map[string]any
	require.NoError(t, json.Unmarshal(dados, &bruto))

	versaoRaw, temVersao := bruto["versao"]
	require.True(t, temVersao, "o envelope precisa ter a chave \"versao\"")
	versao, ok := versaoRaw.(float64)
	require.True(t, ok, "\"versao\" precisa ser um número JSON")

	blocosRaw, temBlocos := bruto["blocos"]
	require.True(t, temBlocos, "o envelope precisa ter a chave \"blocos\"")
	lista, ok := blocosRaw.([]any)
	require.True(t, ok, "\"blocos\" precisa ser um array JSON")

	blocos = make([]map[string]any, len(lista))
	for i, item := range lista {
		m, ok := item.(map[string]any)
		require.True(t, ok, "cada bloco precisa ser um objeto JSON")
		blocos[i] = m
	}
	return versao, blocos
}

// ---------------------------------------------------------------------------
// VersaoFormatoCDM e NovoIndice
// ---------------------------------------------------------------------------

func TestVersaoFormatoCDMEhUm(t *testing.T) {
	t.Parallel()

	// Trava o valor literal: um teste que só checa "> 0" não pegaria alguém
	// mudando a versão inicial do formato por engano numa refatoração.
	assert.Equal(t, 1, VersaoFormatoCDM)
}

func TestNovoIndiceMontaEnvelopeComAVersaoAtual(t *testing.T) {
	t.Parallel()

	blocos := []Bloco{
		deveNovoBloco(Titulo, "Título", 0.95, OrigemEstiloDocx, 0),
		deveNovoBloco(Paragrafo, "texto", 0.4, OrigemHeuristica, 1),
	}

	indice := NovoIndice(blocos)

	assert.Equal(t, VersaoFormatoCDM, indice.Versao)
	assert.Equal(t, blocos, indice.Blocos)
}

func TestNovoIndiceComBlocosNilProduzIndiceValido(t *testing.T) {
	t.Parallel()

	indice := NovoIndice(nil)

	assert.Equal(t, VersaoFormatoCDM, indice.Versao)
	assert.Empty(t, indice.Blocos)
}

// ---------------------------------------------------------------------------
// Papel.MarshalJSON — formato do objeto e omissão de nível zero
// ---------------------------------------------------------------------------

func TestPapelMarshalJSONFormatoPorPapel(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome      string
		papel     Papel
		nomeJSON  string
		temNivel  bool
		nivelJSON float64
	}{
		{nome: "titulo não carrega nivel", papel: Titulo, nomeJSON: "titulo", temNivel: false},
		{nome: "lista de autores não carrega nivel", papel: ListaAutores, nomeJSON: "lista_autores", temNivel: false},
		{nome: "resumo não carrega nivel", papel: Resumo, nomeJSON: "resumo", temNivel: false},
		{nome: "palavras-chave não carrega nivel", papel: PalavrasChave, nomeJSON: "palavras_chave", temNivel: false},
		{nome: "paragrafo não carrega nivel", papel: Paragrafo, nomeJSON: "paragrafo", temNivel: false},
		{nome: "citacao não carrega nivel", papel: Citacao, nomeJSON: "citacao", temNivel: false},
		{nome: "item de lista não carrega nivel", papel: ItemLista, nomeJSON: "item_lista", temNivel: false},
		{nome: "tabela não carrega nivel", papel: Tabela, nomeJSON: "tabela", temNivel: false},
		{nome: "figura não carrega nivel", papel: Figura, nomeJSON: "figura", temNivel: false},
		{nome: "legenda não carrega nivel", papel: Legenda, nomeJSON: "legenda", temNivel: false},
		{nome: "equacao não carrega nivel", papel: Equacao, nomeJSON: "equacao", temNivel: false},
		{nome: "referencia não carrega nivel", papel: Referencia, nomeJSON: "referencia", temNivel: false},
		{nome: "nota de rodapé não carrega nivel", papel: NotaRodape, nomeJSON: "nota_rodape", temNivel: false},
		{nome: "secao nível 1 carrega o nivel", papel: Secao(1), nomeJSON: "secao", temNivel: true, nivelJSON: 1},
		{nome: "secao nível 3 carrega o nivel", papel: Secao(3), nomeJSON: "secao", temNivel: true, nivelJSON: 3},
		{nome: "secao nível 6 carrega o nivel", papel: Secao(6), nomeJSON: "secao", temNivel: true, nivelJSON: 6},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			dados, err := caso.papel.MarshalJSON()
			require.NoError(t, err)

			var decodificado map[string]any
			require.NoError(t, json.Unmarshal(dados, &decodificado))

			assert.Equal(t, caso.nomeJSON, decodificado["nome"])

			nivel, temNivel := decodificado["nivel"]
			if caso.temNivel {
				assert.True(t, temNivel, "papel de seção precisa serializar o campo \"nivel\"")
				assert.Equal(t, caso.nivelJSON, nivel)
				return
			}
			assert.False(t, temNivel, "papel sem hierarquia não pode serializar \"nivel\": nível zero é omitido")
		})
	}
}

// ---------------------------------------------------------------------------
// Papel.UnmarshalJSON — round-trip e as recusas próprias de Papel (regra 1)
// ---------------------------------------------------------------------------

func TestPapelUnmarshalJSONRoundTripTodosOsPapeisConhecidos(t *testing.T) {
	t.Parallel()

	papeis := []Papel{
		Titulo, ListaAutores, Resumo, PalavrasChave, Paragrafo, Citacao,
		ItemLista, Tabela, Figura, Legenda, Equacao, Referencia, NotaRodape,
		Secao(1), Secao(2), Secao(3), Secao(4), Secao(5), Secao(6),
	}

	for _, original := range papeis {
		original := original
		t.Run(original.String(), func(t *testing.T) {
			t.Parallel()

			dados, err := original.MarshalJSON()
			require.NoError(t, err)

			var voltou Papel
			require.NoError(t, voltou.UnmarshalJSON(dados))

			assert.Equal(t, original, voltou, "campos privados (nome, nivel) inclusive")
			assert.Equal(t, original.Nivel(), voltou.Nivel())
		})
	}
}

func TestPapelUnmarshalJSONRecusaNomeDesconhecido(t *testing.T) {
	t.Parallel()

	var papel Papel
	err := papel.UnmarshalJSON([]byte(`{"nome":"conclusao"}`))

	require.Error(t, err)
	exigirCampo(t, err, "papel")
}

func TestPapelUnmarshalJSONRecusaSecaoForaDaFaixa(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome string
		json string
	}{
		{nome: "nivel 0", json: `{"nome":"secao","nivel":0}`},
		{nome: "nivel 7", json: `{"nome":"secao","nivel":7}`},
		{nome: "nivel negativo", json: `{"nome":"secao","nivel":-1}`},
	}

	for _, caso := range casos {
		caso := caso
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			var papel Papel
			err := papel.UnmarshalJSON([]byte(caso.json))

			require.Error(t, err)
			exigirCampo(t, err, "papel")
		})
	}
}

func TestPapelUnmarshalJSONRecusaJSONMalformado(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome string
		json string
	}{
		{nome: "papel é uma string, não objeto", json: `"secao"`},
		{nome: "papel é um número, não objeto", json: `7`},
		{nome: "papel é um array, não objeto", json: `["secao"]`},
		{nome: "sintaxe inválida", json: `{"nome":`},
		{nome: "nivel com tipo errado", json: `{"nome":"secao","nivel":"um"}`},
	}

	for _, caso := range casos {
		caso := caso
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			var papel Papel
			err := papel.UnmarshalJSON([]byte(caso.json))

			require.Error(t, err)

			var invalido *errors.ErroValidacao
			assert.True(t, errors.Como(err, &invalido),
				"JSON malformado precisa virar *errors.ErroValidacao, obteve %T (%v)", err, err)
		})
	}
}

// ---------------------------------------------------------------------------
// Indice.Serializar — chaves snake_case do envelope e de cada bloco
// ---------------------------------------------------------------------------

func TestIndiceSerializarProduzChavesSnakeCase(t *testing.T) {
	t.Parallel()

	blocos := []Bloco{
		deveNovoBloco(Secao(2), "2 Metodologia", 0.9, OrigemEstiloDocx, 5),
	}
	indice := NovoIndice(blocos)

	dados, err := indice.Serializar()
	require.NoError(t, err)

	versao, blocosDecodificados := decodificarEnvelopeGenerico(t, dados)
	assert.Equal(t, float64(VersaoFormatoCDM), versao)
	require.Len(t, blocosDecodificados, 1)

	bloco := blocosDecodificados[0]
	for _, chave := range []string{"papel", "texto_resumo", "confianca", "origem", "ref_xml"} {
		_, existe := bloco[chave]
		assert.Truef(t, existe, "bloco serializado precisa ter a chave %q", chave)
	}

	papel, ok := bloco["papel"].(map[string]any)
	require.True(t, ok, "\"papel\" precisa ser um objeto JSON")
	assert.Equal(t, "secao", papel["nome"])
	assert.Equal(t, float64(2), papel["nivel"])

	assert.Equal(t, "2 Metodologia", bloco["texto_resumo"])
	assert.Equal(t, 0.9, bloco["confianca"])
	assert.Equal(t, "estilo-docx", bloco["origem"])
	assert.Equal(t, float64(5), bloco["ref_xml"])
}

func TestIndiceSerializarOmiteNivelQuandoPapelNaoEhSecao(t *testing.T) {
	t.Parallel()

	blocos := []Bloco{
		deveNovoBloco(Paragrafo, "um parágrafo qualquer", 0.3, OrigemHeuristica, 0),
	}
	dados, err := NovoIndice(blocos).Serializar()
	require.NoError(t, err)

	_, blocosDecodificados := decodificarEnvelopeGenerico(t, dados)
	require.Len(t, blocosDecodificados, 1)

	papel, ok := blocosDecodificados[0]["papel"].(map[string]any)
	require.True(t, ok)
	_, temNivel := papel["nivel"]
	assert.False(t, temNivel, "papel sem hierarquia não pode ter \"nivel\" no envelope")
}

// ---------------------------------------------------------------------------
// Round-trip fiel — a invariante central do recorte
// ---------------------------------------------------------------------------

func TestDesserializarRoundTripTodosOsPapeisETodasAsOrigens(t *testing.T) {
	t.Parallel()

	papeis := []Papel{
		Titulo, ListaAutores, Resumo, PalavrasChave, Paragrafo, Citacao,
		ItemLista, Tabela, Figura, Legenda, Equacao, Referencia, NotaRodape,
		Secao(1), Secao(2), Secao(3), Secao(4), Secao(5), Secao(6),
	}
	origens := []Origem{OrigemEstiloDocx, OrigemHeuristica, OrigemLLM, OrigemUsuario}

	entrada := make([]Bloco, 0, len(papeis))
	for i, papel := range papeis {
		origem := origens[i%len(origens)]
		entrada = append(entrada, deveNovoBloco(papel, "texto de teste", 0.42, origem, i))
	}

	saida := serializarEDesserializar(t, entrada)

	require.Len(t, saida.Blocos, len(entrada))
	for i := range entrada {
		assert.Truef(t, entrada[i] == saida.Blocos[i],
			"bloco %d (papel %q) não sobreviveu ao round-trip: entrada=%+v saida=%+v",
			i, entrada[i].Papel.String(), entrada[i], saida.Blocos[i])
	}
}

func TestDesserializarOrigemUsuarioSobreviveAoRoundTrip(t *testing.T) {
	t.Parallel()

	// A falha mais cara deste recorte: se OrigemUsuario se perdesse na volta
	// do banco, a proteção de Bloco.Reclassificar (correção manual nunca
	// sobrescrita por reclassificação automática) deixaria de valer depois
	// de QUALQUER load — o usuário corrigiria de novo, para sempre.
	corrigidoPeloUsuario := deveNovoBloco(Resumo, "texto que o usuário classificou", 1.0, OrigemUsuario, 12)

	saida := serializarEDesserializar(t, []Bloco{corrigidoPeloUsuario})

	require.Len(t, saida.Blocos, 1)
	assert.Equal(t, OrigemUsuario, saida.Blocos[0].Origem)
	assert.True(t, corrigidoPeloUsuario == saida.Blocos[0])
}

func TestDesserializarConfiancaNosLimitesSobrevive(t *testing.T) {
	t.Parallel()

	entrada := []Bloco{
		deveNovoBloco(Paragrafo, "confiança mínima", 0, OrigemHeuristica, 0),
		deveNovoBloco(Paragrafo, "confiança máxima", 1, OrigemEstiloDocx, 1),
	}

	saida := serializarEDesserializar(t, entrada)

	require.Len(t, saida.Blocos, 2)
	assert.Equal(t, 0.0, saida.Blocos[0].Confianca)
	assert.Equal(t, 1.0, saida.Blocos[1].Confianca)
}

func TestDesserializarRefXMLZeroSobreviveENaoEhConfundidoComAusente(t *testing.T) {
	t.Parallel()

	entrada := []Bloco{deveNovoBloco(Titulo, "primeiro bloco do documento", 0.95, OrigemEstiloDocx, 0)}

	saida := serializarEDesserializar(t, entrada)

	require.Len(t, saida.Blocos, 1)
	assert.Equal(t, 0, saida.Blocos[0].RefXML, "RefXML 0 é o primeiro bloco, não \"campo vazio\"")
}

func TestDesserializarTextoResumoVazioSobrevive(t *testing.T) {
	t.Parallel()

	entrada := []Bloco{deveNovoBloco(Paragrafo, "", 0.4, OrigemHeuristica, 3)}

	saida := serializarEDesserializar(t, entrada)

	require.Len(t, saida.Blocos, 1)
	assert.Equal(t, "", saida.Blocos[0].TextoResumo)
}

func TestDesserializarTextoComCaractereForaDoBMPEAspasEBarraInvertidaSobrevive(t *testing.T) {
	t.Parallel()

	// 😀 é U+1F600, fora do BMP; a citação embutida com aspas e barra
	// invertida é o tipo de texto que quebra um escapador de JSON malfeito.
	texto := "emoji \U0001F600 e uma citação com \"aspas\" e \\ barra invertida"
	entrada := []Bloco{deveNovoBloco(Citacao, texto, 0.6, OrigemLLM, 7)}

	saida := serializarEDesserializar(t, entrada)

	require.Len(t, saida.Blocos, 1)
	assert.Equal(t, []rune(texto), []rune(saida.Blocos[0].TextoResumo))
}

func TestDesserializarTextoComAcentuacaoNFDSobrevive(t *testing.T) {
	t.Parallel()

	// "e" (U+0065) seguido do acento agudo combinante (U+0301) — forma
	// DECOMPOSTA (NFD), não o "é" pré-composto (U+00E9, NFC). Nenhum dos
	// dois pode ser normalizado silenciosamente: são sequências de runas
	// DIFERENTES, mesmo que se pareçam.
	texto := "descrição em forma decomposta"
	entrada := []Bloco{deveNovoBloco(Paragrafo, texto, 0.3, OrigemHeuristica, 2)}

	saida := serializarEDesserializar(t, entrada)

	require.Len(t, saida.Blocos, 1)
	assert.Equal(t, []rune(texto), []rune(saida.Blocos[0].TextoResumo),
		"forma de normalização Unicode não pode mudar: NFD precisa voltar NFD, não virar NFC")
}

func TestDesserializarFatiaDeBlocosVaziaENilProduzemJSONValidoQueVoltaSemQuebrar(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome   string
		blocos []Bloco
	}{
		{nome: "nil", blocos: nil},
		{nome: "fatia vazia não-nil", blocos: []Bloco{}},
	}

	for _, caso := range casos {
		caso := caso
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			saida := serializarEDesserializar(t, caso.blocos)

			assert.Equal(t, VersaoFormatoCDM, saida.Versao)
			assert.Empty(t, saida.Blocos)
		})
	}
}

// ---------------------------------------------------------------------------
// Desserializar — recusa de versão
// ---------------------------------------------------------------------------

func TestDesserializarRecusaVersaoDiferenteDeVersaoFormatoCDM(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome string
		json string
	}{
		{nome: "versao ausente (zero-value do campo)", json: `{"blocos":[]}`},
		{nome: "versao explicitamente 0", json: `{"versao":0,"blocos":[]}`},
		{nome: "versao futura", json: `{"versao":2,"blocos":[]}`},
		{nome: "versao negativa", json: `{"versao":-1,"blocos":[]}`},
	}

	for _, caso := range casos {
		caso := caso
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			_, err := Desserializar([]byte(caso.json))

			require.Error(t, err)
			exigirCampo(t, err, "versao")
		})
	}
}

// ---------------------------------------------------------------------------
// Desserializar — JSON malformado no envelope (regra 5)
// ---------------------------------------------------------------------------

func TestDesserializarRecusaJSONMalformado(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome  string
		dados []byte
	}{
		{nome: "bytes nil", dados: nil},
		{nome: "bytes vazios", dados: []byte{}},
		{nome: "texto que não é json", dados: []byte("isso não é json")},
		{nome: "json é um array, não um objeto", dados: []byte(`["a","b"]`)},
		{nome: "json é uma string, não um objeto", dados: []byte(`"apenas uma string"`)},
		{nome: "json é um número, não um objeto", dados: []byte(`42`)},
		{nome: "json truncado", dados: []byte(`{"versao":1,"blocos":[`)},
		{nome: "confianca de um bloco com tipo errado", dados: []byte(
			`{"versao":1,"blocos":[{"papel":{"nome":"paragrafo"},"texto_resumo":"x","confianca":"alta","origem":"heuristica","ref_xml":0}]}`)},
	}

	for _, caso := range casos {
		caso := caso
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			_, err := Desserializar(caso.dados)

			require.Error(t, err)
			var invalido *errors.ErroValidacao
			assert.True(t, errors.Como(err, &invalido),
				"JSON malformado precisa virar *errors.ErroValidacao, obteve %T (%v)", err, err)
		})
	}
}

// ---------------------------------------------------------------------------
// Desserializar — a invariante central: nenhum Bloco que NovoBloco recusaria
// (regra 3)
// ---------------------------------------------------------------------------

func TestDesserializarRecusaBlocoComPapelDeNomeDesconhecido(t *testing.T) {
	t.Parallel()

	dados := []byte(`{"versao":1,"blocos":[{"papel":{"nome":"conclusao"},"texto_resumo":"x","confianca":0.5,"origem":"heuristica","ref_xml":0}]}`)

	_, err := Desserializar(dados)

	require.Error(t, err)
	exigirCampo(t, err, "papel")
}

func TestDesserializarRecusaBlocoComSecaoForaDaFaixa(t *testing.T) {
	t.Parallel()

	dados := []byte(`{"versao":1,"blocos":[{"papel":{"nome":"secao","nivel":9},"texto_resumo":"x","confianca":0.5,"origem":"heuristica","ref_xml":0}]}`)

	_, err := Desserializar(dados)

	require.Error(t, err)
	exigirCampo(t, err, "papel")
}

func TestDesserializarRecusaBlocoComOrigemDesconhecida(t *testing.T) {
	t.Parallel()

	dados := []byte(`{"versao":1,"blocos":[{"papel":{"nome":"paragrafo"},"texto_resumo":"x","confianca":0.5,"origem":"sistema-externo","ref_xml":0}]}`)

	_, err := Desserializar(dados)

	require.Error(t, err)
	exigirCampo(t, err, "origem")
}

func TestDesserializarRecusaBlocoComConfiancaForaDaFaixa(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome      string
		confianca string
	}{
		{nome: "abaixo de zero", confianca: "-0.1"},
		{nome: "acima de um", confianca: "1.1"},
	}

	for _, caso := range casos {
		caso := caso
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			dados := []byte(`{"versao":1,"blocos":[{"papel":{"nome":"paragrafo"},"texto_resumo":"x","confianca":` +
				caso.confianca + `,"origem":"heuristica","ref_xml":0}]}`)

			_, err := Desserializar(dados)

			require.Error(t, err)
			exigirCampo(t, err, "confianca")
		})
	}
}

func TestDesserializarRecusaBlocoComRefXMLNegativo(t *testing.T) {
	t.Parallel()

	dados := []byte(`{"versao":1,"blocos":[{"papel":{"nome":"paragrafo"},"texto_resumo":"x","confianca":0.5,"origem":"heuristica","ref_xml":-1}]}`)

	_, err := Desserializar(dados)

	require.Error(t, err)
	exigirCampo(t, err, "ref_xml")
}

// TestDesserializarNuncaProduzBlocoQueNovoBlocoRecusaria reforça a garantia,
// como asserção direta contra o domínio, não só contra o nome do campo:
// mesmo que a mensagem ou o campo mudem de nome no futuro, este teste
// continua provando a invariante central do recorte.
func TestDesserializarNuncaProduzBlocoQueNovoBlocoRecusaria(t *testing.T) {
	t.Parallel()

	entradasInvalidas := [][]byte{
		[]byte(`{"versao":1,"blocos":[{"papel":{"nome":"inexistente"},"texto_resumo":"x","confianca":0.5,"origem":"heuristica","ref_xml":0}]}`),
		[]byte(`{"versao":1,"blocos":[{"papel":{"nome":"paragrafo"},"texto_resumo":"x","confianca":2,"origem":"heuristica","ref_xml":0}]}`),
		[]byte(`{"versao":1,"blocos":[{"papel":{"nome":"paragrafo"},"texto_resumo":"x","confianca":0.5,"origem":"bugado","ref_xml":0}]}`),
		[]byte(`{"versao":1,"blocos":[{"papel":{"nome":"paragrafo"},"texto_resumo":"x","confianca":0.5,"origem":"heuristica","ref_xml":-5}]}`),
	}

	for _, dados := range entradasInvalidas {
		indice, err := Desserializar(dados)

		require.Error(t, err, "entrada %s precisava ser recusada", dados)
		assert.Equal(t, Indice{}, indice, "em erro, Desserializar devolve o zero-value, nunca um índice parcial")

		var invalido *errors.ErroValidacao
		require.True(t, errors.Como(err, &invalido))
	}
}

// TestDesserializarNaoEcoaConteudoRecebidoNaMensagemDeErro trava a regra 7 do
// CLAUDE.md num caminho que é fácil de violar sem perceber.
//
// A mensagem destes erros não morre na resposta HTTP: quem lê do banco
// reclassifica com erroLinhaCorrompida (internal/data/postgres/documento.go),
// que EMBUTE err.Error() no erro de aplicação — e esse texto vai para o log.
// Interpolar o JSON recebido na mensagem, ou repassar a do encoding/json (que
// cita o trecho que falhou), colocaria texto do documento do usuário no log
// pela porta dos fundos.
func TestDesserializarNaoEcoaConteudoRecebidoNaMensagemDeErro(t *testing.T) {
	t.Parallel()

	const segredo = "TRECHO-CONFIDENCIAL-DO-ARTIGO-DO-USUARIO-4f1b"

	casos := []struct {
		nome  string
		dados string
	}{
		{nome: "papel desconhecido carregando texto do usuário",
			dados: `{"versao":1,"blocos":[{"papel":{"nome":"` + segredo + `"},"texto_resumo":"` + segredo + `","confianca":0.5,"origem":"heuristica","ref_xml":0}]}`},
		{nome: "origem desconhecida carregando texto do usuário",
			dados: `{"versao":1,"blocos":[{"papel":{"nome":"paragrafo"},"texto_resumo":"` + segredo + `","confianca":0.5,"origem":"` + segredo + `","ref_xml":0}]}`},
		{nome: "confiança fora da faixa carregando texto do usuário",
			dados: `{"versao":1,"blocos":[{"papel":{"nome":"paragrafo"},"texto_resumo":"` + segredo + `","confianca":9,"origem":"heuristica","ref_xml":0}]}`},
		{nome: "json truncado no meio do texto do usuário",
			dados: `{"versao":1,"blocos":[{"papel":{"nome":"paragrafo"},"texto_resumo":"` + segredo},
		{nome: "tipo errado num campo, com o texto do usuário ao lado",
			dados: `{"versao":1,"blocos":[{"papel":{"nome":"paragrafo"},"texto_resumo":"` + segredo + `","confianca":"alta","origem":"heuristica","ref_xml":0}]}`},
		{nome: "versão futura carregando texto do usuário",
			dados: `{"versao":99,"blocos":[{"papel":{"nome":"paragrafo"},"texto_resumo":"` + segredo + `","confianca":0.5,"origem":"heuristica","ref_xml":0}]}`},
	}

	for _, caso := range casos {
		caso := caso
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			_, err := Desserializar([]byte(caso.dados))

			require.Error(t, err)
			assert.NotContains(t, err.Error(), segredo,
				"a mensagem de erro vaza conteúdo do documento do usuário: %s", err.Error())
		})
	}
}

// TestPapelUnmarshalJSONNaoEcoaConteudoRecebido cobre o mesmo risco no ponto
// mais provável de escorregar: o nome do papel vem do JSON e é tentador
// interpolá-lo para "ajudar" quem depura.
func TestPapelUnmarshalJSONNaoEcoaConteudoRecebido(t *testing.T) {
	t.Parallel()

	const segredo = "NOME-QUE-VEIO-DO-ARQUIVO-DO-USUARIO-8c3d"

	var papel Papel
	err := papel.UnmarshalJSON([]byte(`{"nome":"` + segredo + `"}`))

	require.Error(t, err)
	assert.NotContains(t, err.Error(), segredo)
}
