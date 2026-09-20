// RED (TDD): package fila ainda não tem implementação, só este arquivo de
// teste. Ele referencia fila.Laco, fila.NovoLaco e as interfaces
// fila.Reivindicador/fila.Executor/fila.Finalizador — nenhum símbolo existe
// ainda. Espera-se falha de compilação ("undefined: fila.NovoLaco" etc.) até
// o codador entregar internal/infra/fila/laco.go.
//
// Contrato esperado (ver docs/adr/0002-fila-sem-river.md):
//
//	type Reivindicador interface {
//	    // Reivindicar tira o próximo job pendente da fila. ok=false com job
//	    // zero e err nil significa fila vazia — não é erro.
//	    Reivindicar(ctx context.Context) (job entity.Job, ok bool, err error)
//	}
//
//	type Executor interface {
//	    // Executar roda o trabalho de fato (parse/formatação/conversão) para
//	    // o job reivindicado e devolve o resultado a persistir.
//	    Executar(ctx context.Context, job entity.Job) (resultado json.RawMessage, err error)
//	}
//
//	type Finalizador interface {
//	    Concluir(ctx context.Context, id uuid.UUID, resultado json.RawMessage) (entity.Job, error)
//	    Falhar(ctx context.Context, id uuid.UUID, motivo string) (entity.Job, error)
//	}
//
//	func NovoLaco(reivindicador Reivindicador, executor Executor, finalizador Finalizador, intervalo time.Duration) (*Laco, error)
//	func (l *Laco) Executar(ctx context.Context) error
//
// Regras de comportamento exigidas pelos testes abaixo:
//   - Executar roda até ctx ser cancelado; retorna nil no desligamento
//     gracioso (não devolve ctx.Err()).
//   - Fila vazia (ok=false) não é erro: o laço espera `intervalo` e tenta de
//     novo, respeitando o cancelamento do ctx durante a espera.
//   - Um job já reivindicado é SEMPRE terminado (Concluir OU Falhar) antes do
//     laço checar novamente o cancelamento do ctx — nunca abandona um job em
//     "executando". Recomenda-se rodar o trabalho e a finalização com
//     context.WithoutCancel(ctx), para que o cancelamento do ctx externo não
//     aborte um job já em voo.
//   - Falhar é chamado com um motivo não vazio quando Executor devolve erro.
//   - Nenhum log emitido pelo laço pode conter o texto de err.Error() do
//     Executor nem o conteúdo do resultado — CLAUDE.md regra 7. Use
//     log.De(ctx) só com metadado (id do job, tipo), nunca com o erro cru.
package fila

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-halos/formatador/internal/domain/job/entity"
	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/daniel-halos/formatador/internal/infra/log"
)

// intervaloCurto é usado em todo este arquivo para manter os testes rápidos
// sem depender de relógio real além de milissegundos.
const intervaloCurto = 2 * time.Millisecond

// reivindicadorFake entrega os jobs de "jobs" em ordem, um por chamada; uma
// vez esgotados, toda chamada seguinte devolve fila vazia (ok=false, err
// nil), como o Postgres real faria.
type reivindicadorFake struct {
	mu       sync.Mutex
	jobs     []entity.Job
	chamadas int
}

func (r *reivindicadorFake) Reivindicar(context.Context) (entity.Job, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.chamadas++
	if len(r.jobs) == 0 {
		return entity.Job{}, false, nil
	}
	job := r.jobs[0]
	r.jobs = r.jobs[1:]
	return job, true, nil
}

func (r *reivindicadorFake) totalChamadas() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.chamadas
}

var _ Reivindicador = (*reivindicadorFake)(nil)

// executorFake registra os jobs recebidos e, se iniciado/bloquear estiverem
// configurados, sincroniza com o teste: fecha iniciado assim que é chamado e
// só devolve depois que bloquear for fechado — usado para provar que o laço
// termina um job em voo mesmo com o ctx já cancelado.
type executorFake struct {
	mu        sync.Mutex
	chamadas  []entity.Job
	resultado json.RawMessage
	erro      error
	iniciado  chan struct{}
	bloquear  chan struct{}
}

