// RED (TDD): package execucao ainda não existe (só este arquivo de teste).
// Este teste referencia execucao.ServicoInterno, execucao.NovoServicoInterno
// e repository.ExecucaoJobRepo, nenhum dos quais está implementado ainda —
// deve falhar por erro de compilação até o codador entregar
// internal/domain/job/execucao/servico.go e
// internal/domain/job/repository/execucao.go.
package execucao

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-halos/formatador/internal/domain/job/entity"
	"github.com/daniel-halos/formatador/internal/domain/job/repository"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// execucaoRepoFake é o fake de repository.ExecucaoJobRepo usado neste
// arquivo, no mesmo padrão de documentosFake em job/criacao/servico_test.go:
// struct com funções injetáveis por caso, para que cada teste controle
// exatamente o que o repositório devolve e capture o que o serviço enviou.
type execucaoRepoFake struct {
	t      *testing.T
	obter  func(context.Context, uuid.UUID) (entity.Job, error)
	salvar func(context.Context, entity.Job, entity.StatusJob) error
}

func (r *execucaoRepoFake) ObterPorIDInterno(ctx context.Context, id uuid.UUID) (entity.Job, error) {
	if r.obter == nil {
		r.t.Fatal("execução do job tentou ler sem função configurada")
	}
	return r.obter(ctx, id)
}

func (r *execucaoRepoFake) Salvar(ctx context.Context, job entity.Job, statusAtual entity.StatusJob) error {
	if r.salvar == nil {
		r.t.Fatal("execução do job tentou salvar sem função configurada")
	}
	return r.salvar(ctx, job, statusAtual)
}

var _ repository.ExecucaoJobRepo = (*execucaoRepoFake)(nil)

// chamarOperacao roteia para um dos três métodos do contrato, com um
// resultado/motivo fixo, para que os testes table-driven cubram as três
// operações sem triplicar o corpo do teste.
func chamarOperacao(ctx context.Context, servico *ServicoInterno, operacao string, id uuid.UUID) (entity.Job, error) {
	switch operacao {
	case "iniciar":
		return servico.Iniciar(ctx, id)
	case "concluir":
		return servico.Concluir(ctx, id, json.RawMessage(`{"paginas":12}`))
	default:
		return servico.Falhar(ctx, id, "falha simulada no conversor")
	}
}

func TestNovoServicoInternoExigeRepositorio(t *testing.T) {
	t.Parallel()
	servico, err := NovoServicoInterno(nil)
	assert.Nil(t, servico)
	var nulo *errors.ErroArgumentoNulo
	require.ErrorAs(t, err, &nulo)
	assert.Equal(t, "repositorio", nulo.Argumento)
}

// TestOperacoesRejeitamIDZero prova que id == uuid.Nil falha em validação
// antes de qualquer I/O, para as três operações.
func TestOperacoesRejeitamIDZero(t *testing.T) {
	t.Parallel()
	for _, operacao := range []string{"iniciar", "concluir", "falhar"} {
		t.Run(operacao, func(t *testing.T) {
			repo := &execucaoRepoFake{
				t: t,
				obter: func(context.Context, uuid.UUID) (entity.Job, error) {
					t.Fatal("id zero não pode chegar à leitura")
					return entity.Job{}, nil
				},
				salvar: func(context.Context, entity.Job, entity.StatusJob) error {
					t.Fatal("id zero não pode chegar à escrita")
					return nil
				},
			}
			servico, err := NovoServicoInterno(repo)
			require.NoError(t, err)
			obtido, err := chamarOperacao(context.Background(), servico, operacao, uuid.Nil)
			var invalido *errors.ErroValidacao
			require.ErrorAs(t, err, &invalido)
			require.NotEmpty(t, invalido.Campos)
			assert.Equal(t, "id", invalido.Campos[0].Campo)
			assert.Equal(t, entity.Job{}, obtido)
		})
	}
}

