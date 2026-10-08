package entity

import (
	"reflect"
	"testing"

	"github.com/daniel-halos/formatador/internal/infra/errors"
)

func TestTipoJobValido(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome     string
		entrada  TipoJob
		esperado bool
	}{
		{nome: "renderizar preview é válido", entrada: TipoRenderizarPreview, esperado: true},
		{nome: "analisar é válido", entrada: TipoAnalisar, esperado: true},
		{nome: "formatar é válido", entrada: TipoFormatar, esperado: true},
		{nome: "tipo desconhecido é inválido", entrada: TipoJob("compilar"), esperado: false},
		{nome: "tipo vazio é inválido", entrada: TipoJob(""), esperado: false},
		{nome: "tipo com caixa alta é inválido", entrada: TipoJob("FORMATAR"), esperado: false},
		{nome: "tipo com espaço é inválido", entrada: TipoJob(" formatar "), esperado: false},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			if obtido := caso.entrada.Valido(); obtido != caso.esperado {
				t.Fatalf("esperava Valido()==%v para %q, obteve %v", caso.esperado, string(caso.entrada), obtido)
			}
		})
	}
}

func TestTipoJobExigeRuleset(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome     string
		entrada  TipoJob
		esperado bool
	}{
		{nome: "formatar exige ruleset", entrada: TipoFormatar, esperado: true},
		{nome: "analisar não exige ruleset", entrada: TipoAnalisar, esperado: false},
		{nome: "renderizar preview não exige ruleset", entrada: TipoRenderizarPreview, esperado: false},
		{nome: "tipo inválido não exige ruleset", entrada: TipoJob("compilar"), esperado: false},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			if obtido := caso.entrada.ExigeRuleset(); obtido != caso.esperado {
				t.Fatalf("esperava ExigeRuleset()==%v para %q, obteve %v", caso.esperado, string(caso.entrada), obtido)
			}
		})
	}
}

func TestTipoJobString(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome     string
		entrada  TipoJob
		esperado string
	}{
		{nome: "renderizar preview", entrada: TipoRenderizarPreview, esperado: "renderizar_preview"},
		{nome: "analisar", entrada: TipoAnalisar, esperado: "analisar"},
		{nome: "formatar", entrada: TipoFormatar, esperado: "formatar"},
		{nome: "tipo desconhecido devolve o próprio valor", entrada: TipoJob("compilar"), esperado: "compilar"},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			if obtido := caso.entrada.String(); obtido != caso.esperado {
				t.Fatalf("esperava String()==%q, obteve %q", caso.esperado, obtido)
			}
		})
	}
}

// TestTipoJobValidarRulesetParaNovoJobNaoExigente cobre A1 e A4: tipo que não
// exige ruleset devolve nil para qualquer valor do parâmetro, inclusive nil
// (perfil ausente) e ponteiro para false (perfil inativo). Falha se a
// implementação olhar o parâmetro antes de ExigeRuleset. O tipo inválido
// entra aqui de propósito (A4): quem recusa tipo inválido é NovoJob
// (job.go:52), e duas portas escrevendo mensagens concorrentes no mesmo campo
// seria pior do que uma.
func TestTipoJobValidarRulesetParaNovoJobNaoExigente(t *testing.T) {
	t.Parallel()

	ativo, inativo := true, false

	casos := []struct {
		nome  string
		tipo  TipoJob
		ativo *bool
	}{
		{nome: "analisar com perfil ausente", tipo: TipoAnalisar, ativo: nil},
		{nome: "analisar com perfil ativo", tipo: TipoAnalisar, ativo: &ativo},
		{nome: "analisar com perfil inativo", tipo: TipoAnalisar, ativo: &inativo},
		{nome: "renderizar preview com perfil ausente", tipo: TipoRenderizarPreview, ativo: nil},
		{nome: "renderizar preview com perfil ativo", tipo: TipoRenderizarPreview, ativo: &ativo},
		{nome: "renderizar preview com perfil inativo", tipo: TipoRenderizarPreview, ativo: &inativo},
		{nome: "tipo inválido com perfil ausente não é problema deste gate", tipo: TipoJob("formatar_x"), ativo: nil},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			if err := caso.tipo.ValidarRulesetParaNovoJob(caso.ativo); err != nil {
				t.Fatalf("esperava nil para tipo %q, obteve %v", string(caso.tipo), err)
			}
		})
	}
}

// TestTipoJobValidarRulesetParaNovoJobPerfilAtivo é o controle indispensável
// de A2: sem ele, um `return erro` incondicional passaria A3.
func TestTipoJobValidarRulesetParaNovoJobPerfilAtivo(t *testing.T) {
	t.Parallel()

	ativo := true
	if err := TipoFormatar.ValidarRulesetParaNovoJob(&ativo); err != nil {
		t.Fatalf("perfil existente e ativo tem de ser aprovado, obteve %v", err)
	}
}

// TestTipoJobValidarRulesetParaNovoJobRecusaAusenteEInativo cobre A3. Cada
// termo da decisão composta é exercitado sozinho (regra 12): remover
// `ativo == nil` faz o caso ausente voltar nil, remover `!*ativo` faz o
// inativo voltar nil. E o VALOR INTEIRO do erro — Mensagem do envelope E o
// slice Campos — tem de ser idêntico nos dois casos: comparar só Campos
// deixaria passar NovoErroValidacaoCampos("perfil inativo", ...), que
// reabriria a distinção entre ausente e inativo no `descricao` da resposta e
// transformaria o gate em oráculo de enumeração do catálogo.
func TestTipoJobValidarRulesetParaNovoJobRecusaAusenteEInativo(t *testing.T) {
	t.Parallel()

	inativo := false

	erroAusente := TipoFormatar.ValidarRulesetParaNovoJob(nil)
	erroInativo := TipoFormatar.ValidarRulesetParaNovoJob(&inativo)

	var validacaoAusente, validacaoInativo *errors.ErroValidacao
	if !errors.Como(erroAusente, &validacaoAusente) {
		t.Fatalf("perfil ausente devia virar *errors.ErroValidacao, veio %T: %v", erroAusente, erroAusente)
	}
	if !errors.Como(erroInativo, &validacaoInativo) {
		t.Fatalf("perfil inativo devia virar *errors.ErroValidacao, veio %T: %v", erroInativo, erroInativo)
	}

	for nome, validacao := range map[string]*errors.ErroValidacao{"ausente": validacaoAusente, "inativo": validacaoInativo} {
		if len(validacao.Campos) != 1 {
			t.Fatalf("caso %s: esperava exatamente um campo reprovado, obteve %v", nome, validacao.Campos)
		}
		if validacao.Campos[0].Campo != "ruleset_id" {
			t.Fatalf("caso %s: esperava campo ruleset_id, obteve %q", nome, validacao.Campos[0].Campo)
		}
	}

	if !reflect.DeepEqual(*validacaoAusente, *validacaoInativo) {
		t.Fatalf("ausente e inativo têm de ser indistinguíveis no valor inteiro do erro: %#v vs %#v",
			*validacaoAusente, *validacaoInativo)
	}
}
