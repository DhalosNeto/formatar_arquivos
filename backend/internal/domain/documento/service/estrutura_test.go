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

// ---------------------------------------------------------------------------
// RECORTE f3-mutadores-por-papel — C5, critérios A15, A16 e A17
//
// BUG VIVO no código empurrado: Corrigir chama Reclassificar sem restrição de
// papel, então uma requisição do dono pode marcar o ordinal de um w:tbl como
// "paragrafo"; o bloco entra em ReferenciasCorpo e o primeiro mutador recusa
// o alvo que não é w:p, FALHANDO o job — e falha de worker trava
// permanentemente o par (documento, perfil). Este recorte ampliaria a
// exposição, levando o mesmo caminho aos quatro papéis de exceção da §5.2.
//
// Conserto: recusar transição PARA Tabela e transição DE Tabela, com
// *ErroValidacao de campo "papel". A premissa só é verdadeira porque C6 veda
// as fontes não estruturais de cdm.Tabela: com Tabela <=> w:tbl, correção
// humana de ou para Tabela nunca é legítima.
//
// Onde cada termo mora é observável pelas LEITURAS do repositório: PARA Tabela
// recusa junto de papel.Valido(), ANTES de qualquer I/O; DE Tabela exige o
// bloco alvo e recusa DEPOIS de localizá-lo.
// ---------------------------------------------------------------------------

// indiceSerializadoDeTeste monta o CDM persistido a partir dos blocos.
func indiceSerializadoDeTeste(t *testing.T, blocos []cdm.Bloco) json.RawMessage {
	t.Helper()
	bruto, err := cdm.NovoIndice(blocos).Serializar()
	require.NoError(t, err)
	return bruto
}

func exigirCampoPapelRecusado(t *testing.T, err error) {
	t.Helper()
	var alvo *errors.ErroValidacao
	require.ErrorAs(t, err, &alvo, "C5: a recusa é 400, não conflito nem erro de aplicação")
	require.NotEmpty(t, alvo.Campos)
	require.Equal(t, "papel", alvo.Campos[0].Campo)
}

func TestCorrigirRecusaTransicaoParaTabela(t *testing.T) {
	dono, err := vo.NovoDonoSessao(uuid.New())
	require.NoError(t, err)
	require.True(t, cdm.Tabela.Valido(),
		"pré-condição de A15: cdm.Tabela é um papel VÁLIDO; se não fosse, o caso morreria na guarda antiga de papel.Valido() e não provaria o termo novo")

	bruto := indiceSerializadoDeTeste(t, []cdm.Bloco{
		{Papel: cdm.Paragrafo, TextoResumo: "parágrafo do autor", Confianca: .2, Origem: cdm.OrigemEstiloDocx, RefXML: 4},
	})
	id := uuid.New()
	repo := &estruturaFake{documento: entity.Documento{ID: id, Dono: dono, Status: entity.StatusAnalisado, CDM: bruto}}
	servico, err := service.NovoServicoEstrutura(repo)
	require.NoError(t, err)

	indice, err := servico.Corrigir(context.Background(), dono, id, 4, cdm.Tabela)

	exigirCampoPapelRecusado(t, err)
	require.Equal(t, cdm.Indice{}, indice, "A15: o índice não é devolvido nem gravado")
	require.Zero(t, repo.escritas, "A15: o repositório não recebe nenhuma chamada de escrita")
	require.Zero(t, repo.leituras,
		"A15: o TERMO 1 fica junto de papel.Valido(), ANTES de qualquer I/O")
}

func TestCorrigirRecusaTransicaoDeTabela(t *testing.T) {
	dono, err := vo.NovoDonoSessao(uuid.New())
	require.NoError(t, err)

	// Bloco Tabela produzido pela FORMA do nó w:tbl, que é a única fonte de
	// cdm.Tabela depois de C6.
	bruto := indiceSerializadoDeTeste(t, []cdm.Bloco{
		{Papel: cdm.Tabela, TextoResumo: "conteúdo da tabela", Confianca: .95, Origem: cdm.OrigemEstiloDocx, RefXML: 4},
	})
	id := uuid.New()

	for _, papel := range []cdm.Papel{cdm.Paragrafo, cdm.Citacao} {
		t.Run("para "+papel.String(), func(t *testing.T) {
			repo := &estruturaFake{documento: entity.Documento{ID: id, Dono: dono, Status: entity.StatusAnalisado, CDM: bruto}}
			servico, err := service.NovoServicoEstrutura(repo)
			require.NoError(t, err)

			indice, err := servico.Corrigir(context.Background(), dono, id, 4, papel)

			exigirCampoPapelRecusado(t, err)
			require.Equal(t, cdm.Indice{}, indice)
			require.Zero(t, repo.escritas,
				"A16: é exatamente esta sequência que hoje brica o par (documento, perfil); falsifica guardar só a transição PARA Tabela")
			require.Equal(t, 1, repo.leituras,
				"A16: o TERMO 2 exige o bloco alvo, logo recusa DEPOIS de localizá-lo")
		})
	}
}

func TestCorrigirSegueAceitandoTransicaoLegitimaEntreNaoTabelas(t *testing.T) {
	dono, err := vo.NovoDonoSessao(uuid.New())
	require.NoError(t, err)

	blocos := []cdm.Bloco{
		{Papel: cdm.Paragrafo, TextoResumo: "parágrafo que é referência", Confianca: .2, Origem: cdm.OrigemEstiloDocx, RefXML: 4},
		{Papel: cdm.Citacao, TextoResumo: "citação detectada pelo recuo", Confianca: .5, Origem: cdm.OrigemHeuristica, RefXML: 6},
	}
	bruto := indiceSerializadoDeTeste(t, blocos)
	id := uuid.New()

	casos := []struct {
		nome     string
		ref      int
		posicao  int
		papel    cdm.Papel
		anterior cdm.Bloco
	}{
		{"parágrafo vira referência", 4, 0, cdm.Referencia, blocos[0]},
		{"citação vira parágrafo", 6, 1, cdm.Paragrafo, blocos[1]},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			repo := &estruturaFake{documento: entity.Documento{ID: id, Dono: dono, Status: entity.StatusAnalisado, CDM: bruto}}
			servico, err := service.NovoServicoEstrutura(repo)
			require.NoError(t, err)

			indice, err := servico.Corrigir(context.Background(), dono, id, caso.ref, caso.papel)
			require.NoError(t, err, "A17: a guarda de C5 é ESTREITA e não recusa reclassificação legítima")

			esperado := caso.anterior
			esperado.Papel = caso.papel
			esperado.Confianca = 1
			esperado.Origem = cdm.OrigemUsuario
			require.Equal(t, esperado, indice.Blocos[caso.posicao])
			require.Equal(t, 1, repo.escritas, "A17: o índice corrigido é gravado")

			salvo, err := cdm.Desserializar(repo.novo)
			require.NoError(t, err)
			require.Equal(t, indice, salvo)
		})
	}
}
