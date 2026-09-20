// Package entity reúne as entidades do agregado de documento: o estado do
// trabalho enviado pelo usuário e as regras que governam sua evolução.
package entity

// Status é o estágio do documento no fluxo de ingestão, análise e formatação.
// Os valores espelham um a um o CHECK documentos_status_valido da migration
// 00001: acrescentar um estado aqui exige nova migration.
type Status string

const (
	// StatusRecebido indica arquivo aceito e armazenado, ainda sem análise.
	StatusRecebido Status = "recebido"
	// StatusAnalisando indica extração de estrutura em andamento.
	StatusAnalisando Status = "analisando"
	// StatusAnalisado indica CDM disponível e pronto para a formatação.
	StatusAnalisado Status = "analisado"
	// StatusFormatando indica aplicação do ruleset em andamento.
	StatusFormatando Status = "formatando"
	// StatusFormatado indica documento formatado e disponível para download.
	StatusFormatado Status = "formatado"
	// StatusFalhou indica interrupção por erro; admite reprocessamento.
	StatusFalhou Status = "falhou"
)

// transicoesValidas devolve os estados alcançáveis a partir do atual.
//
// É função, e não mapa de pacote, de propósito: mapa exportado ao pacote seria
// estado global mutável e frágil sob teste paralelo (CLAUDE.md, regra 4).
//
// Nenhum estado transita para si mesmo — repetir o comando não é progresso e
// mascara retrabalho de worker. Os estados analisado e formatado voltam para
// analisando ou formatando porque o usuário pode trocar de ruleset e
// reprocessar o mesmo documento; falhou volta pelos mesmos motivos, mas não
// pode falhar de novo sem antes ter retomado o trabalho.
func transicoesValidas(atual Status) []Status {
	switch atual {
	case StatusRecebido:
		return []Status{StatusAnalisando, StatusFalhou}
	case StatusAnalisando:
		return []Status{StatusAnalisado, StatusFalhou}
	case StatusAnalisado:
		return []Status{StatusAnalisando, StatusFormatando, StatusFalhou}
	case StatusFormatando:
		return []Status{StatusFormatado, StatusFalhou}
	case StatusFormatado:
		return []Status{StatusAnalisando, StatusFormatando, StatusFalhou}
	case StatusFalhou:
		return []Status{StatusAnalisando, StatusFormatando}
	default:
		return nil
	}
}

// Valido informa se o status é um dos reconhecidos pelo domínio.
func (s Status) Valido() bool {
	switch s {
	case StatusRecebido, StatusAnalisando, StatusAnalisado,
		StatusFormatando, StatusFormatado, StatusFalhou:
		return true
	default:
		return false
	}
}

// String devolve a representação textual do status, mesmo quando inválido.
func (s Status) String() string { return string(s) }

// PodeTransitarPara informa se a mudança do status atual para novo é permitida.
// Recusa quando qualquer um dos dois lados está fora do conjunto conhecido.
func (s Status) PodeTransitarPara(novo Status) bool {
	if !s.Valido() || !novo.Valido() {
		return false
	}
	for _, permitido := range transicoesValidas(s) {
		if permitido == novo {
			return true
		}
	}
	return false
}
