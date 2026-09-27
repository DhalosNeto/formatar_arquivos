package jobs

import "github.com/daniel-halos/formatador/internal/rotas"

// Roteador monta as rotas de job.
func Roteador(controlador *Controlador) rotas.Roteador {
	roteador := rotas.NovoRoteador()
	roteador.Adicionar(rotas.Get, CaminhoItem, controlador.TratarObtencao)
	return roteador
}
