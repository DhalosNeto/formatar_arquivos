package webmodel

import (
	"time"

	"github.com/google/uuid"
)

// JobResposta é o retrato público de um job de processamento.
//
// entity.Job.Resultado fica de fora DELIBERADAMENTE: é metadado interno, e o
// resultado de um job de documento carrega chave de storage — nada disso tem
// por que atravessar a fronteira HTTP.
type JobResposta struct {
	ID           uuid.UUID  `json:"id"`
	DocumentoID  uuid.UUID  `json:"documento_id"`
	Tipo         string     `json:"tipo"`
	Status       string     `json:"status"`
	Progresso    int        `json:"progresso"`
	Erro         string     `json:"erro,omitempty"`
	CriadoEm     time.Time  `json:"criado_em"`
	IniciadoEm   *time.Time `json:"iniciado_em,omitempty"`
	FinalizadoEm *time.Time `json:"finalizado_em,omitempty"`
}

// BlocoResposta é o retrato público de um bloco do CDM.
type BlocoResposta struct {
	Papel string `json:"papel"`
	// omitempty porque papel que não é seção não carrega hierarquia: emitir
	// "nivel":0 sugeriria um nível que não existe.
	Nivel       int     `json:"nivel,omitempty"`
	TextoResumo string  `json:"texto_resumo"`
	Confianca   float64 `json:"confianca"`
	Origem      string  `json:"origem"`
	RefXML      int     `json:"ref_xml"`
}

// EstruturaResposta é o CDM de um documento, na ordem do corpo. A versão
// acompanha a resposta para o frontend saber qual formato está lendo.
type EstruturaResposta struct {
	Versao int             `json:"versao"`
	Blocos []BlocoResposta `json:"blocos"`
}
