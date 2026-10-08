package cdm

import (
	"encoding/json"

	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// VersaoFormatoCDM é a versão do formato persistido em documentos.cdm_jsonb.
// Ler um formato que não é este é ERRO, não tentativa de adivinhação: um
// documento analisado por uma versão futura do sistema precisa ser
// reanalisado, não interpretado pela metade.
const VersaoFormatoCDM = 1

// Mensagens de recusa. Todas FIXAS e sem interpolação: a mensagem de erro
// sobe para o log, e `erroLinhaCorrompida` em internal/data/postgres a embute
// inteira. Interpolar o JSON recebido colocaria trecho do documento do
// usuário no log pela porta dos fundos (regra 7 do CLAUDE.md).
const (
	mensagemPapelIlegivel      = "o papel do bloco não está num formato legível"
	mensagemPapelDesconhecido  = "papel desconhecido ou seção fora da faixa 1..6"
	mensagemIndiceIlegivel     = "o cdm não está num formato legível"
	mensagemVersaoIncompativel = "o cdm está num formato de versão incompatível"
	mensagemIndiceCorrompido   = "cdm do documento está corrompido"
	mensagemBlocoInvalido      = "bloco do cdm inválido"
	mensagemOrigemDesconhecida = "origem de classificação desconhecida"
	mensagemRefXMLNegativo     = "a referência ao nó XML é um índice ordinal, nunca negativo"
)

// papelJSON é a forma persistida de um Papel. Objeto, e não string composta
// como "secao:2", porque string composta troca alguns bytes por um parser
// próprio e por ambiguidade na volta.
type papelJSON struct {
	Nome string `json:"nome"`
	// omitempty: papel que não é seção não carrega nível, e gravar
	// "nivel":0 sugeriria uma hierarquia que não existe.
	Nivel int `json:"nivel,omitempty"`
}

// MarshalJSON escreve o Papel. Existe porque os campos de Papel são privados
// — sem isto, o encoding/json serializaria um objeto vazio e o papel de todo
// bloco se perderia silenciosamente na ida ao banco.
func (p Papel) MarshalJSON() ([]byte, error) {
	return json.Marshal(papelJSON{Nome: p.nome, Nivel: p.nivel})
}

// UnmarshalJSON reconstrói o Papel validando antes de atribuir: um papel
// desconhecido ou uma seção fora da faixa nunca chegam a existir como valor.
func (p *Papel) UnmarshalJSON(dados []byte) error {
	var bruto papelJSON
	if err := json.Unmarshal(dados, &bruto); err != nil {
		return errors.NovoErroValidacao("papel", mensagemPapelIlegivel)
	}

	candidato := Papel{nome: bruto.Nome, nivel: bruto.Nivel}
	if !candidato.Valido() {
		return errors.NovoErroValidacao("papel", mensagemPapelDesconhecido)
	}

	*p = candidato
	return nil
}

// blocoJSON é a forma persistida de um Bloco. Existe como tipo próprio, em
// vez de tags em Bloco, para que a volta passe OBRIGATORIAMENTE por
// NovoBloco: deixar o encoding/json preencher um Bloco direto contornaria a
// validação e deixaria entrar pelo banco um bloco que o domínio recusaria
// pela porta da frente.
type blocoJSON struct {
	Papel       Papel   `json:"papel"`
	TextoResumo string  `json:"texto_resumo"`
	Confianca   float64 `json:"confianca"`
	Origem      Origem  `json:"origem"`
	RefXML      int     `json:"ref_xml"`
}

// envelopeJSON é o objeto de nível superior. O envelope não é enfeite:
// entity.ValidarCDM recusa qualquer coisa que não comece com "{", então um
// array cru de blocos não poderia ser persistido.
type envelopeJSON struct {
	Versao   int                `json:"versao"`
	Blocos   []blocoJSON        `json:"blocos"`
	Revisoes []RevisaoEstrutura `json:"revisoes,omitempty"`
}

// ErroRefXMLDuplicado marca um índice com dois blocos apontando para o mesmo
// nó do documento. É corrupção, não entrada inválida: RefXML é a posição
// ordinal do bloco no corpo, e duas posições iguais não podem ter sido
// produzidas por nenhum caminho válido de escrita.
var ErroRefXMLDuplicado = errors.Novo("dois blocos referenciam o mesmo nó xml")

// ErroIndiceCorrompido reclassifica uma falha de leitura do CDM para quem o
// leu do BANCO em vez de receber do cliente.
//
// Desserializar devolve *ErroValidacao, que é o certo para quem manda o JSON
// numa requisição e o ERRADO para quem só fez um GET: errors.Envolver não
// reclassifica, então o ErroValidacao sobreviveria na cadeia e a API
// responderia 400, culpando o cliente por uma linha corrompida do servidor.
//
// Existe aqui, e não replicada em cada chamador, porque já esteve em três
// lugares com duas variantes — uma delas descartando a causa.
//
// A causa entra na mensagem porque as mensagens de Desserializar são FIXAS e
// não ecoam o conteúdo recebido (regra 7). Não passe aqui um erro que cite
// texto de documento.
func ErroIndiceCorrompido(causa error) error {
	if causa == nil {
		return errors.NovoErroAplicacao(mensagemIndiceCorrompido)
	}
	return errors.NovoErroAplicacao(mensagemIndiceCorrompido + ": " + causa.Error())
}

// Indice é o CDM inteiro: o índice semântico de um documento, na ordem do
// corpo. É isto que vai para documentos.cdm_jsonb.
type Indice struct {
	Versao   int                `json:"versao"`
	Blocos   []Bloco            `json:"blocos"`
	Revisoes []RevisaoEstrutura `json:"revisoes,omitempty"`
}

// NovoIndice monta o índice já carimbado com a versão do formato corrente.
func NovoIndice(blocos []Bloco) Indice {
	return Indice{Versao: VersaoFormatoCDM, Blocos: blocos}
}

// Serializar produz o JSON que vai para a coluna cdm_jsonb.
func (i Indice) Serializar() ([]byte, error) {
	if err := validarRevisoes(i.Blocos, i.Revisoes); err != nil {
		return nil, err
	}
	// Fatia vazia e não nula: `"blocos": null` obrigaria todo leitor a
	// distinguir nulo de vazio, e um documento sem blocos classificados é
	// uma lista vazia, não uma ausência.
	// A conversão direta é possível porque blocoJSON tem exatamente os mesmos
	// campos de Bloco, só com as tags JSON. Ela vale apenas nesta direção: a
	// VOLTA continua obrigada a passar por NovoBloco, que é o que impede um
	// bloco inválido de entrar no domínio vindo do banco.
	blocos := make([]blocoJSON, 0, len(i.Blocos))
	for _, bloco := range i.Blocos {
		blocos = append(blocos, blocoJSON(bloco))
	}

	dados, err := json.Marshal(envelopeJSON{Versao: i.Versao, Blocos: blocos, Revisoes: i.Revisoes})
	if err != nil {
		return nil, errors.Envolver(err, "ao serializar o cdm")
	}
	return dados, nil
}

// Desserializar reconstrói o índice a partir do JSON persistido.
//
// Todo bloco passa por NovoBloco: o que sai daqui nunca é algo que o domínio
// recusaria. Um valor gravado por uma versão anterior com regra mais frouxa,
// ou uma linha corrompida, falha aqui em vez de circular como bloco inválido.
//
// O erro é sempre *errors.ErroValidacao — os dados é que estão inválidos.
// Quem lê do BANCO precisa RECLASSIFICAR antes de devolver ao cliente: linha
// corrompida é falha do servidor, não requisição ruim. O precedente é
// erroLinhaCorrompida, em internal/data/postgres/documento.go.
func Desserializar(dados []byte) (Indice, error) {
	var envelope envelopeJSON
	if err := json.Unmarshal(dados, &envelope); err != nil {
		// Papel.UnmarshalJSON já devolve um ErroValidacao com o campo certo;
		// substituí-lo por um genérico perderia qual campo reprovou.
		var validacao *errors.ErroValidacao
		if errors.Como(err, &validacao) {
			return Indice{}, validacao
		}
		return Indice{}, errors.NovoErroValidacao("indice", mensagemIndiceIlegivel)
	}

	// Antes de percorrer os blocos: interpretar conteúdo de um formato que
	// não conhecemos é pior que recusá-lo.
	if envelope.Versao != VersaoFormatoCDM {
		return Indice{}, errors.NovoErroValidacao("versao", mensagemVersaoIncompativel)
	}

	blocos := make([]Bloco, 0, len(envelope.Blocos))
	for _, bruto := range envelope.Blocos {
		bloco, err := NovoBloco(bruto.Papel, bruto.TextoResumo, bruto.Confianca, bruto.Origem, bruto.RefXML)
		if err != nil {
			return Indice{}, err
		}
		blocos = append(blocos, bloco)
	}
	if err := validarRevisoes(blocos, envelope.Revisoes); err != nil {
		return Indice{}, err
	}

	return Indice{Versao: envelope.Versao, Blocos: blocos, Revisoes: envelope.Revisoes}, nil
}

func validarRevisoes(blocos []Bloco, revisoes []RevisaoEstrutura) error {
	blocosPorRef := make(map[int]Bloco, len(blocos))
	for _, bloco := range blocos {
		if len(revisoes) > 0 {
			if _, existe := blocosPorRef[bloco.RefXML]; existe {
				return errors.NovoErroValidacao("revisoes", "há referências de bloco duplicadas junto às revisões")
			}
		}
		blocosPorRef[bloco.RefXML] = bloco
	}
	vistas := make(map[int]struct{}, len(revisoes))
	for _, revisao := range revisoes {
		bloco, existe := blocosPorRef[revisao.RefXML]
		motivoValido := revisao.Motivo == "" || revisao.Motivo == motivoClassificadorIndisponivel || revisao.Motivo == motivoRespostaClassificadorInvalida
		if !existe || bloco.Origem == OrigemUsuario || revisao.RefXML < 0 || !finitoEntreZeroEUm(revisao.Confianca) || (revisao.Acao != acaoConfirmar && revisao.Acao != acaoRevisar) || (revisao.Acao == acaoConfirmar && revisao.PapelSugerido == nil) || (revisao.PapelSugerido != nil && !revisao.PapelSugerido.Valido()) || !motivoValido {
			return errors.NovoErroValidacao("revisoes", "a estrutura contém revisão inválida")
		}
		if _, existe := vistas[revisao.RefXML]; existe {
			return errors.NovoErroValidacao("revisoes", "a estrutura contém referências de revisão duplicadas")
		}
		vistas[revisao.RefXML] = struct{}{}
	}
	return nil
}

// Validar confere TRÊS termos por bloco — papel conhecido, origem conhecida e
// RefXML não negativo — mais a versão do formato e a ausência de RefXML
// repetido. Existe porque Indice pode ser montado por literal, sem passar por
// NovoIndice/NovoBloco — é o caso de quem monta o CDM em memória a partir do
// pacote OOXML.
//
// NÃO confere Confianca nem Revisoes, e a assimetria é deliberada neste
// recorte: NovoBloco (bloco.go) reprova Confianca fora de 0..1 e
// Fallback.Aplicar também, mas um Indice montado por literal com
// Confianca: 42 passa aqui; Serializar valida Revisoes por validarRevisoes, e
// um Indice pode passar Validar e ainda falhar Serializar. Ampliar os termos
// exige casos por termo próprios (regra 12), que não estão na spec deste
// recorte.
//
// Ordem das guardas, e ela importa:
//  1. versão incompatível — interpretar um formato desconhecido é pior que
//     recusá-lo;
//  2. RefXML repetido entre QUAISQUER dois blocos de ordinal NÃO NEGATIVO,
//     independentemente do papel: é corrupção e VENCE bloco inválido,
//     devolvendo ErroRefXMLDuplicado cru, sem envelope, para que quem lê do
//     banco reclassifique (ErroIndiceCorrompido) em vez de responder 400
//     culpando o cliente. A varredura IGNORA RefXML < 0 porque -1 não é nó do
//     documento: dois blocos em -1 não são dois blocos apontando para o mesmo
//     nó, são dois ordinais inválidos, e a passagem (3) os recusa em seguida
//     como entrada inválida — 400, não 500. É também o que mantém esta função
//     de acordo com Fallback.Aplicar (fallback.go), que confere validade do
//     bloco antes da duplicata: sem esta condição o MESMO índice virava 500
//     por aqui e 400 por lá;
//  3. blocos inválidos, acumulados como entrada inválida (*ErroValidacao).
//
// O acúmulo registra NO MÁXIMO um CampoInvalido por termo, por quantos blocos
// reprovem: CampoInvalido.String() é copiado para `razoes` da resposta HTTP, e
// 50 blocos de papel zero não podem render 50 razões idênticas.
//
// Função pura: sem I/O, sem context.Context, sem log. Nenhuma mensagem cita
// TextoResumo nem qualquer outro conteúdo do documento (regra 7).
func (i Indice) Validar() error {
	if i.Versao != VersaoFormatoCDM {
		return errors.NovoErroValidacao("versao", mensagemVersaoIncompativel)
	}

	vistas := make(map[int]struct{}, len(i.Blocos))
	for _, bloco := range i.Blocos {
		if bloco.RefXML < 0 {
			continue
		}
		if _, repetido := vistas[bloco.RefXML]; repetido {
			return ErroRefXMLDuplicado
		}
		vistas[bloco.RefXML] = struct{}{}
	}

	validacao := errors.NovoErroValidacaoCampos(mensagemBlocoInvalido)
	var papelReprovado, origemReprovada, refXMLReprovado bool
	for _, bloco := range i.Blocos {
		if !papelReprovado && !bloco.Papel.Valido() {
			validacao.Acrescentar("papel", mensagemPapelDesconhecido)
			papelReprovado = true
		}
		if !origemReprovada && !bloco.Origem.Valido() {
			validacao.Acrescentar("origem", mensagemOrigemDesconhecida)
			origemReprovada = true
		}
		if !refXMLReprovado && bloco.RefXML < 0 {
			validacao.Acrescentar("ref_xml", mensagemRefXMLNegativo)
			refXMLReprovado = true
		}
	}
	if validacao.TemCampos() {
		return validacao
	}
	return nil
}
