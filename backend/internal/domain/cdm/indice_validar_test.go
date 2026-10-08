// Este arquivo é o RED do contrato C1 do recorte f3-integracao-plano-formatacao:
// (Indice).Validar, o invariante do índice no pacote que já o define.
//
// FASE VERMELHA: Validar ainda não existe em serializacao.go. O pacote não
// compila por `i.Validar` estar indefinido — não por erro de sintaxe ou de
// import.
//
// Critérios cobertos aqui: A1 (versão), A2 (um termo por caso, regra 12),
// A3 (RefXML duplicado, inclusive entre papéis diferentes), A4 (índice sem
// blocos) e a parte de A16 que pertence a Indice.Validar (regra 7).
package cdm

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// blocoValidoEm devolve um bloco que passa nos três termos de C1 (papel,
// origem e RefXML), para que cada caso reprove exatamente o termo que quer
// provar. Monta por literal de propósito: C1 existe porque Indice pode ser
// montado sem passar por NovoBloco.
func blocoValidoEm(refXML int) Bloco {
	return Bloco{
		Papel:       Paragrafo,
		TextoResumo: "texto qualquer",
		Confianca:   0.9,
		Origem:      OrigemEstiloDocx,
		RefXML:      refXML,
	}
}

// camposDe extrai os nomes de campo de um *ErroValidacao, falhando se o erro
// não for dessa classe — versão incompatível e bloco inválido são entrada
// inválida, não corrupção.
func camposDe(t *testing.T, err error) []string {
	t.Helper()

	var validacao *errors.ErroValidacao
	require.Error(t, err, "o caso precisa alcançar a guarda que pretende provar")
	require.True(t, errors.Como(err, &validacao), "C1: versão e bloco inválidos são *ErroValidacao; erro foi %T", err)

	nomes := make([]string, 0, len(validacao.Campos))
	for _, campo := range validacao.Campos {
		nomes = append(nomes, campo.Campo)
	}
	return nomes
}

// ---------------------------------------------------------------------------
// A1 — versão do formato
// ---------------------------------------------------------------------------

func TestIndiceValidarVersaoDoFormato(t *testing.T) {
	casos := []struct {
		nome   string
		versao int
		aceita bool
	}{
		{"versão corrente", VersaoFormatoCDM, true},
		{"versão zero", 0, false},
		{"versão futura", VersaoFormatoCDM + 1, false},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			indice := Indice{
				Versao: caso.versao,
				Blocos: []Bloco{blocoValidoEm(0), blocoValidoEm(1)},
			}
			require.True(t, indice.Blocos[0].Papel.Valido() && indice.Blocos[0].Origem.Valido(),
				"pré-condição: os blocos do caso precisam ser válidos, para que só a versão reprove")

			err := indice.Validar()
			if caso.aceita {
				assert.NoError(t, err, "versão corrente com blocos corretos é válida")
				return
			}
			assert.Contains(t, camposDe(t, err), "versao", "A1: a versão incompatível reprova no campo versao")
		})
	}
}

// ---------------------------------------------------------------------------
// A2 — um termo por caso (regra 12): papel, origem e RefXML isolados
// ---------------------------------------------------------------------------

