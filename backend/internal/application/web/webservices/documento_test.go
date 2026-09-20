package webservices_test

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/daniel-halos/formatador/internal/application/web/webservices"
	"github.com/daniel-halos/formatador/internal/domain/documento/entity"
	"github.com/daniel-halos/formatador/internal/domain/documento/repository"
	documentoservice "github.com/daniel-halos/formatador/internal/domain/documento/service"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// registrador acumula, na ordem real de execução, os rótulos de cada chamada
// feita pelos três fakes abaixo. É a única forma confiável de provar que
// Ingerir respeita a ordem "original antes do PDF, inserção antes da
// conversão" exigida pela ficha do investigador.
type registrador struct {
	eventos []string
}

func (r *registrador) registrar(nome string) { r.eventos = append(r.eventos, nome) }

// repositorioFake implementa repository.DocumentoRepo. Cada caso monta o seu,
// sem estado compartilhado entre execuções paralelas.
type repositorioFake struct {
	registrador *registrador

	documentos map[uuid.UUID]entity.Documento

	erroInserir error
	erroPreview error
	erroListar  error

	inseridos         int
	previewsDefinidos int
}

var _ repository.DocumentoRepo = (*repositorioFake)(nil)

func (r *repositorioFake) Inserir(_ context.Context, documento entity.Documento) error {
	r.registrador.registrar("repo.Inserir")
	r.inseridos++
	if r.erroInserir != nil {
		return r.erroInserir
	}
	if r.documentos == nil {
		r.documentos = map[uuid.UUID]entity.Documento{}
	}
	r.documentos[documento.ID] = documento
	return nil
}

func (r *repositorioFake) ObterPorID(_ context.Context, solicitante vo.Dono, id uuid.UUID) (entity.Documento, error) {
	r.registrador.registrar("repo.ObterPorID")
	documento, existe := r.documentos[id]
	if !existe || !solicitante.PodeAcessar(documento.Dono) {
		return entity.Documento{}, errors.NovoErroNaoEncontrado("documento")
	}
	return documento, nil
}

func (r *repositorioFake) ListarPorDono(_ context.Context, solicitante vo.Dono, _, _ int) ([]entity.Documento, error) {
	r.registrador.registrar("repo.ListarPorDono")
	if r.erroListar != nil {
		return nil, r.erroListar
	}
	var encontrados []entity.Documento
	for _, documento := range r.documentos {
		if solicitante.PodeAcessar(documento.Dono) {
			encontrados = append(encontrados, documento)
		}
	}
	return encontrados, nil
}

func (r *repositorioFake) DefinirChavePreviewPDF(_ context.Context, solicitante vo.Dono, id uuid.UUID, chave vo.ChaveStorage) error {
	r.registrador.registrar("repo.DefinirChavePreviewPDF")
	r.previewsDefinidos++
	if r.erroPreview != nil {
		return r.erroPreview
	}
	documento, existe := r.documentos[id]
	if !existe || !solicitante.PodeAcessar(documento.Dono) {
		return errors.NovoErroNaoEncontrado("documento")
	}
	documento.ChaveStoragePDF = &chave
	r.documentos[id] = documento
	return nil
}

// chamadaSalvar registra o que o fake de storage recebeu em cada Salvar.
type chamadaSalvar struct {
	chave       vo.ChaveStorage
	conteudo    []byte
	tamanho     int64
	contentType string
}

// armazenadorFake implementa webservices.ArmazenadorObjetos. Distingue o
// salvamento do original do salvamento do PDF pelo content type, já que é a
// única informação estável entre as duas chamadas.
type armazenadorFake struct {
	registrador *registrador

	chamadasSalvar []chamadaSalvar

	erroSalvarOriginal error
	erroSalvarPDF      error

	urlChamada  bool
	urlChave    vo.ChaveStorage
	urlValidade time.Duration
	urlRetorno  string
	urlErro     error
}

var _ webservices.ArmazenadorObjetos = (*armazenadorFake)(nil)

func (a *armazenadorFake) Salvar(_ context.Context, chave vo.ChaveStorage, conteudo io.Reader, tamanho int64, contentType string) error {
	dados, err := io.ReadAll(conteudo)
	if err != nil {
		return err
	}

	ehPDF := contentType == vo.MIMEPDF
	rotulo := "storage.Salvar:original"
	if ehPDF {
		rotulo = "storage.Salvar:pdf"
	}
	a.registrador.registrar(rotulo)
	a.chamadasSalvar = append(a.chamadasSalvar, chamadaSalvar{chave: chave, conteudo: dados, tamanho: tamanho, contentType: contentType})

	if ehPDF && a.erroSalvarPDF != nil {
		return a.erroSalvarPDF
	}
	if !ehPDF && a.erroSalvarOriginal != nil {
		return a.erroSalvarOriginal
	}
	return nil
}

