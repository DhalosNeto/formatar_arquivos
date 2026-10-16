// RED (TDD) do recorte f3-endpoint-e-download-de-formatacao.
//
// Nada do que este arquivo exercita existe ainda: espera-se falha de
// COMPILAÇÃO ("undefined: webservices.ServicoFormatacao",
// "undefined: webservices.NovoServicoFormatacao",
// "undefined: webmodel.FormatadoResposta") até o codador entregar
// internal/application/web/webservices/formatacao.go e o campo novo em
// webmodel/documento.go.
//
// Contrato travado pela spec ativa (C1, C2, C3, C4):
//
//	func NovoServicoFormatacao(documentos *documentoservice.Servico, jobs *criacao.Servico,
//	    consultas *consulta.Servico, armazenador ArmazenadorObjetos) (*ServicoFormatacao, error)
//	func (s *ServicoFormatacao) Formatar(ctx context.Context, dono vo.Dono, documentoID, rulesetID uuid.UUID) (webmodel.JobResposta, error)
//	func (s *ServicoFormatacao) URLFormatado(ctx context.Context, dono vo.Dono, documentoID, rulesetID uuid.UUID) (webmodel.FormatadoResposta, error)
//
// Este arquivo reusa os fakes e helpers do pacote de teste
// (documento_test.go: repositorioFake, donoDeTeste, documentoDeTeste,
// exigir*; analise_test.go: jobsRepoMemoria, documentoComCDMDeTeste), e
// acrescenta só o que não existia: um fake de ConsultaJobRepo que devolve
// LISTA (o de jobs_test.go entra em panic ao listar) e um armazenador com
// CONTADOR de assinaturas, exigido nominalmente por A8.
//
// LIMITE DECLARADO (A5): validadePadraoPreviewURL é unexported e este arquivo
// é do pacote externo webservices_test, então a validade esperada é repetida
// aqui como valor NOMEADO (validadeEsperadaFormatadoURL). A asserção é de
// igualdade exata, não de "coerência": trocar a validade em documento.go:35
// derruba A5. O que o teste NÃO pode provar é que as duas constantes são a
// MESMA constante — se o codador criar uma constante nova de 15 minutos em
// vez de reusar validadePadraoPreviewURL, este teste continua verde. Essa
// metade de C2(4) é revisão de delta, não teste.
package webservices_test

