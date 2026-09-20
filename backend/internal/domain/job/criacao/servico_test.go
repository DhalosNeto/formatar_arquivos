package criacao

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	documentoentity "github.com/daniel-halos/formatador/internal/domain/documento/entity"
	documentoservice "github.com/daniel-halos/formatador/internal/domain/documento/service"
	"github.com/daniel-halos/formatador/internal/domain/job/entity"
	"github.com/daniel-halos/formatador/internal/domain/job/repository"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

type documentosFake struct {
	t     *testing.T
	obter func(context.Context, vo.Dono, uuid.UUID) (documentoentity.Documento, error)
}

func (r *documentosFake) ObterPorID(ctx context.Context, dono vo.Dono, id uuid.UUID) (documentoentity.Documento, error) {
	return r.obter(ctx, dono, id)
}

func (r *documentosFake) Inserir(context.Context, documentoentity.Documento) error {
	r.t.Fatal("criação de job tentou inserir documento")
	return nil
}

func (r *documentosFake) ListarPorDono(context.Context, vo.Dono, int, int) ([]documentoentity.Documento, error) {
	r.t.Fatal("criação de job tentou listar documentos")
	return nil, nil
}

func (r *documentosFake) DefinirChavePreviewPDF(context.Context, vo.Dono, uuid.UUID, vo.ChaveStorage) error {
	r.t.Fatal("criação de job tentou escrever preview")
	return nil
}

type jobsFake func(context.Context, vo.Dono, entity.Job, uuid.UUID) (entity.Job, error)

func (f jobsFake) InserirOuObter(ctx context.Context, dono vo.Dono, job entity.Job, chave uuid.UUID) (entity.Job, error) {
	return f(ctx, dono, job, chave)
}

var _ repository.CriacaoJobRepo = jobsFake(nil)

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
			var jobs repository.CriacaoJobRepo = jobsFake(func(context.Context, vo.Dono, entity.Job, uuid.UUID) (entity.Job, error) {
				t.Fatal("construtor fez IO")
				return entity.Job{}, nil
			})
			if caso.semJobs {
				jobs = nil
			}
			if caso.semDocs {
				documentos = nil
			}
			servico, err := NovoServico(jobs, documentos)
			assert.Nil(t, servico)
			var ausente *errors.ErroArgumentoNulo
			assert.ErrorAs(t, err, &ausente)
		})
	}
}

