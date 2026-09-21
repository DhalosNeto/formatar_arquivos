// Este arquivo cobre internal/infra/fila.ExecutorDocumento.
//
// renderizar_preview já está implementado: os testes dele aqui são
// NÃO-REGRESSÃO (ficha do investigador) e devem passar hoje, sem mudança de
// produção.
//
// analisar (entity.TipoAnalisar) é RED: a ficha do investigador descreve o
// fluxo esperado (IniciarAnalise -> armazenador.Obter -> ooxml.AnalisarEstrutura
// -> cdm.NovoIndice -> Serializar -> ConcluirAnalise, com MarcarFalha em
// QUALQUER etapa que falhar) mas ExecutorDocumento.Executar hoje só aceita
// entity.TipoRenderizarPreview — todo teste de entity.TipoAnalisar abaixo
// falha por asserção (o executor devolve "tipo de job não suportado") até o
// codador implementar o dispatch. Nenhum símbolo novo é necessário para
// compilar este arquivo: ExecutorDocumento, entity.TipoAnalisar,
// processamento.ServicoInterno e repository.DocumentoInternoRepo já existem.
package fila

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	documentoentity "github.com/daniel-halos/formatador/internal/domain/documento/entity"
	"github.com/daniel-halos/formatador/internal/domain/documento/processamento"
	"github.com/daniel-halos/formatador/internal/domain/documento/repository"
	"github.com/daniel-halos/formatador/internal/domain/job/entity"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	infraerrors "github.com/daniel-halos/formatador/internal/infra/errors"
)

// ---------------------------------------------------------------------------
// Dublês: DocumentoInternoRepo, ArmazenadorObjetos e ConversorPDF.
// Mesmo espírito de repositorioInternoFake em
// internal/domain/documento/processamento/servico_test.go, mas local a este
// pacote — os dois arquivos de teste não compartilham dublê hoje.
// ---------------------------------------------------------------------------

type repoInternoFakeExecutor struct {
	documentos     map[uuid.UUID]documentoentity.Documento
	erroObter      error
	erroDefinirCDM error
	chamadasDefCDM int
}

func novoRepoInternoFakeExecutor(doc documentoentity.Documento) *repoInternoFakeExecutor {
	return &repoInternoFakeExecutor{documentos: map[uuid.UUID]documentoentity.Documento{doc.ID: doc}}
}

func (r *repoInternoFakeExecutor) ObterPorIDInterno(_ context.Context, id uuid.UUID) (documentoentity.Documento, error) {
	if r.erroObter != nil {
		return documentoentity.Documento{}, r.erroObter
	}
	doc, existe := r.documentos[id]
	if !existe {
		return documentoentity.Documento{}, infraerrors.NovoErroNaoEncontrado("documento")
	}
	return doc, nil
}

func (r *repoInternoFakeExecutor) AtualizarStatus(_ context.Context, id uuid.UUID, statusAtual, novoStatus documentoentity.Status) error {
	doc, existe := r.documentos[id]
	if !existe {
		return infraerrors.NovoErroNaoEncontrado("documento")
	}
	if doc.Status != statusAtual {
		return infraerrors.NovoErroConflito("status do documento mudou")
	}
	doc.Status = novoStatus
	r.documentos[id] = doc
	return nil
}

func (r *repoInternoFakeExecutor) DefinirCDM(_ context.Context, id uuid.UUID, cdm json.RawMessage, statusAtual, novoStatus documentoentity.Status) error {
	r.chamadasDefCDM++
	if r.erroDefinirCDM != nil {
		return r.erroDefinirCDM
	}
	doc, existe := r.documentos[id]
	if !existe {
		return infraerrors.NovoErroNaoEncontrado("documento")
	}
	if doc.Status != statusAtual {
		return infraerrors.NovoErroConflito("status do documento mudou")
	}
	doc.Status = novoStatus
	doc.CDM = append(json.RawMessage(nil), cdm...)
	r.documentos[id] = doc
	return nil
}

