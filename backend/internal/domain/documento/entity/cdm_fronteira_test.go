// Este arquivo é a fronteira entre os dois pacotes irmãos de domínio: cdm
// (internal/domain/cdm) NÃO pode importar entity (criaria acoplamento entre
// dois pacotes que hoje só se comunicam por json.RawMessage), então o teste
// que prova "o que cdm.Indice.Serializar produz passa em entity.ValidarCDM"
// não pode morar em cdm. entity já é livre para importar cdm num arquivo de
// teste — só o sentido cdm→entity é proibido, não o inverso — e é exatamente
// aqui, no dono de ValidarCDM, que faz sentido garantir que o formato do
// vizinho é aceito pelo portão de entrada do agregado que vai persistir.
//
// FASE VERMELHA: depende de cdm.NovoIndice e cdm.Indice.Serializar, que
// ainda não existem. Prova indiretamente a mesma falta de símbolo que
// internal/domain/cdm/serializacao_test.go prova diretamente.
package entity

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-halos/formatador/internal/domain/cdm"
)

func TestIndiceSerializadoPassaEmValidarCDM(t *testing.T) {
	t.Parallel()

	blocoTitulo, err := cdm.NovoBloco(cdm.Titulo, "Título do artigo", 0.95, cdm.OrigemEstiloDocx, 0)
	require.NoError(t, err)
	blocoSecao, err := cdm.NovoBloco(cdm.Secao(1), "1 Introdução", 0.9, cdm.OrigemHeuristica, 1)
	require.NoError(t, err)
	blocoTabela, err := cdm.NovoBloco(cdm.Tabela, "conteúdo da tabela", 0.85, cdm.OrigemEstiloDocx, 2)
	require.NoError(t, err)
	// O caso mais importante deste índice: bloco corrigido pelo usuário
	// também precisa passar são e salvo pelo portão de entrada do agregado.
	blocoCorrigido, err := cdm.NovoBloco(cdm.Resumo, "texto que o usuário classificou", 1.0, cdm.OrigemUsuario, 3)
	require.NoError(t, err)

	indice := cdm.NovoIndice([]cdm.Bloco{blocoTitulo, blocoSecao, blocoTabela, blocoCorrigido})
	dados, err := indice.Serializar()
	require.NoError(t, err)

	assert.NoError(t, ValidarCDM(dados),
		"o formato que cdm.Indice.Serializar produz precisa ser aceito pelo portão de entrada do agregado Documento")
}

func TestIndiceVazioSerializadoAindaPassaEmValidarCDM(t *testing.T) {
	t.Parallel()

	// Documento sem nenhum bloco classificado ainda é um objeto JSON válido
	// e não-vazio ({"versao":1,"blocos":...}): ValidarCDM só recusa bytes
	// vazios ou que não comecem em "{", não uma lista de blocos vazia.
	dados, err := cdm.NovoIndice(nil).Serializar()
	require.NoError(t, err)

	assert.NoError(t, ValidarCDM(dados))
}