// TestObterPropagaErroEnvolvido cobre que qualquer erro de
// ObterPorIDInterno — incluindo o não-encontrado de linha ausente — vem
// embrulhado com "obter execução do job", preservando a causa original para
// errors.Is/errors.As, e que nenhuma escrita acontece.
func TestObterPropagaErroEnvolvido(t *testing.T) {
	t.Parallel()
	causas := []error{
		errors.NovoErroNaoEncontrado("job"),
		errors.NovoErroAplicacao("indisponível"),
		context.Canceled,
	}
	for _, operacao := range []string{"iniciar", "concluir", "falhar"} {
		for _, causa := range causas {
			t.Run(operacao+"/"+causa.Error(), func(t *testing.T) {
				escritas := 0
				repo := &execucaoRepoFake{
					t: t,
					obter: func(context.Context, uuid.UUID) (entity.Job, error) {
						return entity.Job{}, causa
					},
					salvar: func(context.Context, entity.Job, entity.StatusJob) error {
						escritas++
						return nil
					},
				}
				servico, err := NovoServicoInterno(repo)
				require.NoError(t, err)
				obtido, err := chamarOperacao(context.Background(), servico, operacao, uuid.New())
				require.Error(t, err)
				assert.Truef(t, errors.E(err, causa), "causa original perdida: %v", err)
				assert.Contains(t, err.Error(), "obter execução do job")
				assert.Equal(t, entity.Job{}, obtido)
				assert.Zero(t, escritas)
			})
		}
	}
}

// TestJobIDDivergenteResultaEmNaoEncontrado simula o repositório devolvendo
// um job cujo ID não bate com o solicitado (corrupção/bug do adaptador): o
// serviço tem que recusar com um *errors.ErroNaoEncontrado direto, sem
// embrulho, e sem escrever.
func TestJobIDDivergenteResultaEmNaoEncontrado(t *testing.T) {
	t.Parallel()
	for _, operacao := range []string{"iniciar", "concluir", "falhar"} {
		t.Run(operacao, func(t *testing.T) {
			id := uuid.New()
			outroID := uuid.New()
			escritas := 0
			repo := &execucaoRepoFake{
				t: t,
				obter: func(context.Context, uuid.UUID) (entity.Job, error) {
					return entity.Job{ID: outroID, Status: entity.StatusExecutando}, nil
				},
				salvar: func(context.Context, entity.Job, entity.StatusJob) error {
					escritas++
					return nil
				},
			}
			servico, err := NovoServicoInterno(repo)
			require.NoError(t, err)
			obtido, err := chamarOperacao(context.Background(), servico, operacao, id)
			require.Error(t, err)
			assert.IsType(t, &errors.ErroNaoEncontrado{}, err, "divergência de ID deve ser erro direto, sem wrapper")
			assert.Equal(t, entity.Job{}, obtido)
			assert.Zero(t, escritas)
		})
	}
}

// TestIniciarArvoreDeTransicao é o caso central pedido pelo investigador:
// prova, por status de origem, a árvore exata de Iniciar.
//
//	a) pendente -> executando direto.
//	b) falhou -> Reenfileirar (falhou->pendente) e SÓ DEPOIS Iniciar
//	   (pendente->executando); o CAS final tem que comparar contra o status
//	   realmente lido (falhou), não contra o intermediário (pendente).
//	c) executando -> conflito da própria entidade, sem forçar nada: outro
//	   worker é o dono.
//	d) concluido/cancelado -> conflito, terminal.
func TestIniciarArvoreDeTransicao(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	casos := []struct {
		nome                string
		statusInicial       entity.StatusJob
		sucesso             bool
		statusAtualEsperado entity.StatusJob
	}{
		{"pendente inicia direto", entity.StatusPendente, true, entity.StatusPendente},
		{"falhou reenfileira e inicia", entity.StatusFalhou, true, entity.StatusFalhou},
		{"executando pertence a outro worker", entity.StatusExecutando, false, ""},
		{"concluido é terminal", entity.StatusConcluido, false, ""},
		{"cancelado é terminal", entity.StatusCancelado, false, ""},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			original := entity.Job{ID: id, Status: caso.statusInicial, Tentativas: 2, Progresso: 40}
			var statusAtualRecebido entity.StatusJob
			var salvo entity.Job
			escritas := 0
			repo := &execucaoRepoFake{
				t: t,
				obter: func(ctx context.Context, recebidoID uuid.UUID) (entity.Job, error) {
					assert.Equal(t, id, recebidoID)
					return original, nil
				},
				salvar: func(ctx context.Context, job entity.Job, statusAtual entity.StatusJob) error {
					escritas++
					statusAtualRecebido = statusAtual
					salvo = job
					return nil
				},
			}
			servico, err := NovoServicoInterno(repo)
			require.NoError(t, err)
			obtido, err := servico.Iniciar(context.Background(), id)
			if caso.sucesso {
				require.NoError(t, err)
				assert.Equal(t, entity.StatusExecutando, obtido.Status)
				assert.Equal(t, original.Tentativas+1, obtido.Tentativas, "tentativa deve incrementar, nunca reiniciar")
				assert.NotNil(t, obtido.IniciadoEm)
				assert.Equal(t, 1, escritas)
				assert.Equal(t, caso.statusAtualEsperado, statusAtualRecebido,
					"CAS deve comparar contra o status realmente lido, não contra um estado intermediário")
				assert.Equal(t, obtido, salvo)
				return
			}
			require.Error(t, err)
			var conflito *errors.ErroConflito
			assert.ErrorAs(t, err, &conflito)
			assert.Equal(t, entity.Job{}, obtido)
			assert.Zero(t, escritas, "conflito de transição não pode escrever")
		})
	}
}

