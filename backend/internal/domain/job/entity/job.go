package entity

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/daniel-halos/formatador/internal/infra/errors"
)

const (
	ProgressoMinimo             = 0
	ProgressoMaximo             = 100
	TamanhoMaximoResultadoBytes = 64 << 10
)

type Job struct {
	ID          uuid.UUID
	DocumentoID uuid.UUID
	RulesetID   *uuid.UUID
	Tipo        TipoJob
	Status      StatusJob
	Tentativas  int
	Progresso   int
	Erro        string
	// Resultado é metadado interno; não deve ser exposto como DTO público ou logado.
	Resultado    json.RawMessage
	CriadoEm     time.Time
	IniciadoEm   *time.Time
	FinalizadoEm *time.Time
}

func NovoJob(documentoID uuid.UUID, tipo TipoJob, rulesetID *uuid.UUID) (Job, error) {
	validacao := errors.NovoErroValidacaoCampos("job inválido")
	if documentoID == uuid.Nil {
		validacao.Acrescentar("documento_id", "documento obrigatório")
	}
	switch {
	case !tipo.Valido():
		validacao.Acrescentar("tipo", "tipo inválido")
	case tipo.ExigeRuleset():
		if rulesetID == nil || *rulesetID == uuid.Nil {
			validacao.Acrescentar("ruleset_id", "ruleset obrigatório")
		}
	case rulesetID != nil:
		validacao.Acrescentar("ruleset_id", "ruleset não permitido para este tipo")
	}
	if validacao.TemCampos() {
		return Job{}, validacao
	}
	var copiaRulesetID *uuid.UUID
	if rulesetID != nil {
		copia := *rulesetID
		copiaRulesetID = &copia
	}
	return Job{
		ID: uuid.New(), DocumentoID: documentoID, RulesetID: copiaRulesetID,
		Tipo: tipo, Status: StatusPendente, CriadoEm: time.Now().UTC(),
	}, nil
}

func (job *Job) validarTransicao(novo StatusJob) error {
	if !job.Status.PodeTransitarPara(novo) {
		return errors.NovoErroConflito("transição de job não permitida")
	}
	return nil
}

func (job *Job) Iniciar() error {
	if err := job.validarTransicao(StatusExecutando); err != nil {
		return err
	}
	agora := time.Now().UTC()
	job.Status = StatusExecutando
	job.Tentativas++
	job.IniciadoEm = &agora
	return nil
}

func (job *Job) DefinirProgresso(progresso int) error {
	if job.Status != StatusExecutando {
		return errors.NovoErroConflito("job não está em execução")
	}
	if progresso < ProgressoMinimo || progresso > ProgressoMaximo || progresso < job.Progresso {
		return errors.NovoErroValidacao("progresso", "progresso deve estar entre 0 e 100 e não pode retroceder")
	}
	job.Progresso = progresso
	return nil
}

func (job *Job) Concluir(resultado json.RawMessage) error {
	if err := job.validarTransicao(StatusConcluido); err != nil {
		return err
	}
	if resultado != nil {
		if len(resultado) > TamanhoMaximoResultadoBytes {
			return errors.NovoErroValidacao("resultado", "resultado excede 64 KiB")
		}
		if !utf8.Valid(resultado) || !json.Valid(resultado) || !bytes.HasPrefix(bytes.TrimSpace(resultado), []byte("{")) {
			return errors.NovoErroValidacao("resultado", "resultado deve ser um objeto JSON UTF-8 válido")
		}
	}
	job.Resultado = append(json.RawMessage(nil), resultado...)
	job.Progresso = ProgressoMaximo
	job.finalizar(StatusConcluido)
	return nil
}

func (job *Job) Falhar(motivo string) error {
	if err := job.validarTransicao(StatusFalhou); err != nil {
		return err
	}
	if strings.TrimSpace(motivo) == "" {
		return errors.NovoErroValidacao("erro", "motivo obrigatório")
	}
	job.Erro = "Não foi possível processar o documento."
	job.finalizar(StatusFalhou)
	return nil
}

func (job *Job) Cancelar() error {
	if err := job.validarTransicao(StatusCancelado); err != nil {
		return err
	}
	job.finalizar(StatusCancelado)
	return nil
}

func (job *Job) finalizar(status StatusJob) {
	agora := time.Now().UTC()
	job.Status = status
	job.FinalizadoEm = &agora
}

func (job *Job) Reenfileirar() error {
	if err := job.validarTransicao(StatusPendente); err != nil {
		return err
	}
	job.Status = StatusPendente
	job.Erro = ""
	job.Resultado = nil
	job.Progresso = ProgressoMinimo
	job.IniciadoEm = nil
	job.FinalizadoEm = nil
	return nil
}

func (job Job) Terminal() bool { return job.Status.Terminal() }

func (job Job) Duracao() (time.Duration, bool) {
	if job.IniciadoEm == nil || job.FinalizadoEm == nil {
		return 0, false
	}
	return job.FinalizadoEm.Sub(*job.IniciadoEm), true
}
