package ooxml

import (
	"maps"

	"github.com/daniel-halos/formatador/internal/domain/formatador"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// AplicarPlano executa um formatador.Plano sobre o pacote: a página nas seções
// correntes; nos ordinais de plano.ReferenciasCorpo, as propriedades de corpo
// que o perfil DECLARA; e, por fim, a entrelinha de cada grupo de
// plano.EntrelinhaPorPapel nos ordinais dele. ReferenciasCorpo vazio aplica
// somente a página, e lista de grupos vazia ou nil mantém o comportamento
// anterior a este campo byte a byte.
//
// Custo conhecido: cada grupo reparseia word/document.xml, então os passes vão
// de 6 para 6+k, com k vindo do ruleset versionado no git, não do usuário.
// Sem teto neste recorte.
//
// ATÔMICA: os mutadores rodam sobre uma cópia rasa do documento, com
// partesSubstituidas clonado, e o mapa só é trocado no receptor depois que
// todos devolverem nil. Se qualquer um recusar, nem o delta da página fica —
// um Salvar posterior em destino novo reproduz o pacote original byte a byte.
// A clonagem rasa é segura porque os valores []byte do mapa são imutáveis:
// toda alteração passa por SubstituirParte, que guarda bytes.Clone.
//
// Não revalida o plano: Planejar é a porta de validação e cada mutador
// revalida o que consome. O erro de um mutador é devolvido como veio, sem
// envolver dados do bloco. Sem log: até ReferenciasCorpo é metadado
// estrutural do documento do usuário (regra 7).
func (documento *Documento) AplicarPlano(plano formatador.Plano, ausentes MargensComplementares) error {
	if documento == nil {
		return errors.NovoErroArgumentoNulo("documento")
	}

	copia := *documento
	// maps.Clone(nil) devolve nil, e SubstituirParte aloca sob demanda.
	copia.partesSubstituidas = maps.Clone(documento.partesSubstituidas)

	if err := copia.AplicarPagina(plano.Pagina, ausentes); err != nil {
		return err
	}

	referencias := plano.ReferenciasCorpo
	corpo := plano.Corpo
	// Campo nil é norma silente: o mutador é PULADO e aquele aspecto do
	// documento sai preservado byte a byte, o que é o espírito do ADR 0001.
	// Pular não é falha. Página e entrelinha são normativas e obrigatórias no
	// schema, então rodam sempre. Os ponteiros aliasam a Definicao e só são
	// lidos — nunca desreferenciados para escrita.
	var etapas []func() error
	if corpo.Alinhamento != nil {
		etapas = append(etapas, func() error { return copia.AplicarAlinhamento(referencias, *corpo.Alinhamento) })
	}
	etapas = append(etapas, func() error { return copia.AplicarEntrelinha(referencias, corpo.Entrelinha) })
	if corpo.RecuoCM != nil {
		etapas = append(etapas, func() error { return copia.AplicarRecuoPrimeiraLinha(referencias, *corpo.RecuoCM) })
	}
	if corpo.EspacoAntesPT != nil {
		etapas = append(etapas, func() error { return copia.AplicarEspacamentoAntes(referencias, *corpo.EspacoAntesPT) })
	}
	if corpo.EspacoDepoisPT != nil {
		etapas = append(etapas, func() error { return copia.AplicarEspacamentoDepois(referencias, *corpo.EspacoDepoisPT) })
	}
	etapas = append(etapas, func() error { return copia.AplicarTipografia(referencias, corpo.Fonte, corpo.TamanhoPT) })
	// Os grupos por papel entram DEPOIS das etapas de corpo. A posição é
	// indiferente ao resultado, porque Planejar garante que os conjuntos são
	// disjuntos entre si e de ReferenciasCorpo; ela é fixada só para a saída
	// ser determinística. Cada grupo recebe SOMENTE entrelinha neste recorte.
	// AplicarEntrelinha já valida a medida e recusa ordinal negativo,
	// repetido, fora do documento e alvo w:tbl — o plano não revalida —, e
	// escreve pela cópia transacional via SubstituirParte, então um grupo que
	// recusa também não deixa delta.
	for _, grupo := range plano.EntrelinhaPorPapel {
		etapas = append(etapas, func() error { return copia.AplicarEntrelinha(grupo.Referencias, grupo.Entrelinha) })
	}
	for _, etapa := range etapas {
		if err := etapa(); err != nil {
			return err
		}
	}

	documento.partesSubstituidas = copia.partesSubstituidas
	return nil
}
