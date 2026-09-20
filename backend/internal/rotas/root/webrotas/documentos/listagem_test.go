package documentos

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/daniel-halos/formatador/internal/application/web/webmodel"
	"github.com/daniel-halos/formatador/internal/application/web/webservices"
	"github.com/daniel-halos/formatador/internal/domain/documento/entity"
	documentoservice "github.com/daniel-halos/formatador/internal/domain/documento/service"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// repositorioEspiaListagem é repository.DocumentoRepo com o comportamento real
// de repositorioMemoria, mais uma gravação dos argumentos recebidos por
// ListarPorDono — é a única forma de provar, do lado de fora, que
// TratarListagem não repassa um limite absurdo cru ao domínio.
type repositorioEspiaListagem struct {
	documentos map[uuid.UUID]entity.Documento

	chamadasListar       int
	limiteRecebido       int
	deslocamentoRecebido int
}

func novoRepositorioEspiaListagem() *repositorioEspiaListagem {
	return &repositorioEspiaListagem{documentos: map[uuid.UUID]entity.Documento{}}
}

func (r *repositorioEspiaListagem) Inserir(_ context.Context, documento entity.Documento) error {
	r.documentos[documento.ID] = documento
	return nil
}

func (r *repositorioEspiaListagem) ObterPorID(_ context.Context, solicitante vo.Dono, id uuid.UUID) (entity.Documento, error) {
	documento, existe := r.documentos[id]
	if !existe || !solicitante.PodeAcessar(documento.Dono) {
		return entity.Documento{}, errors.NovoErroNaoEncontrado("documento")
	}
	return documento, nil
}

func (r *repositorioEspiaListagem) ListarPorDono(_ context.Context, solicitante vo.Dono, limite, deslocamento int) ([]entity.Documento, error) {
	r.chamadasListar++
	r.limiteRecebido = limite
	r.deslocamentoRecebido = deslocamento
	var lista []entity.Documento
	for _, documento := range r.documentos {
		if solicitante.PodeAcessar(documento.Dono) {
			lista = append(lista, documento)
		}
	}
	return lista, nil
}

func (r *repositorioEspiaListagem) DefinirChavePreviewPDF(_ context.Context, solicitante vo.Dono, id uuid.UUID, chave vo.ChaveStorage) error {
	documento, existe := r.documentos[id]
	if !existe || !solicitante.PodeAcessar(documento.Dono) {
		return errors.NovoErroNaoEncontrado("documento")
	}
	documento.ChaveStoragePDF = &chave
	r.documentos[id] = documento
	return nil
}

// controladorComEspiaDeTeste monta a mesma pilha real de controladorDeTeste,
// mas sobre repositorioEspiaListagem, para inspecionar os argumentos que
// chegam ao domínio.
func controladorComEspiaDeTeste(t *testing.T) (*Controlador, *repositorioEspiaListagem) {
	t.Helper()
	repo := novoRepositorioEspiaListagem()
	docservico, err := documentoservice.NovoServico(repo, 0)
	exigirSemErroDocumentos(t, err)
	servico, err := webservices.NovoServicoDocumento(docservico, armazenadorMemoria{}, conversorMemoria{})
	exigirSemErroDocumentos(t, err)
	return NovoControlador(servico), repo
}

// TestTratarListagemSemCookieDevolveListaVazia: quem nunca enviou nada não
// tem sessão ainda, e isso não é um erro — é o estado normal de um visitante
// novo. Não pode virar 404 nem qualquer outro erro.
func TestTratarListagemSemCookieDevolveListaVazia(t *testing.T) {
	t.Parallel()
	controlador, _ := controladorDeTeste(t)
	requisicao := &requisicaoFake{metodo: http.MethodGet, caminho: CaminhoColecao}
	resposta := &respostaFake{}

	exigirSemErroDocumentos(t, controlador.TratarListagem(context.Background(), requisicao, resposta))

	if resposta.status != http.StatusOK {
		t.Fatalf("esperava 200 sem cookie (sessão nova é normal, não erro), obteve %d (codigo=%q descricao=%q)", resposta.status, resposta.codigo, resposta.descricao)
	}
	lista, ok := resposta.corpo.([]webmodel.DocumentoResposta)
	if !ok {
		t.Fatalf("corpo inesperado: %T", resposta.corpo)
	}
	if lista == nil {
		t.Fatal("lista vazia veio nil: serializaria como null, não []")
	}
	if len(lista) != 0 {
		t.Fatalf("esperava lista vazia sem cookie, obteve %d itens", len(lista))
	}
}

// TestTratarListagemParametrosInvalidosOuAusentesUsaPadrao cobre limite e
// deslocamento ausentes, não numéricos ou negativos: nenhum desses casos pode
// estourar a requisição.
func TestTratarListagemParametrosInvalidosOuAusentesUsaPadrao(t *testing.T) {
	t.Parallel()
	casos := []struct {
		nome     string
		consulta map[string]string
	}{
		{"parâmetros ausentes", map[string]string{}},
		{"limite não numérico", map[string]string{"limite": "abc"}},
		{"deslocamento não numérico", map[string]string{"deslocamento": "xyz"}},
		{"limite negativo", map[string]string{"limite": "-1"}},
		{"deslocamento negativo", map[string]string{"deslocamento": "-5"}},
		{"limite vazio", map[string]string{"limite": ""}},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			controlador, _ := controladorDeTeste(t)
			requisicao := &requisicaoFake{
				metodo:   http.MethodGet,
				caminho:  CaminhoColecao,
				cookie:   uuid.New().String(),
				consulta: caso.consulta,
			}
			resposta := &respostaFake{}
			exigirSemErroDocumentos(t, controlador.TratarListagem(context.Background(), requisicao, resposta))
			if resposta.status != http.StatusOK {
				t.Fatalf("esperava 200, obteve %d (codigo=%q descricao=%q)", resposta.status, resposta.codigo, resposta.descricao)
			}
		})
	}
}

