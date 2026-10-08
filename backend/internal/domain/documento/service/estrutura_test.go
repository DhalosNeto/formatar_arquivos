package service_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/daniel-halos/formatador/internal/domain/cdm"
	"github.com/daniel-halos/formatador/internal/domain/documento/entity"
	"github.com/daniel-halos/formatador/internal/domain/documento/service"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type estruturaFake struct {
	documento           entity.Documento
	lerErro, salvarErro error
	leituras, escritas  int
	anterior, novo      json.RawMessage
	ctx                 context.Context
	dono                vo.Dono
	id                  uuid.UUID
	status              entity.Status
}

func (r *estruturaFake) ObterPorID(ctx context.Context, dono vo.Dono, id uuid.UUID) (entity.Documento, error) {
	r.leituras++
	r.ctx = ctx
	r.dono = dono
	r.id = id
	return r.documento, r.lerErro
}
func (r *estruturaFake) SalvarEstrutura(ctx context.Context, dono vo.Dono, id uuid.UUID, anterior, novo json.RawMessage, status entity.Status) error {
	r.escritas++
	r.ctx = ctx
	r.dono = dono
	r.id = id
	r.anterior = anterior
	r.novo = novo
	r.status = status
	return r.salvarErro
}
func TestEstruturaCorrigir(t *testing.T) {
	dono, err := vo.NovoDonoSessao(uuid.New())
	require.NoError(t, err)
	outro, err := vo.NovoDonoUsuario(func() uuid.UUID { _, id := dono.ParaColunas(); return *id }())
	require.NoError(t, err)
	blocos := []cdm.Bloco{{Papel: cdm.Paragrafo, TextoResumo: "texto preservado", Confianca: .4, Origem: cdm.OrigemHeuristica, RefXML: 7}, {Papel: cdm.Titulo, TextoResumo: "outro", Confianca: .8, Origem: cdm.OrigemUsuario, RefXML: 2}}
	bruto, err := cdm.NovoIndice(blocos).Serializar()
	require.NoError(t, err)
	duplicado, err := cdm.NovoIndice(append(append([]cdm.Bloco{}, blocos...), blocos[1])).Serializar()
	require.NoError(t, err)
	id := uuid.New()
	for _, caso := range []struct {
		nome       string
		alterar    func(*estruturaFake)
		dono       vo.Dono
		id         uuid.UUID
		ref        int
		papel      cdm.Papel
		classe     string
		semLeitura bool
	}{
		{nome: "corrige apenas alvo", dono: dono, id: id, ref: 7, papel: cdm.Secao(2)},
		{nome: "dono vazio", id: id, ref: 7, papel: cdm.Titulo, classe: "validacao", semLeitura: true},
		{nome: "id vazio", dono: dono, ref: 7, papel: cdm.Titulo, classe: "validacao", semLeitura: true},
		{nome: "ref negativa", dono: dono, id: id, ref: -1, papel: cdm.Titulo, classe: "validacao", semLeitura: true},
		{nome: "papel invalido", dono: dono, id: id, ref: 7, classe: "validacao", semLeitura: true},
		{nome: "ref ausente", dono: dono, id: id, ref: 99, papel: cdm.Titulo, classe: "validacao"},
		{nome: "id retornado divergente", dono: dono, id: id, ref: 7, papel: cdm.Titulo, classe: "ausente", alterar: func(r *estruturaFake) { r.documento.ID = uuid.New() }},
		{nome: "especie diferente mesmo UUID", dono: dono, id: id, ref: 7, papel: cdm.Titulo, classe: "ausente", alterar: func(r *estruturaFake) { r.documento.Dono = outro }},
		{nome: "cdm ausente", dono: dono, id: id, ref: 7, papel: cdm.Titulo, classe: "conflito", alterar: func(r *estruturaFake) { r.documento.CDM = nil }},
		{nome: "cdm corrompido", dono: dono, id: id, ref: 7, papel: cdm.Titulo, classe: "aplicacao", alterar: func(r *estruturaFake) { r.documento.CDM = json.RawMessage(`{"versao":99,"segredo":"privado"}`) }},
		{nome: "duplicado fora do alvo", dono: dono, id: id, ref: 7, papel: cdm.Titulo, classe: "aplicacao", alterar: func(r *estruturaFake) { r.documento.CDM = duplicado }},
		{nome: "erro leitura", dono: dono, id: id, ref: 7, papel: cdm.Titulo, classe: "aplicacao", alterar: func(r *estruturaFake) { r.lerErro = errors.NovoErroAplicacao("falha leitura") }},
		{nome: "erro CAS", dono: dono, id: id, ref: 7, papel: cdm.Titulo, classe: "conflito", alterar: func(r *estruturaFake) { r.salvarErro = errors.NovoErroConflito("concorrencia") }},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			r := &estruturaFake{documento: entity.Documento{ID: id, Dono: dono, Status: entity.StatusAnalisado, CDM: bruto}}
			if caso.alterar != nil {
				caso.alterar(r)
			}
			s, err := service.NovoServicoEstrutura(r)
			require.NoError(t, err)
			ctx, cancelar := context.WithCancel(context.Background())
			defer cancelar()
			indice, err := s.Corrigir(ctx, caso.dono, caso.id, caso.ref, caso.papel)
			switch caso.classe {
			case "validacao":
				var alvo *errors.ErroValidacao
				require.ErrorAs(t, err, &alvo)
			case "ausente":
				var alvo *errors.ErroNaoEncontrado
				require.ErrorAs(t, err, &alvo)
			case "conflito":
				var alvo *errors.ErroConflito
				require.ErrorAs(t, err, &alvo)
			case "aplicacao":
				var alvo *errors.ErroAplicacao
				require.ErrorAs(t, err, &alvo)
				var validacao *errors.ErroValidacao
				require.False(t, errors.Como(err, &validacao))
				require.NotContains(t, err.Error(), "privado")
			default:
				require.NoError(t, err)
				require.Equal(t, blocos[1], indice.Blocos[1])
				esperado := blocos[0]
				esperado.Papel = caso.papel
				esperado.Confianca = 1
				esperado.Origem = cdm.OrigemUsuario
				require.Equal(t, esperado, indice.Blocos[0])
				require.Equal(t, bruto, []byte(r.anterior))
				salvo, e := cdm.Desserializar(r.novo)
				require.NoError(t, e)
				require.Equal(t, indice, salvo)
				require.Equal(t, entity.StatusAnalisado, r.status)
				require.Equal(t, 1, r.escritas)
			}
			if caso.semLeitura {
				require.Zero(t, r.leituras)
			} else {
				require.Same(t, ctx, r.ctx)
				require.Equal(t, dono, r.dono)
				require.Equal(t, id, r.id)
			}
			if caso.classe != "" && r.salvarErro == nil {
				require.Zero(t, r.escritas)
			}
		})
	}
	for _, status := range []entity.Status{entity.StatusRecebido, entity.StatusAnalisando, entity.StatusFormatando, entity.StatusFormatado, entity.StatusFalhou, "invalido"} {
		t.Run(string(status), func(t *testing.T) {
			r := &estruturaFake{documento: entity.Documento{ID: id, Dono: dono, Status: status, CDM: bruto}}
			s, err := service.NovoServicoEstrutura(r)
			require.NoError(t, err)
			_, err = s.Corrigir(context.Background(), dono, id, 7, cdm.Titulo)
			var conflito *errors.ErroConflito
			require.ErrorAs(t, err, &conflito)
			require.Zero(t, r.escritas)
		})
	}
	_, err = service.NovoServicoEstrutura(nil)
	require.Error(t, err)
}