func TestIndiceValidarReprovaUmTermoPorCaso(t *testing.T) {
	casos := []struct {
		nome   string
		bloco  Bloco
		campo  string
		outros []string
	}{
		{
			nome:   "papel zero com origem e ref válidos",
			bloco:  Bloco{Papel: Papel{}, TextoResumo: "t", Confianca: 0.9, Origem: OrigemEstiloDocx, RefXML: 0},
			campo:  "papel",
			outros: []string{"origem", "ref_xml"},
		},
		{
			nome:   "origem vazia com papel e ref válidos",
			bloco:  Bloco{Papel: Paragrafo, TextoResumo: "t", Confianca: 0.9, Origem: Origem(""), RefXML: 0},
			campo:  "origem",
			outros: []string{"papel", "ref_xml"},
		},
		{
			nome:   "ref_xml negativo com papel e origem válidos",
			bloco:  Bloco{Papel: Paragrafo, TextoResumo: "t", Confianca: 0.9, Origem: OrigemEstiloDocx, RefXML: -1},
			campo:  "ref_xml",
			outros: []string{"papel", "origem"},
		},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			indice := Indice{Versao: VersaoFormatoCDM, Blocos: []Bloco{caso.bloco}}

			campos := camposDe(t, indice.Validar())
			assert.Contains(t, campos, caso.campo, "A2: o termo reprovado precisa aparecer em Campos")
			for _, outro := range caso.outros {
				assert.NotContains(t, campos, outro,
					"A2: o caso isola um termo; %q não deveria reprovar aqui", outro)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// A3 — RefXML duplicado é corrupção, não entrada inválida
// ---------------------------------------------------------------------------

func TestIndiceValidarRefXMLDuplicadoEhCorrupcao(t *testing.T) {
	casos := []struct {
		nome   string
		blocos []Bloco
	}{
		{
			nome: "dois parágrafos no mesmo ref",
			blocos: []Bloco{
				{Papel: Paragrafo, Confianca: 0.9, Origem: OrigemEstiloDocx, RefXML: 2},
				{Papel: Paragrafo, Confianca: 0.9, Origem: OrigemEstiloDocx, RefXML: 2},
			},
		},
		{
			// Pega a implementação que só procura duplicata DENTRO de um
			// subconjunto filtrado por papel.
			nome: "título e parágrafo no mesmo ref",
			blocos: []Bloco{
				{Papel: Titulo, Confianca: 0.9, Origem: OrigemEstiloDocx, RefXML: 2},
				{Papel: Paragrafo, Confianca: 0.9, Origem: OrigemEstiloDocx, RefXML: 2},
			},
		},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			indice := Indice{Versao: VersaoFormatoCDM, Blocos: caso.blocos}
			for _, bloco := range indice.Blocos {
				require.True(t, bloco.Papel.Valido() && bloco.Origem.Valido() && bloco.RefXML >= 0,
					"pré-condição: só a duplicata de RefXML pode reprovar neste caso")
			}

			err := indice.Validar()
			require.Error(t, err, "A3: duas entradas no mesmo nó XML não podem ser aceitas")
			assert.True(t, errors.E(err, ErroRefXMLDuplicado),
				"A3: a duplicata devolve a sentinela ErroRefXMLDuplicado; erro foi %v", err)

			var validacao *errors.ErroValidacao
			assert.False(t, errors.Como(err, &validacao),
				"C1: duplicata é corrupção e NÃO *ErroValidacao — senão a API responderia 400 por linha corrompida")
		})
	}
}

// ---------------------------------------------------------------------------
// A4 — índice sem blocos é válido
// ---------------------------------------------------------------------------

func TestIndiceValidarIndiceSemBlocosEhValido(t *testing.T) {
	casos := []struct {
		nome   string
		blocos []Bloco
	}{
		{"blocos nulos", nil},
		{"blocos vazios", []Bloco{}},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			indice := Indice{Versao: VersaoFormatoCDM, Blocos: caso.blocos}
			assert.NoError(t, indice.Validar(), "A4: documento sem bloco classificado é lista vazia, não erro")
		})
	}
}

// ---------------------------------------------------------------------------
// A16 (parte de Indice.Validar) — conteúdo do usuário nunca entra no erro
// ---------------------------------------------------------------------------

// marcaVazamentoCDM é um literal improvável de aparecer por acidente em
// qualquer mensagem fixa do pacote.
const marcaVazamentoCDM = "ZZMARCA-TEXTO-DO-USUARIO-7f3a9b-CDM"