func TestCriarContrato(t *testing.T) {
	idDono := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	sessao, err := vo.NovoDonoSessao(idDono)
	require.NoError(t, err)
	usuario, err := vo.NovoDonoUsuario(idDono)
	require.NoError(t, err)
	idDocumento := uuid.MustParse("00000000-0000-4000-8000-000000000002")
	idJob := uuid.MustParse("00000000-0000-4000-8000-000000000003")
	idRuleset := uuid.MustParse("00000000-0000-4000-8000-000000000004")
	chave := uuid.MustParse("00000000-0000-4000-8000-000000000005")
	outroID := uuid.MustParse("00000000-0000-4000-8000-000000000006")
	terceiro, err := vo.NovoDonoSessao(outroID)
	require.NoError(t, err)
	zero := uuid.Nil
	type cenario struct {
		dono                    vo.Dono
		dados                   DadosNovoJob
		documento               documentoentity.Documento
		retorno                 entity.Job
		erroDocumento, erroJobs error
	}
	type casoCriacao struct {
		nome, falha string
		chamadas    int
		alterar     func(*cenario)
		causa       error
	}
	casos := make([]casoCriacao, 0, 44)
	casos = append(casos, []casoCriacao{
		{nome: "formatar com UUID igual em ponteiros distintos", chamadas: 2},
		{nome: "usuário autorizado", chamadas: 2, alterar: func(c *cenario) { c.dono, c.documento.Dono = usuario, usuario }},
		{nome: "analisar com nil igual a nil", chamadas: 2, alterar: func(c *cenario) {
			c.dados.Tipo, c.retorno.Tipo = entity.TipoAnalisar, entity.TipoAnalisar
			c.dados.RulesetID, c.retorno.RulesetID = nil, nil
		}},
		{nome: "preview com nil igual a nil", chamadas: 2, alterar: func(c *cenario) {
			c.dados.Tipo, c.retorno.Tipo = entity.TipoRenderizarPreview, entity.TipoRenderizarPreview
			c.dados.RulesetID, c.retorno.RulesetID = nil, nil
		}},
		{nome: "dono vazio", falha: "validacao", alterar: func(c *cenario) { c.dono = vo.Dono{} }},
		{nome: "chave zero", falha: "validacao", alterar: func(c *cenario) { c.dados.ChaveIdempotencia = uuid.Nil }},
		{nome: "documento zero", falha: "validacao", alterar: func(c *cenario) { c.dados.DocumentoID = uuid.Nil }},
		{nome: "tipo vazio", falha: "validacao", alterar: func(c *cenario) { c.dados.Tipo = "" }},
		{nome: "tipo desconhecido", falha: "validacao", alterar: func(c *cenario) { c.dados.Tipo = "payload-privado" }},
		{nome: "formatar sem ruleset", falha: "validacao", alterar: func(c *cenario) { c.dados.RulesetID = nil }},
		{nome: "formatar com ruleset zero", falha: "validacao", alterar: func(c *cenario) { c.dados.RulesetID = &zero }},
		{nome: "analisar com ruleset", falha: "validacao", alterar: func(c *cenario) { c.dados.Tipo = entity.TipoAnalisar }},
		{nome: "preview com ruleset", falha: "validacao", alterar: func(c *cenario) { c.dados.Tipo = entity.TipoRenderizarPreview }},
		{nome: "terceiro", falha: "ausente", chamadas: 1, alterar: func(c *cenario) { c.documento.Dono = terceiro }},
		{nome: "sessão não acessa usuário de mesmo UUID", falha: "ausente", chamadas: 1, alterar: func(c *cenario) { c.documento.Dono = usuario }},
		{nome: "usuário não acessa sessão de mesmo UUID", falha: "ausente", chamadas: 1, alterar: func(c *cenario) { c.dono = usuario }},
		{nome: "documento sem dono", falha: "ausente", chamadas: 1, alterar: func(c *cenario) { c.documento.Dono = vo.Dono{} }},
		{nome: "documento divergente", falha: "ausente", chamadas: 1, alterar: func(c *cenario) { c.documento.ID = outroID }},
		{nome: "documento zero na resposta", falha: "ausente", chamadas: 1, alterar: func(c *cenario) { c.documento.ID = uuid.Nil }},
		{nome: "job sem ID", falha: "ausente", chamadas: 2, alterar: func(c *cenario) { c.retorno.ID = uuid.Nil }},
		{nome: "job de outro documento", falha: "ausente", chamadas: 2, alterar: func(c *cenario) { c.retorno.DocumentoID = outroID }},
		{nome: "job sem documento", falha: "ausente", chamadas: 2, alterar: func(c *cenario) { c.retorno.DocumentoID = uuid.Nil }},
		{nome: "somente tipo divergente", falha: "conflito", chamadas: 2, alterar: func(c *cenario) { c.retorno.Tipo = "payload-privado" }},
		{nome: "somente valor do ruleset divergente", falha: "conflito", chamadas: 2, alterar: func(c *cenario) { c.retorno.RulesetID = &outroID }},
		{nome: "ruleset solicitado e retorno nil", falha: "conflito", chamadas: 2, alterar: func(c *cenario) { c.retorno.RulesetID = nil }},
		{nome: "ruleset nil e retorno preenchido", falha: "conflito", chamadas: 2, alterar: func(c *cenario) {
			c.dados.Tipo, c.retorno.Tipo = entity.TipoAnalisar, entity.TipoAnalisar
			c.dados.RulesetID = nil
		}},
	}...)
	for _, porta := range []string{"documento", "jobs"} {
		for _, falha := range []struct {
			nome, tipo string
			err        error
		}{
			{"ausente direto", "ausente", errors.NovoErroNaoEncontrado("privado")},
			{"ausente envolvido", "ausente", errors.Envolver(errors.NovoErroNaoEncontrado("privado"), "detalhe privado")},
			{"validação", "tipada", errors.NovoErroValidacao("campo", "inválido")},
			{"conflito", "tipada", errors.NovoErroConflito("conflito")},
			{"não autorizado", "tipada", errors.NovoErroNaoAutorizado("não autorizado")},
			{"proibido", "tipada", errors.NovoErroProibido("proibido")},
			{"aplicação", "tipada", errors.NovoErroAplicacao("indisponível")},
			{"cancelamento", "tipada", context.Canceled},
			{"prazo excedido", "tipada", context.DeadlineExceeded},
		} {
			chamadas := 1
			if porta == "jobs" {
				chamadas = 2
			}
			casos = append(casos, casoCriacao{nome: porta + "/" + falha.nome, falha: falha.tipo, chamadas: chamadas, causa: falha.err, alterar: func(c *cenario) {
				if porta == "jobs" {
					c.erroJobs = falha.err
				} else {
					c.erroDocumento = falha.err
				}
			}})
		}
	}
	var mensagemConflito string
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			entradaRuleset, retornoRuleset := idRuleset, idRuleset
			require.NotSame(t, &entradaRuleset, &retornoRuleset)
			c := cenario{
				dono: sessao, dados: DadosNovoJob{DocumentoID: idDocumento, Tipo: entity.TipoFormatar, RulesetID: &entradaRuleset, ChaveIdempotencia: chave},
				documento: documentoentity.Documento{ID: idDocumento, Dono: sessao},
				retorno:   entity.Job{ID: idJob, DocumentoID: idDocumento, Tipo: entity.TipoFormatar, RulesetID: &retornoRuleset, Status: entity.StatusPendente, CriadoEm: time.Unix(100, 0).UTC()},
			}
			if caso.alterar != nil {
				caso.alterar(&c)
			}
			ctx, cancelar := context.WithCancel(context.Background())
			defer cancelar()
			var ordem []string
			documentos, err := documentoservice.NovoServico(&documentosFake{t: t, obter: func(recebido context.Context, dono vo.Dono, id uuid.UUID) (documentoentity.Documento, error) {
				ordem = append(ordem, "documento")
				assert.Same(t, ctx, recebido)
				assert.Equal(t, c.dono, dono)
				assert.Equal(t, c.dados.DocumentoID, id)
				return c.documento, c.erroDocumento
			}}, 0)
			require.NoError(t, err)
			servico, err := NovoServico(jobsFake(func(recebido context.Context, dono vo.Dono, candidato entity.Job, chaveRecebida uuid.UUID) (entity.Job, error) {
				require.Equal(t, []string{"documento"}, ordem, "autorizar antes da única escrita")
				ordem = append(ordem, "job")
				assert.Same(t, ctx, recebido)
				assert.Equal(t, c.dono, dono)
				assert.Equal(t, c.dados.ChaveIdempotencia, chaveRecebida)
				assert.NotEqual(t, uuid.Nil, candidato.ID)
				assert.False(t, candidato.CriadoEm.IsZero())
				assert.Equal(t, entity.Job{ID: candidato.ID, DocumentoID: c.dados.DocumentoID, Tipo: c.dados.Tipo, RulesetID: c.dados.RulesetID, Status: entity.StatusPendente, CriadoEm: candidato.CriadoEm}, candidato)
				return c.retorno, c.erroJobs
			}), documentos)
			require.NoError(t, err)
			obtido, err := servico.Criar(ctx, c.dono, c.dados)
			assert.Len(t, ordem, caso.chamadas)
			if caso.falha == "" {
				require.NoError(t, err)
				assert.Equal(t, c.retorno, obtido)
				return
			}
			require.Error(t, err)
			assert.Equal(t, entity.Job{}, obtido)
			switch caso.falha {
			case "validacao":
				var invalido *errors.ErroValidacao
				assert.ErrorAs(t, err, &invalido)
			case "ausente":
				assert.IsType(t, &errors.ErroNaoEncontrado{}, err, "ausência sem wrapper")
				assert.EqualError(t, err, "job não encontrado")
			case "conflito":
				var conflito *errors.ErroConflito
				require.ErrorAs(t, err, &conflito)
				assert.NotEmpty(t, conflito.Mensagem)
				if mensagemConflito == "" {
					mensagemConflito = conflito.Mensagem
				}
				assert.Equal(t, mensagemConflito, conflito.Mensagem, "conflito fixo para qualquer divergência")
			case "tipada":
				assert.IsType(t, &errors.ErroEnvolvido{}, err)
				assert.ErrorIs(t, err, caso.causa)
				assert.True(t, errors.Como(err, reflect.New(reflect.TypeOf(caso.causa)).Interface()))
			}
			for _, privado := range []string{"payload-privado", idDono.String(), idDocumento.String(), idRuleset.String(), outroID.String(), chave.String()} {
				assert.NotContains(t, err.Error(), privado)
			}
		})
	}
}

