package service

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/daniel-halos/formatador/internal/domain/job/entity"
	"github.com/daniel-halos/formatador/internal/domain/job/repository"
	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/google/uuid"
)

// repositorioFake é uma implementação in-memory e determinística de
// repository.JobRepo, usada só nos testes: registra as chamadas recebidas para
// que o teste possa assertar interações, sem banco e sem rede.
type repositorioFake struct {
	mu               sync.Mutex
	jobs             map[uuid.UUID]entity.Job
	chamadas         []string
	statusAnteriores []entity.StatusJob
	progressos       []int

	erroInserir            error
	erroObterPorID         error
	erroListarPorDocumento error
	erroAtualizar          error
	erroAtualizarProgresso error
}

func novoRepositorioFake() *repositorioFake {
	return &repositorioFake{jobs: make(map[uuid.UUID]entity.Job)}
}

func (r *repositorioFake) Inserir(_ context.Context, job entity.Job) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.chamadas = append(r.chamadas, "Inserir")
	if r.erroInserir != nil {
		return r.erroInserir
	}
	r.jobs[job.ID] = job
	return nil
}

func (r *repositorioFake) ObterPorID(_ context.Context, id uuid.UUID) (entity.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.chamadas = append(r.chamadas, "ObterPorID")
	if r.erroObterPorID != nil {
		return entity.Job{}, r.erroObterPorID
	}
	job, existe := r.jobs[id]
	if !existe {
		return entity.Job{}, errors.NovoErroNaoEncontrado("job")
	}
	return job, nil
}

func (r *repositorioFake) ListarPorDocumento(_ context.Context, documentoID uuid.UUID) ([]entity.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.chamadas = append(r.chamadas, "ListarPorDocumento")
	if r.erroListarPorDocumento != nil {
		return nil, r.erroListarPorDocumento
	}
	encontrados := make([]entity.Job, 0)
	for _, job := range r.jobs {
		if job.DocumentoID == documentoID {
			encontrados = append(encontrados, job)
		}
	}
	return encontrados, nil
}

func (r *repositorioFake) Atualizar(_ context.Context, job entity.Job, statusAnterior entity.StatusJob) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.chamadas = append(r.chamadas, "Atualizar")
	r.statusAnteriores = append(r.statusAnteriores, statusAnterior)
	if r.erroAtualizar != nil {
		return r.erroAtualizar
	}
	guardado, existe := r.jobs[job.ID]
	if !existe {
		return errors.NovoErroNaoEncontrado("job")
	}
	if guardado.Status != statusAnterior {
		return errors.NovoErroConflito("status do job mudou durante a operação")
	}
	r.jobs[job.ID] = job
	return nil
}

func (r *repositorioFake) AtualizarProgresso(_ context.Context, id uuid.UUID, progresso int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.chamadas = append(r.chamadas, "AtualizarProgresso")
	r.progressos = append(r.progressos, progresso)
	if r.erroAtualizarProgresso != nil {
		return r.erroAtualizarProgresso
	}
	job, existe := r.jobs[id]
	if !existe {
		return errors.NovoErroNaoEncontrado("job")
	}
	job.Progresso = progresso
	r.jobs[id] = job
	return nil
}

func (r *repositorioFake) contarChamadas(nome string) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	total := 0
	for _, chamada := range r.chamadas {
		if chamada == nome {
			total++
		}
	}
	return total
}

func (r *repositorioFake) guardar(job entity.Job) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.jobs[job.ID] = job
}

func (r *repositorioFake) obter(id uuid.UUID) (entity.Job, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	job, existe := r.jobs[id]
	return job, existe
}

func (r *repositorioFake) ultimoStatusAnterior() (entity.StatusJob, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.statusAnteriores) == 0 {
		return "", false
	}
	return r.statusAnteriores[len(r.statusAnteriores)-1], true
}

// garante em tempo de compilação que o fake satisfaz a porta do domínio.
var _ repository.JobRepo = (*repositorioFake)(nil)