// conferirSemVazamento varre todo caminho pelo qual o texto do bloco poderia
// chegar ao log ou à resposta HTTP: a mensagem, cada CampoInvalido
// isoladamente (é CampoInvalido.String() que rotasutil.go:36 copia para
// `razoes`), a cadeia inteira de Unwrap e os três verbos de fmt — %#v não
// passa por Stringer, e cdm.Bloco não tem String()/GoString().
func conferirSemVazamento(t *testing.T, err error, marca string) {
	t.Helper()
	require.Error(t, err, "o caso de vazamento precisa realmente falhar")

	assert.NotContains(t, err.Error(), marca, "regra 7: Error() não cita conteúdo do documento")
	for _, formato := range []string{"%v", "%+v", "%#v"} {
		assert.NotContains(t, fmt.Sprintf(formato, err), marca,
			"regra 7: %s sobre o erro não pode expor conteúdo do documento", formato)
	}

	pendentes := []error{err}
	for len(pendentes) > 0 {
		atual := pendentes[len(pendentes)-1]
		pendentes = pendentes[:len(pendentes)-1]
		if atual == nil {
			continue
		}
		assert.NotContains(t, atual.Error(), marca, "regra 7: elo da cadeia de Unwrap cita conteúdo")

		if validacao, ok := atual.(*errors.ErroValidacao); ok { //nolint:errorlint // atual já é um elo desembrulhado da cadeia; errors.As pararia no primeiro match e deixaria elos sem conferir, que é justamente o que A16 precisa varrer.
			assert.NotContains(t, validacao.Mensagem, marca, "regra 7: ErroValidacao.Mensagem cita conteúdo")
			for _, campo := range validacao.Campos {
				assert.NotContains(t, campo.Campo, marca, "regra 7: CampoInvalido.Campo cita conteúdo")
				assert.NotContains(t, campo.Mensagem, marca, "regra 7: CampoInvalido.Mensagem cita conteúdo")
				assert.NotContains(t, campo.String(), marca,
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

func TestIndiceValidarNaoVazaTextoDoUsuarioNoErro(t *testing.T) {
	// A marca vai no TextoResumo de TODOS os blocos de todos os casos: se
	// qualquer caminho de erro imprimir um bloco, ela aparece.
	comMarca := func(blocos ...Bloco) []Bloco {
		for i := range blocos {
			blocos[i].TextoResumo = marcaVazamentoCDM + " parágrafo confidencial"
		}
		return blocos
	}

	casos := []struct {
		nome   string
		indice Indice
	}{
		{
			nome: "versão incompatível",
			indice: Indice{
				Versao: VersaoFormatoCDM + 1,
				Blocos: comMarca(blocoValidoEm(0), blocoValidoEm(1)),
			},
		},
		{
			nome: "bloco inválido",
			indice: Indice{
				Versao: VersaoFormatoCDM,
				Blocos: comMarca(Bloco{Papel: Papel{}, Origem: OrigemEstiloDocx, RefXML: 0}),
			},
		},
		{
			nome: "ref_xml duplicado",
			indice: Indice{
				Versao: VersaoFormatoCDM,
				Blocos: comMarca(blocoValidoEm(2), blocoValidoEm(2)),
			},
		},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			for _, bloco := range caso.indice.Blocos {
				require.True(t, strings.Contains(bloco.TextoResumo, marcaVazamentoCDM),
					"pré-condição: todo bloco do caso carrega a marca")
			}
			conferirSemVazamento(t, caso.indice.Validar(), marcaVazamentoCDM)
		})
	}
}

// ---------------------------------------------------------------------------
// A19a — Campos não pode crescer com o número de blocos
// ---------------------------------------------------------------------------

// contarEm conta quantas vezes um nome de campo aparece na lista reprovada.
func contarEm(nomes []string, campo string) int {
	total := 0
	for _, nome := range nomes {
		if nome == campo {
			total++
		}
	}
	return total
}

// repetirBloco devolve n cópias do bloco, cada uma num RefXML próprio, para
// que o caso reprove só pelo termo que ajusta — nunca por duplicata.
func repetirBloco(n int, ajustar func(ref int) Bloco) []Bloco {
	blocos := make([]Bloco, 0, n)
	for i := 0; i < n; i++ {
		blocos = append(blocos, ajustar(i))
	}
	return blocos
}

func TestIndiceValidarNaoAmplificaCamposPorBloco(t *testing.T) {
	const quantidadeDeBlocos = 50

	casos := []struct {
		nome    string
		campo   string
		ajustar func(ref int) Bloco
	}{
		{
			nome:  "papel zero em todos os blocos",
			campo: "papel",
			ajustar: func(ref int) Bloco {
				return Bloco{Papel: Papel{}, TextoResumo: "t", Confianca: 0.9, Origem: OrigemEstiloDocx, RefXML: ref}
			},
		},
		{
			nome:  "origem vazia em todos os blocos",
			campo: "origem",
			ajustar: func(ref int) Bloco {
				return Bloco{Papel: Paragrafo, TextoResumo: "t", Confianca: 0.9, Origem: Origem(""), RefXML: ref}
			},
		},
		{
			// RefXML distintos e todos negativos: reprova por sinal, não por
			// repetição — -1, -2, -3... nunca colidem entre si.
			nome:  "ref_xml negativo em todos os blocos",
			campo: "ref_xml",
			ajustar: func(ref int) Bloco {
				return Bloco{Papel: Paragrafo, TextoResumo: "t", Confianca: 0.9, Origem: OrigemEstiloDocx, RefXML: -(ref + 1)}
			},
		},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			indice := Indice{Versao: VersaoFormatoCDM, Blocos: repetirBloco(quantidadeDeBlocos, caso.ajustar)}
			require.Len(t, indice.Blocos, quantidadeDeBlocos,
				"pré-condição: o caso precisa ter muitos blocos para que a amplificação apareça")
			vistos := make(map[int]struct{}, len(indice.Blocos))
			for _, bloco := range indice.Blocos {
				_, repetido := vistos[bloco.RefXML]
				require.False(t, repetido,
					"pré-condição: RefXML distintos, senão o caso alcançaria a guarda de duplicata em vez da de bloco inválido")
				vistos[bloco.RefXML] = struct{}{}
			}

			campos := camposDe(t, indice.Validar())
			assert.Equal(t, 1, contarEm(campos, caso.campo),
				"A19: %d blocos reprovados no mesmo termo precisam render UM CampoInvalido, não %d entradas idênticas copiadas para razoes da resposta HTTP",
				quantidadeDeBlocos, quantidadeDeBlocos)
			assert.LessOrEqual(t, len(campos), 3,
				"A19: o payload de razoes não cresce com o número de blocos; Campos foi %v", campos)
		})
	}
}

// ---------------------------------------------------------------------------
// A19b — corrupção vence validação quando os dois defeitos coexistem
// ---------------------------------------------------------------------------

func TestIndiceValidarDuplicadoVenceBlocoInvalido(t *testing.T) {
	casos := []struct {
		nome          string
		blocoInvalido Bloco
	}{
		{
			nome:          "papel zero junto com duplicata",
			blocoInvalido: Bloco{Papel: Papel{}, TextoResumo: "t", Confianca: 0.9, Origem: OrigemEstiloDocx, RefXML: 7},
		},
		{
			nome:          "origem vazia junto com duplicata",
			blocoInvalido: Bloco{Papel: Paragrafo, TextoResumo: "t", Confianca: 0.9, Origem: Origem(""), RefXML: 7},
		},
		{
			nome:          "ref_xml negativo junto com duplicata",
			blocoInvalido: Bloco{Papel: Paragrafo, TextoResumo: "t", Confianca: 0.9, Origem: OrigemEstiloDocx, RefXML: -1},
		},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			indice := Indice{Versao: VersaoFormatoCDM, Blocos: []Bloco{
				blocoValidoEm(2),
				blocoValidoEm(2),
				caso.blocoInvalido,
			}}
			require.Equal(t, indice.Blocos[0].RefXML, indice.Blocos[1].RefXML,
				"pré-condição: o caso precisa conter a duplicata que pretende provar")
			require.False(t,
				caso.blocoInvalido.Papel.Valido() && caso.blocoInvalido.Origem.Valido() && caso.blocoInvalido.RefXML >= 0,
				"pré-condição: o caso precisa conter também um bloco inválido")

			err := indice.Validar()
			require.Error(t, err, "A19: índice corrompido não pode ser aceito")
			assert.True(t, errors.E(err, ErroRefXMLDuplicado),
				"A19: RefXML repetido é corrupção de servidor e precisa continuar identificável mesmo com bloco inválido no mesmo índice; erro foi %v", err)

			var validacao *errors.ErroValidacao
			assert.False(t, errors.Como(err, &validacao),
				"A19: se a corrupção virar *ErroValidacao, a API responde 400 culpando o cliente por uma linha corrompida do banco")
		})
	}
}