import (
	"context"
	"encoding/json"
	"io"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-halos/formatador/internal/application/web/webmodel"
	"github.com/daniel-halos/formatador/internal/application/web/webservices"
	"github.com/daniel-halos/formatador/internal/domain/documento/entity"
	documentoservice "github.com/daniel-halos/formatador/internal/domain/documento/service"
	"github.com/daniel-halos/formatador/internal/domain/job/consulta"
	"github.com/daniel-halos/formatador/internal/domain/job/criacao"
	jobentity "github.com/daniel-halos/formatador/internal/domain/job/entity"
	"github.com/daniel-halos/formatador/internal/domain/job/repository"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// validadeEsperadaFormatadoURL espelha validadePadraoPreviewURL
// (webservices/documento.go:35). Ver o LIMITE DECLARADO no cabeçalho.
const validadeEsperadaFormatadoURL = 15 * time.Minute

// consultaJobsListaFake implementa repository.ConsultaJobRepo devolvendo a
// lista de jobs de um documento na MESMA ordem que o SQL de
// data/postgres/job.go:54-66 garante — criado_em DESC, id DESC. A ordenação é
// feita aqui dentro de propósito: se o teste plantasse a lista já ordenada à
// mão, ele provaria a escolha do primeiro elemento, não a escolha do job mais
// recente.
//
// O filtro de DONO continua fora deste fake: é consulta.Servico quem confere o
// documento do solicitante antes de listar (consulta/servico.go:54-58), e é
// isso que A8 afere. Replicá-lo aqui esconderia a fronteira real.
type consultaJobsListaFake struct {
	porDocumento map[uuid.UUID][]jobentity.Job
	chamadas     int
}

func novaConsultaJobsListaFake() *consultaJobsListaFake {
	return &consultaJobsListaFake{porDocumento: map[uuid.UUID][]jobentity.Job{}}
}

func (r *consultaJobsListaFake) planejar(documentoID uuid.UUID, jobs ...jobentity.Job) {
	r.porDocumento[documentoID] = jobs
}

func (*consultaJobsListaFake) ObterPorID(context.Context, vo.Dono, uuid.UUID) (jobentity.Job, error) {
	panic("URLFormatado não deve consultar job por id")
}

func (r *consultaJobsListaFake) ListarPorDocumento(_ context.Context, _ vo.Dono, documentoID uuid.UUID) ([]jobentity.Job, error) {
	r.chamadas++
	lista := make([]jobentity.Job, len(r.porDocumento[documentoID]))
	copy(lista, r.porDocumento[documentoID])
	sort.SliceStable(lista, func(i, j int) bool {
		if !lista[i].CriadoEm.Equal(lista[j].CriadoEm) {
			return lista[i].CriadoEm.After(lista[j].CriadoEm)
		}
		return lista[i].ID.String() > lista[j].ID.String()
	})
	return lista, nil
}

var _ repository.ConsultaJobRepo = (*consultaJobsListaFake)(nil)

// armazenadorContadorFake implementa webservices.ArmazenadorObjetos contando
// as assinaturas e guardando os argumentos EXATOS da última. O contador é
// exigência nominal de A8 ("ZERO chamadas a URLPreAssinada, aferido por fake
// com CONTADOR"): um booleano não distingue "não assinou" de "assinou e o
// teste não olhou".
type armazenadorContadorFake struct {
	chamadas int
	chave    vo.ChaveStorage
	validade time.Duration
	retorno  string
	erro     error
}

func (a *armazenadorContadorFake) Salvar(context.Context, vo.ChaveStorage, io.Reader, int64, string) error {
	panic("formatação não salva objeto pela camada de aplicação")
}

func (a *armazenadorContadorFake) URLPreAssinada(_ context.Context, chave vo.ChaveStorage, validade time.Duration) (string, error) {
	a.chamadas++
	a.chave, a.validade = chave, validade
	if a.erro != nil {
		return "", a.erro
	}
	if a.retorno == "" {
		return "https://storage.exemplo/formatado-assinado", nil
	}
	return a.retorno, nil
}

var _ webservices.ArmazenadorObjetos = (*armazenadorContadorFake)(nil)

// pilhaFormatacao reúne o serviço sob teste e os dublês para inspeção.
//
// criacao.Servico e consulta.Servico são os REAIS, por exigência explícita de
// A2: um fake de criacao que não replicasse a guarda de criacao/servico.go:67
// ficaria verde sem exercitar contrato algum.
type pilhaFormatacao struct {
	servico     *webservices.ServicoFormatacao
	documentos  *repositorioFake
	jobs        *jobsRepoMemoria
	consultas   *consultaJobsListaFake
	armazenador *armazenadorContadorFake
}

func novaPilhaFormatacao(t *testing.T) pilhaFormatacao {
	t.Helper()
	repo := &repositorioFake{registrador: &registrador{}, documentos: map[uuid.UUID]entity.Documento{}}
	docservico, err := documentoservice.NovoServico(repo, 0)
	require.NoError(t, err)
	jobsRepo := novoJobsRepoMemoria()
	jobsCriacao, err := criacao.NovoServico(jobsRepo, docservico)
	require.NoError(t, err)
	consultaRepo := novaConsultaJobsListaFake()
	consultas, err := consulta.NovoServico(consultaRepo, docservico)
	require.NoError(t, err)
	armazenador := &armazenadorContadorFake{}
	servico, err := webservices.NovoServicoFormatacao(docservico, jobsCriacao, consultas, armazenador)
	require.NoError(t, err)
	return pilhaFormatacao{servico: servico, documentos: repo, jobs: jobsRepo, consultas: consultaRepo, armazenador: armazenador}
}

// documentoFormatavelDeTeste é um documento que passa por TODAS as
// pré-condições de C1: formato docx, status analisado e CDM presente.
func documentoFormatavelDeTeste(t *testing.T, dono vo.Dono) entity.Documento {
	t.Helper()
	return documentoComCDMDeTeste(t, dono)
}

// documentoNaoDocxFormatavelDeTeste isola o TERMO do formato: status e CDM
// estão bons, só o formato não é docx. Sem isolar, a recusa poderia vir do
// termo de status e ninguém notaria a remoção da guarda de formato.
func documentoNaoDocxFormatavelDeTeste(t *testing.T, dono vo.Dono) entity.Documento {
	t.Helper()
	modelo := documentoComCDMDeTeste(t, dono)
	documento, err := entity.NovoDocumento(dono, "artigo.pdf", vo.FormatoPDF, 4096)
	require.NoError(t, err)
	documento.Status = entity.StatusAnalisado
	documento.CDM = modelo.CDM
	return documento
}

func (p pilhaFormatacao) plantar(documentos ...entity.Documento) {
	for _, documento := range documentos {
		p.documentos.documentos[documento.ID] = documento
	}
}

// jobDeFormatacao monta um job de formatar já em um estado dado, como o banco
// o devolveria.
func jobDeFormatacao(documentoID, rulesetID uuid.UUID, status jobentity.StatusJob, criadoEm time.Time) jobentity.Job {
	ruleset := rulesetID
	return jobentity.Job{
		ID: uuid.New(), DocumentoID: documentoID, RulesetID: &ruleset,
		Tipo: jobentity.TipoFormatar, Status: status, CriadoEm: criadoEm,
	}
}

func TestNovoServicoFormatacaoRecusaDependenciaNula(t *testing.T) {
	t.Parallel()
	repo := &repositorioFake{registrador: &registrador{}}
	docservico, err := documentoservice.NovoServico(repo, 0)
	require.NoError(t, err)
	jobsCriacao, err := criacao.NovoServico(novoJobsRepoMemoria(), docservico)
	require.NoError(t, err)
	consultas, err := consulta.NovoServico(novaConsultaJobsListaFake(), docservico)
	require.NoError(t, err)

	casos := []struct {
		nome        string
		documentos  *documentoservice.Servico
		jobs        *criacao.Servico
		consultas   *consulta.Servico
		armazenador webservices.ArmazenadorObjetos
	}{
		{"documentos nulo", nil, jobsCriacao, consultas, &armazenadorContadorFake{}},
		{"jobs nulo", docservico, nil, consultas, &armazenadorContadorFake{}},
		{"consultas nulo", docservico, jobsCriacao, nil, &armazenadorContadorFake{}},
		{"armazenador nulo", docservico, jobsCriacao, consultas, nil},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			_, err := webservices.NovoServicoFormatacao(caso.documentos, caso.jobs, caso.consultas, caso.armazenador)
			var nulo *errors.ErroArgumentoNulo
			require.ErrorAsf(t, err, &nulo, "esperava ErroArgumentoNulo, obteve %T: %v", err, err)
		})
	}
}