func (a *armazenadorFake) URLPreAssinada(_ context.Context, chave vo.ChaveStorage, validade time.Duration) (string, error) {
	a.registrador.registrar("storage.URLPreAssinada")
	a.urlChamada = true
	a.urlChave = chave
	a.urlValidade = validade
	if a.urlErro != nil {
		return "", a.urlErro
	}
	if a.urlRetorno == "" {
		return "https://storage.exemplo/preview-assinado", nil
	}
	return a.urlRetorno, nil
}

// conversorFake implementa webservices.ConversorPDF.
type conversorFake struct {
	registrador *registrador

	chamado        bool
	entradaTamanho int
	saida          []byte
	erro           error
}

var _ webservices.ConversorPDF = (*conversorFake)(nil)

func (c *conversorFake) ConverterParaPDF(_ context.Context, docx []byte) ([]byte, error) {
	c.registrador.registrar("conversor.ConverterParaPDF")
	c.chamado = true
	c.entradaTamanho = len(docx)
	if c.erro != nil {
		return nil, c.erro
	}
	if c.saida == nil {
		return []byte("%PDF-1.4 fake-pdf-bytes"), nil
	}
	return c.saida, nil
}

func exigirSemErro(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
}

func exigirValidacao(t *testing.T, err error) {
	t.Helper()
	var validacao *errors.ErroValidacao
	if !errors.Como(err, &validacao) {
		t.Fatalf("esperava ErroValidacao, obteve %T: %v", err, err)
	}
}

func exigirNaoEncontrado(t *testing.T, err error) {
	t.Helper()
	var naoEncontrado *errors.ErroNaoEncontrado
	if !errors.Como(err, &naoEncontrado) {
		t.Fatalf("esperava ErroNaoEncontrado, obteve %T: %v", err, err)
	}
}

func exigirConflito(t *testing.T, err error) {
	t.Helper()
	var conflito *errors.ErroConflito
	if !errors.Como(err, &conflito) {
		t.Fatalf("esperava ErroConflito, obteve %T: %v", err, err)
	}
}

func donoDeTeste(t *testing.T) vo.Dono {
	t.Helper()
	dono, err := vo.NovoDonoSessao(uuid.New())
	exigirSemErro(t, err)
	return dono
}

func documentoDeTeste(t *testing.T, dono vo.Dono) entity.Documento {
	t.Helper()
	documento, err := entity.NovoDocumento(dono, "artigo.docx", vo.FormatoDocx, 4096)
	exigirSemErro(t, err)
	return documento
}

func documentoComPreviewDeTeste(t *testing.T, dono vo.Dono) entity.Documento {
	t.Helper()
	documento := documentoDeTeste(t, dono)
	chave, err := vo.NovaChavePreviewPDF(documento.ID)
	exigirSemErro(t, err)
	documento.ChaveStoragePDF = &chave
	return documento
}

// zipComPartes monta um pacote ZIP em memória só com as partes informadas,
// para exercitar exatamente a fronteira que ConferirPacoteDocx confere.
func zipComPartes(t *testing.T, partes map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	escritor := zip.NewWriter(&buf)
	for nome, conteudo := range partes {
		parte, err := escritor.Create(nome)
		exigirSemErro(t, err)
		_, err = parte.Write([]byte(conteudo))
		exigirSemErro(t, err)
	}
	exigirSemErro(t, escritor.Close())
	return buf.Bytes()
}

// docxValido monta um pacote DOCX mínimo, mas real, que passa por
// DetectarFormato (assinatura ZIP) e por ConferirPacoteDocx (partes exigidas).
func docxValido(t *testing.T) []byte {
	t.Helper()
	return zipComPartes(t, map[string]string{
		vo.ParteContentTypes:       `<?xml version="1.0"?><Types/>`,
		vo.ParteDocumentoPrincipal: `<?xml version="1.0"?><w:document/>`,
	})
}

