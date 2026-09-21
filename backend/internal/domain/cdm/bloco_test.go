// Package cdm é o Modelo Canônico do Documento (F2): um índice semântico por
// cima do pacote OOXML, não uma cópia dele (docs/adr/0001-docx-in-place.md).
// Bloco{Papel, TextoResumo, Confianca, Origem, RefXML} diz qual é o papel de
// cada w:p/w:tbl do corpo do documento e aponta para o nó correspondente —
// RefXML é o índice ordinal do bloco no corpo, nunca um id injetado no XML do
// usuário (injetar mutaria o documento e quebraria o round-trip byte a byte).
//
// Este arquivo só testa a entidade pura: zero dependência de infra, zero I/O.
// A extração de blocos do pacote (internal/infra/ooxml) e a classificação
// pela camada 1 (estilos nomeados do DOCX) moram em outro pacote e são
// testadas em internal/infra/ooxml/blocos_test.go.
package cdm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// exigirCampo falha o teste quando o erro de validação não reprova o campo
// esperado. Mesmo padrão de internal/domain/vo: assere sobre o nome do campo,
// nunca sobre o texto completo.
func exigirCampo(t *testing.T, err error, campo string) {
	t.Helper()

	var invalido *errors.ErroValidacao
	require.True(t, errors.Como(err, &invalido), "esperava *errors.ErroValidacao, obteve %T (%v)", err, err)

	for _, campoInvalido := range invalido.Campos {
		if campoInvalido.Campo == campo {
			return
		}
	}
	nomes := make([]string, 0, len(invalido.Campos))
	for _, campoInvalido := range invalido.Campos {
		nomes = append(nomes, campoInvalido.Campo)
	}
	t.Fatalf("esperava o campo %q entre %v", campo, nomes)
}

// ---------------------------------------------------------------------------
// Papel — inclusive o caso especial Secao(nível)
// ---------------------------------------------------------------------------

func TestPapelValido(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome     string
		papel    Papel
		esperado bool
	}{
		{nome: "titulo é válido", papel: Titulo, esperado: true},
		{nome: "lista de autores é válida", papel: ListaAutores, esperado: true},
		{nome: "resumo é válido", papel: Resumo, esperado: true},
		{nome: "palavras-chave é válido", papel: PalavrasChave, esperado: true},
		{nome: "paragrafo é válido", papel: Paragrafo, esperado: true},
		{nome: "citacao é válido", papel: Citacao, esperado: true},
		{nome: "item de lista é válido", papel: ItemLista, esperado: true},
		{nome: "tabela é válida", papel: Tabela, esperado: true},
		{nome: "figura é válida", papel: Figura, esperado: true},
		{nome: "legenda é válida", papel: Legenda, esperado: true},
		{nome: "equacao é válida", papel: Equacao, esperado: true},
		{nome: "referencia é válida", papel: Referencia, esperado: true},
		{nome: "nota de rodapé é válida", papel: NotaRodape, esperado: true},

		{nome: "secao com nível 1 é válida", papel: Secao(1), esperado: true},
		{nome: "secao com nível 6 é válida", papel: Secao(6), esperado: true},
		{nome: "secao com nível 3 é válida", papel: Secao(3), esperado: true},
		{nome: "secao com nível 0 é inválida", papel: Secao(0), esperado: false},
		{nome: "secao com nível 7 é inválida", papel: Secao(7), esperado: false},
		{nome: "secao com nível negativo é inválida", papel: Secao(-1), esperado: false},

		{nome: "papel zero-value é inválido", papel: Papel{}, esperado: false},
		{nome: "papel com nome desconhecido é inválido", papel: Papel{nome: "conclusao"}, esperado: false},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, caso.esperado, caso.papel.Valido())
		})
	}
}