func TestNovoServicoRecusaRepositorioNulo(t *testing.T) {
	t.Parallel()

	servico, err := NovoServico(nil)
	if err == nil {
		t.Fatal("esperava erro de argumento nulo")
	}

	var nulo *errors.ErroArgumentoNulo
	if !errors.Como(err, &nulo) {
		t.Fatalf("esperava *ErroArgumentoNulo, obteve %T", err)
	}
	if nulo.Argumento != "repositorio" {
		t.Fatalf("esperava argumento %q, obteve %q", "repositorio", nulo.Argumento)
	}
	if servico != nil {
		t.Fatal("esperava serviço nulo quando o repositório é nulo")
	}
}

func TestServicoCriar(t *testing.T) {
	t.Parallel()

	t.Run("cria job de renderizar preview pendente e persiste", func(t *testing.T) {
		t.Parallel()

		repositorio, servico := servicoDeTeste(t)
		documentoID := uuid.New()

		job, err := servico.Criar(context.Background(), DadosNovoJob{
			DocumentoID: documentoID,
			Tipo:        entity.TipoRenderizarPreview,
		})
		if err != nil {
			t.Fatalf("não esperava erro, obteve %v", err)
		}
		if job.Status != entity.StatusPendente {
			t.Fatalf("esperava status pendente, obteve %q", job.Status)
		}
		if job.ID == uuid.Nil {
			t.Fatal("esperava ID gerado")
		}
		if repositorio.contarChamadas("Inserir") != 1 {
			t.Fatalf("esperava 1 chamada a Inserir, obteve %d", repositorio.contarChamadas("Inserir"))
		}

		guardado, existe := repositorio.obter(job.ID)
		if !existe {
			t.Fatal("esperava job persistido no repositório")
		}
		if guardado.DocumentoID != documentoID {
			t.Fatalf("esperava documento %v, obteve %v", documentoID, guardado.DocumentoID)
		}
		if guardado.Status != entity.StatusPendente {
			t.Fatalf("esperava status pendente persistido, obteve %q", guardado.Status)
		}
	})

	t.Run("formatar sem ruleset não persiste nada", func(t *testing.T) {
		t.Parallel()

		repositorio, servico := servicoDeTeste(t)

		_, err := servico.Criar(context.Background(), DadosNovoJob{
			DocumentoID: uuid.New(),
			Tipo:        entity.TipoFormatar,
		})
		exigirCampoInvalido(t, err, "ruleset_id")

		if repositorio.contarChamadas("Inserir") != 0 {
			t.Fatalf("esperava nenhuma chamada a Inserir, obteve %d", repositorio.contarChamadas("Inserir"))
		}
	})

	t.Run("documento nulo não persiste nada", func(t *testing.T) {
		t.Parallel()

		repositorio, servico := servicoDeTeste(t)

		_, err := servico.Criar(context.Background(), DadosNovoJob{
			DocumentoID: uuid.Nil,
			Tipo:        entity.TipoAnalisar,
		})
		exigirCampoInvalido(t, err, "documento_id")

		if repositorio.contarChamadas("Inserir") != 0 {
			t.Fatalf("esperava nenhuma chamada a Inserir, obteve %d", repositorio.contarChamadas("Inserir"))
		}
	})

	t.Run("erro do repositório é propagado", func(t *testing.T) {
		t.Parallel()

		repositorio, servico := servicoDeTeste(t)
		repositorio.erroInserir = errors.NovoErroConflito("job duplicado")

		_, err := servico.Criar(context.Background(), DadosNovoJob{
			DocumentoID: uuid.New(),
			Tipo:        entity.TipoAnalisar,
		})
		exigirConflito(t, err)
	})
}

