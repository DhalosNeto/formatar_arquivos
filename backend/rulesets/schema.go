// Package rulesets guarda o schema técnico dos perfis de formatação e os
// próprios perfis em YAML.
//
// Vive na raiz do módulo, e não em internal/, por uma restrição do go:embed:
// ele não alcança arquivos fora do diretório do próprio pacote, e os perfis
// precisam ficar num caminho que humanos editam e versionam.
package rulesets

import "embed"

// O filesystem embutido mantém o contrato independente do diretório de execução.
//
//go:embed _schema.json
var arquivos embed.FS

// ObterSchema devolve o schema JSON embutido no binário.
func ObterSchema() ([]byte, error) {
	return arquivos.ReadFile("_schema.json")
}
