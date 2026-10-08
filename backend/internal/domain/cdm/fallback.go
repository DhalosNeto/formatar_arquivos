package cdm

import (
	"context"
	"math"

	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// ClassificadorEstrutura classifica os blocos candidatos à revisão semântica.
type ClassificadorEstrutura interface {
	Classificar(ctx context.Context, blocos []Bloco) ([]JulgamentoEstrutura, error)
}

// JulgamentoEstrutura é a resposta do classificador para uma referência XML.
type JulgamentoEstrutura struct {
	RefXML             int
	Papel              Papel
	Confianca          float64
	SemCorrespondencia bool
}

// PoliticaConfianca determina quando consultar, sugerir ou aplicar uma classe.
type PoliticaConfianca struct {
	LimiteConsulta    float64
	LimiteConfirmacao float64
	LimiteAutomatico  float64
}

// RevisaoEstrutura registra uma decisão que ainda requer atenção humana.
type RevisaoEstrutura struct {
	RefXML        int     `json:"ref_xml"`
	PapelSugerido *Papel  `json:"papel_sugerido,omitempty"`
	Confianca     float64 `json:"confianca"`
	Acao          string  `json:"acao"`
	Motivo        string  `json:"motivo,omitempty"`
}

// Fallback consulta o classificador apenas para blocos de baixa confiança.
type Fallback struct {
	classificador ClassificadorEstrutura
	politica      PoliticaConfianca
}

const (
	maximoCandidatosFallback            = 32
	acaoConfirmar                       = "confirmar"
	acaoRevisar                         = "revisar"
	motivoClassificadorIndisponivel     = "classificador_indisponivel"
	motivoRespostaClassificadorInvalida = "resposta_classificador_invalida"
)

// NovoFallback valida as dependências e limites de confiança.
func NovoFallback(classificador ClassificadorEstrutura, politica PoliticaConfianca) (*Fallback, error) {
	if classificador == nil {
		return nil, errors.NovoErroArgumentoNulo("classificador")
	}
	for _, limite := range []float64{politica.LimiteConsulta, politica.LimiteConfirmacao, politica.LimiteAutomatico} {
		if math.IsNaN(limite) || math.IsInf(limite, 0) || limite < 0 || limite > 1 {
			return nil, errors.NovoErroValidacao("politica", "os limites precisam ser finitos e estar entre 0 e 1")
		}
	}
	if politica.LimiteConfirmacao >= politica.LimiteAutomatico || politica.LimiteConsulta > politica.LimiteAutomatico {
		return nil, errors.NovoErroValidacao("politica", "os limites de confirmação e consulta são incompatíveis")
	}
	return &Fallback{classificador: classificador, politica: politica}, nil
}

// Aplicar consulta no máximo 32 blocos e preserva os demais para revisão.
func (f *Fallback) Aplicar(ctx context.Context, blocos []Bloco) (Indice, error) {
	if err := ctx.Err(); err != nil {
		return Indice{}, err
	}
	indice := NovoIndice(append([]Bloco(nil), blocos...))
	porRef := make(map[int]int, len(blocos))
	for posicao, bloco := range blocos {
		if !bloco.Papel.Valido() || !bloco.Origem.Valido() || !finitoEntreZeroEUm(bloco.Confianca) || bloco.RefXML < 0 {
			return Indice{}, errors.NovoErroValidacao("blocos", "a estrutura contém bloco inválido")
		}
		if _, existe := porRef[bloco.RefXML]; existe {
			return Indice{}, ErroRefXMLDuplicado
		}
		porRef[bloco.RefXML] = posicao
	}
	candidatos := make([]Bloco, 0)
	for _, bloco := range indice.Blocos {
		if bloco.Origem != OrigemUsuario && bloco.Origem != OrigemLLM && bloco.Confianca < f.politica.LimiteConsulta {
			candidatos = append(candidatos, bloco)
		}
	}
	if len(candidatos) == 0 {
		return indice, nil
	}
	consulta := candidatos
	if len(consulta) > maximoCandidatosFallback {
		consulta = consulta[:maximoCandidatosFallback]
	}
	indice.Revisoes = make([]RevisaoEstrutura, 0, len(candidatos))
	if err := ctx.Err(); err != nil {
		return Indice{}, err
	}
	respostas, err := f.classificador.Classificar(ctx, append([]Bloco(nil), consulta...))
	if err != nil {
		if ctx.Err() != nil {
			return Indice{}, ctx.Err()
		}
		for _, bloco := range candidatos {
			indice.Revisoes = append(indice.Revisoes, RevisaoEstrutura{RefXML: bloco.RefXML, Confianca: 0, Acao: acaoRevisar, Motivo: motivoClassificadorIndisponivel})
		}
		return indice, nil
	}
	if err := ctx.Err(); err != nil {
		return Indice{}, err
	}
	mapa, valido := validarJulgamentos(consulta, respostas)
	if !valido {
		for _, bloco := range candidatos {
			indice.Revisoes = append(indice.Revisoes, RevisaoEstrutura{RefXML: bloco.RefXML, Confianca: 0, Acao: acaoRevisar, Motivo: motivoRespostaClassificadorInvalida})
		}
		return indice, nil
	}
	for _, bloco := range consulta {
		j := mapa[bloco.RefXML]
		if j.SemCorrespondencia {
			indice.Revisoes = append(indice.Revisoes, RevisaoEstrutura{RefXML: bloco.RefXML, Confianca: j.Confianca, Acao: acaoRevisar})
			continue
		}
		if j.Confianca >= f.politica.LimiteAutomatico {
			indice.Blocos[porRef[bloco.RefXML]], err = indice.Blocos[porRef[bloco.RefXML]].Reclassificar(j.Papel, j.Confianca, OrigemLLM)
			if err != nil {
				return Indice{}, err
			}
		} else if j.Confianca >= f.politica.LimiteConfirmacao {
			papel := j.Papel
			indice.Revisoes = append(indice.Revisoes, RevisaoEstrutura{RefXML: j.RefXML, PapelSugerido: &papel, Confianca: j.Confianca, Acao: acaoConfirmar})
		} else {
			indice.Revisoes = append(indice.Revisoes, RevisaoEstrutura{RefXML: j.RefXML, Confianca: j.Confianca, Acao: acaoRevisar})
		}
	}
	for _, bloco := range candidatos[len(consulta):] {
		indice.Revisoes = append(indice.Revisoes, RevisaoEstrutura{RefXML: bloco.RefXML, Confianca: 0, Acao: acaoRevisar})
	}
	return indice, nil
}

func validarJulgamentos(candidatos []Bloco, julgamentos []JulgamentoEstrutura) (map[int]JulgamentoEstrutura, bool) {
	if len(candidatos) != len(julgamentos) {
		return nil, false
	}
	permitidos := make(map[int]struct{}, len(candidatos))
	for _, bloco := range candidatos {
		permitidos[bloco.RefXML] = struct{}{}
	}
	resultado := make(map[int]JulgamentoEstrutura, len(julgamentos))
	for _, j := range julgamentos {
		if _, ok := permitidos[j.RefXML]; !ok {
			return nil, false
		}
		if _, existe := resultado[j.RefXML]; existe || !finitoEntreZeroEUm(j.Confianca) {
			return nil, false
		}
		if !j.SemCorrespondencia && !j.Papel.Valido() {
			return nil, false
		}
		resultado[j.RefXML] = j
	}
	return resultado, true
}

func finitoEntreZeroEUm(valor float64) bool {
	return !math.IsNaN(valor) && !math.IsInf(valor, 0) && valor >= 0 && valor <= 1
}