// ---------------------------------------------------------------------------
// A20 — os dois caminhos de recusa falam o mesmo texto (regressão)
// ---------------------------------------------------------------------------

// mensagemDoCampo devolve a mensagem do primeiro CampoInvalido com esse nome.
func mensagemDoCampo(t *testing.T, err error, campo string) string {
	t.Helper()

	var validacao *errors.ErroValidacao
	require.Error(t, err, "o caso precisa alcançar a guarda que pretende provar")
	require.True(t, errors.Como(err, &validacao), "A20: recusa de bloco é *ErroValidacao; erro foi %T", err)
	for _, invalido := range validacao.Campos {
		if invalido.Campo == campo {
			return invalido.Mensagem
		}
	}
	require.FailNowf(t, "campo ausente", "A20: o erro precisa reprovar %q; campos foram %v", campo, validacao.Campos)
	return ""
}

func TestMensagemDeRecusaEhAMesmaNosDoisCaminhos(t *testing.T) {
	casos := []struct {
		nome      string
		campo     string
		constante string
		viaBloco  func() error
		viaIndice func() error
	}{
		{
			nome:      "papel desconhecido",
			campo:     "papel",
			constante: mensagemPapelDesconhecido,
			viaBloco: func() error {
				_, err := NovoBloco(Papel{}, "t", 0.9, OrigemEstiloDocx, 0)
				return err
			},
			viaIndice: func() error {
				return Indice{Versao: VersaoFormatoCDM, Blocos: []Bloco{
					{Papel: Papel{}, TextoResumo: "t", Confianca: 0.9, Origem: OrigemEstiloDocx, RefXML: 0},
				}}.Validar()
			},
		},
		{
			nome:      "origem desconhecida",
			campo:     "origem",
			constante: mensagemOrigemDesconhecida,
			viaBloco: func() error {
				_, err := NovoBloco(Paragrafo, "t", 0.9, Origem(""), 0)
				return err
			},
			viaIndice: func() error {
				return Indice{Versao: VersaoFormatoCDM, Blocos: []Bloco{
					{Papel: Paragrafo, TextoResumo: "t", Confianca: 0.9, Origem: Origem(""), RefXML: 0},
				}}.Validar()
			},
		},
		{
			nome:      "ref_xml negativo",
			campo:     "ref_xml",
			constante: mensagemRefXMLNegativo,
			viaBloco: func() error {
				_, err := NovoBloco(Paragrafo, "t", 0.9, OrigemEstiloDocx, -1)
				return err
			},
			viaIndice: func() error {
				return Indice{Versao: VersaoFormatoCDM, Blocos: []Bloco{
					{Papel: Paragrafo, TextoResumo: "t", Confianca: 0.9, Origem: OrigemEstiloDocx, RefXML: -1},
				}}.Validar()
			},
		},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			require.NotEmpty(t, caso.constante, "pré-condição: a constante do pacote precisa ter texto")

			deBloco := mensagemDoCampo(t, caso.viaBloco(), caso.campo)
			deIndice := mensagemDoCampo(t, caso.viaIndice(), caso.campo)

			assert.Equal(t, caso.constante, deBloco,
				"A20: NovoBloco precisa falar pela constante do pacote, não por literal duplicado")
			assert.Equal(t, caso.constante, deIndice,
				"A20: Indice.Validar precisa falar pela constante do pacote")
			assert.Equal(t, deBloco, deIndice,
				"A20: o mesmo defeito recusado por caminhos diferentes não pode ter dois textos")
		})
	}
}