func TestCorrigirRemoveSomenteRevisaoDoAlvo(t *testing.T) {
	dono, err := vo.NovoDonoSessao(uuid.New())
	require.NoError(t, err)
	blocos := []cdm.Bloco{{Papel: cdm.Paragrafo, TextoResumo: "alvo", Confianca: .4, Origem: cdm.OrigemHeuristica, RefXML: 7}, {Papel: cdm.Paragrafo, TextoResumo: "outro", Confianca: .4, Origem: cdm.OrigemHeuristica, RefXML: 9}}
	indice := cdm.NovoIndice(blocos)
	papel := cdm.Resumo
	indice.Revisoes = []cdm.RevisaoEstrutura{{RefXML: 7, Confianca: .4, Acao: "revisar"}, {RefXML: 9, PapelSugerido: &papel, Confianca: .6, Acao: "confirmar"}}
	bruto, err := indice.Serializar()
	require.NoError(t, err)
	doc := entity.Documento{ID: uuid.New(), Dono: dono, Status: entity.StatusAnalisado, CDM: bruto}
	repo := &estruturaFake{documento: doc}
	servico, err := service.NovoServicoEstrutura(repo)
	require.NoError(t, err)
	resultado, err := servico.Corrigir(context.Background(), dono, doc.ID, 7, cdm.Titulo)
	require.NoError(t, err)
	require.Len(t, resultado.Revisoes, 1)
	require.Equal(t, 9, resultado.Revisoes[0].RefXML)
}
