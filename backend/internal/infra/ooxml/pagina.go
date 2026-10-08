package ooxml

import (
	"bytes"
	"io"

	"github.com/daniel-halos/formatador/internal/domain/ruleset"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// MargensComplementares fornece as medidas de cabeçalho, rodapé e medianiz
// somente quando o atributo correspondente ainda não existe no documento.
type MargensComplementares struct {
	CabecalhoTwips int
	RodapeTwips    int
	MedianizTwips  int
}

const limitePaginaTwips = 31680

type medidasPagina struct {
	largura, altura                       int
	superior, inferior, esquerda, direita int
	cabecalho, rodape, medianiz           int
}

// AplicarPagina atualiza tamanho e margens das seções correntes, preservando
// lexicalmente o XML e aplicando a parte principal de forma atômica.
func (documento *Documento) AplicarPagina(pagina ruleset.Pagina, ausentes MargensComplementares) error {
	if documento == nil {
		return errors.NovoErroArgumentoNulo("documento")
	}
	medidas, err := converterPagina(pagina, ausentes)
	if err != nil {
		return err
	}
	conteudo, err := documento.abrirDocumentoPrincipal()
	if err != nil {
		return errors.NovoErroAplicacao("não foi possível ler o documento principal")
	}
	defer func() { _ = conteudo.Close() }()
	original, err := io.ReadAll(io.LimitReader(conteudo, limiteXMLPagina+1))
	if err != nil {
		return errors.NovoErroAplicacao("não foi possível ler o documento principal")
	}
	if len(original) > limiteXMLPagina {
		return erroXMLPagina()
	}
	alterado, mudou, err := aplicarPaginaXML(original, medidas)
	if err != nil {
		return err
	}
	if !mudou || bytes.Equal(original, alterado) {
		return nil
	}
	if err := documento.SubstituirParte(parteDocumentoPrincipal, alterado); err != nil {
		return err
	}
	return nil
}

func converterPagina(pagina ruleset.Pagina, ausentes MargensComplementares) (medidasPagina, error) {
	if err := pagina.Validar(); err != nil {
		return medidasPagina{}, erroParametrosPagina()
	}
	valores := []float64{pagina.LarguraCM, pagina.AlturaCM, pagina.Margens.SuperiorCM, pagina.Margens.InferiorCM, pagina.Margens.EsquerdaCM, pagina.Margens.DireitaCM}
	convertidos := make([]int, len(valores))
	for i, valor := range valores {
		convertido, err := vo.CentimetrosParaTwips(valor)
		if err != nil || convertido < 0 || convertido > limitePaginaTwips {
			return medidasPagina{}, erroParametrosPagina()
		}
		convertidos[i] = convertido
	}
	for _, valor := range []int{ausentes.CabecalhoTwips, ausentes.RodapeTwips, ausentes.MedianizTwips} {
		if valor < 0 || valor > limitePaginaTwips {
			return medidasPagina{}, erroParametrosPagina()
		}
	}
	if convertidos[2]+convertidos[3] >= convertidos[1] || convertidos[4]+convertidos[5] >= convertidos[0] {
		return medidasPagina{}, erroParametrosPagina()
	}
	return medidasPagina{
		largura: convertidos[0], altura: convertidos[1],
		superior: convertidos[2], inferior: convertidos[3], esquerda: convertidos[4], direita: convertidos[5],
		cabecalho: ausentes.CabecalhoTwips, rodape: ausentes.RodapeTwips, medianiz: ausentes.MedianizTwips,
	}, nil
}

func erroParametrosPagina() error {
	return errors.NovoErroValidacao("pagina", "medidas de página inválidas")
}
