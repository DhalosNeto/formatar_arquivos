// Package formatador monta o plano de formatação: o que aplicar e onde,
// decidido a partir do índice semântico (CDM) e do perfil da revista
// (ruleset). É domínio puro — não conhece OOXML, ZIP, banco nem HTTP. Quem
// executa o plano é um adaptador de infra.
package formatador

import (
	"sort"

	"github.com/daniel-halos/formatador/internal/domain/cdm"
	"github.com/daniel-halos/formatador/internal/domain/ruleset"
)

// Plano é a decisão já tomada: as medidas da página, a tipografia do corpo e
// os ordinais dos blocos que recebem a formatação de corpo.
//
// ReferenciasCorpo carrega RefXML, a posição ordinal do bloco entre os filhos
// diretos de w:body — metadado estrutural, nunca texto do usuário.
type Plano struct {
	Pagina           ruleset.Pagina
	Corpo            ruleset.Corpo
	ReferenciasCorpo []int
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

	// Só cdm.Paragrafo: título, seção, tabela, figura, legenda, citação,
	// referência e nota têm regra própria e estão fora deste recorte.
	referencias := make([]int, 0, len(indice.Blocos))
	for _, bloco := range indice.Blocos {
		if bloco.Papel == cdm.Paragrafo {
			referencias = append(referencias, bloco.RefXML)
		}
	}
	// Ordem crescente: o índice pode vir reordenado por correção do usuário, e
	// os mutadores de corpo trabalham por ordinal. Validar já garantiu que não
	// há RefXML repetido, então ordenar não precisa deduplicar.
	sort.Ints(referencias)

	return Plano{
		Pagina:           definicao.Pagina,
		Corpo:            definicao.Corpo,
		ReferenciasCorpo: referencias,
	}, nil
}