func TestPapelSecaoCarregaONivelInformado(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome  string
		nivel int
	}{
		{nome: "nível 1", nivel: 1},
		{nome: "nível 4", nivel: 4},
		{nome: "nível 6", nivel: 6},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, caso.nivel, Secao(caso.nivel).Nivel(),
				"Secao(n).Nivel() precisa devolver exatamente o nível informado na construção")
		})
	}

	assert.Equal(t, 0, Titulo.Nivel(), "papel que não é secao não carrega nível")
	assert.NotEqual(t, Secao(1), Secao(2), "seções de níveis diferentes não são o mesmo papel")
	assert.Equal(t, Secao(2), Secao(2), "duas seções do mesmo nível são o mesmo papel")
}

// ---------------------------------------------------------------------------
// Origem
// ---------------------------------------------------------------------------

func TestOrigemValida(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome     string
		origem   Origem
		esperado bool
	}{
		{nome: "estilo-docx é válida", origem: OrigemEstiloDocx, esperado: true},
		{nome: "heuristica é válida", origem: OrigemHeuristica, esperado: true},
		{nome: "llm é válida", origem: OrigemLLM, esperado: true},
		{nome: "usuario é válida", origem: OrigemUsuario, esperado: true},
		{nome: "vazia é inválida", origem: Origem(""), esperado: false},
		{nome: "desconhecida é inválida", origem: Origem("sistema"), esperado: false},
		{nome: "maiúscula não bate: case sensitive", origem: Origem("USUARIO"), esperado: false},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, caso.esperado, caso.origem.Valido())
		})
	}
}

// ---------------------------------------------------------------------------
// NovoBloco
// ---------------------------------------------------------------------------

func TestNovoBlocoPapelValidoEInvalido(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome      string
		papel     Papel
		esperaErr bool
	}{
		{nome: "paragrafo é aceito", papel: Paragrafo, esperaErr: false},
		{nome: "secao com nível válido é aceita", papel: Secao(2), esperaErr: false},
		{nome: "secao com nível 0 é recusada", papel: Secao(0), esperaErr: true},
		{nome: "secao com nível 7 é recusada", papel: Secao(7), esperaErr: true},
		{nome: "papel zero-value é recusado", papel: Papel{}, esperaErr: true},
		{nome: "papel desconhecido é recusado", papel: Papel{nome: "introito"}, esperaErr: true},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			bloco, err := NovoBloco(caso.papel, "texto qualquer", 0.9, OrigemEstiloDocx, 0)
			if caso.esperaErr {
				require.Error(t, err)
				assert.Equal(t, Bloco{}, bloco, "em erro, o construtor devolve o zero-value")
				exigirCampo(t, err, "papel")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, caso.papel, bloco.Papel)
		})
	}
}

func TestNovoBlocoConfiancaLimites(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome      string
		confianca float64
		esperaErr bool
	}{
		{nome: "0 é o limite inferior aceito", confianca: 0, esperaErr: false},
		{nome: "1 é o limite superior aceito", confianca: 1, esperaErr: false},
		{nome: "0.5 está dentro da faixa", confianca: 0.5, esperaErr: false},
		{nome: "-0.1 abaixo do limite é recusado", confianca: -0.1, esperaErr: true},
		{nome: "1.1 acima do limite é recusado", confianca: 1.1, esperaErr: true},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			bloco, err := NovoBloco(Paragrafo, "texto", caso.confianca, OrigemHeuristica, 0)
			if caso.esperaErr {
				require.Error(t, err)
				assert.Equal(t, Bloco{}, bloco)
				exigirCampo(t, err, "confianca")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, caso.confianca, bloco.Confianca)
		})
	}
}

