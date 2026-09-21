package ooxml

import (
	"bytes"

	"github.com/daniel-halos/formatador/internal/domain/cdm"
)

// AnalisarEstrutura roda o pipeline de classificação sobre um pacote DOCX em
// memória e devolve o índice de blocos já classificado.
//
// Existe para que quem dispara a análise — o executor da fila — não precise
// conhecer a ORDEM das camadas. A ordem importa: a camada 2 recebe o que a
// camada 1 produziu e usa a confiança dela para decidir o que reescrever.
// Espalhar essa sequência por cada chamador é como ela passa a divergir.
//
// Erro de estrutura do arquivo sobe como *errors.ErroValidacao, vindo de
// Abrir e de ExtrairBlocos: é culpa do arquivo enviado, não do servidor.
func AnalisarEstrutura(docx []byte) ([]cdm.Bloco, error) {
	documento, err := Abrir(bytes.NewReader(docx), int64(len(docx)))
	if err != nil {
		return nil, err
	}

	brutos, err := documento.ExtrairBlocos()
	if err != nil {
		return nil, err
	}

	classificados := make([]cdm.Bloco, len(brutos))
	for i, bruto := range brutos {
		classificados[i] = ClassificarPorEstiloDocx(bruto)
	}

	return cdm.AplicarHeuristica(classificados)
}
