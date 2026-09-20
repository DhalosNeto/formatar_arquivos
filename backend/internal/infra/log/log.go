// Package log expõe o logger estruturado da aplicação (slog em JSON) e a
// propagação de identificadores de correlação pelo context.
//
// Regra do projeto: conteúdo de documento do usuário NUNCA vai para o log.
package log

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
)

type chaveContexto string

const (
	chaveIDRequisicao chaveContexto = "id_requisicao"
	chaveIDUsuario    chaveContexto = "id_usuario"
	chaveIDJob        chaveContexto = "id_job"
)

// Atributos de log usados em todo o projeto.
const (
	AtributoIDRequisicao = "id_requisicao"
	AtributoIDUsuario    = "id_usuario"
	AtributoIDJob        = "id_job"
)

// Novo cria um logger JSON no nível informado, escrevendo em saida.
func Novo(nivel string, saida io.Writer) *slog.Logger {
	opcoes := &slog.HandlerOptions{Level: converterNivel(nivel)}
	return slog.New(slog.NewJSONHandler(saida, opcoes))
}

// NovoPadrao cria o logger da aplicação em os.Stdout e o define como padrão do slog.
func NovoPadrao(nivel string) *slog.Logger {
	logger := Novo(nivel, os.Stdout)
	slog.SetDefault(logger)
	return logger
}

func converterNivel(nivel string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(nivel)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error", "erro":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// ComIDRequisicao devolve um context carregando o identificador da requisição.
func ComIDRequisicao(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, chaveIDRequisicao, id)
}

// ComIDUsuario devolve um context carregando o identificador do usuário logado.
func ComIDUsuario(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, chaveIDUsuario, id)
}

// ComIDJob devolve um context carregando o identificador do job em execução.
func ComIDJob(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, chaveIDJob, id)
}

// IDRequisicao lê o identificador da requisição do context.
func IDRequisicao(ctx context.Context) string { return texto(ctx, chaveIDRequisicao) }

// IDUsuario lê o identificador do usuário do context.
func IDUsuario(ctx context.Context) string { return texto(ctx, chaveIDUsuario) }

// IDJob lê o identificador do job do context.
func IDJob(ctx context.Context) string { return texto(ctx, chaveIDJob) }

func texto(ctx context.Context, chave chaveContexto) string {
	if ctx == nil {
		return ""
	}
	valor, _ := ctx.Value(chave).(string)
	return valor
}

// De devolve um logger já decorado com os identificadores presentes no context.
// É a forma preferida de logar em qualquer camada: log.De(ctx).Info("...").
func De(ctx context.Context) *slog.Logger {
	logger := slog.Default()
	for _, par := range []struct {
		atributo string
		valor    string
	}{
		{AtributoIDRequisicao, IDRequisicao(ctx)},
		{AtributoIDUsuario, IDUsuario(ctx)},
		{AtributoIDJob, IDJob(ctx)},
	} {
		if par.valor != "" {
			logger = logger.With(par.atributo, par.valor)
		}
	}
	return logger
}