func TestNovoBlocoOrigemValidaEInvalida(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome      string
		origem    Origem
		esperaErr bool
	}{
		{nome: "estilo-docx é aceita", origem: OrigemEstiloDocx, esperaErr: false},
		{nome: "heuristica é aceita", origem: OrigemHeuristica, esperaErr: false},
		{nome: "llm é aceita", origem: OrigemLLM, esperaErr: false},
		{nome: "usuario é aceita", origem: OrigemUsuario, esperaErr: false},
		{nome: "vazia é recusada", origem: Origem(""), esperaErr: true},
		{nome: "desconhecida é recusada", origem: Origem("sistema-externo"), esperaErr: true},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			bloco, err := NovoBloco(Paragrafo, "texto", 0.5, caso.origem, 0)
			if caso.esperaErr {
				require.Error(t, err)
				assert.Equal(t, Bloco{}, bloco)
				exigirCampo(t, err, "origem")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, caso.origem, bloco.Origem)
		})
	}
}

func TestNovoBlocoSemTextoEValido(t *testing.T) {
	t.Parallel()

	// Parágrafo vazio existe em documento real (linha em branco entre seções,
	// por exemplo). Recusar bloco sem texto quebraria a extração real.
	bloco, err := NovoBloco(Paragrafo, "", 0.4, OrigemHeuristica, 5)

	require.NoError(t, err)
	assert.Equal(t, "", bloco.TextoResumo)
	assert.Equal(t, 5, bloco.RefXML)
}

func TestNovoBlocoRefXMLNegativoEhRecusado(t *testing.T) {
	t.Parallel()

	bloco, err := NovoBloco(Paragrafo, "texto", 0.5, OrigemHeuristica, -1)

	require.Error(t, err)
	assert.Equal(t, Bloco{}, bloco)
	exigirCampo(t, err, "ref_xml")
}

func TestNovoBlocoRefXMLZeroEhAceito(t *testing.T) {
	t.Parallel()

	// RefXML é o índice ORDINAL do bloco no corpo — 0 é o primeiro bloco do
	// documento, um valor perfeitamente válido, não "ausente".
	bloco, err := NovoBloco(Titulo, "Título do artigo", 0.95, OrigemEstiloDocx, 0)

	require.NoError(t, err)
	assert.Equal(t, 0, bloco.RefXML)
}

func TestNovoBlocoAcumulaTodosOsCamposReprovados(t *testing.T) {
	t.Parallel()

	// Papel, confiança e origem todos inválidos ao mesmo tempo: o construtor
	// acumula, não para no primeiro campo (mesmo padrão de entity.Documento).
	_, err := NovoBloco(Papel{}, "texto", 5.0, Origem("bugado"), 0)

	require.Error(t, err)
	exigirCampo(t, err, "papel")
	exigirCampo(t, err, "confianca")
	exigirCampo(t, err, "origem")
}

// ---------------------------------------------------------------------------
// Reclassificar — a proteção central deste recorte: correção do usuário
// nunca é sobrescrita por reclassificação automática.
// ---------------------------------------------------------------------------

func TestBlocoReclassificarAplicaQuandoNaoHaCorrecaoDeUsuario(t *testing.T) {
	t.Parallel()

	origens := []Origem{OrigemEstiloDocx, OrigemHeuristica, OrigemLLM}

	for _, origemAtual := range origens {
		t.Run("bloco com origem "+origemAtual.String()+" aceita nova classificação", func(t *testing.T) {
			t.Parallel()

			bloco, err := NovoBloco(Paragrafo, "texto original", 0.3, origemAtual, 7)
			require.NoError(t, err)

			reclassificado, err := bloco.Reclassificar(Secao(2), 0.9, OrigemHeuristica)

			require.NoError(t, err)
			assert.Equal(t, Secao(2), reclassificado.Papel)
			assert.Equal(t, 0.9, reclassificado.Confianca)
			assert.Equal(t, OrigemHeuristica, reclassificado.Origem)
			// Texto e posição não mudam: só a classificação semântica muda.
			assert.Equal(t, bloco.TextoResumo, reclassificado.TextoResumo)
			assert.Equal(t, bloco.RefXML, reclassificado.RefXML)
		})
	}
}