// ---------------------------------------------------------------------------
// C1 — Formatar
// ---------------------------------------------------------------------------

// TestFormatarMesmoParDevolveMesmoJob cobre A1.
func TestFormatarMesmoParDevolveMesmoJob(t *testing.T) {
	t.Parallel()
	pilha := novaPilhaFormatacao(t)
	dono := donoDeTeste(t)
	documento := documentoFormatavelDeTeste(t, dono)
	pilha.plantar(documento)
	ruleset := uuid.New()

	primeiro, err := pilha.servico.Formatar(context.Background(), dono, documento.ID, ruleset)
	require.NoError(t, err)
	require.Equal(t, jobentity.TipoFormatar.String(), primeiro.Tipo)
	require.Equal(t, jobentity.StatusPendente.String(), primeiro.Status)

	segundo, err := pilha.servico.Formatar(context.Background(), dono, documento.ID, ruleset)
	require.NoError(t, err)

	assert.Equal(t, primeiro.ID, segundo.ID, "o mesmo par (documento, perfil) tem que devolver o MESMO job")
	assert.Len(t, pilha.jobs.porChave, 1, "só um job deveria ter sido inserido de fato")
}

// TestFormatarPerfilDiferenteCriaJobDiferente cobre A2.
//
// Roda sobre o criacao.Servico REAL e sobre um repositório que honra a
// unicidade de (documento_id, chave): se a chave de idempotência derivar só do
// documentoID, a segunda chamada cai em criacao/servico.go:67-69 e volta
// *errors.ErroConflito — o usuário fica PRESO ao primeiro perfil. Ele não
// recebe o perfil errado em silêncio, e é por isso que a asserção é de erro
// nil + job diferente, não de troca de perfil.
func TestFormatarPerfilDiferenteCriaJobDiferente(t *testing.T) {
	t.Parallel()
	pilha := novaPilhaFormatacao(t)
	dono := donoDeTeste(t)
	documento := documentoFormatavelDeTeste(t, dono)
	pilha.plantar(documento)
	perfilA, perfilB := uuid.New(), uuid.New()

	primeiro, err := pilha.servico.Formatar(context.Background(), dono, documento.ID, perfilA)
	require.NoError(t, err)

	segundo, err := pilha.servico.Formatar(context.Background(), dono, documento.ID, perfilB)
	require.NoErrorf(t, err, "perfil novo sobre o mesmo documento não pode ser conflito: %v", err)
	assert.NotEqual(t, primeiro.ID, segundo.ID, "cada perfil precisa do seu job")
	require.Len(t, pilha.jobs.porChave, 2, "dois perfis distintos, duas chaves de idempotência distintas")

	// O ruleset pedido tem de chegar ao job inserido: a JobResposta não carrega
	// ruleset_id (webmodel/documento.go), então a verificação é no repositório.
	perfisInseridos := map[uuid.UUID]bool{}
	for _, job := range pilha.jobs.porChave {
		require.NotNil(t, job.RulesetID, "job de formatar sem ruleset_id")
		perfisInseridos[*job.RulesetID] = true
	}
	assert.True(t, perfisInseridos[perfilA] && perfisInseridos[perfilB],
		"os dois perfis pedidos tinham de estar nos jobs inseridos, obtive %v", perfisInseridos)
}

