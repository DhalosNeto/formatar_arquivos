// Package config lê a configuração da aplicação a partir de variáveis de
// ambiente. Nenhum segredo é embutido no código: tudo chega por env.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// Config agrupa toda a configuração da aplicação.
type Config struct {
	Porta      string
	Ambiente   string
	NivelLog   string
	Postgres   Postgres
	Storage    Storage
	Conversor  Conversor
	Telemetria Telemetria
	LLM        LLM
}

// Postgres configura a conexão com o banco.
type Postgres struct {
	DSN                string
	MaxConexoes        int32
	TempoLimiteConexao time.Duration
}

// Storage configura o bucket S3/MinIO onde os documentos vivem.
type Storage struct {
	Endpoint  string
	Bucket    string
	AccessKey string
	SecretKey string
	Regiao    string
}

// Conversor configura o serviço de conversão DOCX para PDF.
type Conversor struct {
	URL         string
	TempoLimite time.Duration
}

// Telemetria configura a exportação de traces.
type Telemetria struct {
	EndpointOTLP string
	NomeServico  string
}

// LLM configura o classificador de estrutura por modelo de linguagem.
type LLM struct {
	Habilitado      bool
	ChaveAPI        string
	Modelo          string
	LimiteConfianca float64
}

// AmbienteDesenvolvimento identifica o ambiente local.
const AmbienteDesenvolvimento = "desenvolvimento"

