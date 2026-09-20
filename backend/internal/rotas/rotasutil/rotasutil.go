package rotasutil

import (
	"context"
	"net/http"

	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/daniel-halos/formatador/internal/infra/log"
	"github.com/daniel-halos/formatador/internal/rotas"
)

// descricaoGenerica é o que o cliente vê quando o erro é interno. O detalhe
// real fica só no log: mensagem de erro interna não vaza para fora.
const descricaoGenerica = "erro interno ao processar a requisição"

// TratarErro converte um erro de domínio na resposta HTTP correspondente.
// Todo manipulador deve terminar com `return rotasutil.TratarErro(ctx, resposta, err)`.
func TratarErro(ctx context.Context, resposta rotas.Resposta, err error) error {
	status, codigo, descricao, razoes := Classificar(err)

	if status >= http.StatusInternalServerError {
		log.De(ctx).Error("falha ao processar requisição", "erro", err.Error(), "status", status)
	} else {
		log.De(ctx).Info("requisição rejeitada", "codigo", codigo, "status", status)
	}

	return resposta.Erro(status, codigo, descricao, razoes)
}

// Classificar mapeia um erro de domínio para status, código, descrição e razões.
// Erros desconhecidos viram 500 com descrição genérica: detalhe interno não vaza.
func Classificar(err error) (status int, codigo, descricao string, razoes []string) {
	var invalido *errors.ErroValidacao
	if errors.Como(err, &invalido) {
		for _, campo := range invalido.Campos {
			razoes = append(razoes, campo.String())
		}
		return http.StatusBadRequest, CodigoRequisicaoInvalida, invalido.Mensagem, razoes
	}

	var naoEncontrado *errors.ErroNaoEncontrado
	if errors.Como(err, &naoEncontrado) {
		return http.StatusNotFound, CodigoNaoEncontrado, naoEncontrado.Error(), nil
	}

	var conflito *errors.ErroConflito
	if errors.Como(err, &conflito) {
		return http.StatusConflict, CodigoConflito, conflito.Error(), nil
	}

	var naoAutorizado *errors.ErroNaoAutorizado
	if errors.Como(err, &naoAutorizado) {
		return http.StatusUnauthorized, CodigoNaoAutorizado, naoAutorizado.Error(), nil
	}

	var proibido *errors.ErroProibido
	if errors.Como(err, &proibido) {
		return http.StatusForbidden, CodigoAcessoRestrito, proibido.Error(), nil
	}

	return http.StatusInternalServerError, CodigoErroInterno, descricaoGenerica, nil
}