// novoServicoDeTeste monta a pilha completa (fakes de repositório, storage e
// conversor por trás do serviço de domínio real) e devolve os fakes para
// inspeção pós-chamada.
func novoServicoDeTeste(t *testing.T) (*webservices.ServicoDocumento, *repositorioFake, *armazenadorFake, *conversorFake, *registrador) {
	t.Helper()
	reg := &registrador{}
	repo := &repositorioFake{registrador: reg}
	docservico, err := documentoservice.NovoServico(repo, 0)
	exigirSemErro(t, err)
	armazenador := &armazenadorFake{registrador: reg}
	conversor := &conversorFake{registrador: reg}
	servico, err := webservices.NovoServicoDocumento(docservico, armazenador, conversor)
	exigirSemErro(t, err)
	return servico, repo, armazenador, conversor, reg
}

func TestNovoServicoDocumentoRecusaDependenciaNula(t *testing.T) {
	t.Parallel()
	repo := &repositorioFake{}
	docservico, err := documentoservice.NovoServico(repo, 0)
	exigirSemErro(t, err)
	armazenador := &armazenadorFake{}
	conversor := &conversorFake{}

	casos := []struct {
		nome        string
		documentos  *documentoservice.Servico
		armazenador webservices.ArmazenadorObjetos
		conversor   webservices.ConversorPDF
	}{
		{"servico de documentos nulo", nil, armazenador, conversor},
		{"armazenador nulo", docservico, nil, conversor},
		{"conversor nulo", docservico, armazenador, nil},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			_, err := webservices.NovoServicoDocumento(caso.documentos, caso.armazenador, caso.conversor)
			var nulo *errors.ErroArgumentoNulo
			if !errors.Como(err, &nulo) {
				t.Fatalf("esperava ErroArgumentoNulo, obteve %T: %v", err, err)
			}
		})
	}
}

func TestIngerirDocxValidoSalvaConverteERegistraNaOrdemCorreta(t *testing.T) {
	t.Parallel()
	servico, repo, armazenador, conversor, reg := novoServicoDeTeste(t)
	dono := donoDeTeste(t)
	conteudo := docxValido(t)

	resposta, err := servico.Ingerir(context.Background(), dono, "artigo.docx", conteudo)
	exigirSemErro(t, err)

	if resposta.Status != "recebido" {
		t.Fatalf("esperava status recebido, obteve %q", resposta.Status)
	}
	if !resposta.TemPreview {
		t.Fatal("esperava TemPreview true após ingestão completa")
	}
	if resposta.NomeOriginal != "artigo.docx" {
		t.Fatalf("nome original divergente: %q", resposta.NomeOriginal)
	}
	if resposta.Formato != string(vo.FormatoDocx) {
		t.Fatalf("formato divergente: %q", resposta.Formato)
	}
	if resposta.ID == uuid.Nil {
		t.Fatal("esperava um ID atribuído")
	}
	if resposta.TamanhoBytes != int64(len(conteudo)) {
		t.Fatalf("tamanho divergente: %d", resposta.TamanhoBytes)
	}

	// repo.ObterPorID antes da escrita NÃO é desperdício e não deve ser
	// "otimizado" para fora: é o guarda que o domínio impõe em
	// documentoservice.RegistrarPreviewPDF. Sem ele, dono vazio e ID vazio
	// deixam de ser ErroValidacao, e o preview de um terceiro passa a ALCANÇAR
	// a escrita (o UPDATE filtra dono e não afeta linha, mas a tentativa
	// acontece). Quatro testes de internal/domain/documento/service quebram se
	// alguém remover aquela leitura.
	esperado := []string{
		"storage.Salvar:original",
		"repo.Inserir",
		"conversor.ConverterParaPDF",
		"storage.Salvar:pdf",
		"repo.ObterPorID",
		"repo.DefinirChavePreviewPDF",
	}
	if len(reg.eventos) != len(esperado) {
		t.Fatalf("ordem de eventos inesperada: %v", reg.eventos)
	}
	for i, nome := range esperado {
		if reg.eventos[i] != nome {
			t.Fatalf("evento %d: esperava %q, obteve %q (sequência completa: %v)", i, nome, reg.eventos[i], reg.eventos)
		}
	}

	if len(armazenador.chamadasSalvar) != 2 {
		t.Fatalf("esperava 2 chamadas a Salvar, obteve %d", len(armazenador.chamadasSalvar))
	}
	if repo.inseridos != 1 || repo.previewsDefinidos != 1 {
		t.Fatalf("esperava 1 inserção e 1 definição de preview, obteve %d/%d", repo.inseridos, repo.previewsDefinidos)
	}
	if !conversor.chamado || conversor.entradaTamanho != len(conteudo) {
		t.Fatal("conversor não recebeu o conteúdo original")
	}
}

