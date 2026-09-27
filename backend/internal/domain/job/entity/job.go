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

// Limites do job.
const (
	ProgressoMinimo             = 0
	ProgressoMaximo             = 100
	TamanhoMaximoResultadoBytes = 64 << 10
)

// Job é uma unidade de trabalho assíncrono sobre um documento.
//
// A tabela `jobs` é a própria fila do sistema: não há broker externo, e a
// reivindicação usa FOR UPDATE SKIP LOCKED no Postgres (ver
// docs/adr/0002-fila-sem-river.md). As transições abaixo existem para que dois
// workers concorrentes não avancem o mesmo job duas vezes.
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

// NovoJob cria um job pendente, acumulando todos os campos reprovados num só
// erro de validação.
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

// Iniciar move o job para executando e incrementa Tentativas.
//
// O incremento acontece aqui, não no fim: um job que morre no meio precisa
// deixar registro de que foi tentado, senão um retry futuro nunca saberia
// quantas vezes já falhou.
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

// DefinirProgresso avança o progresso do job em execução.
//
// Progresso não retrocede: uma barra que anda para trás é pior que uma barra
// parada, porque quem olha conclui que o trabalho foi perdido.
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

// Concluir finaliza o job com sucesso, guardando o resultado.
//
// O resultado tem teto de TamanhoMaximoResultadoBytes e é metadado INTERNO:
// nunca vai para DTO público nem para log. Conteúdo de documento do usuário
// não entra aqui (regra 7).
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

// Falhar finaliza o job com erro.
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

// Cancelar encerra o job sem execução.
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

// Reenfileirar devolve o job para pendente, permitindo nova tentativa.
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

// Terminal informa se o job já chegou a um estado final.
func (job Job) Terminal() bool { return job.Status.Terminal() }

// Duracao devolve quanto o job levou, e false quando ele ainda não começou ou
// não terminou.
func (job Job) Duracao() (time.Duration, bool) {
	if job.IniciadoEm == nil || job.FinalizadoEm == nil {
		return 0, false
	}
	return job.FinalizadoEm.Sub(*job.IniciadoEm), true
}
