// Package rotasutil traduz erros de domínio em respostas HTTP.
package rotasutil

// Códigos de erro devolvidos pela API, estáveis o bastante para o front reagir a eles.
const (
	CodigoErroInterno        = "erro_interno"
	CodigoRequisicaoInvalida = "requisicao_invalida"
	CodigoNaoAutorizado      = "acesso_nao_autorizado"
	CodigoAcessoRestrito     = "acesso_restrito"
	CodigoNaoEncontrado      = "recurso_nao_encontrado"
	CodigoConflito           = "conflito"
	CodigoLimiteExcedido     = "limite_de_requisicoes_excedido"
	CodigoArquivoInvalido    = "arquivo_invalido"
	CodigoArquivoMuitoGrande = "arquivo_muito_grande"
)
