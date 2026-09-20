package log_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/daniel-halos/formatador/internal/infra/log"
)

func TestNovoEscreveJSONNoNivelConfigurado(t *testing.T) {
	var saida bytes.Buffer
	logger := log.Novo("warn", &saida)

	logger.Info("não deve aparecer")
	logger.Warn("deve aparecer", "campo", "valor")

	linhas := linhasJSON(t, saida.Bytes())
	if len(linhas) != 1 {
		t.Fatalf("esperava 1 linha de log, obteve %d: %s", len(linhas), saida.String())
	}
	if linhas[0]["msg"] != "deve aparecer" {
		t.Fatalf("mensagem inesperada: %v", linhas[0])
	}
	if linhas[0]["campo"] != "valor" {
		t.Fatalf("esperava o atributo campo=valor, obteve %v", linhas[0])
	}
}

func TestNivelInvalidoCaiParaInfo(t *testing.T) {
	var saida bytes.Buffer
	logger := log.Novo("qualquer-coisa", &saida)

	logger.Debug("não deve aparecer")
	logger.Info("deve aparecer")

	if linhas := linhasJSON(t, saida.Bytes()); len(linhas) != 1 {
		t.Fatalf("esperava apenas a linha de info, obteve %d", len(linhas))
	}
}

func TestDeDecoraLoggerComIdentificadoresDoContexto(t *testing.T) {
	var saida bytes.Buffer
	slog.SetDefault(log.Novo("info", &saida))

	ctx := log.ComIDJob(log.ComIDUsuario(log.ComIDRequisicao(context.Background(), "req-1"), "usr-2"), "job-3")
	log.De(ctx).Info("processando")

	linhas := linhasJSON(t, saida.Bytes())
	if len(linhas) != 1 {
		t.Fatalf("esperava 1 linha, obteve %d", len(linhas))
	}
	esperados := map[string]string{
		log.AtributoIDRequisicao: "req-1",
		log.AtributoIDUsuario:    "usr-2",
		log.AtributoIDJob:        "job-3",
	}
	for atributo, esperado := range esperados {
		if linhas[0][atributo] != esperado {
			t.Fatalf("esperava %s=%s, obteve %v", atributo, esperado, linhas[0][atributo])
		}
	}
}

func TestDeOmiteIdentificadoresAusentes(t *testing.T) {
	var saida bytes.Buffer
	slog.SetDefault(log.Novo("info", &saida))

	log.De(context.Background()).Info("sem correlação")

	linhas := linhasJSON(t, saida.Bytes())
	for _, atributo := range []string{log.AtributoIDRequisicao, log.AtributoIDUsuario, log.AtributoIDJob} {
		if _, presente := linhas[0][atributo]; presente {
			t.Fatalf("não esperava o atributo %s em %v", atributo, linhas[0])
		}
	}
}

func TestLeituraDeIdentificadoresComContextoNulo(t *testing.T) {
	if id := log.IDRequisicao(nil); id != "" { //nolint:staticcheck // contexto nulo é justamente o caso sob teste
		t.Fatalf("esperava vazio, obteve %q", id)
	}
}

func linhasJSON(t *testing.T, bruto []byte) []map[string]any {
	t.Helper()
	var linhas []map[string]any
	for _, linha := range bytes.Split(bytes.TrimSpace(bruto), []byte("\n")) {
		if len(linha) == 0 {
			continue
		}
		var registro map[string]any
		if err := json.Unmarshal(linha, &registro); err != nil {
			t.Fatalf("log não é JSON válido (%v): %s", err, linha)
		}
		linhas = append(linhas, registro)
	}
	return linhas
}
