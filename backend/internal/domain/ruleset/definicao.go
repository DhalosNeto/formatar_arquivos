package ruleset

import (
	"maps"
	"math"
	"net/url"
	"regexp"
	"slices"
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
	// EntrelinhaPorPapel é a entrelinha dos papéis de exceção da §5.2,
	// indexada pelo nome do papel. Chave ausente — e mapa vazio — significa
	// "a norma é silente, preserve o documento", como nos cinco ponteiros de
	// Corpo. As chaves aceitas são SOMENTE as de papeisEntrelinha.
	//
	// `omitempty` é CONTRATO de imutabilidade, não estilo, pelo mesmo motivo
	// dos cinco ponteiros de Corpo: inserirRuleset compara definicao=$4::jsonb
	// e o checksum é sha256 do json.Marshal, então um perfil já semeado que
	// não declara o campo precisa continuar serializando SEM esta chave.
	EntrelinhaPorPapel map[string]float64 `json:"entrelinha_por_papel,omitempty" yaml:"entrelinha_por_papel,omitempty"`
}

// papeisEntrelinha é a lista branca de papéis que podem receber entrelinha
// própria: os quatro de exceção da ABNT NBR 14724:2024 §5.2.
//
// Lista LITERAL aqui, e não delegação a cdm.ParaPapel. Delegar aceitaria
// "paragrafo", cujo grupo seria idêntico a Plano.ReferenciasCorpo e produziria
// duas AplicarEntrelinha sobre os mesmos ordinais, a segunda sobrescrevendo em
// silêncio; e aceitaria "tabela", que levaria todo ordinal w:tbl ao plano para
// o mutador recusar, brickando qualquer documento com tabela. Também mantém o
// domínio sem a aresta ruleset -> cdm e exclui "secao", de que a ordenação
// determinística dos grupos do plano depende (Secao(1) e Secao(2) têm o mesmo
// Papel.String()).
var papeisEntrelinha = map[string]struct{}{
	"citacao": {}, "referencia": {}, "legenda": {}, "nota_rodape": {},
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
//
// Cinco campos são PONTEIRO porque a norma pode ser silente neles: nil
// significa "a norma não diz, preserve o que o documento já tem", e o mutador
// correspondente é pulado. Ponteiro e não valor vazio porque, nos três
// numéricos, 0 é valor legítimo ("zerar o espaço") e não serve de sentinela.
//
// TamanhoPT e Entrelinha seguem por valor e obrigatórios: a ABNT NBR
// 14724:2024 os define (§5.1 e §5.2).
//
// `omitempty` nos cinco é CONTRATO, não estilo: prepararRulesets faz
// json.Marshal + sha256 e inserirRuleset compara definicao=$4::jsonb para
// impor imutabilidade. {"fonte":null} é jsonb DIFERENTE de chave ausente, e
// um re-seed da mesma versão bateria em conflito contra uma versão imutável.
//
// ALIASING: formatador.Plano carrega um Corpo POR VALOR, então estes cinco
// ponteiros aliasam a Definicao lida do banco. São SOMENTE LEITURA — nenhum
// mutador desreferencia para escrever.
type Corpo struct {
	Fonte          *string  `json:"fonte,omitempty" yaml:"fonte,omitempty"`
	TamanhoPT      float64  `json:"tamanho_pt" yaml:"tamanho_pt"`
	Entrelinha     float64  `json:"entrelinha" yaml:"entrelinha"`
	RecuoCM        *float64 `json:"recuo_cm,omitempty" yaml:"recuo_cm,omitempty"`
	EspacoAntesPT  *float64 `json:"espaco_antes_pt,omitempty" yaml:"espaco_antes_pt,omitempty"`
	EspacoDepoisPT *float64 `json:"espaco_depois_pt,omitempty" yaml:"espaco_depois_pt,omitempty"`
	Alinhamento    *string  `json:"alinhamento,omitempty" yaml:"alinhamento,omitempty"`
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
	if err := definicao.Corpo.validar(); err != nil {
		return err
	}
	return definicao.validarEntrelinhaPorPapel()
}

// validarEntrelinhaPorPapel confere a lista branca e depois as medidas, em
// DUAS passadas sobre as chaves ORDENADAS.
//
// A ordem e a separação das passadas são o que torna a mensagem de erro
// determinística: com dois defeitos coexistindo — por exemplo
// {"paragrafo": 1.0, "citacao": 0} — um único `range` devolveria o campo
// "entrelinha_por_papel" ou "corpo" conforme a ordem de iteração do mapa, e
// isso seria indeterminismo observável no 400 da API.
//
// A medida inválida sai com o campo "corpo", herdado de ValidarEntrelinha, e
// não com "entrelinha_por_papel": é duplicação de validador que não se faz, ao
// custo de uma mensagem enganosa para quem consome o 400 — registrado em
// docs/contrato-api.md.
func (definicao Definicao) validarEntrelinhaPorPapel() error {
	chaves := slices.Sorted(maps.Keys(definicao.EntrelinhaPorPapel))
	for _, chave := range chaves {
		if _, aceita := papeisEntrelinha[chave]; !aceita {
			return invalido("entrelinha_por_papel")
		}
	}
	for _, chave := range chaves {
		if err := ValidarEntrelinha(definicao.EntrelinhaPorPapel[chave]); err != nil {
			return err
		}
	}
	return nil
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

// Validar confere as dimensões e margens da página sem exigir o restante do ruleset.
func (pagina Pagina) Validar() error {
	return pagina.validar()
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

// ValidarAlinhamento valida o alinhamento isoladamente, sem exigir um Corpo completo.
func ValidarAlinhamento(alinhamento string) error {
	switch alinhamento {
	case "esquerda", "direita", "centralizado", "justificado":
		return nil
	default:
		return invalido("corpo.alinhamento")
	}
}

// ValidarEntrelinha confere um múltiplo isolado, inclusive sua representação inteira.
// Zero após arredondamento não representa uma entrelinha válida.
func ValidarEntrelinha(multiplo float64) error {
	unidades, err := vo.EntrelinhaParaUnidades(multiplo)
	if err != nil || unidades <= 0 {
		return invalido("corpo")
	}
	return nil
}

// ValidarRecuoCM aceita recuo não negativo que possa ser representado em twips.
func ValidarRecuoCM(recuo float64) error {
	if _, err := vo.CentimetrosParaTwips(recuo); err != nil {
		return invalido("corpo.recuo")
	}
	return nil
}

// ValidarFonteCorpo aceita somente nomes que podem ser escritos em XML 1.0.
func ValidarFonteCorpo(fonte string) error {
	if !textoValido(fonte, 100) {
		return invalido("corpo.fonte")
	}
	for _, caractere := range fonte {
		if caractere != '\t' && caractere != '\n' && caractere != '\r' &&
			(caractere < 0x20 || caractere > 0x10ffff ||
				caractere >= 0xd800 && caractere <= 0xdfff ||
				caractere >= 0xfffe && caractere <= 0xffff) {
			return invalido("corpo.fonte")
		}
	}
	return nil
}

// ValidarTamanhoCorpoPT exige tamanho representável e positivo em meios-pontos.
func ValidarTamanhoCorpoPT(tamanho float64) error {
	convertido, err := vo.PontosParaMeiosPontos(tamanho)
	if err != nil || convertido <= 0 {
		return invalido("corpo.tamanho")
	}
	return nil
}

// validar aceita nil em cada um dos cinco campos silentes e valida o valor
// somente quando o campo está presente.
func (corpo Corpo) validar() error {
	if corpo.Fonte != nil {
		if err := ValidarFonteCorpo(*corpo.Fonte); err != nil {
			return err
		}
	}
	if err := ValidarTamanhoCorpoPT(corpo.TamanhoPT); err != nil {
		return err
	}
	if corpo.Alinhamento != nil {
		if err := ValidarAlinhamento(*corpo.Alinhamento); err != nil {
			return err
		}
	}
	if err := ValidarEntrelinha(corpo.Entrelinha); err != nil {
		return err
	}
	if corpo.RecuoCM != nil {
		if err := ValidarRecuoCM(*corpo.RecuoCM); err != nil {
			return err
		}
	}
	for _, medida := range []*float64{corpo.EspacoAntesPT, corpo.EspacoDepoisPT} {
		if medida == nil {
			continue
		}
		if _, err := vo.PontosParaTwips(*medida); err != nil {
			return invalido("corpo")
		}
	}
	return nil
}