func TestServicoObter(t *testing.T) {
	t.Parallel()

	t.Run("identificador nulo é inválido", func(t *testing.T) {
		t.Parallel()

		repositorio, servico := servicoDeTeste(t)

		_, err := servico.Obter(context.Background(), uuid.Nil)
		exigirCampoInvalido(t, err, "id")

		if repositorio.contarChamadas("ObterPorID") != 0 {
			t.Fatal("esperava que o serviço nem consultasse o repositório")
		}
	})

	t.Run("job inexistente devolve não encontrado", func(t *testing.T) {
		t.Parallel()

		_, servico := servicoDeTeste(t)

		_, err := servico.Obter(context.Background(), uuid.New())
		if err == nil {
			t.Fatal("esperava erro de não encontrado")
		}
		var naoEncontrado *errors.ErroNaoEncontrado
		if !errors.Como(err, &naoEncontrado) {
			t.Fatalf("esperava *ErroNaoEncontrado, obteve %T", err)
		}
	})

	t.Run("job existente é devolvido", func(t *testing.T) {
		t.Parallel()

		repositorio, servico := servicoDeTeste(t)
		criado := criarJob(t, entity.TipoAnalisar, nil)
		repositorio.guardar(criado)

		obtido, err := servico.Obter(context.Background(), criado.ID)
		if err != nil {
			t.Fatalf("não esperava erro, obteve %v", err)
		}
		if obtido.ID != criado.ID {
			t.Fatalf("esperava job %v, obteve %v", criado.ID, obtido.ID)
		}
	})
}

func TestServicoListarDoDocumento(t *testing.T) {
	t.Parallel()

	t.Run("documento nulo é inválido", func(t *testing.T) {
		t.Parallel()

		_, servico := servicoDeTeste(t)

		_, err := servico.ListarDoDocumento(context.Background(), uuid.Nil)
		exigirCampoInvalido(t, err, "documento_id")
	})

	t.Run("documento sem jobs devolve lista vazia sem erro", func(t *testing.T) {
		t.Parallel()

		_, servico := servicoDeTeste(t)

		jobs, err := servico.ListarDoDocumento(context.Background(), uuid.New())
		if err != nil {
			t.Fatalf("não esperava erro, obteve %v", err)
		}
		if len(jobs) != 0 {
			t.Fatalf("esperava lista vazia, obteve %d", len(jobs))
		}
	})

	t.Run("devolve apenas os jobs do documento", func(t *testing.T) {
		t.Parallel()

		repositorio, servico := servicoDeTeste(t)
		documentoID := uuid.New()

		primeiro := criarJob(t, entity.TipoAnalisar, nil)
		primeiro.DocumentoID = documentoID
		repositorio.guardar(primeiro)

		segundo := criarJob(t, entity.TipoRenderizarPreview, nil)
		segundo.DocumentoID = documentoID
		repositorio.guardar(segundo)

		deOutro := criarJob(t, entity.TipoAnalisar, nil)
		repositorio.guardar(deOutro)

		jobs, err := servico.ListarDoDocumento(context.Background(), documentoID)
		if err != nil {
			t.Fatalf("não esperava erro, obteve %v", err)
		}
		if len(jobs) != 2 {
			t.Fatalf("esperava 2 jobs, obteve %d", len(jobs))
		}
		for _, job := range jobs {
			if job.DocumentoID != documentoID {
				t.Fatalf("esperava apenas jobs de %v, obteve job de %v", documentoID, job.DocumentoID)
			}
		}
	})
}