func (e *executorFake) Executar(_ context.Context, job entity.Job) (json.RawMessage, error) {
	e.mu.Lock()
	e.chamadas = append(e.chamadas, job)
	e.mu.Unlock()
	if e.iniciado != nil {
		close(e.iniciado)
	}
	if e.bloquear != nil {
		<-e.bloquear
	}
	return e.resultado, e.erro
}

func (e *executorFake) totalChamadas() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.chamadas)
}

var _ Executor = (*executorFake)(nil)

type chamadaConcluir struct {
	id        uuid.UUID
	resultado json.RawMessage
}

type chamadaFalhar struct {
	id     uuid.UUID
	motivo string
}

// finalizadorFake registra as chamadas de Concluir/Falhar; equivale, para
// este teste, ao job/execucao.ServicoInterno real usado em produção.
type finalizadorFake struct {
	mu       sync.Mutex
	concluiu []chamadaConcluir
	falhou   []chamadaFalhar
}

func (f *finalizadorFake) Concluir(_ context.Context, id uuid.UUID, resultado json.RawMessage) (entity.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.concluiu = append(f.concluiu, chamadaConcluir{id, resultado})
	return entity.Job{ID: id, Status: entity.StatusConcluido}, nil
}

func (f *finalizadorFake) Falhar(_ context.Context, id uuid.UUID, motivo string) (entity.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.falhou = append(f.falhou, chamadaFalhar{id, motivo})
	return entity.Job{ID: id, Status: entity.StatusFalhou}, nil
}

func (f *finalizadorFake) snapshot() (concluiu []chamadaConcluir, falhou []chamadaFalhar) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]chamadaConcluir(nil), f.concluiu...), append([]chamadaFalhar(nil), f.falhou...)
}

var _ Finalizador = (*finalizadorFake)(nil)

// executarEEncerrar sobe o laço numa goroutine e devolve um canal com o erro
// de retorno de Executar, para que o teste possa cancelar o ctx e esperar o
// encerramento gracioso sem depender de sleep fixo.
func executarEEncerrar(laco *Laco, ctx context.Context) <-chan error {
	encerrado := make(chan error, 1)
	go func() { encerrado <- laco.Executar(ctx) }()
	return encerrado
}

func aguardarEncerramento(t *testing.T, encerrado <-chan error) error {
	t.Helper()
	select {
	case err := <-encerrado:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("laço não encerrou dentro do prazo após o cancelamento do contexto")
		return nil
	}
}

func TestNovoLacoExigeDependencias(t *testing.T) {
	reiv := &reivindicadorFake{}
	exec := &executorFake{}
	fin := &finalizadorFake{}

	casos := []struct {
		nome              string
		reivindicador     Reivindicador
		executor          Executor
		finalizador       Finalizador
		intervalo         time.Duration
		argumentoEsperado string
	}{
		{"reivindicador nulo", nil, exec, fin, intervaloCurto, "reivindicador"},
		{"executor nulo", reiv, nil, fin, intervaloCurto, "executor"},
		{"finalizador nulo", reiv, exec, nil, intervaloCurto, "finalizador"},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			laco, err := NovoLaco(caso.reivindicador, caso.executor, caso.finalizador, caso.intervalo)
			assert.Nil(t, laco)
			var nulo *errors.ErroArgumentoNulo
			require.ErrorAs(t, err, &nulo)
			assert.Equal(t, caso.argumentoEsperado, nulo.Argumento)
		})
	}
}

func TestNovoLacoExigeIntervaloPositivo(t *testing.T) {
	laco, err := NovoLaco(&reivindicadorFake{}, &executorFake{}, &finalizadorFake{}, 0)
	assert.Nil(t, laco)
	require.Error(t, err)
}