// TestFormatarRecusaPreCondicaoDoDocumento cobre A3, termo por termo.
//
// Classe e campo são os MESMOS que o worker devolve em
// fila/executor.go:327-342, para as duas camadas não divergirem: formato não
// docx é *errors.ErroValidacao no campo "formato"; status não formatável é
// *errors.ErroConflito. Sem esta guarda o job é criado, o worker o recusa, e
// A1/A16 garantem que o par fica trancado para sempre.
//
// O termo do CDM NÃO é testado aqui de propósito (C1): `analisado` implica CDM
// não vazio, e duplicar o termo convidaria alguém a "consertar" a assimetria.
func TestFormatarRecusaPreCondicaoDoDocumento(t *testing.T) {
	t.Parallel()
	casos := []struct {
		nome   string
		status entity.Status
		naoDoc bool
		classe string
		campo  string
	}{
		{nome: "status recebido", status: entity.StatusRecebido, classe: "conflito"},
		{nome: "status analisando", status: entity.StatusAnalisando, classe: "conflito"},
		{nome: "status formatando", status: entity.StatusFormatando, classe: "conflito"},
		{nome: "status falhou", status: entity.StatusFalhou, classe: "conflito"},
		{nome: "formato nao docx", naoDoc: true, classe: "validacao", campo: "formato"},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()
			pilha := novaPilhaFormatacao(t)
			dono := donoDeTeste(t)
			documento := documentoFormatavelDeTeste(t, dono)
			if caso.naoDoc {
				documento = documentoNaoDocxFormatavelDeTeste(t, dono)
			} else {
				documento.Status = caso.status
			}
			pilha.plantar(documento)

			_, err := pilha.servico.Formatar(context.Background(), dono, documento.ID, uuid.New())
			require.Error(t, err)

			switch caso.classe {
			case "conflito":
				exigirConflito(t, err)
				var validacao *errors.ErroValidacao
				assert.Falsef(t, errors.Como(err, &validacao),
					"status não formatável é conflito, nunca validação: %v", err)
			case "validacao":
				var validacao *errors.ErroValidacao
				require.Truef(t, errors.Como(err, &validacao), "esperava ErroValidacao, obteve %T: %v", err, err)
				require.Len(t, validacao.Campos, 1)
				assert.Equal(t, caso.campo, validacao.Campos[0].Campo,
					"o campo tem de ser o mesmo que executor.go:334 devolve")
			}
			assert.Zero(t, pilha.jobs.chamadas, "nenhum job pode ser criado quando a pré-condição reprova")
			assert.Empty(t, pilha.jobs.porChave)
		})
	}
}

