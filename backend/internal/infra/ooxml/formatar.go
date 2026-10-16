package ooxml

import (
	"maps"

	"github.com/daniel-halos/formatador/internal/domain/formatador"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// AplicarPlano executa um formatador.Plano sobre o pacote: a página nas seções
// correntes e, nos ordinais de plano.ReferenciasCorpo, as propriedades de
// corpo que o perfil DECLARA. ReferenciasCorpo vazio aplica somente a página.
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
	for _, etapa := range etapas {
		if err := etapa(); err != nil {
			return err
		}
	}

	documento.partesSubstituidas = copia.partesSubstituidas
	return nil
}