func TestIngerirRecusaAntesDeQualquerIO(t *testing.T) {
	t.Parallel()
	casos := []struct {
		nome     string
		conteudo []byte
	}{
		{"nao eh zip nem pdf", []byte("isto nao e nada reconhecivel como docx")},
		{"prefixo pdf", append([]byte("%PDF-1.7"), bytes.Repeat([]byte{0}, 100)...)},
		{"zip sem document.xml principal", zipComPartes(t, map[string]string{vo.ParteContentTypes: `<?xml version="1.0"?><Types/>`})},
		{"conteudo vazio", nil},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			servico, repo, armazenador, _, _ := novoServicoDeTeste(t)
			_, err := servico.Ingerir(context.Background(), donoDeTeste(t), "artigo.docx", caso.conteudo)
			exigirValidacao(t, err)
			if len(armazenador.chamadasSalvar) != 0 {
				t.Fatal("Salvar foi chamado apesar da validação ter falhado")
			}
			if repo.inseridos != 0 {
				t.Fatal("Inserir foi chamado apesar da validação ter falhado")
			}
		})
	}
}

func TestIngerirConteudoAcimaDoTetoRecusaNoTamanho(t *testing.T) {
	t.Parallel()
	servico, repo, armazenador, _, _ := novoServicoDeTeste(t)
	grande := append(docxValido(t), bytes.Repeat([]byte{0}, int(documentoservice.TamanhoMaximoPadraoBytes))...)

	_, err := servico.Ingerir(context.Background(), donoDeTeste(t), "artigo.docx", grande)
	var validacao *errors.ErroValidacao
	if !errors.Como(err, &validacao) {
		t.Fatalf("esperava ErroValidacao, obteve %T: %v", err, err)
	}
	temCampoTamanho := false
	for _, campo := range validacao.Campos {
		if campo.Campo == "tamanho_bytes" {
			temCampoTamanho = true
		}
	}
	if !temCampoTamanho {
		t.Fatalf("esperava campo tamanho_bytes reprovado, obteve %v", validacao.Campos)
	}
	if len(armazenador.chamadasSalvar) != 0 || repo.inseridos != 0 {
		t.Fatal("arquivo acima do teto chegou ao I/O")
	}
}

func TestIngerirDonoVazioNaoFazIO(t *testing.T) {
	t.Parallel()
	servico, repo, armazenador, _, _ := novoServicoDeTeste(t)

	_, err := servico.Ingerir(context.Background(), vo.Dono{}, "artigo.docx", docxValido(t))
	exigirValidacao(t, err)
	if len(armazenador.chamadasSalvar) != 0 || repo.inseridos != 0 {
		t.Fatal("dono vazio chegou ao I/O")
	}
}

func TestIngerirSalvarOriginalFalhaNaoRegistraNemConverte(t *testing.T) {
	t.Parallel()
	servico, repo, armazenador, conversor, _ := novoServicoDeTeste(t)
	armazenador.erroSalvarOriginal = errors.NovoErroAplicacao("storage indisponível")

	_, err := servico.Ingerir(context.Background(), donoDeTeste(t), "artigo.docx", docxValido(t))
	if err == nil {
		t.Fatal("esperava erro")
	}
	if repo.inseridos != 0 {
		t.Fatal("Inserir foi chamado apesar da falha ao salvar o original")
	}
	if conversor.chamado {
		t.Fatal("conversor foi chamado apesar da falha ao salvar o original")
	}
	if len(armazenador.chamadasSalvar) != 1 {
		t.Fatalf("esperava 1 tentativa de Salvar, obteve %d", len(armazenador.chamadasSalvar))
	}
}

func TestIngerirConversaoFalhaMasDocumentoJaFoiInserido(t *testing.T) {
	t.Parallel()
	servico, repo, armazenador, conversor, _ := novoServicoDeTeste(t)
	conversor.erro = errors.NovoErroAplicacao("conversor indisponível")

	_, err := servico.Ingerir(context.Background(), donoDeTeste(t), "artigo.docx", docxValido(t))
	if err == nil {
		t.Fatal("esperava erro")
	}
	if repo.inseridos != 1 {
		t.Fatalf("esperava documento já inserido antes da conversão falhar, obteve %d inserções", repo.inseridos)
	}
	if repo.previewsDefinidos != 0 {
		t.Fatal("preview não deveria ter sido definido após falha na conversão")
	}
	if len(armazenador.chamadasSalvar) != 1 {
		t.Fatalf("esperava só o original salvo, obteve %d chamadas", len(armazenador.chamadasSalvar))
	}
}

