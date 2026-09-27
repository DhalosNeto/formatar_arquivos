package entity

// StatusJob é o estado de um job na fila.
type StatusJob string

const (
	StatusPendente   StatusJob = "pendente"
	StatusExecutando StatusJob = "executando"
	StatusConcluido  StatusJob = "concluido"
	StatusFalhou     StatusJob = "falhou"
	StatusCancelado  StatusJob = "cancelado"
)

// Valido informa se o status é um dos conhecidos.
func (status StatusJob) Valido() bool {
	switch status {
	case StatusPendente, StatusExecutando, StatusConcluido, StatusFalhou, StatusCancelado:
		return true
	default:
		return false
	}
}

func (status StatusJob) String() string { return string(status) }

// Terminal informa se o status é final, sem transição de saída.
func (status StatusJob) Terminal() bool {
	return status == StatusConcluido || status == StatusFalhou || status == StatusCancelado
}

// PodeTransitarPara informa se a transição é permitida. É o que impede dois
// workers concorrentes de avançarem o mesmo job duas vezes.
func (status StatusJob) PodeTransitarPara(novo StatusJob) bool {
	switch status {
	case StatusPendente:
		return novo == StatusExecutando || novo == StatusCancelado
	case StatusExecutando:
		return novo == StatusConcluido || novo == StatusFalhou || novo == StatusCancelado
	case StatusFalhou:
		return novo == StatusPendente
	default:
		return false
	}
}