// TestConcluirFalharCaminhoFeliz prova o caminho feliz completo de Concluir
// e Falhar a partir de "executando" (única origem válida para ambos) e que
// o statusAtual enviado ao repositório é o status ANTES da mutação — o
// teste que pegaria alguém invertendo statusAnterior := job.Status para
// depois de aplicar a transição.
func TestConcluirFalharCaminhoFeliz(t *testing.T) {
	t.Parallel()
	for _, caso := range []struct {
		nome           string
		operacao       string
		statusEsperado entity.StatusJob
	}{
		{"concluir", "concluir", entity.StatusConcluido},
		{"falhar", "falhar", entity.StatusFalhou},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			id := uuid.New()
			original := entity.Job{ID: id, Status: entity.StatusExecutando, Tentativas: 1, Progresso: 30}
			var statusAtualRecebido entity.StatusJob
			var salvo entity.Job
			escritas := 0
			repo := &execucaoRepoFake{
				t:     t,
				obter: func(context.Context, uuid.UUID) (entity.Job, error) { return original, nil },
				salvar: func(ctx context.Context, job entity.Job, statusAtual entity.StatusJob) error {
					escritas++
					statusAtualRecebido = statusAtual
					salvo = job
					return nil
				},
			}
			servico, err := NovoServicoInterno(repo)
			require.NoError(t, err)
			obtido, err := chamarOperacao(context.Background(), servico, caso.operacao, id)
			require.NoError(t, err)
			assert.Equal(t, caso.statusEsperado, obtido.Status)
			assert.Equal(t, entity.StatusExecutando, statusAtualRecebido, "CAS deve usar o status ANTES da mutação")
			assert.Equal(t, 1, escritas)
			assert.Equal(t, obtido, salvo)
			assert.NotNil(t, obtido.FinalizadoEm)
		})
	}
}

// TestConcluirFalharTransicaoInvalidaNaoEnvolve prova que o erro de
// transição vindo da entidade (*errors.ErroConflito) chega intacto, sem
// passar por errors.Envolver — repetir Concluir num job já concluído, ou
// chamar Falhar de um estado que não permite ir a "falhou".
func TestConcluirFalharTransicaoInvalidaNaoEnvolve(t *testing.T) {
	t.Parallel()
	for _, caso := range []struct {
		nome, operacao string
		statusInicial  entity.StatusJob
	}{
		{"concluir job já concluído", "concluir", entity.StatusConcluido},
		{"falhar job já falhou", "falhar", entity.StatusFalhou},
		{"falhar job pendente (só executando permite falhar)", "falhar", entity.StatusPendente},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			id := uuid.New()
			original := entity.Job{ID: id, Status: caso.statusInicial}
			escritas := 0
			repo := &execucaoRepoFake{
				t:     t,
				obter: func(context.Context, uuid.UUID) (entity.Job, error) { return original, nil },
				salvar: func(context.Context, entity.Job, entity.StatusJob) error {
					escritas++
					return nil
				},
			}
			servico, err := NovoServicoInterno(repo)
			require.NoError(t, err)
			obtido, err := chamarOperacao(context.Background(), servico, caso.operacao, id)
			require.Error(t, err)
			assert.IsType(t, &errors.ErroConflito{}, err, "transição inválida da entidade não pode vir embrulhada")
			assert.Equal(t, entity.Job{}, obtido)
			assert.Zero(t, escritas)
		})
	}
}