// TestLacoExecutaTrabalhoQuandoHaJobEChamaConcluir é o caminho feliz: job
// disponível, trabalho sem erro, o laço tem que persistir a conclusão com o
// resultado devolvido pelo Executor.
func TestLacoExecutaTrabalhoQuandoHaJobEChamaConcluir(t *testing.T) {
	job := entity.Job{ID: uuid.New(), Status: entity.StatusExecutando}
	resultado := json.RawMessage(`{"paginas":3}`)
	reiv := &reivindicadorFake{jobs: []entity.Job{job}}
	exec := &executorFake{resultado: resultado}
	fin := &finalizadorFake{}

	laco, err := NovoLaco(reiv, exec, fin, intervaloCurto)
	require.NoError(t, err)

	ctx, cancelar := context.WithCancel(context.Background())
	defer cancelar()
	encerrado := executarEEncerrar(laco, ctx)

	require.Eventually(t, func() bool {
		concluiu, _ := fin.snapshot()
		return len(concluiu) == 1
	}, 2*time.Second, time.Millisecond, "Concluir deveria ter sido chamado para o job reivindicado")

	cancelar()
	err = aguardarEncerramento(t, encerrado)
	assert.NoError(t, err, "encerramento gracioso não deve devolver erro")

	concluiu, falhou := fin.snapshot()
	require.Len(t, concluiu, 1)
	assert.Equal(t, job.ID, concluiu[0].id)
	assert.Equal(t, resultado, concluiu[0].resultado)
	assert.Empty(t, falhou, "trabalho sem erro não pode chamar Falhar")
	assert.Equal(t, 1, exec.totalChamadas())
}

// TestLacoTrabalhoComErroChamaFalharSemDeixarExecutando prova que um erro do
// Executor termina o job via Falhar, com um motivo não vazio, e nunca via
// Concluir — o job não pode ficar preso em "executando".
func TestLacoTrabalhoComErroChamaFalharSemDeixarExecutando(t *testing.T) {
	job := entity.Job{ID: uuid.New(), Status: entity.StatusExecutando}
	reiv := &reivindicadorFake{jobs: []entity.Job{job}}
	exec := &executorFake{erro: errors.Novo("falha simulada ao converter documento")}
	fin := &finalizadorFake{}

	laco, err := NovoLaco(reiv, exec, fin, intervaloCurto)
	require.NoError(t, err)

	ctx, cancelar := context.WithCancel(context.Background())
	defer cancelar()
	encerrado := executarEEncerrar(laco, ctx)

	require.Eventually(t, func() bool {
		_, falhou := fin.snapshot()
		return len(falhou) == 1
	}, 2*time.Second, time.Millisecond, "Falhar deveria ter sido chamado para o job com erro")

	cancelar()
	err = aguardarEncerramento(t, encerrado)
	assert.NoError(t, err)

	concluiu, falhou := fin.snapshot()
	assert.Empty(t, concluiu, "trabalho com erro não pode chamar Concluir")
	require.Len(t, falhou, 1)
	assert.Equal(t, job.ID, falhou[0].id)
	assert.NotEmpty(t, falhou[0].motivo, "motivo da falha não pode ser vazio")
}

// TestLacoFilaVaziaNaoEhErro prova que ok=false não interrompe o laço nem
// devolve erro: ele espera o intervalo configurado e tenta de novo.
func TestLacoFilaVaziaNaoEhErro(t *testing.T) {
	reiv := &reivindicadorFake{} // sem jobs: toda chamada devolve ok=false
	exec := &executorFake{}
	fin := &finalizadorFake{}

	laco, err := NovoLaco(reiv, exec, fin, intervaloCurto)
	require.NoError(t, err)

	ctx, cancelar := context.WithCancel(context.Background())
	defer cancelar()
	encerrado := executarEEncerrar(laco, ctx)

	require.Eventually(t, func() bool {
		return reiv.totalChamadas() >= 3
	}, 2*time.Second, time.Millisecond, "o laço deveria ter tentado reivindicar mais de uma vez")

	cancelar()
	err = aguardarEncerramento(t, encerrado)
	assert.NoError(t, err, "fila vazia seguida de cancelamento não pode devolver erro")

	assert.Zero(t, exec.totalChamadas(), "sem job, o executor não pode ser chamado")
	concluiu, falhou := fin.snapshot()
	assert.Empty(t, concluiu)
	assert.Empty(t, falhou)
}