func (r *repoInternoFakeExecutor) status(id uuid.UUID) documentoentity.Status {
	return r.documentos[id].Status
}

var _ repository.DocumentoInternoRepo = (*repoInternoFakeExecutor)(nil)

// chamadaSalvarExecutor registra o que armazenadorExecutorFake.Salvar recebeu.
type chamadaSalvarExecutor struct {
	chave       vo.ChaveStorage
	conteudo    []byte
	contentType string
}

type armazenadorExecutorFake struct {
	conteudo   []byte
	erroObter  error
	erroSalvar error
	salvos     []chamadaSalvarExecutor
}

func (a *armazenadorExecutorFake) Obter(context.Context, vo.ChaveStorage) (io.ReadCloser, error) {
	if a.erroObter != nil {
		return nil, a.erroObter
	}
	return io.NopCloser(bytes.NewReader(a.conteudo)), nil
}

func (a *armazenadorExecutorFake) Salvar(_ context.Context, chave vo.ChaveStorage, conteudo io.Reader, _ int64, contentType string) error {
	dados, err := io.ReadAll(conteudo)
	if err != nil {
		return err
	}
	if a.erroSalvar != nil {
		return a.erroSalvar
	}
	a.salvos = append(a.salvos, chamadaSalvarExecutor{chave: chave, conteudo: dados, contentType: contentType})
	return nil
}

var _ ArmazenadorObjetos = (*armazenadorExecutorFake)(nil)

type conversorExecutorFake struct {
	saida          []byte
	erro           error
	chamado        bool
	entradaTamanho int
}

func (c *conversorExecutorFake) ConverterParaPDF(_ context.Context, docx []byte) ([]byte, error) {
	c.chamado = true
	c.entradaTamanho = len(docx)
	if c.erro != nil {
		return nil, c.erro
	}
	if c.saida == nil {
		return []byte("%PDF-1.4 fake"), nil
	}
	return c.saida, nil
}

var _ ConversorPDF = (*conversorExecutorFake)(nil)

// ---------------------------------------------------------------------------
// Montagem de documento e docx sintético
// ---------------------------------------------------------------------------

func documentoDeTesteExecutor(t *testing.T, status documentoentity.Status) documentoentity.Documento {
	t.Helper()
	dono, err := vo.NovoDonoSessao(uuid.New())
	require.NoError(t, err)
	documento, err := documentoentity.NovoDocumento(dono, "artigo.docx", vo.FormatoDocx, 4096)
	require.NoError(t, err)
	documento.Status = status
	return documento
}