func TestServicoIniciar(t *testing.T) {
	t.Parallel()

	t.Run("atualiza com guarda otimista no status pendente", func(t *testing.T) {
		t.Parallel()

		repositorio, servico := servicoDeTeste(t)
		criado := criarJob(t, entity.TipoAnalisar, nil)
		repositorio.guardar(criado)

		job, err := servico.Iniciar(context.Background(), criado.ID)
		if err != nil {
			t.Fatalf("não esperava erro, obteve %v", err)
		}
		if job.Status != entity.StatusExecutando {
			t.Fatalf("esperava status executando, obteve %q", job.Status)
		}
		if job.Tentativas != 1 {
			t.Fatalf("esperava 1 tentativa, obteve %d", job.Tentativas)
		}

		if repositorio.contarChamadas("Atualizar") != 1 {
			t.Fatalf("esperava 1 chamada a Atualizar, obteve %d", repositorio.contarChamadas("Atualizar"))
		}
		anterior, registrado := repositorio.ultimoStatusAnterior()
		if !registrado {
			t.Fatal("esperava statusAnterior registrado na chamada a Atualizar")
		}
		if anterior != entity.StatusPendente {
			t.Fatalf("esperava statusAnterior pendente, obteve %q", anterior)
		}

		guardado, _ := repositorio.obter(criado.ID)
		if guardado.Status != entity.StatusExecutando {
			t.Fatalf("esperava status executando persistido, obteve %q", guardado.Status)
		}
	})

	t.Run("job inexistente devolve não encontrado", func(t *testing.T) {
		t.Parallel()

		repositorio, servico := servicoDeTeste(t)

		_, err := servico.Iniciar(context.Background(), uuid.New())
		var naoEncontrado *errors.ErroNaoEncontrado
		if !errors.Como(err, &naoEncontrado) {
			t.Fatalf("esperava *ErroNaoEncontrado, obteve %T", err)
		}
		if repositorio.contarChamadas("Atualizar") != 0 {
			t.Fatal("esperava nenhuma chamada a Atualizar")
		}
	})

	t.Run("job já executando devolve conflito sem atualizar", func(t *testing.T) {
		t.Parallel()

		repositorio, servico := servicoDeTeste(t)
		criado := criarJob(t, entity.TipoAnalisar, nil)
		if err := criado.Iniciar(); err != nil {
			t.Fatalf("pré-condição: %v", err)
		}
		repositorio.guardar(criado)

		_, err := servico.Iniciar(context.Background(), criado.ID)
		exigirConflito(t, err)

		if repositorio.contarChamadas("Atualizar") != 0 {
			t.Fatal("esperava nenhuma chamada a Atualizar quando a transição é inválida")
		}
	})

	t.Run("conflito do repositório chega ao chamador como ErroConflito", func(t *testing.T) {
		t.Parallel()

		repositorio, servico := servicoDeTeste(t)
		criado := criarJob(t, entity.TipoAnalisar, nil)
		repositorio.guardar(criado)
		repositorio.erroAtualizar = errors.NovoErroConflito("outro worker pegou o job")

		_, err := servico.Iniciar(context.Background(), criado.ID)
		exigirConflito(t, err)
	})
}

func TestServicoReportarProgresso(t *testing.T) {
	t.Parallel()

	t.Run("usa AtualizarProgresso e não Atualizar", func(t *testing.T) {
		t.Parallel()

		repositorio, servico := servicoDeTeste(t)
		criado := criarJob(t, entity.TipoAnalisar, nil)
		if err := criado.Iniciar(); err != nil {
			t.Fatalf("pré-condição: %v", err)
		}
		repositorio.guardar(criado)

		if err := servico.ReportarProgresso(context.Background(), criado.ID, 50); err != nil {
			t.Fatalf("não esperava erro, obteve %v", err)
		}
		if repositorio.contarChamadas("AtualizarProgresso") != 1 {
			t.Fatalf("esperava 1 chamada a AtualizarProgresso, obteve %d",
				repositorio.contarChamadas("AtualizarProgresso"))
		}
		if repositorio.contarChamadas("Atualizar") != 0 {
			t.Fatalf("esperava nenhuma chamada a Atualizar, obteve %d", repositorio.contarChamadas("Atualizar"))
		}

		guardado, _ := repositorio.obter(criado.ID)
		if guardado.Progresso != 50 {
			t.Fatalf("esperava progresso 50 persistido, obteve %d", guardado.Progresso)
		}
	})

	t.Run("job inexistente devolve não encontrado", func(t *testing.T) {
		t.Parallel()

		_, servico := servicoDeTeste(t)

		err := servico.ReportarProgresso(context.Background(), uuid.New(), 10)
		var naoEncontrado *errors.ErroNaoEncontrado
		if !errors.Como(err, &naoEncontrado) {
			t.Fatalf("esperava *ErroNaoEncontrado, obteve %T", err)
		}
	})

	t.Run("progresso fora da faixa é inválido", func(t *testing.T) {
		t.Parallel()

		casos := []struct {
			nome  string
			valor int
		}{
			{nome: "negativo", valor: -1},
			{nome: "acima do máximo", valor: 101},
		}

		for _, caso := range casos {
			t.Run(caso.nome, func(t *testing.T) {
				t.Parallel()

				repositorio, servico := servicoDeTeste(t)
				criado := criarJob(t, entity.TipoAnalisar, nil)
				if err := criado.Iniciar(); err != nil {
					t.Fatalf("pré-condição: %v", err)
				}
				repositorio.guardar(criado)

				err := servico.ReportarProgresso(context.Background(), criado.ID, caso.valor)
				exigirCampoInvalido(t, err, "progresso")

				if repositorio.contarChamadas("AtualizarProgresso") != 0 {
					t.Fatal("esperava nenhuma chamada a AtualizarProgresso com valor inválido")
				}
			})
		}
	})

	t.Run("job pendente devolve conflito", func(t *testing.T) {
		t.Parallel()

		repositorio, servico := servicoDeTeste(t)
		criado := criarJob(t, entity.TipoAnalisar, nil)
		repositorio.guardar(criado)

		exigirConflito(t, servico.ReportarProgresso(context.Background(), criado.ID, 50))
		if repositorio.contarChamadas("AtualizarProgresso") != 0 {
			t.Fatal("esperava nenhuma chamada a AtualizarProgresso para job pendente")
		}
	})
}