// TestFormatarAceitaDocumentoJaFormatado é o outro lado de A3: aceitar
// StatusFormatado é deliberado (executor.go:320-322) — exigir só `analisado`
// obrigaria reanálise para trocar de perfil. Sem este caso, uma guarda
// `status != analisado` passaria A3 inteiro.
func TestFormatarAceitaDocumentoJaFormatado(t *testing.T) {
	t.Parallel()
	pilha := novaPilhaFormatacao(t)
	dono := donoDeTeste(t)
	documento := documentoFormatavelDeTeste(t, dono)
	documento.Status = entity.StatusFormatado
	pilha.plantar(documento)

	resposta, err := pilha.servico.Formatar(context.Background(), dono, documento.ID, uuid.New())
	require.NoError(t, err)
	assert.Equal(t, jobentity.TipoFormatar.String(), resposta.Tipo)
}

// TestFormatarDocumentoAlheioNaoEncontrado cobre A4.
//
// REDAÇÃO: "documento de outro dono" e "sessão desconhecida". NÃO existe
// sessão revogada — o cookie é bearer sem tabela (sessao/sessao.go:84-98), e
// chamar de revogada faria alguém concluir que TOCTOU de revogação está
// coberto. O caso literal "sem cookie" não chega a este serviço: ele morre em
// sessao.Existente, e está coberto por A13 no controlador.
func TestFormatarDocumentoAlheioNaoEncontrado(t *testing.T) {
	t.Parallel()
	casos := []struct {
		nome    string
		plantar bool
	}{
		{"documento de outro dono", true},
		{"sessao desconhecida, documento inexistente", false},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()
			pilha := novaPilhaFormatacao(t)
			dono := donoDeTeste(t)
			solicitante := donoDeTeste(t)
			documento := documentoFormatavelDeTeste(t, dono)
			if caso.plantar {
				pilha.plantar(documento)
			}

			_, err := pilha.servico.Formatar(context.Background(), solicitante, documento.ID, uuid.New())
			exigirNaoEncontrado(t, err)
			assert.Empty(t, pilha.jobs.porChave, "nenhum job para documento que o solicitante não acessa")
		})
	}
}

