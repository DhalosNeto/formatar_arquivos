package ruleset

import (
	"math"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// Definicao é o perfil de formatação de uma revista ou norma: o que o motor
// aplica ao documento.
//
// Uma vez semeada, uma versão é IMUTÁVEL — mudança de diretriz cria vN+1. É o
// que torna um documento já formatado reproduzível: ele guarda slug + versão.
type Definicao struct {
	Slug   string `json:"slug" yaml:"slug"`
	Versao int    `json:"versao" yaml:"versao"`
	Nome   string `json:"nome" yaml:"nome"`
	Fonte  string `json:"fonte" yaml:"fonte"`
	Pagina Pagina `json:"pagina" yaml:"pagina"`
	Corpo  Corpo  `json:"corpo" yaml:"corpo"`
}

// Pagina são as dimensões e margens da página, em centímetros.
type Pagina struct {
	LarguraCM float64 `json:"largura_cm" yaml:"largura_cm"`
	AlturaCM  float64 `json:"altura_cm" yaml:"altura_cm"`
	Margens   Margens `json:"margens" yaml:"margens"`
}

// Margens são as quatro margens da página, em centímetros.
type Margens struct {
	SuperiorCM float64 `json:"superior_cm" yaml:"superior_cm"`
	InferiorCM float64 `json:"inferior_cm" yaml:"inferior_cm"`
	EsquerdaCM float64 `json:"esquerda_cm" yaml:"esquerda_cm"`
	DireitaCM  float64 `json:"direita_cm" yaml:"direita_cm"`
}

// Corpo é a tipografia do texto corrido.
type Corpo struct {
	Fonte          string  `json:"fonte" yaml:"fonte"`
	TamanhoPT      float64 `json:"tamanho_pt" yaml:"tamanho_pt"`
	Entrelinha     float64 `json:"entrelinha" yaml:"entrelinha"`
	RecuoCM        float64 `json:"recuo_cm" yaml:"recuo_cm"`
	EspacoAntesPT  float64 `json:"espaco_antes_pt" yaml:"espaco_antes_pt"`
	EspacoDepoisPT float64 `json:"espaco_depois_pt" yaml:"espaco_depois_pt"`
	Alinhamento    string  `json:"alinhamento" yaml:"alinhamento"`
}

// Validar confere o perfil inteiro antes de qualquer uso.
//
// As dimensões precisam continuar positivas DEPOIS da conversão para as
// unidades inteiras do OOXML, e as margens não podem consumir a página nem
// antes nem depois do arredondamento: um perfil que passa em centímetros e
// falha em twips produziria documento inválido só na gravação.
func (definicao Definicao) Validar() error {
	if err := definicao.validarMetadados(); err != nil {
		return err
	}
	if err := definicao.Pagina.validar(); err != nil {
		return err
	}
	return definicao.Corpo.validar()
}

func (definicao Definicao) validarMetadados() error {
	if !textoValido(definicao.Slug, 80) || !regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`).MatchString(definicao.Slug) {
		return invalido("slug")
	}
	if definicao.Versao < 1 || definicao.Versao > math.MaxInt32 {
		return invalido("versao")
	}
	if !textoValido(definicao.Nome, 200) {
		return invalido("nome")
	}
	if !textoValido(definicao.Fonte, 2048) || strings.ContainsFunc(definicao.Fonte, unicode.IsSpace) {
		return invalido("fonte")
	}
	endereco, err := url.Parse(definicao.Fonte)
	if err != nil || endereco.Hostname() == "" || endereco.User != nil || (endereco.Scheme != "http" && endereco.Scheme != "https") {
		return invalido("fonte")
	}
	return nil
}

func textoValido(texto string, limite int) bool {
	return utf8.ValidString(texto) && strings.TrimSpace(texto) != "" && utf8.RuneCountInString(texto) <= limite && !strings.ContainsFunc(texto, unicode.IsControl)
}

func invalido(campo string) error {
	return errors.NovoErroValidacao(campo, "definição de ruleset inválida")
}

func (pagina Pagina) validar() error {
	if err := validarEixo(pagina.LarguraCM, pagina.Margens.EsquerdaCM, pagina.Margens.DireitaCM); err != nil {
		return err
	}
	return validarEixo(pagina.AlturaCM, pagina.Margens.SuperiorCM, pagina.Margens.InferiorCM)
}

func validarEixo(dimensao, margemInicial, margemFinal float64) error {
	tamanho, err := vo.CentimetrosParaTwips(dimensao)
	if err != nil || tamanho <= 0 {
		return invalido("pagina")
	}
	inicial, err := vo.CentimetrosParaTwips(margemInicial)
	if err != nil {
		return invalido("pagina.margens")
	}
	final, err := vo.CentimetrosParaTwips(margemFinal)
	if err != nil {
		return invalido("pagina.margens")
	}
	if margemInicial+margemFinal >= dimensao || int64(inicial)+int64(final) >= int64(tamanho) {
		return invalido("pagina.margens")
	}
	return nil
}

func (corpo Corpo) validar() error {
	if !textoValido(corpo.Fonte, 100) {
		return invalido("corpo.fonte")
	}
	switch corpo.Alinhamento {
	case "esquerda", "direita", "centralizado", "justificado":
	default:
		return invalido("corpo.alinhamento")
	}
	medidas := []struct {
		valor     float64
		converter func(float64) (int, error)
		positivo  bool
	}{
		{corpo.TamanhoPT, vo.PontosParaMeiosPontos, true},
		{corpo.Entrelinha, vo.EntrelinhaParaUnidades, true},
		{corpo.RecuoCM, vo.CentimetrosParaTwips, false},
		{corpo.EspacoAntesPT, vo.PontosParaTwips, false},
		{corpo.EspacoDepoisPT, vo.PontosParaTwips, false},
	}
	for _, medida := range medidas {
		convertido, err := medida.converter(medida.valor)
		if err != nil || (medida.positivo && convertido <= 0) {
			return invalido("corpo")
		}
	}
	return nil
}
