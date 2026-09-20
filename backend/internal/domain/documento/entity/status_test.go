package entity

import "testing"

// todosOsStatus é a lista canônica esperada pelo contrato. Qualquer acréscimo ou
// remoção no domínio precisa passar por aqui antes.
var todosOsStatus = []Status{
	StatusRecebido,
	StatusAnalisando,
	StatusAnalisado,
	StatusFormatando,
	StatusFormatado,
	StatusFalhou,
}

// transicoesPermitidas é a tabela de verdade das transições de status.
var transicoesPermitidas = map[Status][]Status{
	StatusRecebido:   {StatusAnalisando, StatusFalhou},
	StatusAnalisando: {StatusAnalisado, StatusFalhou},
	StatusAnalisado:  {StatusAnalisando, StatusFormatando, StatusFalhou},
	StatusFormatando: {StatusFormatado, StatusFalhou},
	StatusFormatado:  {StatusAnalisando, StatusFormatando, StatusFalhou},
	StatusFalhou:     {StatusAnalisando, StatusFormatando},
}

func TestConjuntoDeStatusEhExatamenteOEsperado(t *testing.T) {
	t.Parallel()

	textosEsperados := []string{"recebido", "analisando", "analisado", "formatando", "formatado", "falhou"}

	if len(todosOsStatus) != len(textosEsperados) {
		t.Fatalf("esperava %d status, a lista tem %d", len(textosEsperados), len(todosOsStatus))
	}

	for indice, status := range todosOsStatus {
		if !status.Valido() {
			t.Fatalf("o status %q deveria ser válido", status)
		}
		if status.String() != textosEsperados[indice] {
			t.Fatalf("String() = %q, esperava %q", status.String(), textosEsperados[indice])
		}
	}

	vistos := make(map[Status]bool, len(todosOsStatus))
	for _, status := range todosOsStatus {
		if vistos[status] {
			t.Fatalf("o status %q aparece duplicado", status)
		}
		vistos[status] = true
	}
}

func TestStatusForaDoConjuntoEhInvalido(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome   string
		status Status
	}{
		{nome: "status vazio", status: Status("")},
		{nome: "status inventado", status: Status("cancelado")},
		{nome: "status em maiúsculas", status: Status("RECEBIDO")},
		{nome: "status com espaço", status: Status(" recebido")},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			if caso.status.Valido() {
				t.Fatalf("o status %q não deveria ser válido", caso.status)
			}
		})
	}
}

func TestPodeTransitarParaCobreOProdutoCartesiano(t *testing.T) {
	t.Parallel()

	for _, atual := range todosOsStatus {
		for _, novo := range todosOsStatus {
			nome := string(atual) + " para " + string(novo)
			esperado := permitida(atual, novo)

			t.Run(nome, func(t *testing.T) {
				t.Parallel()

				if obtido := atual.PodeTransitarPara(novo); obtido != esperado {
					t.Fatalf("PodeTransitarPara(%q) a partir de %q = %v, esperava %v", novo, atual, obtido, esperado)
				}
			})
		}
	}
}

func TestPodeTransitarParaRejeitaStatusInvalido(t *testing.T) {
	t.Parallel()

	if StatusRecebido.PodeTransitarPara(Status("cancelado")) {
		t.Fatal("não deveria permitir transição para status fora do conjunto")
	}
	if Status("cancelado").PodeTransitarPara(StatusAnalisando) {
		t.Fatal("não deveria permitir transição a partir de status fora do conjunto")
	}
}

func permitida(atual, novo Status) bool {
	for _, candidato := range transicoesPermitidas[atual] {
		if candidato == novo {
			return true
		}
	}
	return false
}
