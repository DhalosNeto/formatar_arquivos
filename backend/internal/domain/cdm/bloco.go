package cdm

import "github.com/daniel-halos/formatador/internal/infra/errors"

// NivelSecaoMinimo e NivelSecaoMaximo delimitam a hierarquia de seções que o
// CDM representa. Seis níveis é o que o OOXML declara como Heading1..Heading6
// e o que as normas acadêmicas usam na prática.
const (
	NivelSecaoMinimo = 1
	NivelSecaoMaximo = 6
)

// Papel é o que um bloco do documento É: título, resumo, seção de nível N,
// referência. Value object comparável com campos privados — só os
// construtores deste pacote produzem um Papel válido, então `==` entre dois
// papéis é confiável.
type Papel struct {
	nome  string
	nivel int
}

// Papéis sem nível. Secao é o único que carrega hierarquia.
var (
	Titulo        = Papel{nome: "titulo"}
	ListaAutores  = Papel{nome: "lista_autores"}
	Resumo        = Papel{nome: "resumo"}
	PalavrasChave = Papel{nome: "palavras_chave"}
	Paragrafo     = Papel{nome: "paragrafo"}
	Citacao       = Papel{nome: "citacao"}
	ItemLista     = Papel{nome: "item_lista"}
	Tabela        = Papel{nome: "tabela"}
	Figura        = Papel{nome: "figura"}
	Legenda       = Papel{nome: "legenda"}
	Equacao       = Papel{nome: "equacao"}
	Referencia    = Papel{nome: "referencia"}
	NotaRodape    = Papel{nome: "nota_rodape"}
)

const nomePapelSecao = "secao"

// papeisSemNivel indexa os papéis que não carregam hierarquia, para que
// Valido reprove qualquer nome inventado fora deste pacote.
var papeisSemNivel = map[string]struct{}{
	Titulo.nome: {}, ListaAutores.nome: {}, Resumo.nome: {}, PalavrasChave.nome: {},
	Paragrafo.nome: {}, Citacao.nome: {}, ItemLista.nome: {}, Tabela.nome: {},
	Figura.nome: {}, Legenda.nome: {}, Equacao.nome: {}, Referencia.nome: {},
	NotaRodape.nome: {},
}

// Secao devolve o papel de seção do nível informado. Nível fora de
// NivelSecaoMinimo..NivelSecaoMaximo produz um Papel inválido em vez de
// entrar em pânico: quem constrói passa por NovoBloco, que reprova.
func Secao(nivel int) Papel { return Papel{nome: nomePapelSecao, nivel: nivel} }

// Valido informa se o papel é um dos conhecidos e, no caso de seção, se o
// nível está na faixa suportada.
func (p Papel) Valido() bool {
	if p.nome == nomePapelSecao {
		return p.nivel >= NivelSecaoMinimo && p.nivel <= NivelSecaoMaximo
	}
	if p.nivel != 0 {
		return false
	}
	_, conhecido := papeisSemNivel[p.nome]
	return conhecido
}

// Nivel devolve a hierarquia da seção; zero para os papéis que não a carregam.
func (p Papel) Nivel() int { return p.nivel }

// String devolve o nome do papel, sem o nível.
func (p Papel) String() string { return p.nome }

// Origem registra QUEM classificou o bloco. Importa porque correção do
// usuário nunca é sobrescrita por reclassificação automática.
type Origem string

const (
	OrigemEstiloDocx Origem = "estilo-docx"
	OrigemHeuristica Origem = "heuristica"
	OrigemLLM        Origem = "llm"
	OrigemUsuario    Origem = "usuario"
)

// Valido informa se a origem é uma das quatro camadas de classificação.
func (o Origem) Valido() bool {
	switch o {
	case OrigemEstiloDocx, OrigemHeuristica, OrigemLLM, OrigemUsuario:
		return true
	}
	return false
}

func (o Origem) String() string { return string(o) }

// Bloco é uma entrada do índice semântico: diz qual é o papel de um bloco do
// corpo do documento e aponta para ele por RefXML, o índice ordinal entre os
// filhos diretos de w:body. TextoResumo é um trecho para exibição e para a
// camada de LLM — o texto íntegro vive no pacote OOXML, nunca aqui.
type Bloco struct {
	Papel       Papel
	TextoResumo string
	Confianca   float64
	Origem      Origem
	RefXML      int
}

// NovoBloco valida os cinco campos de uma vez, acumulando todos os reprovados
// em um único *errors.ErroValidacao.
func NovoBloco(papel Papel, textoResumo string, confianca float64, origem Origem, refXML int) (Bloco, error) {
	validacao := errors.NovoErroValidacaoCampos("bloco do cdm inválido")

	if !papel.Valido() {
		validacao.Acrescentar("papel", "papel desconhecido ou seção fora da faixa 1..6")
	}
	if confianca < 0 || confianca > 1 {
		validacao.Acrescentar("confianca", "a confiança precisa estar entre 0 e 1")
	}
	if !origem.Valido() {
		validacao.Acrescentar("origem", "origem de classificação desconhecida")
	}
	if refXML < 0 {
		validacao.Acrescentar("ref_xml", "a referência ao nó XML é um índice ordinal, nunca negativo")
	}

	if validacao.TemCampos() {
		return Bloco{}, validacao
	}

	return Bloco{
		Papel:       papel,
		TextoResumo: textoResumo,
		Confianca:   confianca,
		Origem:      origem,
		RefXML:      refXML,
	}, nil
}

// Reclassificar devolve uma cópia do bloco com nova classificação semântica;
// texto e posição nunca mudam.
//
// Bloco corrigido pelo usuário é imutável para as camadas automáticas: a
// tentativa é ignorada silenciosamente (no-op, sem erro), porque rodar a
// heurística de novo é fluxo normal do sistema, não falha de ninguém. Só uma
// nova correção do próprio usuário sobrescreve a anterior.
func (b Bloco) Reclassificar(papel Papel, confianca float64, origem Origem) (Bloco, error) {
	if b.Origem == OrigemUsuario && origem != OrigemUsuario {
		return b, nil
	}
	return NovoBloco(papel, b.TextoResumo, confianca, origem, b.RefXML)
}
