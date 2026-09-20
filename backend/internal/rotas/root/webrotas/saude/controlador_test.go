package saude_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/daniel-halos/formatador/internal/infra/log"
	"github.com/daniel-halos/formatador/internal/rotas"
	"github.com/daniel-halos/formatador/internal/rotas/root/webrotas/saude"
)

func TestSaudeRespondeOkSemConsultarDependencias(t *testing.T) {
	controlador := saude.NovoControlador("1.0.0", &verificadorFalso{nome: "postgres", erro: errors.Novo("fora do ar")})
	resposta := &respostaEspia{}

	if err := controlador.TratarSaude(context.Background(), nil, resposta); err != nil {
		t.Fatalf("não esperava erro, obteve %v", err)
	}

	if resposta.status != http.StatusOK {
		t.Fatalf("esperava 200, obteve %d", resposta.status)
	}
	corpo, ok := resposta.corpo.(saude.RespostaSaude)
	if !ok {
		t.Fatalf("corpo inesperado: %T", resposta.corpo)
	}
	if corpo.Versao != "1.0.0" {
		t.Fatalf("esperava a versão no corpo, obteve %q", corpo.Versao)
	}
}

func TestProntidaoOkComTodasAsDependenciasDisponiveis(t *testing.T) {
	controlador := saude.NovoControlador("1.0.0",
		&verificadorFalso{nome: "postgres"},
		&verificadorFalso{nome: "storage"},
	)
	resposta := &respostaEspia{}

	if err := controlador.TratarProntidao(context.Background(), nil, resposta); err != nil {
		t.Fatalf("não esperava erro, obteve %v", err)
	}

	if resposta.status != http.StatusOK {
		t.Fatalf("esperava 200, obteve %d", resposta.status)
	}
	corpo := resposta.corpo.(saude.RespostaProntidao)
	if !corpo.Pronto || len(corpo.Dependencias) != 2 {
		t.Fatalf("esperava 2 dependências disponíveis, obteve %+v", corpo)
	}
}

func TestProntidaoDevolve503QuandoDependenciaFalha(t *testing.T) {
	controlador := saude.NovoControlador("1.0.0",
		&verificadorFalso{nome: "postgres"},
		&verificadorFalso{nome: "conversor", erro: errors.Novo("conexão recusada")},
	)
	resposta := &respostaEspia{}

	if err := controlador.TratarProntidao(context.Background(), nil, resposta); err != nil {
		t.Fatalf("não esperava erro, obteve %v", err)
	}

	if resposta.status != http.StatusServiceUnavailable {
		t.Fatalf("esperava 503, obteve %d", resposta.status)
	}
	if len(resposta.razoes) != 1 {
		t.Fatalf("esperava 1 razão, obteve %v", resposta.razoes)
	}
	if resposta.razoes[0] != "conversor: dependência indisponível" {
		t.Fatalf("razão inesperada: %q", resposta.razoes[0])
	}
}

func TestProntidaoNaoVazaErroOriginalNaResposta(t *testing.T) {
	segredo1 := "AKIAEXEMPLO"
	segredo2 := "minio:9000"
	controlador := saude.NovoControlador("1.0.0",
		&verificadorFalso{nome: "storage", erro: errors.Novo("dial tcp " + segredo2 + ": " + segredo1 + " invalid credentials")},
	)
	resposta := &respostaEspia{}

	if err := controlador.TratarProntidao(context.Background(), nil, resposta); err != nil {
		t.Fatalf("não esperava erro, obteve %v", err)
	}

	// resposta.Erro só expõe codigo/descricao/razoes ao cliente (ver echo.go
	// CorpoErro); é essa a única superfície que pode vazar o erro original.
	razoesConcatenadas := strings.Join(resposta.razoes, " | ")
	if strings.Contains(razoesConcatenadas, segredo1) || strings.Contains(razoesConcatenadas, segredo2) {
		t.Fatalf("razões vazaram o erro original: %q", razoesConcatenadas)
	}
	if strings.Contains(resposta.descricao, segredo1) || strings.Contains(resposta.descricao, segredo2) {
		t.Fatalf("descrição vazou o erro original: %q", resposta.descricao)
	}
}