func TestMensagemDeEnvelopeEhAMesmaNosDoisCaminhos(t *testing.T) {
	envelopeDe := func(t *testing.T, err error) string {
		t.Helper()
		var validacao *errors.ErroValidacao
		require.Error(t, err, "o caso precisa alcançar a guarda que pretende provar")
		require.True(t, errors.Como(err, &validacao), "A20: recusa de bloco é *ErroValidacao; erro foi %T", err)
		return validacao.Mensagem
	}

	_, erroDeBloco := NovoBloco(Papel{}, "t", 0.9, OrigemEstiloDocx, 0)
	erroDeIndice := Indice{Versao: VersaoFormatoCDM, Blocos: []Bloco{
		{Papel: Papel{}, TextoResumo: "t", Confianca: 0.9, Origem: OrigemEstiloDocx, RefXML: 0},
	}}.Validar()

	assert.Equal(t, mensagemBlocoInvalido, envelopeDe(t, erroDeBloco),
		"A20: o envelope de NovoBloco precisa vir da constante mensagemBlocoInvalido")
	assert.Equal(t, mensagemBlocoInvalido, envelopeDe(t, erroDeIndice),
		"A20: o envelope de Indice.Validar precisa vir da constante mensagemBlocoInvalido")
}

// ---------------------------------------------------------------------------
// A21 — INTERAÇÃO de dois termos: RefXML negativo E repetido ao mesmo tempo
// ---------------------------------------------------------------------------
//
// Nenhum caso de um termo por caso alcança este defeito (regra 12):
// TestIndiceValidarReprovaUmTermoPorCaso usa UM bloco negativo (não repete) e
// TestIndiceValidarNaoAmplificaCamposPorBloco usa negativos DISTINTOS
// (-(ref+1), que nunca colidem). Só quando os dois termos valem juntos — ref
// negativo E igual — a varredura de duplicata colide no mapa antes de a guarda
// de validade por bloco rodar, e o erro sai como corrupção de servidor.
//
// Dois blocos com RefXML -1 não são "dois blocos apontando para o mesmo nó
// XML": -1 não é nó nenhum. São dois ordinais inválidos, logo entrada inválida.