func TestBlocoReclassificarNaoSobrescreveCorrecaoDoUsuario(t *testing.T) {
	t.Parallel()

	corrigidoPeloUsuario, err := NovoBloco(Resumo, "texto que o usuário classificou", 1.0, OrigemUsuario, 12)
	require.NoError(t, err)

	tentativasAutomaticas := []struct {
		nome   string
		origem Origem
	}{
		{nome: "tentativa da camada de estilo-docx", origem: OrigemEstiloDocx},
		{nome: "tentativa da camada heurística", origem: OrigemHeuristica},
		{nome: "tentativa da camada llm", origem: OrigemLLM},
	}

	for _, tentativa := range tentativasAutomaticas {
		t.Run(tentativa.nome, func(t *testing.T) {
			t.Parallel()

			resultado, err := corrigidoPeloUsuario.Reclassificar(Paragrafo, 0.99, tentativa.origem)

			require.NoError(t, err, "reclassificação automática ignorada não é erro, é no-op")
			assert.Equal(t, corrigidoPeloUsuario, resultado,
				"a correção do usuário precisa sair EXATAMENTE igual: nenhum campo pode mudar")
		})
	}
}

func TestBlocoReclassificarPermiteNovaCorrecaoDoProprioUsuario(t *testing.T) {
	t.Parallel()

	corrigidoPeloUsuario, err := NovoBloco(Resumo, "primeira correção", 1.0, OrigemUsuario, 12)
	require.NoError(t, err)

	// O usuário pode corrigir de novo (mudar de ideia): só a reclassificação
	// AUTOMÁTICA é bloqueada, não uma nova correção manual.
	segundaCorrecao, err := corrigidoPeloUsuario.Reclassificar(PalavrasChave, 1.0, OrigemUsuario)

	require.NoError(t, err)
	assert.Equal(t, PalavrasChave, segundaCorrecao.Papel)
	assert.Equal(t, OrigemUsuario, segundaCorrecao.Origem)
}

func TestBlocoReclassificarCadeiaCompletaDasTresCamadasEDepoisUsuario(t *testing.T) {
	t.Parallel()

	// estilo-docx classifica primeiro, sempre.
	bloco, err := NovoBloco(Paragrafo, "1 introducao", 0.4, OrigemEstiloDocx, 3)
	require.NoError(t, err)

	// heuristica melhora a leitura.
	bloco, err = bloco.Reclassificar(Secao(1), 0.7, OrigemHeuristica)
	require.NoError(t, err)
	assert.Equal(t, OrigemHeuristica, bloco.Origem)

	// llm refina ainda mais.
	bloco, err = bloco.Reclassificar(Secao(1), 0.85, OrigemLLM)
	require.NoError(t, err)
	assert.Equal(t, OrigemLLM, bloco.Origem)

	// usuário corrige manualmente: a partir daqui, ninguém mais escreve por cima.
	bloco, err = bloco.Reclassificar(Titulo, 1.0, OrigemUsuario)
	require.NoError(t, err)
	assert.Equal(t, OrigemUsuario, bloco.Origem)
	assert.Equal(t, Titulo, bloco.Papel)

	// qualquer camada automática que rode depois é ignorada.
	protegido, err := bloco.Reclassificar(Secao(4), 0.99, OrigemLLM)
	require.NoError(t, err)
	assert.Equal(t, bloco, protegido)
}

func TestBlocoReclassificarValidaEntradaQuandoAplica(t *testing.T) {
	t.Parallel()

	bloco, err := NovoBloco(Paragrafo, "texto", 0.5, OrigemHeuristica, 1)
	require.NoError(t, err)

	// Bloco não está protegido (origem != usuario), então a validação normal
	// do construtor se aplica: confiança fora de faixa é recusada mesmo aqui.
	_, err = bloco.Reclassificar(Secao(3), 7.0, OrigemLLM)

	require.Error(t, err)
	exigirCampo(t, err, "confianca")
}