// Carregar monta a configuração a partir do ambiente e valida o que é obrigatório.
func Carregar() (Config, error) {
	cfg := Config{
		Porta:    texto("PORTA", "8080"),
		Ambiente: texto("AMBIENTE", AmbienteDesenvolvimento),
		NivelLog: texto("NIVEL_LOG", "info"),
		Postgres: Postgres{
			DSN:                texto("POSTGRES_DSN", ""),
			MaxConexoes:        inteiro32("POSTGRES_MAX_CONEXOES", 10),
			TempoLimiteConexao: duracao("POSTGRES_TEMPO_LIMITE", 5*time.Second),
		},
		Storage: Storage{
			Endpoint:  texto("STORAGE_ENDPOINT", "http://localhost:9000"),
			Bucket:    texto("STORAGE_BUCKET", "documentos"),
			AccessKey: texto("STORAGE_ACCESS_KEY", ""),
			SecretKey: texto("STORAGE_SECRET_KEY", ""),
			Regiao:    texto("STORAGE_REGIAO", "us-east-1"),
		},
		Conversor: Conversor{
			URL:         texto("CONVERSOR_URL", "http://localhost:2004"),
			TempoLimite: duracao("CONVERSOR_TEMPO_LIMITE", 2*time.Minute),
		},
		Telemetria: Telemetria{
			EndpointOTLP: texto("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
			NomeServico:  texto("OTEL_SERVICE_NAME", "formatador-api"),
		},
		LLM: LLM{
			Habilitado:      booleano("LLM_HABILITADO", false),
			ChaveAPI:        texto("ANTHROPIC_API_KEY", ""),
			Modelo:          texto("LLM_MODELO", "claude-opus-5"),
			LimiteConfianca: decimal("LLM_LIMITE_CONFIANCA", 0.7),
		},
	}

	if err := cfg.validar(); err != nil {
		return Config{}, errors.Envolver(err)
	}
	return cfg, nil
}

// EhDesenvolvimento informa se a aplicação roda em ambiente local.
func (c Config) EhDesenvolvimento() bool {
	return strings.EqualFold(c.Ambiente, AmbienteDesenvolvimento)
}

func (c Config) validar() error {
	invalidos := &errors.ErroValidacao{Mensagem: "configuração inválida"}

	if c.Postgres.DSN == "" {
		invalidos.Acrescentar("POSTGRES_DSN", "obrigatório")
	}
	if c.Storage.Bucket == "" {
		invalidos.Acrescentar("STORAGE_BUCKET", "obrigatório")
	}
	if c.LLM.Habilitado && c.LLM.ChaveAPI == "" {
		invalidos.Acrescentar("ANTHROPIC_API_KEY", "obrigatória quando LLM_HABILITADO=true")
	}
	if !c.EhDesenvolvimento() && (c.Storage.AccessKey == "" || c.Storage.SecretKey == "") {
		invalidos.Acrescentar("STORAGE_ACCESS_KEY", "obrigatória fora do ambiente de desenvolvimento")
	}

	if invalidos.TemCampos() {
		return invalidos
	}
	return nil
}

func texto(chave, padrao string) string {
	if valor := strings.TrimSpace(os.Getenv(chave)); valor != "" {
		return valor
	}
	return padrao
}

// inteiro32 lê um inteiro limitado à faixa de int32, caindo no padrão quando o
// valor é inválido ou está fora da faixa.
func inteiro32(chave string, padrao int32) int32 {
	valor, err := strconv.ParseInt(texto(chave, ""), 10, 32)
	if err != nil || valor <= 0 {
		return padrao
	}
	return int32(valor)
}

func decimal(chave string, padrao float64) float64 {
	valor, err := strconv.ParseFloat(texto(chave, ""), 64)
	if err != nil {
		return padrao
	}
	return valor
}

func booleano(chave string, padrao bool) bool {
	valor, err := strconv.ParseBool(texto(chave, ""))
	if err != nil {
		return padrao
	}
	return valor
}

func duracao(chave string, padrao time.Duration) time.Duration {
	valor, err := time.ParseDuration(texto(chave, ""))
	if err != nil {
		return padrao
	}
	return valor
}

// valorOmitido substitui todo segredo nas representações textuais abaixo.
const valorOmitido = "[omitido]"

// As representações textuais existem para cumprir a regra 11 do CLAUDE.md:
// campo privado não protege log, e `%#v` não passa pelo Stringer. Sem
// GoStringer, um `slog.Any("config", cfg)` ou um `fmt.Sprintf("%#v", cfg)`
// num handler de pânico imprimiria a senha do Postgres, a secret key do S3 e
// a chave da API do LLM — credenciais vivas, não só privacidade.
//
// O que não é segredo continua visível: endpoint, bucket e região são o que
// torna o dump útil para diagnóstico.

func (p Postgres) String() string {
	return fmt.Sprintf("config.Postgres{DSN:%s MaxConexoes:%d TempoLimiteConexao:%s}",
		valorOmitido, p.MaxConexoes, p.TempoLimiteConexao)
}

// GoString cobre o verbo %#v, que ignora o Stringer.
func (p Postgres) GoString() string { return p.String() }

func (s Storage) String() string {
	return fmt.Sprintf("config.Storage{Endpoint:%q Bucket:%q AccessKey:%s SecretKey:%s Regiao:%q}",
		s.Endpoint, s.Bucket, valorOmitido, valorOmitido, s.Regiao)
}

func (s Storage) GoString() string { return s.String() }

func (l LLM) String() string {
	return fmt.Sprintf("config.LLM{Habilitado:%t ChaveAPI:%s Modelo:%q LimiteConfianca:%v}",
		l.Habilitado, valorOmitido, l.Modelo, l.LimiteConfianca)
}

func (l LLM) GoString() string { return l.String() }

// String em Config também é necessária: %#v aplica o GoStringer ao operando,
// mas não garante aplicá-lo a cada campo aninhado. Formatar o agregado aqui
// tira essa dúvida do caminho.
func (c Config) String() string {
	return fmt.Sprintf("config.Config{Porta:%q Ambiente:%q NivelLog:%q Postgres:%s Storage:%s Conversor:%+v Telemetria:%+v LLM:%s}",
		c.Porta, c.Ambiente, c.NivelLog, c.Postgres, c.Storage, c.Conversor, c.Telemetria, c.LLM)
}

func (c Config) GoString() string { return c.String() }
