package saude

import "github.com/daniel-halos/formatador/internal/rotas"

// Caminhos atendidos por este roteador.
const (
	CaminhoSaude     = "/saude"
	CaminhoProntidao = "/prontidao"
)

// Roteador monta as rotas de saúde.
func Roteador(controlador *Controlador) rotas.Roteador {
	roteador := rotas.NovoRoteador()
	roteador.Adicionar(rotas.Get, CaminhoSaude, controlador.TratarSaude)
	roteador.Adicionar(rotas.Get, CaminhoProntidao, controlador.TratarProntidao)
	return roteador
}