func TestServicoConcluir(t *testing.T) {
	t.Parallel()

	t.Run("conclui job em execução", func(t *testing.T) {
		t.Parallel()

		repositorio, servico := servicoDeTeste(t)
		criado := criarJob(t, entity.TipoAnalisar, nil)
		if err := criado.Iniciar(); err != nil {
			t.Fatalf("pré-condição: %v", err)
		}
		repositorio.guardar(criado)

		job, err := servico.Concluir(context.Background(), criado.ID, json.RawMessage(`{"ok":true}`))
		if err != nil {
			t.Fatalf("não esperava erro, obteve %v", err)
		}
		if job.Status != entity.StatusConcluido {
			t.Fatalf("esperava status concluido, obteve %q", job.Status)
		}
		if job.Progresso != entity.ProgressoMaximo {
			t.Fatalf("esperava progresso %d, obteve %d", entity.ProgressoMaximo, job.Progresso)
		}

		anterior, _ := repositorio.ultimoStatusAnterior()
		if anterior != entity.StatusExecutando {
			t.Fatalf("esperava statusAnterior executando, obteve %q", anterior)
		}
	})

	t.Run("job pendente devolve conflito", func(t *testing.T) {
		t.Parallel()

		repositorio, servico := servicoDeTeste(t)
		criado := criarJob(t, entity.TipoAnalisar, nil)
		repositorio.guardar(criado)

		_, err := servico.Concluir(context.Background(), criado.ID, nil)
		exigirConflito(t, err)
		if repositorio.contarChamadas("Atualizar") != 0 {
			t.Fatal("esperava nenhuma chamada a Atualizar")
		}
	})
}