// indiceComRefsNegativos monta um índice cujos blocos só reprovam no termo
// ref_xml, com os ordinais informados.
func indiceComRefsNegativos(refs ...int) Indice {
	blocos := make([]Bloco, 0, len(refs))
	for _, ref := range refs {
		blocos = append(blocos, blocoValidoEm(ref))
	}
	return Indice{Versao: VersaoFormatoCDM, Blocos: blocos}
}

func TestIndiceValidarRefXMLNegativoRepetidoEhEntradaInvalida(t *testing.T) {
	casos := []struct {
		nome string
		refs []int
	}{
		{"dois blocos no mesmo ref_xml -1", []int{-1, -1}},
		{"negativo repetido convivendo com ref válido", []int{0, -1, -1}},
		{"três blocos no mesmo ref_xml negativo", []int{-7, -7, -7}},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			indice := indiceComRefsNegativos(caso.refs...)

			negativosRepetidos := false
			vistos := make(map[int]struct{}, len(indice.Blocos))
			for _, bloco := range indice.Blocos {
				require.True(t, bloco.Papel.Valido() && bloco.Origem.Valido(),
					"pré-condição: papel e origem válidos, para que só o termo ref_xml reprove")
				if _, repetido := vistos[bloco.RefXML]; repetido && bloco.RefXML < 0 {
					negativosRepetidos = true
				}
				vistos[bloco.RefXML] = struct{}{}
			}
			require.True(t, negativosRepetidos,
				"pré-condição: o caso precisa ter ref_xml negativo E repetido, que é a interação dos dois termos que A21 prova")

			err := indice.Validar()
			require.Error(t, err, "A21: ordinal negativo não pode ser aceito")

			assert.False(t, errors.E(err, ErroRefXMLDuplicado),
				"A21: -1 não é nó XML; dois ordinais negativos iguais são entrada inválida, não corrupção do índice. Erro foi %v", err)
			assert.Contains(t, camposDe(t, err), "ref_xml",
				"A21: a recusa precisa alcançar a guarda de ref_xml, a mesma que um único bloco negativo alcança")
		})
	}
}

