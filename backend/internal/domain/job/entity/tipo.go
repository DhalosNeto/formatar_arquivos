package entity

import "github.com/daniel-halos/formatador/internal/infra/errors"

// TipoJob é a espécie de trabalho que um job representa.
type TipoJob string

const (
	TipoRenderizarPreview TipoJob = "renderizar_preview"
	TipoAnalisar          TipoJob = "analisar"
	TipoFormatar          TipoJob = "formatar"
)

// Valido informa se o tipo é um dos suportados.
func (tipo TipoJob) Valido() bool {
	switch tipo {
	case TipoRenderizarPreview, TipoAnalisar, TipoFormatar:
		return true
	default:
		return false
	}
}

// ExigeRuleset informa se o tipo precisa de um perfil de formatação.
// Só formatar exige: análise e preview não escolhem norma.
func (tipo TipoJob) ExigeRuleset() bool { return tipo == TipoFormatar }

// ValidarRulesetParaNovoJob aprova ou recusa o perfil de formatação de um job
// NOVO. Regra pura: o parâmetro é o flag `ativo` já lido do catálogo, e nil
// significa PERFIL AUSENTE — ausente CONFIRMADO, nunca "não foi possível
// saber". Quem chama só pode passar nil depois de uma leitura que devolveu
// zero linhas; mapear falha de leitura para nil transformaria erro de
// infraestrutura em 400 culpando o cliente.
//
// Tipo que não exige ruleset devolve nil ignorando o parâmetro, e tipo
// inválido cai aqui também: quem recusa tipo inválido é NovoJob (job.go:52),
// e duas portas com mensagens concorrentes no mesmo campo seria pior.
// Atenção: ExigeRuleset é FAIL-OPEN para tipo novo (é `tipo == TipoFormatar`),
// logo um tipo futuro que exija norma escapa deste gate em silêncio.
//
// Perfil ausente e perfil inativo devolvem o MESMO valor de erro, por um único
// return: a indistinguibilidade é requisito de segurança, senão o gate vira
// oráculo de enumeração do catálogo. Por isso também não recebe UUID algum.
// Complementa NovoJob (job.go:51-56), que já exige RulesetID presente no mesmo
// campo: lá o que se sabe sem banco, aqui o que só o banco sabe.
func (tipo TipoJob) ValidarRulesetParaNovoJob(ativo *bool) error {
	if !tipo.ExigeRuleset() {
		return nil
	}
	if ativo == nil || !*ativo {
		return errors.NovoErroValidacao("ruleset_id", "perfil de formatação indisponível")
	}
	return nil
}

func (tipo TipoJob) String() string { return string(tipo) }
