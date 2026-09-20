package consulta

import (
	"context"
	"reflect"
	"testing"
	"time"

	documentoentity "github.com/daniel-halos/formatador/internal/domain/documento/entity"
	documentoservice "github.com/daniel-halos/formatador/internal/domain/documento/service"
	"github.com/daniel-halos/formatador/internal/domain/job/entity"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Devolve inclusive dados adversariais: a autorização cabe ao serviço real.
type documentosFake struct {
	t     *testing.T
	obter func(context.Context, vo.Dono, uuid.UUID) (documentoentity.Documento, error)
}

func (r *documentosFake) ObterPorID(ctx context.Context, dono vo.Dono, id uuid.UUID) (documentoentity.Documento, error) {
	return r.obter(ctx, dono, id)
}

func (r *documentosFake) Inserir(context.Context, documentoentity.Documento) error {
	r.t.Fatal("consulta tentou inserir documento")
	return nil
}

func (r *documentosFake) ListarPorDono(context.Context, vo.Dono, int, int) ([]documentoentity.Documento, error) {
	r.t.Fatal("consulta tentou listar documentos")
	return nil, nil
}

func (r *documentosFake) DefinirChavePreviewPDF(context.Context, vo.Dono, uuid.UUID, vo.ChaveStorage) error {
	r.t.Fatal("consulta tentou escrever preview")
	return nil
}

type jobsFake struct {
	obter  func(context.Context, vo.Dono, uuid.UUID) (entity.Job, error)
	listar func(context.Context, vo.Dono, uuid.UUID) ([]entity.Job, error)
}

func (r *jobsFake) ObterPorID(ctx context.Context, dono vo.Dono, id uuid.UUID) (entity.Job, error) {
	return r.obter(ctx, dono, id)
}

func (r *jobsFake) ListarPorDocumento(ctx context.Context, dono vo.Dono, id uuid.UUID) ([]entity.Job, error) {
	return r.listar(ctx, dono, id)
}

func TestNovoServicoRecusaDependenciasAusentes(t *testing.T) {
	for _, caso := range []struct {
		nome             string
		semJobs, semDocs bool
	}{
		{"jobs ausentes", true, false},
		{"documentos ausentes", false, true},
		{"ambos ausentes", true, true},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			documentos, err := documentoservice.NovoServico(&documentosFake{t: t}, 0)
			require.NoError(t, err)
			if caso.semDocs {
				documentos = nil
			}
			var servico *Servico
			if caso.semJobs {
				servico, err = NovoServico(nil, documentos)
			} else {
				servico, err = NovoServico(&jobsFake{}, documentos)
			}
			assert.Nil(t, servico)
			require.Error(t, err)
		})
	}
}

