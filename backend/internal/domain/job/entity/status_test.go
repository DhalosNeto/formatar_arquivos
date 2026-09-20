package entity

import "testing"

// statusConhecidos é o conjunto fechado de status aceitos pelo domínio.
var statusConhecidos = []StatusJob{
	StatusPendente,
	StatusExecutando,
	StatusConcluido,
	StatusFalhou,
	StatusCancelado,
}

// transicoesPermitidas replica, de forma independente da implementação, a
// tabela de transições definida no contrato do domínio.
var transicoesPermitidas = map[StatusJob][]StatusJob{
	StatusPendente:   {StatusExecutando, StatusCancelado},
	StatusExecutando: {StatusConcluido, StatusFalhou, StatusCancelado},
	StatusConcluido:  {},
	StatusFalhou:     {StatusPendente},
	StatusCancelado:  {},
}

func TestStatusJobValido(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome     string
		entrada  StatusJob
		esperado bool
	}{
		{nome: "pendente é válido", entrada: StatusPendente, esperado: true},
		{nome: "executando é válido", entrada: StatusExecutando, esperado: true},
		{nome: "concluido é válido", entrada: StatusConcluido, esperado: true},
		{nome: "falhou é válido", entrada: StatusFalhou, esperado: true},
		{nome: "cancelado é válido", entrada: StatusCancelado, esperado: true},
		{nome: "status vazio é inválido", entrada: StatusJob(""), esperado: false},
		{nome: "status desconhecido é inválido", entrada: StatusJob("aguardando"), esperado: false},
		{nome: "status com acento é inválido", entrada: StatusJob("concluído"), esperado: false},
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

func TestStatusJobString(t *testing.T) {
	t.Parallel()

	for _, status := range statusConhecidos {
		t.Run("string de "+string(status), func(t *testing.T) {
			t.Parallel()

			if obtido := status.String(); obtido != string(status) {
				t.Fatalf("esperava String()==%q, obteve %q", string(status), obtido)
			}
		})
	}
}

func TestStatusJobTerminal(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome     string
		entrada  StatusJob
		esperado bool
	}{
		{nome: "pendente não é terminal", entrada: StatusPendente, esperado: false},
		{nome: "executando não é terminal", entrada: StatusExecutando, esperado: false},
		{nome: "concluido é terminal", entrada: StatusConcluido, esperado: true},
		{nome: "falhou é terminal", entrada: StatusFalhou, esperado: true},
		{nome: "cancelado é terminal", entrada: StatusCancelado, esperado: true},
		{nome: "status inválido não é terminal", entrada: StatusJob("aguardando"), esperado: false},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			if obtido := caso.entrada.Terminal(); obtido != caso.esperado {
				t.Fatalf("esperava Terminal()==%v para %q, obteve %v", caso.esperado, string(caso.entrada), obtido)
			}
		})
	}
}

func TestStatusJobPodeTransitarParaProdutoCartesiano(t *testing.T) {
	t.Parallel()

	for _, atual := range statusConhecidos {
		for _, novo := range statusConhecidos {
			atual, novo := atual, novo
			esperado := transicaoPermitida(atual, novo)

			nome := "de " + string(atual) + " para " + string(novo)
			t.Run(nome, func(t *testing.T) {
				t.Parallel()

				if obtido := atual.PodeTransitarPara(novo); obtido != esperado {
					t.Fatalf("esperava PodeTransitarPara(%q)==%v a partir de %q, obteve %v",
						string(novo), esperado, string(atual), obtido)
				}
			})
		}
	}
}

func TestStatusJobRejeitaTransicaoParaStatusInvalido(t *testing.T) {
	t.Parallel()

	for _, atual := range statusConhecidos {
		atual := atual
		t.Run("de "+string(atual)+" para status desconhecido", func(t *testing.T) {
			t.Parallel()

			if atual.PodeTransitarPara(StatusJob("aguardando")) {
				t.Fatalf("estado %q não deveria transitar para status desconhecido", string(atual))
			}
			if atual.PodeTransitarPara(StatusJob("")) {
				t.Fatalf("estado %q não deveria transitar para status vazio", string(atual))
			}
		})
	}
}

func transicaoPermitida(atual, novo StatusJob) bool {
	for _, permitido := range transicoesPermitidas[atual] {
		if permitido == novo {
			return true
		}
	}
	return false
}
