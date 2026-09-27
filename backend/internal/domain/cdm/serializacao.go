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
	Versao int         `json:"versao"`
	Blocos []blocoJSON `json:"blocos"`
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
	Versao int     `json:"versao"`
	Blocos []Bloco `json:"blocos"`
}

// NovoIndice monta o índice já carimbado com a versão do formato corrente.
func NovoIndice(blocos []Bloco) Indice {
	return Indice{Versao: VersaoFormatoCDM, Blocos: blocos}
}

// Serializar produz o JSON que vai para a coluna cdm_jsonb.
func (i Indice) Serializar() ([]byte, error) {
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

	dados, err := json.Marshal(envelopeJSON{Versao: i.Versao, Blocos: blocos})
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

	return Indice{Versao: envelope.Versao, Blocos: blocos}, nil
}