func TestConsultasAutorizadas(t *testing.T) {
	idDono := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	sessao, err := vo.NovoDonoSessao(idDono)
	require.NoError(t, err)
	usuario, err := vo.NovoDonoUsuario(idDono)
	require.NoError(t, err)
	outraSessao, err := vo.NovoDonoSessao(uuid.MustParse("00000000-0000-4000-8000-000000000002"))
	require.NoError(t, err)
	idDocumento := uuid.MustParse("00000000-0000-4000-8000-000000000003")
	idJob := uuid.MustParse("00000000-0000-4000-8000-000000000004")
	outroID := uuid.MustParse("00000000-0000-4000-8000-000000000005")

	type cenario struct {
		dono                    vo.Dono
		id                      uuid.UUID
		documento               documentoentity.Documento
		job                     entity.Job
		lista                   []entity.Job
		erroJobs, erroDocumento error
	}
	type casoConsulta struct {
		nome, somente, falha    string
		alterar                 func(*cenario)
		ordemObter, ordemListar []string
		causa                   error
	}
	casos := make([]casoConsulta, 0, 34)
	casos = append(casos, []casoConsulta{
		{nome: "sessão autorizada"},
		{nome: "usuário autorizado", alterar: func(c *cenario) { c.dono, c.documento.Dono = usuario, usuario }},
		{nome: "lista vazia autorizada", somente: "listar", alterar: func(c *cenario) { c.lista = nil }},
		{nome: "dono vazio sem IO", falha: "validacao", alterar: func(c *cenario) { c.dono = vo.Dono{} }},
		{nome: "ID vazio sem IO", falha: "validacao", alterar: func(c *cenario) { c.id = uuid.Nil }},
		{nome: "sessões diferentes", falha: "ausente", alterar: func(c *cenario) { c.documento.Dono = outraSessao }, ordemObter: []string{"job", "documento"}, ordemListar: []string{"documento"}},
		{nome: "sessão não acessa usuário de mesmo UUID", falha: "ausente", alterar: func(c *cenario) { c.documento.Dono = usuario }, ordemObter: []string{"job", "documento"}, ordemListar: []string{"documento"}},
		{nome: "usuário não acessa sessão de mesmo UUID", falha: "ausente", alterar: func(c *cenario) { c.dono = usuario }, ordemObter: []string{"job", "documento"}, ordemListar: []string{"documento"}},
		{nome: "documento com ID divergente", falha: "ausente", alterar: func(c *cenario) { c.documento.ID = outroID }, ordemObter: []string{"job", "documento"}, ordemListar: []string{"documento"}},
		{nome: "documento sem dono", falha: "ausente", alterar: func(c *cenario) { c.documento.Dono = vo.Dono{} }, ordemObter: []string{"job", "documento"}, ordemListar: []string{"documento"}},
		{nome: "lista vazia exige autorização", somente: "listar", falha: "ausente", alterar: func(c *cenario) { c.lista = nil; c.documento.Dono = outraSessao }, ordemListar: []string{"documento"}},
		{nome: "job com ID divergente", somente: "obter", falha: "ausente", alterar: func(c *cenario) { c.job.ID = outroID }, ordemObter: []string{"job"}},
		{nome: "job com ID zero", somente: "obter", falha: "ausente", alterar: func(c *cenario) { c.job.ID = uuid.Nil }, ordemObter: []string{"job"}},
		{nome: "job sem documento", somente: "obter", falha: "ausente", alterar: func(c *cenario) { c.job.DocumentoID = uuid.Nil }, ordemObter: []string{"job"}},
		{nome: "segundo job pertence a outro documento", somente: "listar", falha: "ausente", alterar: func(c *cenario) { c.lista[1].DocumentoID = outroID }, ordemListar: []string{"documento", "jobs"}},
		{nome: "segundo job sem ID", somente: "listar", falha: "ausente", alterar: func(c *cenario) { c.lista[1].ID = uuid.Nil }, ordemListar: []string{"documento", "jobs"}},
	}...)
	for _, porta := range []string{"jobs", "documento"} {
		for _, falha := range []struct {
			nome, tipo string
			err        error
		}{
			{"ausente direto", "ausente", errors.NovoErroNaoEncontrado("recurso privado")},
			{"ausente envolvido", "ausente", errors.Envolver(errors.NovoErroNaoEncontrado("recurso privado"), "detalhe privado")},
			{"validação", "tipada", errors.NovoErroValidacao("consulta", "inválida")},
			{"conflito", "tipada", errors.NovoErroConflito("conflito")},
			{"não autorizado", "tipada", errors.NovoErroNaoAutorizado("credencial inválida")},
			{"proibido", "tipada", errors.NovoErroProibido("operação proibida")},
			{"aplicação", "tipada", errors.NovoErroAplicacao("indisponível")},
			{"cancelamento", "tipada", context.Canceled},
			{"prazo excedido", "tipada", context.DeadlineExceeded},
		} {
			caso := casoConsulta{nome: porta + "/" + falha.nome, falha: falha.tipo, causa: falha.err}
			if porta == "jobs" {
				caso.alterar = func(c *cenario) { c.erroJobs = errors.Envolver(falha.err) }
				caso.ordemObter, caso.ordemListar = []string{"job"}, []string{"documento", "jobs"}
			} else {
				caso.alterar = func(c *cenario) { c.erroDocumento = errors.Envolver(falha.err) }
				caso.ordemObter, caso.ordemListar = []string{"job", "documento"}, []string{"documento"}
			}
			// Ausência direta também atravessa a porta sem envelope.
			if falha.nome == "ausente direto" {
				caso.alterar = func(c *cenario) {
					if porta == "jobs" {
						c.erroJobs = falha.err
					} else {
						c.erroDocumento = falha.err
					}
				}
			}
			casos = append(casos, caso)
		}
	}

	for _, operacao := range []string{"obter", "listar"} {
		for _, caso := range casos {
			if caso.somente != "" && caso.somente != operacao {
				continue
			}
			t.Run(operacao+"/"+caso.nome, func(t *testing.T) {
				job := entity.Job{ID: idJob, DocumentoID: idDocumento, Tipo: entity.TipoAnalisar, Status: entity.StatusPendente, CriadoEm: time.Unix(100, 0).UTC()}
				segundo := job
				segundo.ID, segundo.CriadoEm = outroID, time.Unix(200, 0).UTC()
				c := cenario{dono: sessao, id: idJob, documento: documentoentity.Documento{ID: idDocumento, Dono: sessao}, job: job, lista: []entity.Job{segundo, job}}
				if operacao == "listar" {
					c.id = idDocumento
				}
				if caso.alterar != nil {
					caso.alterar(&c)
				}
				ctx, cancelar := context.WithCancel(context.Background())
				defer cancelar()
				var ordem []string
				registrar := func(nome string, recebido context.Context, dono vo.Dono, id, esperado uuid.UUID) {
					ordem = append(ordem, nome)
					assert.True(t, recebido == ctx, "contexto não propagado")
					assert.True(t, dono.Igual(c.dono), "dono não propagado")
					assert.Equal(t, esperado, id, "ID não propagado")
				}
				documentos, err := documentoservice.NovoServico(&documentosFake{t: t, obter: func(recebido context.Context, dono vo.Dono, id uuid.UUID) (documentoentity.Documento, error) {
					registrar("documento", recebido, dono, id, idDocumento)
					return c.documento, c.erroDocumento
				}}, 0)
				require.NoError(t, err)
				jobs := &jobsFake{
					obter: func(recebido context.Context, dono vo.Dono, id uuid.UUID) (entity.Job, error) {
						require.Equal(t, "obter", operacao)
						registrar("job", recebido, dono, id, c.id)
						return c.job, c.erroJobs
					},
					listar: func(recebido context.Context, dono vo.Dono, id uuid.UUID) ([]entity.Job, error) {
						require.Equal(t, "listar", operacao)
						registrar("jobs", recebido, dono, id, c.id)
						return c.lista, c.erroJobs
					},
				}
				servico, err := NovoServico(jobs, documentos)
				require.NoError(t, err)
				require.NotNil(t, servico)
				var obtido entity.Job
				var lista []entity.Job
				esperada := caso.ordemObter
				if operacao == "obter" {
					obtido, err = servico.Obter(ctx, c.dono, c.id)
				} else {
					lista, err = servico.ListarDoDocumento(ctx, c.dono, c.id)
					esperada = caso.ordemListar
				}
				if caso.falha == "" {
					require.NoError(t, err)
					if operacao == "obter" {
						assert.Equal(t, c.job, obtido)
						esperada = []string{"job", "documento"}
					} else {
						if len(c.lista) == 0 {
							assert.Empty(t, lista)
						} else {
							assert.Equal(t, c.lista, lista, "preservar ordem da porta")
						}
						esperada = []string{"documento", "jobs"}
					}
				} else {
					require.Error(t, err)
					assert.Equal(t, entity.Job{}, obtido, "recusa não pode retornar job")
					assert.Empty(t, lista, "recusa não pode retornar lista parcial")
					switch caso.falha {
					case "validacao":
						var invalido *errors.ErroValidacao
						assert.ErrorAs(t, err, &invalido)
					case "ausente":
						var ausente *errors.ErroNaoEncontrado
						require.ErrorAs(t, err, &ausente)
						assert.Equal(t, "job", ausente.Recurso)
						assert.Equal(t, errors.NovoErroNaoEncontrado("job").Error(), err.Error())
					case "tipada":
						assert.ErrorIs(t, err, caso.causa)
						assert.True(t, errors.Como(err, reflect.New(reflect.TypeOf(caso.causa)).Interface()), "tipo perdido")
					}
				}
				assert.Equal(t, esperada, ordem, "ordem e número de consultas")
			})
		}
	}
}
