package entity

import "testing"

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