// TestIndiceValidarEFallbackClassificamIgualOMesmoIndice fixa a coerência
// intra-pacote: hoje serializacao.go confere duplicata antes da validade e
// fallback.go confere validade antes da duplicata, então o MESMO insumo vira
// 500 por um caminho e 400 pelo outro. Nenhuma API de LLM é chamada: Aplicar
// recusa antes de consultar, e o caso exige chamadas == 0 do fake.
func TestIndiceValidarEFallbackClassificamIgualOMesmoIndice(t *testing.T) {
	classificacaoDe := func(err error) (ehValidacao bool, ehDuplicado bool) {
		var validacao *errors.ErroValidacao
		return errors.Como(err, &validacao), errors.E(err, ErroRefXMLDuplicado)
	}

	casos := []struct {
		nome string
		refs []int
	}{
		{"negativo repetido", []int{-1, -1}},
		{"negativo único", []int{-1}},
		{"negativo repetido com ref válido", []int{0, -1, -1}},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			indice := indiceComRefsNegativos(caso.refs...)

			fake := &classificadorEstruturaFake{}
			fallback, err := NovoFallback(fake, PoliticaConfianca{LimiteConsulta: .5, LimiteConfirmacao: .6, LimiteAutomatico: .9})
			require.NoError(t, err, "pré-condição: o fallback do caso precisa ser construível")

			_, erroDoFallback := fallback.Aplicar(context.Background(), append([]Bloco(nil), indice.Blocos...))
			require.Error(t, erroDoFallback, "pré-condição: Aplicar precisa recusar o mesmo insumo")
			require.Zero(t, fake.chamadas,
				"pré-condição: a recusa acontece antes de qualquer consulta ao classificador — nenhum teste chama LLM")

			erroDoValidar := indice.Validar()
			require.Error(t, erroDoValidar, "pré-condição: Validar precisa recusar o mesmo insumo")

			validarEhValidacao, validarEhDuplicado := classificacaoDe(erroDoValidar)
			fallbackEhValidacao, fallbackEhDuplicado := classificacaoDe(erroDoFallback)

			assert.Equal(t, fallbackEhValidacao, validarEhValidacao,
				"A21: o mesmo índice não pode ser *ErroValidacao por um caminho e não ser pelo outro (Validar=%v, Fallback=%v)", erroDoValidar, erroDoFallback)
			assert.Equal(t, fallbackEhDuplicado, validarEhDuplicado,
				"A21: o mesmo índice não pode ser ErroRefXMLDuplicado por um caminho e não ser pelo outro (Validar=%v, Fallback=%v)", erroDoValidar, erroDoFallback)
			assert.True(t, validarEhValidacao && fallbackEhValidacao,
				"A21: ordinal negativo é entrada inválida nos dois caminhos — 400, não 500")
			assert.False(t, validarEhDuplicado || fallbackEhDuplicado,
				"A21: ordinal negativo não é duplicata de nó XML em nenhum dos dois caminhos")
		})
	}
}