// montarDocxMinimoExecutor monta um pacote docx mínimo, um parágrafo por
// texto informado, na ordem dada.
func montarDocxMinimoExecutor(t *testing.T, paragrafos ...string) []byte {
	t.Helper()

	var corpo strings.Builder
	for _, texto := range paragrafos {
		var escapado bytes.Buffer
		require.NoError(t, xml.EscapeText(&escapado, []byte(texto)))
		corpo.WriteString("<w:p><w:r><w:t>" + escapado.String() + "</w:t></w:r></w:p>")
	}
	documentoXML := []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` + corpo.String() + `</w:body></w:document>`)

	contentTypes := []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`)
	rels := []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`)

	var buf bytes.Buffer
	escritor := zip.NewWriter(&buf)
	for _, entrada := range []struct {
		nome     string
		conteudo []byte
	}{
		{"[Content_Types].xml", contentTypes},
		{"_rels/.rels", rels},
		{"word/document.xml", documentoXML},
	} {
		parte, err := escritor.Create(entrada.nome)
		require.NoError(t, err)
		_, err = parte.Write(entrada.conteudo)
		require.NoError(t, err)
	}
	require.NoError(t, escritor.Close())
	return buf.Bytes()
}

// lerFixtureExecutor lê um arquivo de backend/testdata/. Este pacote roda com
// cwd em backend/internal/infra/fila, três níveis abaixo de backend/ — mesma
// profundidade de internal/infra/ooxml.
func lerFixtureExecutor(t *testing.T, nome string) []byte {
	t.Helper()
	caminho := filepath.Join("..", "..", "..", "testdata", nome)
	dados, err := os.ReadFile(caminho)
	require.NoError(t, err, "fixture %q precisa existir em backend/testdata/", nome)
	return dados
}

func executorDeTesteExecutor(
	t *testing.T,
	repo *repoInternoFakeExecutor,
	armazenador *armazenadorExecutorFake,
	conversor *conversorExecutorFake,
) *ExecutorDocumento {
	t.Helper()
	servicoInterno, err := processamento.NovoServicoInterno(repo)
	require.NoError(t, err)
	executor, err := NovoExecutorDocumento(servicoInterno, armazenador, conversor)
	require.NoError(t, err)
	return executor
}

// ---------------------------------------------------------------------------
// Tipo desconhecido: nunca sucesso silencioso.
// ---------------------------------------------------------------------------

func TestExecutarTipoDesconhecidoDevolveErroSemMudarODocumento(t *testing.T) {
	t.Parallel()
	documento := documentoDeTesteExecutor(t, documentoentity.StatusRecebido)
	repo := novoRepoInternoFakeExecutor(documento)
	executor := executorDeTesteExecutor(t, repo, &armazenadorExecutorFake{}, &conversorExecutorFake{})

	job := entity.Job{ID: uuid.New(), DocumentoID: documento.ID, Tipo: entity.TipoFormatar, Status: entity.StatusExecutando}
	resultado, err := executor.Executar(context.Background(), job)

	require.Error(t, err)
	assert.Nil(t, resultado)
	assert.Equal(t, documentoentity.StatusRecebido, repo.status(documento.ID),
		"tipo não suportado não pode mudar o status do documento")
}

// ---------------------------------------------------------------------------
// renderizar_preview: NÃO-REGRESSÃO — já implementado, precisa continuar
// funcionando igual depois que o dispatch ganhar entity.TipoAnalisar.
// ---------------------------------------------------------------------------

func TestExecutarRenderizarPreviewFelizContinuaFuncionando(t *testing.T) {
	t.Parallel()
	documento := documentoDeTesteExecutor(t, documentoentity.StatusRecebido)
	repo := novoRepoInternoFakeExecutor(documento)
	armazenador := &armazenadorExecutorFake{conteudo: montarDocxMinimoExecutor(t, "conteúdo do documento")}
	conversor := &conversorExecutorFake{saida: []byte("%PDF-1.4 conteudo-convertido")}
	executor := executorDeTesteExecutor(t, repo, armazenador, conversor)

	job := entity.Job{ID: uuid.New(), DocumentoID: documento.ID, Tipo: entity.TipoRenderizarPreview, Status: entity.StatusExecutando}
	resultado, err := executor.Executar(context.Background(), job)
	require.NoError(t, err)

	var corpo struct {
		ChavePreviewPDF string `json:"chave_preview_pdf"`
	}
	require.NoError(t, json.Unmarshal(resultado, &corpo))
	chaveEsperada, err := vo.NovaChavePreviewPDF(documento.ID)
	require.NoError(t, err)
	assert.Equal(t, chaveEsperada.String(), corpo.ChavePreviewPDF)

	require.Len(t, armazenador.salvos, 1)
	assert.Equal(t, "%PDF-1.4 conteudo-convertido", string(armazenador.salvos[0].conteudo))
	assert.Equal(t, vo.MIMEPDF, armazenador.salvos[0].contentType)
	assert.True(t, conversor.chamado)
	assert.Equal(t, len(armazenador.conteudo), conversor.entradaTamanho)
}

func TestExecutarRenderizarPreviewFalhaEmCadaEtapaDevolveErro(t *testing.T) {
	t.Parallel()
	casos := []struct {
		nome    string
		prepara func(*armazenadorExecutorFake, *conversorExecutorFake)
	}{
		{"obter original falha", func(a *armazenadorExecutorFake, _ *conversorExecutorFake) {
			a.erroObter = infraerrors.NovoErroAplicacao("storage indisponível")
		}},
		{"converter falha", func(_ *armazenadorExecutorFake, c *conversorExecutorFake) {
			c.erro = infraerrors.NovoErroAplicacao("conversor indisponível")
		}},
		{"salvar preview falha", func(a *armazenadorExecutorFake, _ *conversorExecutorFake) {
			a.erroSalvar = infraerrors.NovoErroAplicacao("storage indisponível ao salvar")
		}},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()
			documento := documentoDeTesteExecutor(t, documentoentity.StatusRecebido)
			repo := novoRepoInternoFakeExecutor(documento)
			armazenador := &armazenadorExecutorFake{conteudo: montarDocxMinimoExecutor(t, "conteúdo")}
			conversor := &conversorExecutorFake{}
			caso.prepara(armazenador, conversor)
			executor := executorDeTesteExecutor(t, repo, armazenador, conversor)

			job := entity.Job{ID: uuid.New(), DocumentoID: documento.ID, Tipo: entity.TipoRenderizarPreview, Status: entity.StatusExecutando}
			resultado, err := executor.Executar(context.Background(), job)

			require.Error(t, err)
			assert.Nil(t, resultado)
		})
	}
}

// ---------------------------------------------------------------------------
// analisar: RED — dispatch ainda não existe; estes testes falham por
// asserção contra o "tipo de job não suportado" atual, não por símbolo
// indefinido (todos os símbolos usados já existem).
// ---------------------------------------------------------------------------

func TestExecutarAnalisarFelizGravaCDMEConcluiAnalise(t *testing.T) {
	t.Parallel()
	documento := documentoDeTesteExecutor(t, documentoentity.StatusRecebido)
	repo := novoRepoInternoFakeExecutor(documento)
	armazenador := &armazenadorExecutorFake{conteudo: lerFixtureExecutor(t, "artigo-real-libreoffice.docx")}
	executor := executorDeTesteExecutor(t, repo, armazenador, &conversorExecutorFake{})

	job := entity.Job{ID: uuid.New(), DocumentoID: documento.ID, Tipo: entity.TipoAnalisar, Status: entity.StatusExecutando}
	resultado, err := executor.Executar(context.Background(), job)
	require.NoError(t, err)

	// Regra 7: o resultado do JOB (json.RawMessage devolvido, que vai para a
	// coluna jobs.resultado) não carrega conteúdo do documento — no máximo a
	// contagem de blocos. O CDM completo (com texto_resumo) é gravado à parte,
	// no documento, por ConcluirAnalise — não aqui.
	var corpo map[string]any
	require.NoError(t, json.Unmarshal(resultado, &corpo))
	contagem, ok := corpo["blocos"].(float64)
	require.Truef(t, ok, "esperava campo \"blocos\" numérico no resultado do job, obteve %#v", corpo)
	assert.Equal(t, float64(40), contagem)
	assert.NotContainsf(t, string(resultado), "PERCEPÇÃO DE ESTUDANTES",
		"resultado do job de análise não pode carregar texto do documento (CLAUDE.md regra 7): %s", resultado)

	assert.Equal(t, documentoentity.StatusAnalisado, repo.status(documento.ID))
	assert.NotEmpty(t, repo.documentos[documento.ID].CDM, "ConcluirAnalise deveria ter gravado o CDM no documento")
	assert.Equal(t, 1, repo.chamadasDefCDM)
}

func TestExecutarAnalisarStorageFalhaMarcaFalhaEDevolveErro(t *testing.T) {
	t.Parallel()
	documento := documentoDeTesteExecutor(t, documentoentity.StatusRecebido)
	repo := novoRepoInternoFakeExecutor(documento)
	armazenador := &armazenadorExecutorFake{erroObter: infraerrors.NovoErroAplicacao("storage indisponível")}
	executor := executorDeTesteExecutor(t, repo, armazenador, &conversorExecutorFake{})

	job := entity.Job{ID: uuid.New(), DocumentoID: documento.ID, Tipo: entity.TipoAnalisar, Status: entity.StatusExecutando}
	resultado, err := executor.Executar(context.Background(), job)

	require.Error(t, err)
	assert.Nil(t, resultado)
	assert.Equal(t, documentoentity.StatusFalhou, repo.status(documento.ID),
		"falha do storage não pode deixar o documento preso em analisando")
}

func TestExecutarAnalisarBytesQueNaoSaoDocxMarcaFalhaEDevolveErro(t *testing.T) {
	t.Parallel()
	documento := documentoDeTesteExecutor(t, documentoentity.StatusRecebido)
	repo := novoRepoInternoFakeExecutor(documento)
	armazenador := &armazenadorExecutorFake{conteudo: []byte("isto não é um docx, só texto solto")}
	executor := executorDeTesteExecutor(t, repo, armazenador, &conversorExecutorFake{})

	job := entity.Job{ID: uuid.New(), DocumentoID: documento.ID, Tipo: entity.TipoAnalisar, Status: entity.StatusExecutando}
	resultado, err := executor.Executar(context.Background(), job)

	require.Error(t, err)
	assert.Nil(t, resultado)
	assert.Equal(t, documentoentity.StatusFalhou, repo.status(documento.ID),
		"documento que não é um docx válido não pode deixar o documento preso em analisando")
}

func TestExecutarAnalisarFalhaAoGravarCDMMarcaFalhaSemVazarConteudo(t *testing.T) {
	t.Parallel()
	documento := documentoDeTesteExecutor(t, documentoentity.StatusRecebido)
	repo := novoRepoInternoFakeExecutor(documento)
	repo.erroDefinirCDM = infraerrors.NovoErroAplicacao("banco indisponível ao gravar o cdm")

	segredo := "TRECHO-CONFIDENCIAL-DO-ARTIGO-DO-USUARIO-9f2c716a"
	armazenador := &armazenadorExecutorFake{conteudo: montarDocxMinimoExecutor(t, segredo, "outro parágrafo qualquer")}
	executor := executorDeTesteExecutor(t, repo, armazenador, &conversorExecutorFake{})

	job := entity.Job{ID: uuid.New(), DocumentoID: documento.ID, Tipo: entity.TipoAnalisar, Status: entity.StatusExecutando}
	resultado, err := executor.Executar(context.Background(), job)

	require.Error(t, err)
	assert.Nil(t, resultado)
	assert.Equal(t, documentoentity.StatusFalhou, repo.status(documento.ID),
		"falha ao gravar o cdm não pode deixar o documento preso em analisando")
	assert.NotContainsf(t, err.Error(), segredo,
		"erro de falha ao gravar o cdm não pode vazar conteúdo do documento do usuário: %v", err)
}

func TestExecutarAnalisarResultadoNaoVazaConteudoDoDocumento(t *testing.T) {
	t.Parallel()
	documento := documentoDeTesteExecutor(t, documentoentity.StatusRecebido)
	repo := novoRepoInternoFakeExecutor(documento)
	segredo := "TRECHO-CONFIDENCIAL-DO-ARTIGO-DO-USUARIO-9f2c716a"
	armazenador := &armazenadorExecutorFake{conteudo: montarDocxMinimoExecutor(t, segredo)}
	executor := executorDeTesteExecutor(t, repo, armazenador, &conversorExecutorFake{})

	job := entity.Job{ID: uuid.New(), DocumentoID: documento.ID, Tipo: entity.TipoAnalisar, Status: entity.StatusExecutando}
	resultado, err := executor.Executar(context.Background(), job)
	require.NoError(t, err)

	assert.NotContainsf(t, string(resultado), segredo,
		"resultado do job de análise não pode conter conteúdo do documento (CLAUDE.md regra 7): %s", resultado)
}