func TestCriarPreservaRepeticaoTerminalENovaChave(t *testing.T) {
	// A porta é roteirizada: prova delegação e preservação, não atomicidade SQL.
	for _, status := range []entity.StatusJob{entity.StatusConcluido, entity.StatusFalhou, entity.StatusCancelado} {
		t.Run(string(status), func(t *testing.T) {
			dono, err := vo.NovoDonoSessao(uuid.MustParse("00000000-0000-4000-8000-000000000001"))
			require.NoError(t, err)
			idDocumento := uuid.MustParse("00000000-0000-4000-8000-000000000002")
			chave := uuid.MustParse("00000000-0000-4000-8000-000000000003")
			novaChave := uuid.MustParse("00000000-0000-4000-8000-000000000004")
			inicio, fim := time.Unix(100, 0).UTC(), time.Unix(200, 0).UTC()
			terminal := entity.Job{ID: chave, DocumentoID: idDocumento, Tipo: entity.TipoAnalisar, Status: status, Tentativas: 3, Progresso: 100, Erro: "falha fixa", Resultado: []byte(`{"blocos":2}`), CriadoEm: inicio, IniciadoEm: &inicio, FinalizadoEm: &fim}
			novo := entity.Job{ID: novaChave, DocumentoID: idDocumento, Tipo: entity.TipoAnalisar, Status: entity.StatusPendente, CriadoEm: fim}
			ctx, cancelar := context.WithCancel(context.Background())
			defer cancelar()
			var ordem []string
			documentos, err := documentoservice.NovoServico(&documentosFake{t: t, obter: func(recebido context.Context, solicitante vo.Dono, id uuid.UUID) (documentoentity.Documento, error) {
				assert.Same(t, ctx, recebido)
				assert.Equal(t, dono, solicitante)
				assert.Equal(t, idDocumento, id)
				ordem = append(ordem, "documento")
				return documentoentity.Documento{ID: id, Dono: dono}, nil
			}}, 0)
			require.NoError(t, err)
			var chaveEsperada uuid.UUID
			servico, err := NovoServico(jobsFake(func(recebido context.Context, solicitante vo.Dono, candidato entity.Job, recebida uuid.UUID) (entity.Job, error) {
				assert.Same(t, ctx, recebido)
				assert.Equal(t, dono, solicitante)
				assert.Equal(t, chaveEsperada, recebida)
				assert.Equal(t, idDocumento, candidato.DocumentoID)
				require.Equal(t, []string{"documento"}, ordem)
				ordem = append(ordem, "job")
				if recebida == novaChave {
					return novo, nil
				}
				return terminal, nil
			}), documentos)
			require.NoError(t, err)
			for _, chamada := range []struct {
				nome     string
				chave    uuid.UUID
				esperado entity.Job
			}{{"primeira repetição", chave, terminal}, {"segunda repetição", chave, terminal}, {"nova chave", novaChave, novo}} {
				t.Run(chamada.nome, func(t *testing.T) {
					ordem = nil
					chaveEsperada = chamada.chave
					obtido, err := servico.Criar(ctx, dono, DadosNovoJob{DocumentoID: idDocumento, Tipo: entity.TipoAnalisar, ChaveIdempotencia: chamada.chave})
					require.NoError(t, err)
					assert.Equal(t, chamada.esperado, obtido, "preservar ID, estado, tentativas, resultado e datas da porta")
					assert.Equal(t, []string{"documento", "job"}, ordem)
				})
			}
		})
	}
}
