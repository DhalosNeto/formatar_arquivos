package ooxml

import (
	"strconv"

	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// AplicarEspacamentoAntes grava w:before em twips nos parágrafos selecionados.
// Garante o valor direto no XML, não a distância visual após herança e layout.
func (documento *Documento) AplicarEspacamentoAntes(referencias []int, pontos float64) error {
	return documento.aplicarEspacamentoParagrafo(referencias, pontos, "before")
}

// AplicarEspacamentoDepois grava w:after em twips nos parágrafos selecionados.
// Garante o valor direto no XML, não a distância visual após herança e layout.
func (documento *Documento) AplicarEspacamentoDepois(referencias []int, pontos float64) error {
	return documento.aplicarEspacamentoParagrafo(referencias, pontos, "after")
}

func (documento *Documento) aplicarEspacamentoParagrafo(referencias []int, pontos float64, atributo string) error {
	if documento == nil {
		return errors.NovoErroArgumentoNulo("documento")
	}
	twips, err := vo.PontosParaTwips(pontos)
	if err != nil {
		return err
	}
	return documento.aplicarPropriedadeParagrafos(referencias, "spacing", map[string]string{
		atributo: strconv.Itoa(twips),
	})
}
