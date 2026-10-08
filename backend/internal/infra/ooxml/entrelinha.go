package ooxml

import (
	"strconv"

	"github.com/daniel-halos/formatador/internal/domain/ruleset"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// AplicarEntrelinha escreve o múltiplo de linha nos parágrafos selecionados.
// Define line e lineRule=auto, preservando espaçamento antes/depois e estilos.
// Não altera snapToGrid/docGrid nem promete equivalência visual entre editores.
// Referências usam os ordinais de ExtrairBlocos; tabelas não podem ser alvos.
func (documento *Documento) AplicarEntrelinha(referencias []int, multiplo float64) error {
	if documento == nil {
		return errors.NovoErroArgumentoNulo("documento")
	}
	if err := ruleset.ValidarEntrelinha(multiplo); err != nil {
		return err
	}
	unidades, err := vo.EntrelinhaParaUnidades(multiplo)
	if err != nil {
		return err
	}
	return documento.aplicarPropriedadeParagrafos(referencias, "spacing", map[string]string{
		"line":     strconv.Itoa(unidades),
		"lineRule": "auto",
	})
}