func TestIngerirSalvarPDFFalhaNaoDefinePreview(t *testing.T) {
	t.Parallel()
	servico, repo, armazenador, conversor, _ := novoServicoDeTeste(t)
	armazenador.erroSalvarPDF = errors.NovoErroAplicacao("storage indisponível para o pdf")

	_, err := servico.Ingerir(context.Background(), donoDeTeste(t), "artigo.docx", docxValido(t))
	if err == nil {
		t.Fatal("esperava erro")
	}
	if repo.inseridos != 1 {
		t.Fatalf("esperava documento inserido, obteve %d inserções", repo.inseridos)
	}
	if repo.previewsDefinidos != 0 {
		t.Fatal("preview não deveria ter sido definido após falha ao salvar o pdf")
	}
	if !conversor.chamado {
		t.Fatal("conversor deveria ter sido chamado antes de tentar salvar o pdf")
	}
	if len(armazenador.chamadasSalvar) != 2 {
		t.Fatalf("esperava original e pdf tentados, obteve %d chamadas", len(armazenador.chamadasSalvar))
	}
}

func TestObterDeDonoDiferenteNaoEncontrado(t *testing.T) {
	t.Parallel()
	servico, repo, _, _, _ := novoServicoDeTeste(t)
	dono := donoDeTeste(t)
	outroDono := donoDeTeste(t)
	documento := documentoDeTeste(t, dono)
	repo.documentos = map[uuid.UUID]entity.Documento{documento.ID: documento}

	_, err := servico.Obter(context.Background(), outroDono, documento.ID)
	exigirNaoEncontrado(t, err)
}

func TestURLPreviewFeliz(t *testing.T) {
	t.Parallel()
	servico, repo, armazenador, _, _ := novoServicoDeTeste(t)
	dono := donoDeTeste(t)
	documento := documentoComPreviewDeTeste(t, dono)
	repo.documentos = map[uuid.UUID]entity.Documento{documento.ID: documento}
	armazenador.urlRetorno = "https://storage.exemplo/preview-assinado"

	resposta, err := servico.URLPreview(context.Background(), dono, documento.ID)
	exigirSemErro(t, err)

	if resposta.URL != "https://storage.exemplo/preview-assinado" {
		t.Fatalf("URL divergente: %q", resposta.URL)
	}
	if !armazenador.urlChamada {
		t.Fatal("esperava que URLPreAssinada fosse chamado")
	}
	if armazenador.urlValidade != 15*time.Minute {
		t.Fatalf("esperava validade de 15 minutos, obteve %v", armazenador.urlValidade)
	}
	chavePreview, err := vo.NovaChavePreviewPDF(documento.ID)
	exigirSemErro(t, err)
	if armazenador.urlChave != chavePreview {
		t.Fatalf("esperava a chave do preview, obteve %q", armazenador.urlChave)
	}

	esperado := time.Now().Add(15 * time.Minute)
	diferenca := resposta.ExpiraEm.Sub(esperado)
	if diferenca < -5*time.Second || diferenca > 5*time.Second {
		t.Fatalf("ExpiraEm fora da tolerância: %v (diferença %v)", resposta.ExpiraEm, diferenca)
	}
}

func TestURLPreviewSemPreviewConflito(t *testing.T) {
	t.Parallel()
	servico, repo, armazenador, _, _ := novoServicoDeTeste(t)
	dono := donoDeTeste(t)
	documento := documentoDeTeste(t, dono) // sem ChaveStoragePDF
	repo.documentos = map[uuid.UUID]entity.Documento{documento.ID: documento}

	_, err := servico.URLPreview(context.Background(), dono, documento.ID)
	exigirConflito(t, err)
	if armazenador.urlChamada {
		t.Fatal("URLPreAssinada não deveria ter sido chamado sem preview")
	}
}

func TestURLPreviewDonoErradoNaoEncontrado(t *testing.T) {
	t.Parallel()
	servico, repo, armazenador, _, _ := novoServicoDeTeste(t)
	dono := donoDeTeste(t)
	outroDono := donoDeTeste(t)
	documento := documentoComPreviewDeTeste(t, dono)
	repo.documentos = map[uuid.UUID]entity.Documento{documento.ID: documento}

	_, err := servico.URLPreview(context.Background(), outroDono, documento.ID)
	exigirNaoEncontrado(t, err)
	if armazenador.urlChamada {
		t.Fatal("URLPreAssinada não deveria ter sido chamado para dono alheio")
	}
}
