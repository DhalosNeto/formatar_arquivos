package ruleset

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"

	domainruleset "github.com/daniel-halos/formatador/internal/domain/ruleset"
	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/daniel-halos/formatador/rulesets"
)

// TamanhoMaximoBytes é o teto de um arquivo de perfil. Existe antes de
// qualquer parsing: um YAML de 500 MB não deve virar árvore em memória para só
// então ser recusado.
const TamanhoMaximoBytes = 65536
const numeroMaximoNos = 256
const profundidadeMaxima = 16

// Carregar lê um perfil de formatação de bytes YAML.
//
// Aplica, nesta ordem: teto de TamanhoMaximoBytes, subconjunto contratado de
// YAML, schema JSON e validação do domínio. A resolução externa de schema e a
// chamada à URL do campo fonte são explicitamente BLOQUEADAS — um perfil é um
// arquivo do repositório, não uma instrução para buscar coisas na rede.
//
// Erros não ecoam o conteúdo de origem.
func Carregar(conteudo []byte) (domainruleset.Definicao, error) {
	var definicao domainruleset.Definicao
	if len(conteudo) == 0 || len(conteudo) > TamanhoMaximoBytes {
		return definicao, erroConteudo()
	}
	objeto, err := lerYAML(conteudo)
	if err != nil {
		return definicao, err
	}
	esquema, err := compilarSchema()
	if err != nil {
		return definicao, err
	}
	if err := esquema.Validate(objeto); err != nil {
		return definicao, erroConteudo()
	}
	dados, err := json.Marshal(objeto)
	if err != nil {
		return definicao, erroConteudo()
	}
	if err := json.Unmarshal(dados, &definicao); err != nil {
		return domainruleset.Definicao{}, erroConteudo()
	}
	if err := definicao.Validar(); err != nil {
		return domainruleset.Definicao{}, erroConteudo()
	}
	return definicao, nil
}

func erroConteudo() error {
	// Causas do parser e do schema podem carregar conteúdo privado do documento.
	return errors.NovoErroValidacao("ruleset", "conteúdo de ruleset inválido")
}

func lerYAML(conteudo []byte) (any, error) {
	decodificador := yaml.NewDecoder(bytes.NewReader(conteudo))
	var documento yaml.Node
	if err := decodificador.Decode(&documento); err != nil {
		return nil, erroConteudo()
	}
	quantidade := 0
	if err := validarNo(&documento, 1, &quantidade); err != nil {
		return nil, err
	}
	var adicional yaml.Node
	if err := decodificador.Decode(&adicional); !errors.E(err, io.EOF) {
		return nil, erroConteudo()
	}
	var objeto any
	// Decodificar em any conserva o tipo do escalar; strings não viram números.
	if err := documento.Decode(&objeto); err != nil {
		return nil, erroConteudo()
	}
	dados, err := json.Marshal(objeto)
	if err != nil {
		return nil, erroConteudo()
	}
	if err := json.Unmarshal(dados, &objeto); err != nil {
		return nil, erroConteudo()
	}
	return objeto, nil
}

func validarNo(no *yaml.Node, profundidade int, quantidade *int) error {
	*quantidade++
	if *quantidade > numeroMaximoNos || profundidade > profundidadeMaxima || no.Anchor != "" || no.Kind == yaml.AliasNode {
		return erroConteudo()
	}
	if err := validarTipoNo(no); err != nil {
		return err
	}
	for _, filho := range no.Content {
		if err := validarNo(filho, profundidade+1, quantidade); err != nil {
			return err
		}
	}
	return nil
}

func validarTipoNo(no *yaml.Node) error {
	switch no.Kind {
	case yaml.DocumentNode:
		if len(no.Content) != 1 {
			return erroConteudo()
		}
	case yaml.MappingNode:
		if no.Tag != "!!map" {
			return erroConteudo()
		}
		if err := validarChaves(no); err != nil {
			return err
		}
	case yaml.SequenceNode:
		if no.Tag != "!!seq" {
			return erroConteudo()
		}
	case yaml.ScalarNode:
		switch no.Tag {
		case "!!str", "!!int", "!!float", "!!bool":
		default:
			return erroConteudo()
		}
	default:
		return erroConteudo()
	}
	return nil
}

func validarChaves(no *yaml.Node) error {
	chaves := make(map[string]bool)
	for indice := 0; indice < len(no.Content); indice += 2 {
		chave := no.Content[indice]
		if chave.Kind != yaml.ScalarNode || chave.Tag != "!!str" || chave.Value == "<<" || chaves[chave.Value] {
			return erroConteudo()
		}
		chaves[chave.Value] = true
	}
	return nil
}

type carregadorBloqueado struct{}

// Load satisfaz a interface de carregamento do compilador de schema
// devolvendo erro sempre: é o que impede a compilação de buscar um $ref
// remoto.
func (carregadorBloqueado) Load(string) (any, error) {
	return nil, errors.NovoErroValidacao("schema", "resolução externa de schema proibida")
}

func compilarSchema() (*jsonschema.Schema, error) {
	conteudo, err := rulesets.ObterSchema()
	if err != nil {
		return nil, errors.NovoErroAplicacao("schema de ruleset indisponível")
	}
	documento, err := jsonschema.UnmarshalJSON(bytes.NewReader(conteudo))
	if err != nil {
		return nil, errors.NovoErroAplicacao("schema de ruleset inválido")
	}
	compilador := jsonschema.NewCompiler()
	compilador.UseLoader(carregadorBloqueado{})
	compilador.AssertFormat()
	const endereco = "urn:formatador:ruleset:schema:1"
	if err := compilador.AddResource(endereco, documento); err != nil {
		return nil, errors.NovoErroAplicacao("schema de ruleset inválido")
	}
	esquema, err := compilador.Compile(endereco)
	if err != nil {
		return nil, errors.NovoErroAplicacao("schema de ruleset inválido")
	}
	return esquema, nil
}