func TestServicoFalhar(t *testing.T) {
	t.Parallel()

	t.Run("marca job em execução como falhou", func(t *testing.T) {
		t.Parallel()

		repositorio, servico := servicoDeTeste(t)
		criado := criarJob(t, entity.TipoAnalisar, nil)
		if err := criado.Iniciar(); err != nil {
			t.Fatalf("pré-condição: %v", err)
		}
		repositorio.guardar(criado)

		job, err := servico.Falhar(context.Background(), criado.ID, "conversor indisponível")
		if err != nil {
			t.Fatalf("não esperava erro, obteve %v", err)
		}
		if job.Status != entity.StatusFalhou {
			t.Fatalf("esperava status falhou, obteve %q", job.Status)
		}
		if job.Erro != "conversor indisponível" {
			t.Fatalf("esperava motivo preservado, obteve %q", job.Erro)
		}

		guardado, _ := repositorio.obter(criado.ID)
		if guardado.Status != entity.StatusFalhou {
			t.Fatalf("esperava status falhou persistido, obteve %q", guardado.Status)
		}
	})

	t.Run("motivo vazio é inválido", func(t *testing.T) {
		t.Parallel()

		repositorio, servico := servicoDeTeste(t)
		criado := criarJob(t, entity.TipoAnalisar, nil)
		if err := criado.Iniciar(); err != nil {
			t.Fatalf("pré-condição: %v", err)
		}
		repositorio.guardar(criado)

		_, err := servico.Falhar(context.Background(), criado.ID, "   ")
		exigirCampoInvalido(t, err, "erro")
		if repositorio.contarChamadas("Atualizar") != 0 {
			t.Fatal("esperava nenhuma chamada a Atualizar")
		}
	})
}

func TestServicoCancelar(t *testing.T) {
	t.Parallel()

	t.Run("cancela job pendente", func(t *testing.T) {
		t.Parallel()

		repositorio, servico := servicoDeTeste(t)
		criado := criarJob(t, entity.TipoAnalisar, nil)
		repositorio.guardar(criado)

		job, err := servico.Cancelar(context.Background(), criado.ID)
		if err != nil {
			t.Fatalf("não esperava erro, obteve %v", err)
		}
		if job.Status != entity.StatusCancelado {
			t.Fatalf("esperava status cancelado, obteve %q", job.Status)
		}
		anterior, _ := repositorio.ultimoStatusAnterior()
		if anterior != entity.StatusPendente {
			t.Fatalf("esperava statusAnterior pendente, obteve %q", anterior)
		}
	})

	t.Run("cancelar job concluído devolve conflito", func(t *testing.T) {
		t.Parallel()

		repositorio, servico := servicoDeTeste(t)
		criado := criarJob(t, entity.TipoAnalisar, nil)
		if err := criado.Iniciar(); err != nil {
			t.Fatalf("pré-condição: %v", err)
		}
		if err := criado.Concluir(nil); err != nil {
			t.Fatalf("pré-condição: %v", err)
		}
		repositorio.guardar(criado)

		_, err := servico.Cancelar(context.Background(), criado.ID)
		exigirConflito(t, err)
	})
}

func TestServicoReenfileirar(t *testing.T) {
	t.Parallel()

	t.Run("job que falhou volta para pendente preservando tentativas", func(t *testing.T) {
		t.Parallel()

		repositorio, servico := servicoDeTeste(t)
		criado := criarJob(t, entity.TipoAnalisar, nil)
		if err := criado.Iniciar(); err != nil {
			t.Fatalf("pré-condição: %v", err)
		}
		if err := criado.Falhar("falha temporária"); err != nil {
			t.Fatalf("pré-condição: %v", err)
		}
		repositorio.guardar(criado)

		job, err := servico.Reenfileirar(context.Background(), criado.ID)
		if err != nil {
			t.Fatalf("não esperava erro, obteve %v", err)
		}
		if job.Status != entity.StatusPendente {
			t.Fatalf("esperava status pendente, obteve %q", job.Status)
		}
		if job.Tentativas != criado.Tentativas {
			t.Fatalf("esperava tentativas preservadas (%d), obteve %d", criado.Tentativas, job.Tentativas)
		}
		if job.Erro != "" {
			t.Fatalf("esperava erro zerado, obteve %q", job.Erro)
		}
		anterior, _ := repositorio.ultimoStatusAnterior()
		if anterior != entity.StatusFalhou {
			t.Fatalf("esperava statusAnterior falhou, obteve %q", anterior)
		}
	})

	t.Run("reenfileirar job pendente devolve conflito", func(t *testing.T) {
		t.Parallel()

		repositorio, servico := servicoDeTeste(t)
		criado := criarJob(t, entity.TipoAnalisar, nil)
		repositorio.guardar(criado)

		_, err := servico.Reenfileirar(context.Background(), criado.ID)
		exigirConflito(t, err)
	})
}

