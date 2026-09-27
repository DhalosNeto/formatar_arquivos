package cdm

import "github.com/daniel-halos/formatador/internal/infra/errors"

// ParaPapel converte nome e nível para um papel reconhecido.
func ParaPapel(nome string, nivel int) (Papel, error) {
	papel := Papel{nome: nome, nivel: nivel}
	if !papel.Valido() {
		return Papel{}, errors.NovoErroValidacao("papel", mensagemPapelDesconhecido)
	}
	return papel, nil
}