// TestFormatarSobreJobFalhadoDevolveJobTerminal cobre A16: a PERDA
// PERMANENTE do par é comportamento NOMEADO, não acidental.
//
// O job é levado a `falhou` no próprio repositório (é o worker quem falha, não
// a camada de aplicação) e o segundo POST tem de devolver aquele job terminal,
// sem criar outro e sem reiniciar. Quem muda isso está mudando o contrato
// declarado em docs/contrato-api.md (C6 item d), não corrigindo um bug.
func TestFormatarSobreJobFalhadoDevolveJobTerminal(t *testing.T) {
	t.Parallel()
	pilha := novaPilhaFormatacao(t)
	dono := donoDeTeste(t)
	documento := documentoFormatavelDeTeste(t, dono)
	pilha.plantar(documento)
	ruleset := uuid.New()

	primeiro, err := pilha.servico.Formatar(context.Background(), dono, documento.ID, ruleset)
	require.NoError(t, err)
	require.Len(t, pilha.jobs.porChave, 1, "pré-condição: um job criado")

	for chave, job := range pilha.jobs.porChave {
		job.Status = jobentity.StatusFalhou
		job.Erro = "falha do worker"
		pilha.jobs.porChave[chave] = job
	}

	segundo, err := pilha.servico.Formatar(context.Background(), dono, documento.ID, ruleset)
	require.NoError(t, err)
	assert.Equal(t, primeiro.ID, segundo.ID, "o par trancado devolve o MESMO job")
	assert.Equal(t, jobentity.StatusFalhou.String(), segundo.Status,
		"o job devolvido é o terminal, sem reinício — é a perda permanente que o contrato declara")
	assert.Len(t, pilha.jobs.porChave, 1, "nenhum job novo")
}

// ---------------------------------------------------------------------------
// C2 / C3 — URLFormatado
// ---------------------------------------------------------------------------

// TestURLFormatadoAssinaChaveEValidadeExatas cobre A5.
func TestURLFormatadoAssinaChaveEValidadeExatas(t *testing.T) {
	t.Parallel()
	pilha := novaPilhaFormatacao(t)
	dono := donoDeTeste(t)
	documento := documentoFormatavelDeTeste(t, dono)
	documento.Status = entity.StatusFormatado
	pilha.plantar(documento)
	ruleset := uuid.New()

	agora := time.Now().UTC()
	// Um job concluído mais ANTIGO do mesmo par, para o "mais recente" de
	// C2(2) ser escolha e não coincidência de lista com um elemento.
	pilha.consultas.planejar(documento.ID,
		jobDeFormatacao(documento.ID, ruleset, jobentity.StatusConcluido, agora.Add(-time.Hour)),
		jobDeFormatacao(documento.ID, ruleset, jobentity.StatusConcluido, agora.Add(-time.Minute)),
	)
	pilha.armazenador.retorno = "https://storage.exemplo/assinada-de-teste"

	chaveEsperada, err := vo.NovaChaveFormatado(documento.ID, ruleset)
	require.NoError(t, err)

	antes := time.Now()
	resposta, err := pilha.servico.URLFormatado(context.Background(), dono, documento.ID, ruleset)
	depois := time.Now()
	require.NoError(t, err)

	require.Equal(t, 1, pilha.armazenador.chamadas, "uma e só uma assinatura")
	assert.Equal(t, chaveEsperada, pilha.armazenador.chave,
		"a chave tem de ser DERIVADA por vo.NovaChaveFormatado(documentoID, rulesetID), nunca lida do Resultado do job")
	assert.Equal(t, validadeEsperadaFormatadoURL, pilha.armazenador.validade,
		"a validade tem de ser exatamente a mesma do preview (15 minutos)")
	assert.Equal(t, "https://storage.exemplo/assinada-de-teste", resposta.URL)
	assert.False(t, resposta.ExpiraEm.Before(antes.Add(validadeEsperadaFormatadoURL)),
		"ExpiraEm antes da janela: %v", resposta.ExpiraEm)
	assert.False(t, resposta.ExpiraEm.After(depois.Add(validadeEsperadaFormatadoURL)),
		"ExpiraEm depois da janela: %v", resposta.ExpiraEm)
}

