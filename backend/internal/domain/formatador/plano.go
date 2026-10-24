// Package formatador monta o plano de formatação: o que aplicar e onde,
// decidido a partir do índice semântico (CDM) e do perfil da revista
// (ruleset). É domínio puro — não conhece OOXML, ZIP, banco nem HTTP. Quem
// executa o plano é um adaptador de infra.
package formatador

import (
	"maps"
	"slices"
	"sort"

	"github.com/daniel-halos/formatador/internal/domain/cdm"
	"github.com/daniel-halos/formatador/internal/domain/ruleset"
)

// Plano é a decisão já tomada: as medidas da página, a tipografia do corpo e
// os ordinais dos blocos que recebem a formatação de corpo.
//
// ReferenciasCorpo carrega RefXML, a posição ordinal do bloco entre os filhos
// diretos de w:body — metadado estrutural, nunca texto do usuário.
// Corpo é copiado POR VALOR, mas cinco de seus campos são PONTEIRO — Fonte,
// Alinhamento, RecuoCM, EspacoAntesPT e EspacoDepoisPT, em que nil significa
// "a norma é silente, preserve o documento". Logo o Plano ALIASA a Definicao
// de onde veio. Esses cinco são SOMENTE LEITURA: nenhum consumidor pode
// desreferenciar para escrever, senão altera o ruleset lido do banco por baixo
// de quem o compartilhe. Quem acrescentar campo opcional herda a regra.
type Plano struct {
	Pagina           ruleset.Pagina
	Corpo            ruleset.Corpo
	ReferenciasCorpo []int
	// EntrelinhaPorPapel são os papéis de exceção que recebem entrelinha
	// própria, um grupo por papel, ordenados por Papel.String() crescente.
	EntrelinhaPorPapel []GrupoEntrelinha
}

// GrupoEntrelinha é a medida de entrelinha de um papel de exceção e os
// ordinais dos blocos autorizados que a recebem.
//
// Carrega SÓ valores — float64 e int —, então a regra de aliasing do Plano não
// o alcança: Referencias é uma slice que Planejar aloca, não um pedaço da
// Definicao. Quem acrescentar campo aqui herda as duas regras: se for ponteiro
// para dentro do ruleset, é SOMENTE LEITURA como os cinco de Corpo.
type GrupoEntrelinha struct {
	Papel       cdm.Papel
	Entrelinha  float64
	Referencias []int
}

// Planejar valida as duas entradas e seleciona os parágrafos genéricos do
// corpo.
//
// É a porta de validação do fluxo: o adaptador que executa o plano não
// revalida. Os erros das duas fontes sobem como vieram, sem reclassificação —
// o *ErroValidacao da definição precisa continuar virando 400, e
// cdm.ErroRefXMLDuplicado precisa continuar identificável por errors.E para
// que quem leu o índice do banco o trate como corrupção do servidor.
//
// Função pura: sem I/O, sem context.Context, sem estado de pacote, sem log.
func Planejar(indice cdm.Indice, definicao ruleset.Definicao) (Plano, error) {
	if err := indice.Validar(); err != nil {
		return Plano{}, err
	}
	if err := definicao.Validar(); err != nil {
		return Plano{}, err
	}

	// ReferenciasCorpo é só cdm.Paragrafo: título, seção, tabela, figura e
	// nota têm regra própria e estão fora deste recorte. Legenda, citação e
	// referência só saem daqui para um grupo quando o perfil declara a medida
	// E a origem autoriza.
	referencias := make([]int, 0, len(indice.Blocos))
	porPapel := make(map[string][]int, len(definicao.EntrelinhaPorPapel))
	for _, bloco := range indice.Blocos {
		if bloco.Papel == cdm.Paragrafo {
			referencias = append(referencias, bloco.RefXML)
			continue
		}
		if _, declarado := definicao.EntrelinhaPorPapel[bloco.Papel.String()]; !declarado {
			continue
		}
		// PISO DE AUTORIZAÇÃO: muta só o bloco cujo papel o DOCUMENTO declara
		// (OrigemEstiloDocx) ou cujo papel o dono confirmou (OrigemUsuario).
		// Palpite automático — recuo, região, regra textual, LLM — não
		// autoriza, por confiança nenhuma: o bloco fica fora do plano
		// INTEIRO, e como ReferenciasCorpo exige Papel == cdm.Paragrafo,
		// nenhum mutador o alcança e o parágrafo sai preservado.
		if bloco.Origem != cdm.OrigemEstiloDocx && bloco.Origem != cdm.OrigemUsuario {
			continue
		}
		porPapel[bloco.Papel.String()] = append(porPapel[bloco.Papel.String()], bloco.RefXML)
	}
	// Ordem crescente: o índice pode vir reordenado por correção do usuário, e
	// os mutadores de corpo trabalham por ordinal. Validar já garantiu que não
	// há RefXML repetido, então ordenar não precisa deduplicar.
	sort.Ints(referencias)

	grupos, err := agruparPorPapel(porPapel, definicao.EntrelinhaPorPapel)
	if err != nil {
		return Plano{}, err
	}

	return Plano{
		Pagina:             definicao.Pagina,
		Corpo:              definicao.Corpo,
		ReferenciasCorpo:   referencias,
		EntrelinhaPorPapel: grupos,
	}, nil
}

// agruparPorPapel monta a saída na ordem de Papel.String() crescente.
//
// Só emite grupo que tenha bloco autorizado: papel declarado pelo perfil sem
// nenhum bloco correspondente no índice não vira grupo vazio.
//
// A ordenação por nome SÓ é total porque a lista branca do ruleset exclui
// "secao" — Secao(1) e Secao(2) têm o mesmo Papel.String(). Se alguém ampliar
// a lista branca para seções, a chave de ordenação precisa passar a incluir o
// nível.
func agruparPorPapel(porPapel map[string][]int, medidas map[string]float64) ([]GrupoEntrelinha, error) {
	grupos := make([]GrupoEntrelinha, 0, len(porPapel))
	for _, nome := range slices.Sorted(maps.Keys(porPapel)) {
		// As chaves vêm de Papel.String() de blocos que o índice já validou,
		// então ParaPapel não falha aqui; o erro sobe em vez de virar pânico.
		papel, err := cdm.ParaPapel(nome, 0)
		if err != nil {
			return nil, err
		}
		referencias := porPapel[nome]
		sort.Ints(referencias)
		grupos = append(grupos, GrupoEntrelinha{Papel: papel, Entrelinha: medidas[nome], Referencias: referencias})
	}
	return grupos, nil
}
