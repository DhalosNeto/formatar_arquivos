// Package root compõe o roteador completo da API a partir dos roteadores de
// cada recurso.
package root

import (
	"github.com/daniel-halos/formatador/internal/rotas"
	"github.com/daniel-halos/formatador/internal/rotas/root/webrotas/documentos"
	"github.com/daniel-halos/formatador/internal/rotas/root/webrotas/saude"
)

// PrefixoAPI é o prefixo de versão de toda a API.
const PrefixoAPI = "/v1"

// Dependencias reúne o que o roteador raiz precisa para ser montado.
type Dependencias struct {
	Saude                    *saude.Controlador
	Documentos               *documentos.Controlador
	TamanhoMaximoUploadBytes int64
}

// Roteador monta o roteador raiz da aplicação.
// As rotas de saúde ficam fora de qualquer middleware de autenticação, porque
// são consumidas pelo orquestrador de containers e não por usuários.
func Roteador(dependencias Dependencias) rotas.Roteador {
	raiz := rotas.NovoRoteador()
	raiz.Registrar(saude.Roteador(dependencias.Saude), "")
	raiz.Registrar(documentos.Roteador(dependencias.Documentos, dependencias.TamanhoMaximoUploadBytes), "")
	return raiz
}
