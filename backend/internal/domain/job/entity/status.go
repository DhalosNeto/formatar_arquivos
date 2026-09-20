package entity

type StatusJob string

const (
	StatusPendente   StatusJob = "pendente"
	StatusExecutando StatusJob = "executando"
	StatusConcluido  StatusJob = "concluido"
	StatusFalhou     StatusJob = "falhou"
	StatusCancelado  StatusJob = "cancelado"
)

func (status StatusJob) Valido() bool {
	switch status {
	case StatusPendente, StatusExecutando, StatusConcluido, StatusFalhou, StatusCancelado:
		return true
	default:
		return false
	}
}

func (status StatusJob) String() string { return string(status) }

func (status StatusJob) Terminal() bool {
	return status == StatusConcluido || status == StatusFalhou || status == StatusCancelado
}

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
