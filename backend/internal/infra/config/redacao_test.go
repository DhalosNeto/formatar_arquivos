package config

import (
	"fmt"
	"strings"
	"testing"
)

// segredosDeTeste são valores improváveis de aparecer por acidente: se um
// deles vazar para a saída formatada, é porque o campo foi impresso.
const (
	senhaNoDSN    = "senha-postgres-nao-pode-vazar-91af"
	secretKeyS3   = "secret-key-s3-nao-pode-vazar-7b2c"
	accessKeyS3   = "access-key-s3-nao-pode-vazar-3d18"
	chaveAPIDoLLM = "chave-api-llm-nao-pode-vazar-5e40"
)

func configDeTeste() Config {
	return Config{
		Porta:    "8080",
		Ambiente: AmbienteDesenvolvimento,
		NivelLog: "info",
		Postgres: Postgres{DSN: "postgres://app:" + senhaNoDSN + "@db:5432/formatador"},
		Storage: Storage{
			Endpoint:  "http://minio:9000",
			Bucket:    "documentos",
			AccessKey: accessKeyS3,
			SecretKey: secretKeyS3,
			Regiao:    "us-east-1",
		},
		LLM: LLM{Habilitado: true, ChaveAPI: chaveAPIDoLLM, Modelo: "claude-opus-5"},
	}
}

// TestFormatacaoNaoVazaSegredo cobre os dois verbos separadamente de
// propósito: %v passa pelo Stringer e %#v NÃO — é exatamente essa diferença
// que a regra 11 do CLAUDE.md existe para travar. Um teste que só exercitasse
// %v aprovaria um tipo sem GoStringer.
func TestFormatacaoNaoVazaSegredo(t *testing.T) {
	t.Parallel()

	cfg := configDeTeste()

	alvos := []struct {
		nome  string
		valor any
	}{
		{nome: "Config", valor: cfg},
		{nome: "Postgres", valor: cfg.Postgres},
		{nome: "Storage", valor: cfg.Storage},
		{nome: "LLM", valor: cfg.LLM},
	}
	segredos := []string{senhaNoDSN, secretKeyS3, accessKeyS3, chaveAPIDoLLM}

	for _, alvo := range alvos {
		for _, verbo := range []string{"%v", "%+v", "%s", "%#v"} {
			t.Run(alvo.nome+" com "+verbo, func(t *testing.T) {
				t.Parallel()

				saida := fmt.Sprintf(verbo, alvo.valor)
				for _, segredo := range segredos {
					if strings.Contains(saida, segredo) {
						t.Fatalf("o verbo %s de %s vazou um segredo: %s", verbo, alvo.nome, saida)
					}
				}
			})
		}
	}
}

// TestFormatacaoPreservaOQueNaoEhSegredo: redigir demais transforma o dump em
// ruído. Endpoint, bucket e região são o que torna o log útil no diagnóstico.
func TestFormatacaoPreservaOQueNaoEhSegredo(t *testing.T) {
	t.Parallel()

	saida := fmt.Sprintf("%v", configDeTeste())

	for _, esperado := range []string{"minio:9000", "documentos", "us-east-1", "8080", "claude-opus-5"} {
		if !strings.Contains(saida, esperado) {
			t.Errorf("esperava %q na saída formatada, obtive: %s", esperado, saida)
		}
	}
}