// TestTratarListagemLimiteAbsurdoNaoRepassaCru prova, observando o que chega
// ao repositório, que um limite absurdo vindo da query string não atravessa
// a borda HTTP sem um teto. O valor exato do teto é decisão do domínio
// (privada); aqui só se prova que 100000 nunca chega inteiro.
func TestTratarListagemLimiteAbsurdoNaoRepassaCru(t *testing.T) {
	t.Parallel()
	controlador, repo := controladorComEspiaDeTeste(t)
	requisicao := &requisicaoFake{
		metodo:   http.MethodGet,
		caminho:  CaminhoColecao,
		cookie:   uuid.New().String(),
		consulta: map[string]string{"limite": "100000"},
	}
	resposta := &respostaFake{}

	exigirSemErroDocumentos(t, controlador.TratarListagem(context.Background(), requisicao, resposta))

	if resposta.status != http.StatusOK {
		t.Fatalf("esperava 200, obteve %d", resposta.status)
	}
	if repo.chamadasListar != 1 {
		t.Fatalf("esperava 1 chamada a ListarPorDono, obteve %d", repo.chamadasListar)
	}
	if repo.limiteRecebido == 100000 {
		t.Fatal("limite absurdo chegou cru ao domínio, sem teto explícito")
	}
	if repo.limiteRecebido <= 0 || repo.limiteRecebido > 1000 {
		t.Fatalf("limite fora de qualquer teto razoável: %d", repo.limiteRecebido)
	}
}

// TestTratarListagemDevolveDocumentosDaSessao é o caminho feliz completo:
// cria um documento pela rota de criação e confere que ele aparece na
// listagem da mesma sessão, sem vazar campo interno.
func TestTratarListagemDevolveDocumentosDaSessao(t *testing.T) {
	t.Parallel()
	controlador, _ := controladorDeTeste(t)
	requisicaoCriacao := &requisicaoFake{
		metodo:  http.MethodPost,
		caminho: CaminhoColecao,
		arquivo: arquivoFormularioDeTeste(t, CampoArquivo, "artigo.docx", docxValidoDocumentos(t)),
	}
	respostaCriacao := &respostaFake{}
	exigirSemErroDocumentos(t, controlador.TratarCriacao(context.Background(), requisicaoCriacao, respostaCriacao))
	if respostaCriacao.status != http.StatusCreated {
		t.Fatalf("pré-condição falhou: criação não retornou 201 (status=%d)", respostaCriacao.status)
	}
	corpoCriado, ok := respostaCriacao.corpo.(webmodel.DocumentoResposta)
	if !ok {
		t.Fatalf("corpo de criação inesperado: %T", respostaCriacao.corpo)
	}
	sessaoID := respostaCriacao.cookies[0].Value

	requisicaoListagem := &requisicaoFake{
		metodo:  http.MethodGet,
		caminho: CaminhoColecao,
		cookie:  sessaoID,
	}
	respostaListagem := &respostaFake{}
	exigirSemErroDocumentos(t, controlador.TratarListagem(context.Background(), requisicaoListagem, respostaListagem))

	if respostaListagem.status != http.StatusOK {
		t.Fatalf("esperava 200, obteve %d", respostaListagem.status)
	}
	lista, ok := respostaListagem.corpo.([]webmodel.DocumentoResposta)
	if !ok {
		t.Fatalf("corpo inesperado: %T", respostaListagem.corpo)
	}
	if len(lista) != 1 || lista[0].ID != corpoCriado.ID {
		t.Fatalf("esperava só o documento da sessão, obteve %+v", lista)
	}
}

// TestTratarListagemIsolaPorSessao prova que uma sessão nunca vê o documento
// de outra na listagem.
func TestTratarListagemIsolaPorSessao(t *testing.T) {
	t.Parallel()
	controlador, repo := controladorDeTeste(t)
	sessaoIDA := uuid.New()
	donoA, err := vo.NovoDonoSessao(sessaoIDA)
	exigirSemErroDocumentos(t, err)
	donoB, err := vo.NovoDonoSessao(uuid.New())
	exigirSemErroDocumentos(t, err)
	documentoA, err := entity.NovoDocumento(donoA, "de-a.docx", vo.FormatoDocx, 100)
	exigirSemErroDocumentos(t, err)
	documentoB, err := entity.NovoDocumento(donoB, "de-b.docx", vo.FormatoDocx, 100)
	exigirSemErroDocumentos(t, err)
	exigirSemErroDocumentos(t, repo.Inserir(context.Background(), documentoA))
	exigirSemErroDocumentos(t, repo.Inserir(context.Background(), documentoB))

	requisicao := &requisicaoFake{
		metodo:  http.MethodGet,
		caminho: CaminhoColecao,
		cookie:  sessaoIDA.String(),
	}
	resposta := &respostaFake{}
	exigirSemErroDocumentos(t, controlador.TratarListagem(context.Background(), requisicao, resposta))

	lista, ok := resposta.corpo.([]webmodel.DocumentoResposta)
	if !ok {
		t.Fatalf("corpo inesperado: %T", resposta.corpo)
	}
	if len(lista) != 1 || lista[0].ID != documentoA.ID {
		t.Fatalf("listagem vazou documento de outra sessão: %+v", lista)
	}
}