// TestLacoContextoCanceladoTerminaJobEmVoo é o caso central: o ctx é
// cancelado ENQUANTO o Executor ainda está rodando; o laço tem que esperar o
// trabalho terminar, finalizar o job (Concluir/Falhar) e só então retornar —
// nunca abandonar o job em "executando", e nunca reivindicar um job novo
// depois do cancelamento.
func TestLacoContextoCanceladoTerminaJobEmVoo(t *testing.T) {
	job := entity.Job{ID: uuid.New(), Status: entity.StatusExecutando}
	reiv := &reivindicadorFake{jobs: []entity.Job{job}}
	iniciado := make(chan struct{})
	liberar := make(chan struct{})
	exec := &executorFake{iniciado: iniciado, bloquear: liberar, resultado: json.RawMessage(`{"ok":true}`)}
	fin := &finalizadorFake{}

	laco, err := NovoLaco(reiv, exec, fin, intervaloCurto)
	require.NoError(t, err)

	ctx, cancelar := context.WithCancel(context.Background())
	encerrado := executarEEncerrar(laco, ctx)

	select {
	case <-iniciado:
	case <-time.After(2 * time.Second):
		t.Fatal("executor não foi chamado a tempo")
	}

	// O job está "em voo" dentro do Executor: cancela o laço agora, antes de
	// liberar o trabalho, para provar que o cancelamento não interrompe o
	// job já reivindicado.
	cancelar()
	close(liberar)

	err = aguardarEncerramento(t, encerrado)
	assert.NoError(t, err)

	assert.Equal(t, 1, reiv.totalChamadas(), "não pode reivindicar um novo job após o cancelamento")
	concluiu, falhou := fin.snapshot()
	require.Len(t, concluiu, 1, "o job em voo tem que ser concluído, não abandonado")
	assert.Equal(t, job.ID, concluiu[0].id)
	assert.Empty(t, falhou)
}

// TestLacoNaoLogaConteudoDoErroDeExecucao é a regra 7 do CLAUDE.md aplicada
// ao worker: o texto devolvido pelo Executor pode conter trecho do documento
// do usuário (mensagem de erro do parser/conversor); esse texto não pode
// aparecer em log algum emitido pelo laço.
func TestLacoNaoLogaConteudoDoErroDeExecucao(t *testing.T) {
	var saida bytes.Buffer
	slog.SetDefault(log.Novo("debug", &saida))

	segredoDoUsuario := "TRECHO_CONFIDENCIAL_DO_ARTIGO_DO_USUARIO_文_😀"
	job := entity.Job{ID: uuid.New(), Status: entity.StatusExecutando}
	reiv := &reivindicadorFake{jobs: []entity.Job{job}}
	exec := &executorFake{erro: errors.Novo("falha ao converter: " + segredoDoUsuario)}
	fin := &finalizadorFake{}

	laco, err := NovoLaco(reiv, exec, fin, intervaloCurto)
	require.NoError(t, err)

	ctx, cancelar := context.WithCancel(context.Background())
	defer cancelar()
	encerrado := executarEEncerrar(laco, ctx)

	require.Eventually(t, func() bool {
		_, falhou := fin.snapshot()
		return len(falhou) == 1
	}, 2*time.Second, time.Millisecond)

	cancelar()
	_ = aguardarEncerramento(t, encerrado)

	require.NotContainsf(t, saida.String(), segredoDoUsuario,
		"conteúdo do documento do usuário vazou para o log: %s", saida.String())
}
