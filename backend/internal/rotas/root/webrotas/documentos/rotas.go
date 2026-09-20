package documentos

import "github.com/daniel-halos/formatador/internal/rotas"

// Roteador monta as rotas de documento.
//
// A criação recebe LimitarCorpo como middleware de rota, e não global: o teto
// de bytes precisa valer para o upload sem estrangular as demais rotas, que
// trafegam apenas JSON pequeno. É a primeira barreira do achado A2 — o corpo
// nunca é lido inteiro só porque o cliente declarou um tamanho.
func Roteador(controlador *Controlador, tamanhoMaximoUploadBytes int64) rotas.Roteador {
	roteador := rotas.NovoRoteador()
	roteador.Adicionar(rotas.Post, CaminhoColecao, controlador.TratarCriacao,
		rotas.LimitarCorpo(tamanhoMaximoUploadBytes))
	roteador.Adicionar(rotas.Get, CaminhoColecao, controlador.TratarListagem)
	roteador.Adicionar(rotas.Get, CaminhoItem, controlador.TratarObtencao)
	roteador.Adicionar(rotas.Get, CaminhoPreview, controlador.TratarPreview)
	return roteador
}