// TestURLFormatadoNaoEntregaArtefatoDeOutroPerfil cobre A6, o ALTO da
// segurança: perfil A concluído e perfil B falhado ou pendente, GET pedindo B.
//
// Sem o filtro por rulesetID, "o último job de formatação concluído" devolve o
// artefato de A com 200, e o cliente não tem como detectar, porque a
// FormatadoResposta só traz url e validade. Nenhuma URL pode ser assinada.
func TestURLFormatadoNaoEntregaArtefatoDeOutroPerfil(t *testing.T) {
	t.Parallel()
	casos := []struct {
		nome      string
		statusDeB jobentity.StatusJob
	}{
		{"B falhou", jobentity.StatusFalhou},
		{"B pendente", jobentity.StatusPendente},
		{"B executando", jobentity.StatusExecutando},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()
			pilha := novaPilhaFormatacao(t)
			dono := donoDeTeste(t)
			documento := documentoFormatavelDeTeste(t, dono)
			documento.Status = entity.StatusFormatado
			pilha.plantar(documento)
			perfilA, perfilB := uuid.New(), uuid.New()

			agora := time.Now().UTC()
			pilha.consultas.planejar(documento.ID,
				jobDeFormatacao(documento.ID, perfilA, jobentity.StatusConcluido, agora.Add(-time.Hour)),
				jobDeFormatacao(documento.ID, perfilB, caso.statusDeB, agora.Add(-time.Minute)),
			)

			resposta, err := pilha.servico.URLFormatado(context.Background(), dono, documento.ID, perfilB)
			exigirConflito(t, err)
			assert.Empty(t, resposta.URL, "nenhuma URL pode voltar no erro")
			assert.Zero(t, pilha.armazenador.chamadas,
				"pedir o perfil B NUNCA pode assinar o artefato do perfil A")

			chaveDeA, errChave := vo.NovaChaveFormatado(documento.ID, perfilA)
			require.NoError(t, errChave)
			assert.NotEqual(t, chaveDeA, pilha.armazenador.chave)
		})
	}
}

// TestURLFormatadoIndisponivelPorTermo cobre A7.
//
// Os quatro primeiros casos são VALORES de um mesmo termo (o filtro de status)
// e colapsam num caminho só — não provam cobertura de condição. O quinto é o
// termo do TIPO e é o único que morre se o filtro `Tipo == TipoFormatar` for
// removido: um `analisar` concluído passaria por um filtro só de status e o
// serviço assinaria a chave de um formatado que nunca existiu.
//
// É este teste que exercita a SELEÇÃO EM GO definida por C3 — tipo e status
// sobre o resultado de ListarPorDocumento, com o filtro de DONO permanecendo
// no SQL (coberto por A8).
func TestURLFormatadoIndisponivelPorTermo(t *testing.T) {
	t.Parallel()
	ruleset := uuid.New()
	casos := []struct {
		nome  string
		termo string
		jobs  func(documentoID uuid.UUID, agora time.Time) []jobentity.Job
	}{
		{
			nome:  "par sem job algum",
			termo: "status",
			jobs:  func(uuid.UUID, time.Time) []jobentity.Job { return nil },
		},
		{
			nome:  "job do par pendente",
			termo: "status",
			jobs: func(documentoID uuid.UUID, agora time.Time) []jobentity.Job {
				return []jobentity.Job{jobDeFormatacao(documentoID, ruleset, jobentity.StatusPendente, agora)}
			},
		},
		{
			nome:  "job do par executando",
			termo: "status",
			jobs: func(documentoID uuid.UUID, agora time.Time) []jobentity.Job {
				return []jobentity.Job{jobDeFormatacao(documentoID, ruleset, jobentity.StatusExecutando, agora)}
			},
		},
		{
			nome:  "job do par falhado",
			termo: "status",
			jobs: func(documentoID uuid.UUID, agora time.Time) []jobentity.Job {
				return []jobentity.Job{jobDeFormatacao(documentoID, ruleset, jobentity.StatusFalhou, agora)}
			},
		},
		{
			nome:  "TERMO DO TIPO: analisar concluido e nenhum formatar",
			termo: "tipo",
			jobs: func(documentoID uuid.UUID, agora time.Time) []jobentity.Job {
				return []jobentity.Job{{
					ID: uuid.New(), DocumentoID: documentoID, Tipo: jobentity.TipoAnalisar,
					Status: jobentity.StatusConcluido, CriadoEm: agora,
				}}
			},
		},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()
			pilha := novaPilhaFormatacao(t)
			dono := donoDeTeste(t)
			documento := documentoFormatavelDeTeste(t, dono)
			pilha.plantar(documento)
			pilha.consultas.planejar(documento.ID, caso.jobs(documento.ID, time.Now().UTC())...)

			resposta, err := pilha.servico.URLFormatado(context.Background(), dono, documento.ID, ruleset)

			exigirConflito(t, err)
			var ausente *errors.ErroNaoEncontrado
			assert.Falsef(t, errors.Como(err, &ausente),
				"par sem artefato pronto é CONFLITO, não ausência (mesma postura de URLPreview): %v", err)
			assert.Empty(t, resposta.URL)
			assert.Zerof(t, pilha.armazenador.chamadas,
				"termo %q: nenhuma URL pode ser assinada sem job de formatar concluído do par", caso.termo)
		})
	}
}