// TestConcluirResultadoInvalidoNaoEnvolve prova que a validação de payload
// feita pela própria entidade (Job.Concluir) também chega sem embrulho.
func TestConcluirResultadoInvalidoNaoEnvolve(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	original := entity.Job{ID: id, Status: entity.StatusExecutando}
	escritas := 0
	repo := &execucaoRepoFake{
		t:      t,
		obter:  func(context.Context, uuid.UUID) (entity.Job, error) { return original, nil },
		salvar: func(context.Context, entity.Job, entity.StatusJob) error { escritas++; return nil },
	}
	servico, err := NovoServicoInterno(repo)
	require.NoError(t, err)
	obtido, err := servico.Concluir(context.Background(), id, json.RawMessage(`"conteudo_privado"`))
	require.Error(t, err)
	assert.IsType(t, &errors.ErroValidacao{}, err, "validação de resultado da entidade não pode vir embrulhada")
	assert.NotContains(t, err.Error(), "conteudo_privado")
	assert.Equal(t, entity.Job{}, obtido)
	assert.Zero(t, escritas)
}

// TestFalharMotivoVazioNaoEnvolve prova o mesmo para Job.Falhar: motivo
// em branco é validação da entidade, sem embrulho.
func TestFalharMotivoVazioNaoEnvolve(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	original := entity.Job{ID: id, Status: entity.StatusExecutando}
	escritas := 0
	repo := &execucaoRepoFake{
		t:      t,
		obter:  func(context.Context, uuid.UUID) (entity.Job, error) { return original, nil },
		salvar: func(context.Context, entity.Job, entity.StatusJob) error { escritas++; return nil },
	}
	servico, err := NovoServicoInterno(repo)
	require.NoError(t, err)
	obtido, err := servico.Falhar(context.Background(), id, "   ")
	require.Error(t, err)
	assert.IsType(t, &errors.ErroValidacao{}, err, "validação de motivo da entidade não pode vir embrulhada")
	assert.Equal(t, entity.Job{}, obtido)
	assert.Zero(t, escritas)
}

// TestSalvarErroPropagaEnvolvido prova que qualquer erro de Salvar — CAS
// perdido incluso — chega embrulhado, preservando a causa original.
func TestSalvarErroPropagaEnvolvido(t *testing.T) {
	t.Parallel()
	causas := []error{
		errors.NovoErroConflito("status do job mudou"),
		errors.NovoErroAplicacao("indisponível"),
		context.DeadlineExceeded,
	}
	for _, operacao := range []string{"iniciar", "concluir", "falhar"} {
		for _, causa := range causas {
			t.Run(operacao+"/"+causa.Error(), func(t *testing.T) {
				id := uuid.New()
				statusInicial := entity.StatusPendente
				if operacao != "iniciar" {
					statusInicial = entity.StatusExecutando
				}
				original := entity.Job{ID: id, Status: statusInicial}
				repo := &execucaoRepoFake{
					t:      t,
					obter:  func(context.Context, uuid.UUID) (entity.Job, error) { return original, nil },
					salvar: func(context.Context, entity.Job, entity.StatusJob) error { return causa },
				}
				servico, err := NovoServicoInterno(repo)
				require.NoError(t, err)
				obtido, err := chamarOperacao(context.Background(), servico, operacao, id)
				require.Error(t, err)
				assert.Truef(t, errors.E(err, causa), "causa original perdida: %v", err)
				assert.IsType(t, &errors.ErroEnvolvido{}, err, "erro de Salvar precisa vir embrulhado")
				assert.Equal(t, entity.Job{}, obtido)
			})
		}
	}
}