func TestServicoCicloCompleto(t *testing.T) {
	t.Parallel()

	repositorio, servico := servicoDeTeste(t)
	ctx := context.Background()

	criado, err := servico.Criar(ctx, DadosNovoJob{
		DocumentoID: uuid.New(),
		Tipo:        entity.TipoAnalisar,
	})
	if err != nil {
		t.Fatalf("não esperava erro em Criar, obteve %v", err)
	}

	if _, err := servico.Iniciar(ctx, criado.ID); err != nil {
		t.Fatalf("não esperava erro em Iniciar, obteve %v", err)
	}
	if err := servico.ReportarProgresso(ctx, criado.ID, 50); err != nil {
		t.Fatalf("não esperava erro em ReportarProgresso, obteve %v", err)
	}

	intermediario, existe := repositorio.obter(criado.ID)
	if !existe {
		t.Fatal("esperava job no repositório")
	}
	if intermediario.Progresso != 50 {
		t.Fatalf("esperava progresso 50, obteve %d", intermediario.Progresso)
	}
	if intermediario.Status != entity.StatusExecutando {
		t.Fatalf("esperava status executando, obteve %q", intermediario.Status)
	}

	concluido, err := servico.Concluir(ctx, criado.ID, json.RawMessage(`{"blocos":12}`))
	if err != nil {
		t.Fatalf("não esperava erro em Concluir, obteve %v", err)
	}
	if concluido.Status != entity.StatusConcluido {
		t.Fatalf("esperava status concluido, obteve %q", concluido.Status)
	}

	final, _ := repositorio.obter(criado.ID)
	if final.Status != entity.StatusConcluido {
		t.Fatalf("esperava status concluido persistido, obteve %q", final.Status)
	}
	if final.Progresso != entity.ProgressoMaximo {
		t.Fatalf("esperava progresso %d persistido, obteve %d", entity.ProgressoMaximo, final.Progresso)
	}
	if final.Tentativas != 1 {
		t.Fatalf("esperava 1 tentativa, obteve %d", final.Tentativas)
	}
	if _, ok := final.Duracao(); !ok {
		t.Fatal("esperava duração disponível no job concluído")
	}
}

func servicoDeTeste(t *testing.T) (*repositorioFake, *Servico) {
	t.Helper()

	repositorio := novoRepositorioFake()
	servico, err := NovoServico(repositorio)
	if err != nil {
		t.Fatalf("pré-condição: não esperava erro em NovoServico, obteve %v", err)
	}
	return repositorio, servico
}

func criarJob(t *testing.T, tipo entity.TipoJob, rulesetID *uuid.UUID) entity.Job {
	t.Helper()

	if tipo.ExigeRuleset() && rulesetID == nil {
		id := uuid.New()
		rulesetID = &id
	}

	job, err := entity.NovoJob(uuid.New(), tipo, rulesetID)
	if err != nil {
		t.Fatalf("pré-condição: não esperava erro em NovoJob, obteve %v", err)
	}
	return job
}

func exigirConflito(t *testing.T, err error) {
	t.Helper()

	if err == nil {
		t.Fatal("esperava *ErroConflito, obteve nil")
	}
	var conflito *errors.ErroConflito
	if !errors.Como(err, &conflito) {
		t.Fatalf("esperava *ErroConflito, obteve %T", err)
	}
}

func exigirCampoInvalido(t *testing.T, err error, campo string) {
	t.Helper()

	if err == nil {
		t.Fatalf("esperava *ErroValidacao no campo %q, obteve nil", campo)
	}
	var invalido *errors.ErroValidacao
	if !errors.Como(err, &invalido) {
		t.Fatalf("esperava *ErroValidacao, obteve %T", err)
	}
	for _, reprovado := range invalido.Campos {
		if reprovado.Campo == campo {
			return
		}
	}
	t.Fatalf("esperava o campo %q entre %v", campo, invalido.Campos)
}