func TestProntidaoLogaErroRealDaDependencia(t *testing.T) {
	var saida bytes.Buffer
	slog.SetDefault(log.Novo("info", &saida))

	segredo := "AKIAEXEMPLO invalid credentials"
	controlador := saude.NovoControlador("1.0.0",
		&verificadorFalso{nome: "storage", erro: errors.Novo(segredo)},
	)
	resposta := &respostaEspia{}

	if err := controlador.TratarProntidao(context.Background(), nil, resposta); err != nil {
		t.Fatalf("não esperava erro, obteve %v", err)
	}

	logado := saida.String()
	if !strings.Contains(logado, segredo) {
		t.Fatalf("esperava o erro real logado, obteve %q", logado)
	}
	if !strings.Contains(logado, "storage") {
		t.Fatalf("esperava o nome da dependência logado, obteve %q", logado)
	}
}

func TestProntidaoDuasDependenciasFalhamGeramDuasRazoes(t *testing.T) {
	controlador := saude.NovoControlador("1.0.0",
		&verificadorFalso{nome: "postgres", erro: errors.Novo("conexão recusada")},
		&verificadorFalso{nome: "storage", erro: errors.Novo("timeout")},
	)
	resposta := &respostaEspia{}

	if err := controlador.TratarProntidao(context.Background(), nil, resposta); err != nil {
		t.Fatalf("não esperava erro, obteve %v", err)
	}

	if len(resposta.razoes) != 2 {
		t.Fatalf("esperava 2 razões, obteve %v", resposta.razoes)
	}
	for _, razao := range resposta.razoes {
		if !strings.HasSuffix(razao, ": dependência indisponível") {
			t.Fatalf("razão fora do formato esperado: %q", razao)
		}
	}
}

func TestProntidaoSemDependenciasEstaPronta(t *testing.T) {
	resposta := &respostaEspia{}

	if err := saude.NovoControlador("1.0.0").TratarProntidao(context.Background(), nil, resposta); err != nil {
		t.Fatalf("não esperava erro, obteve %v", err)
	}

	if resposta.status != http.StatusOK {
		t.Fatalf("esperava 200, obteve %d", resposta.status)
	}
}

func TestProntidaoNaoTravaComDependenciaQueIgnoraOContexto(t *testing.T) {
	lento := &verificadorFalso{nome: "lento", respeitarContexto: true}
	resposta := &respostaEspia{}

	if err := saude.NovoControlador("1.0.0", lento).TratarProntidao(context.Background(), nil, resposta); err != nil {
		t.Fatalf("não esperava erro, obteve %v", err)
	}

	if !lento.recebeuPrazo {
		t.Fatal("esperava que o verificador recebesse um contexto com prazo")
	}
}

type verificadorFalso struct {
	nome              string
	erro              error
	respeitarContexto bool
	recebeuPrazo      bool
}

func (v *verificadorFalso) Nome() string { return v.nome }

func (v *verificadorFalso) Verificar(ctx context.Context) error {
	if v.respeitarContexto {
		_, temPrazo := ctx.Deadline()
		v.recebeuPrazo = temPrazo
	}
	return v.erro
}

type respostaEspia struct {
	status    int
	corpo     any
	codigo    string
	descricao string
	razoes    []string
}

func (r *respostaEspia) SemConteudo() error { r.status = http.StatusNoContent; return nil }
func (r *respostaEspia) Ok(corpo any) error { r.status, r.corpo = http.StatusOK, corpo; return nil }
func (r *respostaEspia) Criado(corpo any) error {
	r.status, r.corpo = http.StatusCreated, corpo
	return nil
}
func (r *respostaEspia) Aceito(corpo any) error {
	r.status, r.corpo = http.StatusAccepted, corpo
	return nil
}
func (r *respostaEspia) Erro(status int, codigo, descricao string, razoes []string) error {
	r.status, r.codigo, r.descricao, r.razoes = status, codigo, descricao, razoes
	return nil
}
func (r *respostaEspia) DefinirCabecalho(string, string) {}
func (r *respostaEspia) Status() int                     { return r.status }
func (r *respostaEspia) Escritor() http.ResponseWriter   { return nil }

var _ rotas.Resposta = (*respostaEspia)(nil)