// TestURLFormatadoDeDocumentoAlheioNaoAssina cobre A8.
//
// A leitura escopada ao dono PRECEDE a assinatura: é a obrigação herdada de
// URLPreview. O contador em zero é a prova; um fake sem contador não distingue
// "não assinou" de "o teste não olhou".
//
// REDAÇÃO: "documento de outro dono" e "sessão desconhecida", nunca "sessão
// revogada" — revogação não existe no sistema.
func TestURLFormatadoDeDocumentoAlheioNaoAssina(t *testing.T) {
	t.Parallel()
	casos := []struct {
		nome    string
		plantar bool
	}{
		{"documento de outro dono", true},
		{"sessao desconhecida, documento inexistente", false},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()
			pilha := novaPilhaFormatacao(t)
			dono := donoDeTeste(t)
			solicitante := donoDeTeste(t)
			documento := documentoFormatavelDeTeste(t, dono)
			documento.Status = entity.StatusFormatado
			if caso.plantar {
				pilha.plantar(documento)
			}
			ruleset := uuid.New()
			// Artefato do dono legítimo PRONTO: se a autorização falhar, há o que
			// vazar. Sem isto o teste passaria por ausência de dados.
			pilha.consultas.planejar(documento.ID,
				jobDeFormatacao(documento.ID, ruleset, jobentity.StatusConcluido, time.Now().UTC()))

			resposta, err := pilha.servico.URLFormatado(context.Background(), solicitante, documento.ID, ruleset)

			exigirNaoEncontrado(t, err)
			assert.Empty(t, resposta.URL)
			assert.Zero(t, pilha.armazenador.chamadas,
				"ZERO chamadas a URLPreAssinada para documento que o solicitante não acessa")
		})
	}
}

// ---------------------------------------------------------------------------
// C4 — FormatadoResposta
// ---------------------------------------------------------------------------

// TestFormatadoRespostaSerializaSomenteURLEExpiraEm cobre A9.
func TestFormatadoRespostaSerializaSomenteURLEExpiraEm(t *testing.T) {
	t.Parallel()
	bruto, err := json.Marshal(webmodel.FormatadoResposta{
		URL:      "https://storage.exemplo/assinada",
		ExpiraEm: time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)

	var campos map[string]any
	require.NoError(t, json.Unmarshal(bruto, &campos))

	chaves := make([]string, 0, len(campos))
	for chave := range campos {
		chaves = append(chaves, chave)
	}
	sort.Strings(chaves)
	assert.Equal(t, []string{"expira_em", "url"}, chaves,
		"FormatadoResposta carrega SOMENTE url e expira_em, na mesma forma de PreviewResposta")
}
