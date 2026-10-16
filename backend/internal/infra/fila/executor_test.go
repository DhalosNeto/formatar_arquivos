// Testes de preview e análise estrutural do ExecutorDocumento, incluindo
// preservação das causas e confidencialidade quando a marcação de falha falha.
package fila

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-halos/formatador/internal/domain/cdm"
	documentoentity "github.com/daniel-halos/formatador/internal/domain/documento/entity"
	"github.com/daniel-halos/formatador/internal/domain/documento/processamento"
	"github.com/daniel-halos/formatador/internal/domain/documento/repository"
	documentoservice "github.com/daniel-halos/formatador/internal/domain/documento/service"
	"github.com/daniel-halos/formatador/internal/domain/formatador"
	"github.com/daniel-halos/formatador/internal/domain/job/entity"
	"github.com/daniel-halos/formatador/internal/domain/ruleset"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	infraerrors "github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/daniel-halos/formatador/internal/infra/log"
	"github.com/daniel-halos/formatador/internal/infra/ooxml"
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

type repoFalhaExecutor struct {
	*repoInternoFakeExecutor
	erroMarcarFalha error
	erroLerFalha    error
	leituras        int
	marcacoes       int
}

func (repositorio *repoFalhaExecutor) ObterPorIDInterno(ctx context.Context, id uuid.UUID) (documentoentity.Documento, error) {
	repositorio.leituras++
	if repositorio.leituras == 3 && repositorio.erroLerFalha != nil {
		return documentoentity.Documento{}, repositorio.erroLerFalha
	}
	return repositorio.repoInternoFakeExecutor.ObterPorIDInterno(ctx, id)
}

func (repositorio *repoFalhaExecutor) AtualizarStatus(ctx context.Context, id uuid.UUID, statusAtual, novoStatus documentoentity.Status) error {
	if novoStatus == documentoentity.StatusFalhou {
		repositorio.marcacoes++
		if repositorio.erroMarcarFalha != nil {
			return repositorio.erroMarcarFalha
		}
	}
	return repositorio.repoInternoFakeExecutor.AtualizarStatus(ctx, id, statusAtual, novoStatus)
}

func TestNovoExecutorRecusaRegistradorNulo(t *testing.T) {
	documento := documentoDeTesteExecutor(t, documentoentity.StatusRecebido)
	servico, err := processamento.NovoServicoInterno(novoRepoInternoFakeExecutor(documento))
	require.NoError(t, err)
	executor, err := NovoExecutorDocumento(servico, &armazenadorExecutorFake{}, &conversorExecutorFake{}, &rulesetsExecutorFake{}, nil)
	assert.Nil(t, executor)
	var argumento *infraerrors.ErroArgumentoNulo
	require.ErrorAs(t, err, &argumento)
}

func TestExecutarAnalisarPreservaCausasAoMarcarFalha(t *testing.T) {
	for _, etapa := range []string{"persistência funciona", "leitura falha", "CAS falha"} {
		t.Run(etapa, func(t *testing.T) {
			var saida bytes.Buffer
			registrador := log.Novo("debug", &saida)
			falhaPersistencia := etapa != "persistência funciona"
			documento := documentoDeTesteExecutor(t, documentoentity.StatusRecebido)
			erroOriginal := infraerrors.NovoErroConflito("TRECHO-SIGILOSO-ORIGINAL")
			erroPersistencia := infraerrors.NovoErroAplicacao("TRECHO-SIGILOSO-PERSISTENCIA")
			repo := &repoFalhaExecutor{repoInternoFakeExecutor: novoRepoInternoFakeExecutor(documento)}
			switch etapa {
			case "CAS falha":
				repo.erroMarcarFalha = erroPersistencia
			case "leitura falha":
				repo.erroLerFalha = erroPersistencia
			}
			servico, err := processamento.NovoServicoInterno(repo)
			require.NoError(t, err)
			executor, err := NovoExecutorDocumento(servico, &armazenadorExecutorFake{erroObter: erroOriginal}, &conversorExecutorFake{}, &rulesetsExecutorFake{}, registrador)
			require.NoError(t, err)

			resultado, err := executor.Executar(context.Background(), entity.Job{DocumentoID: documento.ID, Tipo: entity.TipoAnalisar})

			require.Error(t, err)
			assert.Nil(t, resultado)
			assert.ErrorIs(t, err, erroOriginal)
			assert.Equal(t, 3, repo.leituras)
			if etapa == "leitura falha" {
				assert.Zero(t, repo.marcacoes)
			} else {
				assert.Equal(t, 1, repo.marcacoes)
			}
			var causaOriginal *infraerrors.ErroConflito
			require.ErrorAs(t, err, &causaOriginal)
			assert.Same(t, erroOriginal, causaOriginal)
			if falhaPersistencia {
				assert.ErrorIs(t, err, erroPersistencia)
				var causaPersistencia *infraerrors.ErroAplicacao
				require.ErrorAs(t, err, &causaPersistencia)
				assert.Same(t, erroPersistencia, causaPersistencia)
				var composto *infraerrors.ErroPersistirFalha
				ok := infraerrors.Como(err, &composto)
				require.True(t, ok)
				require.Len(t, composto.Unwrap(), 2)
				assert.ErrorIs(t, composto.Unwrap()[0], erroOriginal)
				assert.ErrorIs(t, composto.Unwrap()[1], erroPersistencia)
				assert.EqualError(t, err, "falha ao persistir estado de falha do documento")
				assert.NotContains(t, err.Error(), erroOriginal.Error())
				assert.NotContains(t, err.Error(), erroPersistencia.Error())
				assert.Equal(t, documentoentity.StatusAnalisando, repo.status(documento.ID))
				var registro map[string]any
				require.NoError(t, json.Unmarshal(saida.Bytes(), &registro))
				assert.Equal(t, "falha ao persistir estado de falha do documento", registro["msg"])
				assert.Equal(t, "ERROR", registro["level"])
			} else {
				assert.NotErrorIs(t, err, erroPersistencia)
				assert.Equal(t, documentoentity.StatusFalhou, repo.status(documento.ID))
				assert.Empty(t, saida.String())
			}
			assert.NotContains(t, saida.String(), erroOriginal.Error())
			assert.NotContains(t, saida.String(), erroPersistencia.Error())
		})
	}
}

func TestExecutarAnalisarNaoMarcaFalhaSemErroDeExtracao(t *testing.T) {
	t.Parallel()
	for _, nome := range []string{"sucesso", "erro ao iniciar"} {
		t.Run(nome, func(t *testing.T) {
			t.Parallel()
			documento := documentoDeTesteExecutor(t, documentoentity.StatusRecebido)
			repo := &repoFalhaExecutor{repoInternoFakeExecutor: novoRepoInternoFakeExecutor(documento)}
			erroInicio := infraerrors.NovoErroAplicacao("banco indisponível")
			if nome == "erro ao iniciar" {
				repo.erroObter = erroInicio
			}
			servico, err := processamento.NovoServicoInterno(repo)
			require.NoError(t, err)
			executor, err := NovoExecutorDocumento(servico, &armazenadorExecutorFake{conteudo: montarDocxMinimoExecutor(t, "texto")}, &conversorExecutorFake{}, &rulesetsExecutorFake{}, log.Novo("debug", io.Discard))
			require.NoError(t, err)
			resultado, err := executor.Executar(context.Background(), entity.Job{DocumentoID: documento.ID, Tipo: entity.TipoAnalisar})
			assert.Zero(t, repo.marcacoes)
			if nome == "erro ao iniciar" {
				assert.ErrorIs(t, err, erroInicio)
				assert.Nil(t, resultado)
				assert.Equal(t, 1, repo.leituras)
				assert.Equal(t, documentoentity.StatusRecebido, repo.status(documento.ID))
			} else {
				require.NoError(t, err)
				assert.NotEmpty(t, resultado)
				assert.Equal(t, 3, repo.leituras)
				assert.Equal(t, documentoentity.StatusAnalisado, repo.status(documento.ID))
			}
		})
	}
}

type classificadorExecutorFake struct{ chamadas int }

func (f *classificadorExecutorFake) Classificar(_ context.Context, blocos []cdm.Bloco) ([]cdm.JulgamentoEstrutura, error) {
	f.chamadas++
	respostas := make([]cdm.JulgamentoEstrutura, 0, len(blocos))
	for _, bloco := range blocos {
		respostas = append(respostas, cdm.JulgamentoEstrutura{RefXML: bloco.RefXML, Papel: cdm.Titulo, Confianca: .99})
	}
	return respostas, nil
}

func TestExecutarAnalisarPersisteIndiceComFallback(t *testing.T) {
	t.Parallel()
	doc := documentoDeTesteExecutor(t, documentoentity.StatusRecebido)
	repo := novoRepoInternoFakeExecutor(doc)
	servico, err := processamento.NovoServicoInterno(repo)
	require.NoError(t, err)
	classificador := &classificadorExecutorFake{}
	fallback, err := cdm.NovoFallback(classificador, cdm.PoliticaConfianca{LimiteConsulta: .9, LimiteConfirmacao: .8, LimiteAutomatico: .95})
	require.NoError(t, err)
	executor, err := NovoExecutorDocumento(servico, &armazenadorExecutorFake{conteudo: montarDocxMinimoExecutor(t, "texto ordinário")}, &conversorExecutorFake{}, &rulesetsExecutorFake{}, log.Novo("debug", io.Discard), fallback)
	require.NoError(t, err)
	_, err = executor.Executar(context.Background(), entity.Job{DocumentoID: doc.ID, Tipo: entity.TipoAnalisar})
	require.NoError(t, err)
	require.Equal(t, 1, classificador.chamadas)
	indice, err := cdm.Desserializar(repo.documentos[doc.ID].CDM)
	require.NoError(t, err)
	require.NotEmpty(t, indice.Blocos)
	require.Equal(t, cdm.OrigemLLM, indice.Blocos[0].Origem)
}

func TestExecutarPreviewFalhoNaoMarcaFalhaDoDocumento(t *testing.T) {
	t.Parallel()
	documento := documentoDeTesteExecutor(t, documentoentity.StatusRecebido)
	repo := &repoFalhaExecutor{repoInternoFakeExecutor: novoRepoInternoFakeExecutor(documento)}
	servico, err := processamento.NovoServicoInterno(repo)
	require.NoError(t, err)
	erroOriginal := infraerrors.NovoErroAplicacao("conversor indisponível")
	executor, err := NovoExecutorDocumento(servico, &armazenadorExecutorFake{}, &conversorExecutorFake{erro: erroOriginal}, &rulesetsExecutorFake{}, log.Novo("debug", io.Discard))
	require.NoError(t, err)

	resultado, err := executor.Executar(context.Background(), entity.Job{DocumentoID: documento.ID, Tipo: entity.TipoRenderizarPreview})

	assert.Nil(t, resultado)
	assert.ErrorIs(t, err, erroOriginal)
	assert.Zero(t, repo.marcacoes)
	assert.Equal(t, documentoentity.StatusRecebido, repo.status(documento.ID))
}

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
	// obtencoes existe para A6: "nada é baixado" precisa de contador, porque
	// um Obter que falha e um Obter que nunca aconteceu são indistinguíveis
	// pelo erro devolvido.
	obtencoes int
}

func (a *armazenadorExecutorFake) Obter(context.Context, vo.ChaveStorage) (io.ReadCloser, error) {
	a.obtencoes++
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
	executor, err := NovoExecutorDocumento(servicoInterno, armazenador, conversor, &rulesetsExecutorFake{}, log.Novo("debug", io.Discard))
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

	// Era entity.TipoFormatar até este recorte: agora que formatar é
	// suportado, o caso precisa de um tipo que o dispatch realmente não
	// conheça, senão a guarda de "nunca sucesso silencioso" ficaria sem prova.
	job := entity.Job{ID: uuid.New(), DocumentoID: documento.ID, Tipo: entity.TipoJob("tipo_que_nao_existe"), Status: entity.StatusExecutando}
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
// Análise estrutural: persistência do CDM e tratamento dos caminhos de falha.
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

// ===========================================================================
// RED do recorte f3-formatacao-ponta-a-ponta-worker: branch entity.TipoFormatar
// (C3), teto de leitura compartilhado (C4) e porta de perfil obrigatória (C6).
//
// Critérios cobertos: A5, A6, A7, A8, A9, A10, A11, A12, A13, A14, A16 e A17.
//
// ORDEM DOS PARÂMETROS DE NovoExecutorDocumento assumida por este arquivo:
//     (documentos, armazenador, conversor, rulesets, registrador, fallbacks...)
// A porta de perfil entra ANTES do registrador porque a posição variádica já
// é dos fallbacks e o registrador é o último não variádico hoje. C6 exige que
// seja obrigatória, não a posição; se o codador escolher outra, são as seis
// chamadas já existentes mais estas que mudam.
// ===========================================================================

// ---------------------------------------------------------------------------
// Dublê da porta de perfil (C6) e repositório com CONTADOR POR TRANSIÇÃO.
// ---------------------------------------------------------------------------

// rulesetsExecutorFake é a porta de leitura de perfil. ativo é explícito em
// cada caso: o padrão zero (false) é o caso de takedown de A5, e deixá-lo
// implícito faria um caso feliz passar por engano.
type rulesetsExecutorFake struct {
	definicao ruleset.Definicao
	ativo     bool
	erro      error
	chamadas  int
	recebidos []uuid.UUID
}

func (r *rulesetsExecutorFake) ObterPorID(_ context.Context, id uuid.UUID) (ruleset.Definicao, bool, error) {
	r.chamadas++
	r.recebidos = append(r.recebidos, id)
	if r.erro != nil {
		return ruleset.Definicao{}, false, r.erro
	}
	return r.definicao, r.ativo, nil
}

// repoFormatarFake conta LEITURAS e TRANSIÇÕES POR STATUS DE DESTINO.
//
// O executor fala com *processamento.ServicoInterno, que é concreto: o único
// ponto onde se pode contar "quantas vezes IniciarFormatacao/MarcarFalha
// foram chamadas" é o repositório que o serviço usa. Contar por status de
// destino é o que A5, A6 e A16 exigem — "não foi para formatado" seria frouxo.
//
// leituras é o segundo discriminador: uma transição recusada pela ENTIDADE
// (recebido -> formatando, por exemplo) não chega ao CAS, então o contador de
// transições sozinho não distinguiria "não chamou IniciarFormatacao" de
// "chamou e a entidade recusou". Cada transição do serviço faz uma leitura
// extra, então leituras fixa isso.
// cdmNaLeitura é o gancho de A18: na leitura de número N, o CDM PERSISTIDO
// passa a ser o informado, SEM mudar o status. É o que um job analisar
// concorrente deixa atrás de si ao reanalisar um documento já analisado
// (ConcluirAnalise -> DefinirCDM, analisado -> analisado), e é exatamente por
// isso que o CAS de IniciarFormatacao não detecta a corrida: o status é o
// mesmo nos dois lados. Determinístico por contagem de leitura, sem
// concorrência real no teste.
type repoFormatarFake struct {
	*repoInternoFakeExecutor
	leituras     int
	cdmNaLeitura map[int]json.RawMessage
	transicoes   map[documentoentity.Status]int
	erros        map[documentoentity.Status]error
}

func novoRepoFormatarFake(doc documentoentity.Documento) *repoFormatarFake {
	return &repoFormatarFake{
		repoInternoFakeExecutor: novoRepoInternoFakeExecutor(doc),
		transicoes:              map[documentoentity.Status]int{},
		erros:                   map[documentoentity.Status]error{},
	}
}

func (r *repoFormatarFake) ObterPorIDInterno(ctx context.Context, id uuid.UUID) (documentoentity.Documento, error) {
	r.leituras++
	if novo, trocar := r.cdmNaLeitura[r.leituras]; trocar {
		doc, existe := r.documentos[id]
		if !existe {
			return documentoentity.Documento{}, infraerrors.NovoErroNaoEncontrado("documento")
		}
		doc.CDM = append(json.RawMessage(nil), novo...)
		r.documentos[id] = doc
	}
	return r.repoInternoFakeExecutor.ObterPorIDInterno(ctx, id)
}

func (r *repoFormatarFake) AtualizarStatus(ctx context.Context, id uuid.UUID, statusAtual, novoStatus documentoentity.Status) error {
	r.transicoes[novoStatus]++
	if erro := r.erros[novoStatus]; erro != nil {
		return erro
	}
	return r.repoInternoFakeExecutor.AtualizarStatus(ctx, id, statusAtual, novoStatus)
}

var _ repository.DocumentoInternoRepo = (*repoFormatarFake)(nil)

// ---------------------------------------------------------------------------
// Perfil sintético. Medidas DISTINTAS das do arquivo de testdata, senão
// AplicarPagina não produz delta e A7 ficaria vacuoso.
// ---------------------------------------------------------------------------

func definicaoSinteticaExecutor() ruleset.Definicao {
	return ruleset.Definicao{
		Slug:   "revista-sintetica",
		Versao: 3,
		Nome:   "Revista Sintética de Teste",
		Fonte:  "https://exemplo.test/diretrizes",
		Pagina: ruleset.Pagina{
			LarguraCM: 21, AlturaCM: 29.7,
			Margens: ruleset.Margens{SuperiorCM: 2, InferiorCM: 2, EsquerdaCM: 3, DireitaCM: 2},
		},
		Corpo: ruleset.Corpo{
			Fonte:          apontarExecutor("Times New Roman"),
			TamanhoPT:      12,
			Entrelinha:     1.5,
			RecuoCM:        apontarExecutor(1.25),
			EspacoAntesPT:  apontarExecutor(0.0),
			EspacoDepoisPT: apontarExecutor(6.0),
			Alinhamento:    apontarExecutor("justificado"),
		},
	}
}

// apontarExecutor devolve o endereço de um literal: os cinco campos silentes
// de ruleset.Corpo são ponteiro.
func apontarExecutor[T any](valor T) *T { return &valor }

func rulesetsAtivoExecutor() *rulesetsExecutorFake {
	return &rulesetsExecutorFake{definicao: definicaoSinteticaExecutor(), ativo: true}
}

// cdmDoDocxExecutor produz o CDM do pacote pela MESMA porta que o job de
// análise usa, em vez de um índice inventado: é o dado que estaria em
// documentos.cdm_jsonb depois de analisar.
func cdmDoDocxExecutor(t *testing.T, docx []byte) json.RawMessage {
	t.Helper()
	blocos, err := ooxml.AnalisarEstrutura(docx)
	require.NoError(t, err, "pré-condição: o pacote do caso precisa ser analisável")
	dados, err := cdm.NovoIndice(blocos).Serializar()
	require.NoError(t, err)
	return dados
}

// documentoFormatavelExecutor monta o documento no estado em que um job de
// formatação o encontra: formato docx, CDM gravado e o status pedido.
func documentoFormatavelExecutor(t *testing.T, status documentoentity.Status, cdmPersistido json.RawMessage) documentoentity.Documento {
	t.Helper()
	documento := documentoDeTesteExecutor(t, status)
	documento.CDM = cdmPersistido
	return documento
}

func executorFormatarDeTeste(
	t *testing.T,
	repo repository.DocumentoInternoRepo,
	armazenador ArmazenadorObjetos,
	rulesets *rulesetsExecutorFake,
	registrador *slog.Logger,
) *ExecutorDocumento {
	t.Helper()
	servico, err := processamento.NovoServicoInterno(repo)
	require.NoError(t, err)
	executor, err := NovoExecutorDocumento(servico, armazenador, &conversorExecutorFake{}, rulesets, registrador)
	require.NoError(t, err)
	return executor
}

func jobFormatarExecutor(documentoID uuid.UUID, rulesetID *uuid.UUID) entity.Job {
	return entity.Job{
		ID:          uuid.New(),
		DocumentoID: documentoID,
		RulesetID:   rulesetID,
		Tipo:        entity.TipoFormatar,
		Status:      entity.StatusExecutando,
	}
}

func idRulesetExecutor() *uuid.UUID {
	id := uuid.New()
	return &id
}

// ---------------------------------------------------------------------------
// Inspetores do pacote salvo. Locais porque os helpers equivalentes de
// internal/infra/ooxml são privados daquele pacote.
// ---------------------------------------------------------------------------

const namespaceWExecutor = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"

func parteDoZipExecutor(t *testing.T, pacote []byte, nome string) []byte {
	t.Helper()
	leitor, err := zip.NewReader(bytes.NewReader(pacote), int64(len(pacote)))
	require.NoError(t, err, "o pacote salvo precisa ser um ZIP válido")
	for _, arquivo := range leitor.File {
		if arquivo.Name != nome {
			continue
		}
		aberto, err := arquivo.Open()
		require.NoError(t, err)
		defer func() { require.NoError(t, aberto.Close()) }()
		dados, err := io.ReadAll(aberto)
		require.NoError(t, err)
		return dados
	}
	t.Fatalf("parte %q ausente do pacote", nome)
	return nil
}

func nomesDoZipExecutor(t *testing.T, pacote []byte) []string {
	t.Helper()
	leitor, err := zip.NewReader(bytes.NewReader(pacote), int64(len(pacote)))
	require.NoError(t, err)
	nomes := make([]string, 0, len(leitor.File))
	for _, arquivo := range leitor.File {
		nomes = append(nomes, arquivo.Name)
	}
	return nomes
}

// atributosDoElementoExecutor devolve os atributos do namespace w de cada
// ocorrência do elemento pedido, na ordem do documento.
func atributosDoElementoExecutor(t *testing.T, documentoXML []byte, local string) []map[string]string {
	t.Helper()
	decodificador := xml.NewDecoder(bytes.NewReader(documentoXML))
	encontrados := []map[string]string{}
	for {
		token, err := decodificador.Token()
		if infraerrors.E(err, io.EOF) {
			break
		}
		require.NoError(t, err, "word/document.xml salvo precisa ser XML válido")
		inicio, ok := token.(xml.StartElement)
		if !ok || inicio.Name.Local != local || inicio.Name.Space != namespaceWExecutor {
			continue
		}
		atributos := map[string]string{}
		for _, atributo := range inicio.Attr {
			if atributo.Name.Space == namespaceWExecutor {
				atributos[atributo.Name.Local] = atributo.Value
			}
		}
		encontrados = append(encontrados, atributos)
	}
	return encontrados
}

// elementoInspecionado é um filho de w:pPr ou de w:rPr com seus atributos.
type elementoInspecionado struct {
	XMLName   xml.Name
	Atributos []xml.Attr `xml:",any,attr"`
	Ignorado  string     `xml:",chardata"`
}

type propriedadesInspecionadas struct {
	Filhos []elementoInspecionado `xml:",any"`
}

type runInspecionado struct {
	RPr    *propriedadesInspecionadas `xml:"rPr"`
	Textos []string                   `xml:"t"`
}

type paragrafoInspecionado struct {
	PPr  *propriedadesInspecionadas `xml:"pPr"`
	Runs []runInspecionado          `xml:"r"`
}

// paragrafosPorOrdinalExecutor indexa os w:p pelo ORDINAL entre os filhos
// diretos de w:body que são bloco (w:p ou w:tbl) — os mesmos ordinais que
// formatador.Plano.ReferenciasCorpo carrega. Contar só w:p daria outro índice
// em qualquer documento com tabela intercalada.
func paragrafosPorOrdinalExecutor(t *testing.T, documentoXML []byte) map[int]paragrafoInspecionado {
	t.Helper()
	decodificador := xml.NewDecoder(bytes.NewReader(documentoXML))
	paragrafos := map[int]paragrafoInspecionado{}
	dentroDoCorpo := false
	ordinal := -1
	for {
		token, err := decodificador.Token()
		if infraerrors.E(err, io.EOF) {
			break
		}
		require.NoError(t, err, "word/document.xml salvo precisa ser XML válido")
		inicio, ok := token.(xml.StartElement)
		if !ok || inicio.Name.Space != namespaceWExecutor {
			continue
		}
		if !dentroDoCorpo {
			if inicio.Name.Local == "body" {
				dentroDoCorpo = true
			}
			continue
		}
		switch inicio.Name.Local {
		case "p":
			ordinal++
			var paragrafo paragrafoInspecionado
			require.NoError(t, decodificador.DecodeElement(&paragrafo, &inicio))
			paragrafos[ordinal] = paragrafo
		case "tbl":
			ordinal++
			require.NoError(t, decodificador.Skip())
		default:
			require.NoError(t, decodificador.Skip())
		}
	}
	require.NotEmpty(t, paragrafos, "o corpo salvo precisa ter parágrafos a inspecionar")
	return paragrafos
}

// atributosUnicosExecutor exige EXATAMENTE um filho com o nome pedido e
// devolve seus atributos do namespace w. Exigir um só é o que prova que os
// mutadores reusaram o mesmo w:spacing em vez de criar um segundo.
func atributosUnicosExecutor(t *testing.T, propriedades *propriedadesInspecionadas, local string) map[string]string {
	t.Helper()
	require.NotNilf(t, propriedades, "esperava propriedades com %s", local)
	atributos := map[string]string{}
	encontrados := 0
	for _, filho := range propriedades.Filhos {
		if filho.XMLName.Local != local || filho.XMLName.Space != namespaceWExecutor {
			continue
		}
		encontrados++
		for _, atributo := range filho.Atributos {
			if atributo.Name.Space == namespaceWExecutor || atributo.Name.Space == "" {
				atributos[atributo.Name.Local] = atributo.Value
			}
		}
	}
	require.Equalf(t, 1, encontrados, "esperava exatamente um w:%s", local)
	return atributos
}

func twipsExecutor(t *testing.T, centimetros float64) string {
	t.Helper()
	twips, err := vo.CentimetrosParaTwips(centimetros)
	require.NoError(t, err)
	return strconv.Itoa(twips)
}

func twipsDePontosExecutor(t *testing.T, pontos float64) string {
	t.Helper()
	twips, err := vo.PontosParaTwips(pontos)
	require.NoError(t, err)
	return strconv.Itoa(twips)
}

// textosWTExecutor é a sequência de runes dos w:t na ordem do documento — o
// invariante de integridade de texto do ADR 0001.
func textosWTExecutor(t *testing.T, documentoXML []byte) []string {
	t.Helper()
	decodificador := xml.NewDecoder(bytes.NewReader(documentoXML))
	textos := []string{}
	for {
		token, err := decodificador.Token()
		if infraerrors.E(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		inicio, ok := token.(xml.StartElement)
		if !ok || inicio.Name.Local != "t" || inicio.Name.Space != namespaceWExecutor {
			continue
		}
		var texto string
		require.NoError(t, decodificador.DecodeElement(&texto, &inicio))
		textos = append(textos, texto)
	}
	return textos
}

// montarDocxCorpoExecutor monta um pacote docx com o XML de corpo informado,
// para os casos que precisam de w:pPr ou de um document.xml fora do comum.
func montarDocxCorpoExecutor(t *testing.T, corpoXML string) []byte {
	t.Helper()
	documentoXML := []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="` + namespaceWExecutor + `"><w:body>` + corpoXML + `</w:body></w:document>`)
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

// cdmManualExecutor monta o CDM que ESTARIA persistido, com os papéis
// escolhidos pelo caso. Necessário onde a heurística classificaria o
// parágrafo de outro jeito e o caso precisa de um ordinal específico dentro
// de ReferenciasCorpo.
func cdmManualExecutor(t *testing.T, papeis map[int]cdm.Papel) json.RawMessage {
	t.Helper()
	ordinais := make([]int, 0, len(papeis))
	for ordinal := range papeis {
		ordinais = append(ordinais, ordinal)
	}
	sort.Ints(ordinais)
	blocos := make([]cdm.Bloco, 0, len(papeis))
	for _, ordinal := range ordinais {
		bloco, err := cdm.NovoBloco(papeis[ordinal], "resumo", 1, cdm.OrigemUsuario, ordinal)
		require.NoError(t, err)
		blocos = append(blocos, bloco)
	}
	dados, err := cdm.NovoIndice(blocos).Serializar()
	require.NoError(t, err)
	return dados
}

// ---------------------------------------------------------------------------
// A14 (C6) — porta de perfil OBRIGATÓRIA.
// ---------------------------------------------------------------------------

func TestNovoExecutorRecusaPortaDePerfilNula(t *testing.T) {
	t.Parallel()
	documento := documentoDeTesteExecutor(t, documentoentity.StatusRecebido)
	servico, err := processamento.NovoServicoInterno(novoRepoInternoFakeExecutor(documento))
	require.NoError(t, err)

	executor, err := NovoExecutorDocumento(servico, &armazenadorExecutorFake{}, &conversorExecutorFake{}, nil, log.Novo("debug", io.Discard))

	assert.Nil(t, executor, "A14: dependência obrigatória não pode render executor utilizável")
	var argumento *infraerrors.ErroArgumentoNulo
	require.True(t, infraerrors.Como(err, &argumento),
		"A14: esperava *ErroArgumentoNulo; falha se a dependência virar opcional")
}

// ---------------------------------------------------------------------------
// A6 (C3) — pré-condições recusadas, um termo por caso.
// ---------------------------------------------------------------------------

func TestExecutarFormatarRecusaPreCondicoesSemTransitarNemLerPerfil(t *testing.T) {
	t.Parallel()
	docx := montarDocxMinimoExecutor(t, "parágrafo ordinário do corpo do artigo")
	cdmValido := cdmDoDocxExecutor(t, docx)

	documentoPDF := func(t *testing.T) documentoentity.Documento {
		t.Helper()
		dono, err := vo.NovoDonoSessao(uuid.New())
		require.NoError(t, err)
		documento, err := documentoentity.NovoDocumento(dono, "artigo.pdf", vo.FormatoPDF, 4096)
		require.NoError(t, err)
		documento.Status = documentoentity.StatusAnalisado
		documento.CDM = cdmValido
		return documento
	}

	casos := []struct {
		nome      string
		documento func(*testing.T) documentoentity.Documento
		rulesetID *uuid.UUID
	}{
		// Defesa em profundidade: inalcançável em produção pelo CHECK
		// jobs_ruleset_obrigatorio_para_formatar (migration linha 69).
		// Declarado para não ser removido como código morto.
		{
			nome: "ruleset do job nulo",
			documento: func(t *testing.T) documentoentity.Documento {
				return documentoFormatavelExecutor(t, documentoentity.StatusAnalisado, cdmValido)
			},
			rulesetID: nil,
		},
		{nome: "formato não é docx", documento: documentoPDF, rulesetID: idRulesetExecutor()},
		{
			nome: "documento ainda em recebido",
			documento: func(t *testing.T) documentoentity.Documento {
				return documentoFormatavelExecutor(t, documentoentity.StatusRecebido, cdmValido)
			},
			rulesetID: idRulesetExecutor(),
		},
		{
			nome: "cdm vazio",
			documento: func(t *testing.T) documentoentity.Documento {
				return documentoFormatavelExecutor(t, documentoentity.StatusAnalisado, nil)
			},
			rulesetID: idRulesetExecutor(),
		},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()
			documento := caso.documento(t)
			statusInicial := documento.Status
			repo := novoRepoFormatarFake(documento)
			armazenador := &armazenadorExecutorFake{conteudo: docx}
			rulesets := rulesetsAtivoExecutor()
			executor := executorFormatarDeTeste(t, repo, armazenador, rulesets, log.Novo("debug", io.Discard))

			resultado, err := executor.Executar(context.Background(), jobFormatarExecutor(documento.ID, caso.rulesetID))

			require.Error(t, err, "A6: pré-condição reprovada nunca devolve sucesso")
			assert.Nil(t, resultado)
			assert.Empty(t, repo.transicoes, "A6: ZERO transições — nem iniciar, nem marcar falha")
			assert.Equal(t, statusInicial, repo.status(documento.ID))
			// O passo (1) lê o documento: UMA leitura é o esperado. Duas
			// indicariam que uma transição foi tentada depois da guarda.
			assert.Equal(t, 1, repo.leituras, "A6: ObterPorIDInterno do documento É chamado, e só uma vez")
			assert.Zero(t, rulesets.chamadas, "A6: ConsultaRulesetRepo.ObterPorID NÃO é chamado")
			assert.Zero(t, armazenador.obtencoes, "A6: nada é baixado")
			assert.Empty(t, armazenador.salvos, "A6: nada é salvo")
		})
	}
}

// ---------------------------------------------------------------------------
// A5 (C3) — TAKEDOWN: perfil retirado de circulação.
// ---------------------------------------------------------------------------

func TestExecutarFormatarRecusaPerfilInativoSemNenhumaTransicao(t *testing.T) {
	t.Parallel()
	docx := montarDocxMinimoExecutor(t, "parágrafo ordinário do corpo do artigo")
	documento := documentoFormatavelExecutor(t, documentoentity.StatusAnalisado, cdmDoDocxExecutor(t, docx))
	repo := novoRepoFormatarFake(documento)
	armazenador := &armazenadorExecutorFake{conteudo: docx}
	rulesets := &rulesetsExecutorFake{definicao: definicaoSinteticaExecutor(), ativo: false}
	executor := executorFormatarDeTeste(t, repo, armazenador, rulesets, log.Novo("debug", io.Discard))
	rulesetID := idRulesetExecutor()

	resultado, err := executor.Executar(context.Background(), jobFormatarExecutor(documento.ID, rulesetID))

	require.Error(t, err, "A5: perfil fora de circulação não executa")
	assert.Nil(t, resultado)
	require.Equal(t, 1, rulesets.chamadas, "A5: o perfil precisa ter sido consultado para o caso valer")
	assert.Equal(t, []uuid.UUID{*rulesetID}, rulesets.recebidos, "A5: o id consultado é o do job")

	// A parte que NÃO pode ser frouxa: "não foi para formatado" passaria com
	// uma implementação que transita para formatando e cai em marcarFalha —
	// e essa implementação mandaria para falhou TODO documento com job
	// pendente quando um operador retira um perfil do catálogo.
	assert.Equal(t, documentoentity.StatusAnalisado, repo.status(documento.ID),
		"A5: o documento permanece EXATAMENTE em analisado")
	assert.Empty(t, repo.transicoes, "A5: ZERO transições — nem IniciarFormatacao, nem MarcarFalha")
	assert.Zero(t, repo.transicoes[documentoentity.StatusFormatando])
	assert.Zero(t, repo.transicoes[documentoentity.StatusFalhou])
	assert.Equal(t, 1, repo.leituras, "A5: só a leitura do passo (1)")
	assert.Empty(t, armazenador.salvos, "A5: nenhuma chamada a Salvar")
}

// ---------------------------------------------------------------------------
// A17 (C3) — falha de INFRAESTRUTURA na leitura do perfil, passo (3).
//
// Mesma forma de A5, causa oposta: aqui o catálogo está indisponível, não o
// perfil retirado. No passo (3) ainda não houve transição, então não há do que
// marcar falha; e levar o documento a falhou por indisponibilidade de banco
// obrigaria o usuário a reparar um documento sem defeito algum. A falha não é
// atribuível ao documento: ela sobe para o laço, que reenfileira.
// ---------------------------------------------------------------------------

func TestExecutarFormatarPropagaErroDeInfraestruturaDoPerfilSemTocarNoDocumento(t *testing.T) {
	t.Parallel()
	docx := montarDocxMinimoExecutor(t, "parágrafo ordinário do corpo do artigo")
	documento := documentoFormatavelExecutor(t, documentoentity.StatusAnalisado, cdmDoDocxExecutor(t, docx))
	repo := novoRepoFormatarFake(documento)
	armazenador := &armazenadorExecutorFake{conteudo: docx}
	erroDriver := infraerrors.NovoErroAplicacao("catálogo de perfis indisponível")
	rulesets := &rulesetsExecutorFake{definicao: definicaoSinteticaExecutor(), ativo: true, erro: erroDriver}
	executor := executorFormatarDeTeste(t, repo, armazenador, rulesets, log.Novo("debug", io.Discard))
	rulesetID := idRulesetExecutor()

	resultado, err := executor.Executar(context.Background(), jobFormatarExecutor(documento.ID, rulesetID))

	require.Error(t, err)
	assert.Nil(t, resultado)
	assert.True(t, infraerrors.E(err, erroDriver), "A17: o erro da porta sobe preservando a causa")

	// A distinção que importa: indisponibilidade de banco NÃO pode ser
	// reclassificada como entrada inválida, senão o contrato HTTP devolve 400
	// para um documento que não tem defeito nenhum.
	var validacao *infraerrors.ErroValidacao
	assert.False(t, infraerrors.Como(err, &validacao), "A17: NÃO é *ErroValidacao")

	require.Equal(t, 1, rulesets.chamadas, "A17: o perfil precisa ter sido consultado para o caso valer")
	assert.Equal(t, []uuid.UUID{*rulesetID}, rulesets.recebidos, "A17: o id consultado é o do job")

	assert.Equal(t, documentoentity.StatusAnalisado, repo.status(documento.ID),
		"A17: o documento permanece EXATAMENTE em analisado")
	assert.Empty(t, repo.transicoes, "A17: ZERO transições — nem IniciarFormatacao, nem MarcarFalha")
	assert.Zero(t, repo.transicoes[documentoentity.StatusFormatando])
	assert.Zero(t, repo.transicoes[documentoentity.StatusFalhou])
	assert.Equal(t, 1, repo.leituras, "A17: só a leitura do passo (1)")
	assert.Zero(t, armazenador.obtencoes, "A17: nada é baixado")
	assert.Empty(t, armazenador.salvos, "A17: nada é salvo")
}

// ---------------------------------------------------------------------------
// A16 (C3) — disputa entre dois workers no passo (4).
// ---------------------------------------------------------------------------

func TestExecutarFormatarNaoMarcaFalhaQuandoPerdeOCASDeIniciar(t *testing.T) {
	t.Parallel()
	docx := montarDocxMinimoExecutor(t, "parágrafo ordinário do corpo do artigo")
	documento := documentoFormatavelExecutor(t, documentoentity.StatusAnalisado, cdmDoDocxExecutor(t, docx))
	repo := novoRepoFormatarFake(documento)
	// Exatamente o que AtualizarStatus faz em zero linhas quando o CAS perde
	// (data/postgres/documento.go:140-142).
	conflito := infraerrors.NovoErroConflito("status do documento mudou")
	repo.erros[documentoentity.StatusFormatando] = conflito
	armazenador := &armazenadorExecutorFake{conteudo: docx}
	executor := executorFormatarDeTeste(t, repo, armazenador, rulesetsAtivoExecutor(), log.Novo("debug", io.Discard))

	resultado, err := executor.Executar(context.Background(), jobFormatarExecutor(documento.ID, idRulesetExecutor()))

	require.Error(t, err)
	assert.Nil(t, resultado)
	assert.True(t, infraerrors.E(err, conflito), "A16: o erro sobe envolvido, preservando a causa")
	var tipado *infraerrors.ErroConflito
	assert.True(t, infraerrors.Como(err, &tipado), "A16: *ErroConflito continua identificável")

	require.Equal(t, 1, repo.transicoes[documentoentity.StatusFormatando],
		"A16: o caso precisa ter tentado iniciar para valer")
	// formatando -> falhou É transição válida: um marcarFalha aqui mataria a
	// execução viva do outro worker (precedente de executor.go:110-112).
	assert.Zero(t, repo.transicoes[documentoentity.StatusFalhou], "A16: ZERO chamadas a MarcarFalha")
	assert.Empty(t, armazenador.salvos, "A16: nenhum artefato salvo")
}

// ---------------------------------------------------------------------------
// A7 (C3) — caminho feliz: os SEIS mutadores de corpo e a página.
// ---------------------------------------------------------------------------

func TestExecutarFormatarFelizSalvaArtefatoComAsSeisPropriedades(t *testing.T) {
	t.Parallel()
	original := lerFixtureExecutor(t, "artigo-real-libreoffice.docx")
	cdmPersistido := cdmDoDocxExecutor(t, original)
	documento := documentoFormatavelExecutor(t, documentoentity.StatusAnalisado, cdmPersistido)
	repo := novoRepoFormatarFake(documento)
	armazenador := &armazenadorExecutorFake{conteudo: original}
	definicao := definicaoSinteticaExecutor()
	rulesets := &rulesetsExecutorFake{definicao: definicao, ativo: true}
	executor := executorFormatarDeTeste(t, repo, armazenador, rulesets, log.Novo("debug", io.Discard))
	rulesetID := idRulesetExecutor()

	// Pré-condição: as medidas do perfil DIFEREM das do arquivo, senão a
	// aplicação da página não produziria delta e o caso seria vacuoso.
	require.NotContains(t, string(parteDoZipExecutor(t, original, "word/document.xml")),
		`w:left="`+twipsExecutor(t, definicao.Pagina.Margens.EsquerdaCM)+`"`)

	resultado, err := executor.Executar(context.Background(), jobFormatarExecutor(documento.ID, rulesetID))
	require.NoError(t, err)

	chaveEsperada, err := vo.NovaChaveFormatado(documento.ID, *rulesetID)
	require.NoError(t, err)
	require.Len(t, armazenador.salvos, 1, "A7: exatamente um artefato salvo")
	assert.Equal(t, chaveEsperada, armazenador.salvos[0].chave, "A7: a chave é a de NovaChaveFormatado")
	assert.Equal(t, vo.MIMEDocx, armazenador.salvos[0].contentType, "A7: MIME de DOCX")

	salvo := armazenador.salvos[0].conteudo
	documentoXML := parteDoZipExecutor(t, salvo, "word/document.xml")

	// Página: pgSz e pgMar com os twips convertidos das medidas do perfil.
	for _, secao := range atributosDoElementoExecutor(t, documentoXML, "pgSz") {
		assert.Equal(t, twipsExecutor(t, definicao.Pagina.LarguraCM), secao["w"])
		assert.Equal(t, twipsExecutor(t, definicao.Pagina.AlturaCM), secao["h"])
	}
	require.NotEmpty(t, atributosDoElementoExecutor(t, documentoXML, "pgSz"), "A7: o pacote salvo precisa ter w:pgSz")
	for _, margens := range atributosDoElementoExecutor(t, documentoXML, "pgMar") {
		assert.Equal(t, twipsExecutor(t, definicao.Pagina.Margens.SuperiorCM), margens["top"])
		assert.Equal(t, twipsExecutor(t, definicao.Pagina.Margens.InferiorCM), margens["bottom"])
		assert.Equal(t, twipsExecutor(t, definicao.Pagina.Margens.EsquerdaCM), margens["left"])
		assert.Equal(t, twipsExecutor(t, definicao.Pagina.Margens.DireitaCM), margens["right"])
	}
	require.NotEmpty(t, atributosDoElementoExecutor(t, documentoXML, "pgMar"), "A7: o pacote salvo precisa ter w:pgMar")

	// Corpo: os SEIS mutadores que AplicarPlano roda (formatar.go:38-48).
	// Nomear só alinhamento, espaçamento e recuo deixaria a tipografia sem
	// rodar com o critério passando.
	indice, err := cdm.Desserializar(cdmPersistido)
	require.NoError(t, err)
	plano, err := formatador.Planejar(indice, definicao)
	require.NoError(t, err)
	require.NotEmpty(t, plano.ReferenciasCorpo, "pré-condição: o pacote real tem parágrafos de corpo")

	entrelinha, err := vo.EntrelinhaParaUnidades(definicao.Corpo.Entrelinha)
	require.NoError(t, err)
	meiosPontos, err := vo.PontosParaMeiosPontos(definicao.Corpo.TamanhoPT)
	require.NoError(t, err)

	paragrafos := paragrafosPorOrdinalExecutor(t, documentoXML)
	for _, ordinal := range plano.ReferenciasCorpo {
		paragrafo, existe := paragrafos[ordinal]
		require.Truef(t, existe, "ordinal %d do plano precisa existir no corpo salvo", ordinal)

		assert.Equal(t, "both", atributosUnicosExecutor(t, paragrafo.PPr, "jc")["val"], "A7: w:jc")
		espacamento := atributosUnicosExecutor(t, paragrafo.PPr, "spacing")
		assert.Equal(t, strconv.Itoa(entrelinha), espacamento["line"], "A7: w:spacing/@w:line")
		assert.Equal(t, "auto", espacamento["lineRule"], "A7: w:spacing/@w:lineRule")
		assert.Equal(t, twipsDePontosExecutor(t, *definicao.Corpo.EspacoAntesPT), espacamento["before"], "A7: w:spacing/@w:before")
		assert.Equal(t, twipsDePontosExecutor(t, *definicao.Corpo.EspacoDepoisPT), espacamento["after"], "A7: w:spacing/@w:after")
		assert.Equal(t, twipsExecutor(t, *definicao.Corpo.RecuoCM),
			atributosUnicosExecutor(t, paragrafo.PPr, "ind")["firstLine"], "A7: w:ind/@w:firstLine")

		runsDeTexto := 0
		for _, run := range paragrafo.Runs {
			if len(run.Textos) == 0 {
				continue
			}
			runsDeTexto++
			fontes := atributosUnicosExecutor(t, run.RPr, "rFonts")
			assert.Equal(t, *definicao.Corpo.Fonte, fontes["ascii"], "A7: w:rFonts/@w:ascii")
			assert.Equal(t, *definicao.Corpo.Fonte, fontes["hAnsi"], "A7: w:rFonts/@w:hAnsi")
			assert.Equal(t, strconv.Itoa(meiosPontos), atributosUnicosExecutor(t, run.RPr, "sz")["val"], "A7: w:sz")
			assert.Equal(t, strconv.Itoa(meiosPontos), atributosUnicosExecutor(t, run.RPr, "szCs")["val"], "A7: w:szCs")
		}
		require.Positivef(t, runsDeTexto, "ordinal %d precisa ter run de texto para a tipografia valer", ordinal)
	}

	assert.Equal(t, documentoentity.StatusFormatado, repo.status(documento.ID), "A7: documento em formatado")
	assert.Equal(t, 1, repo.transicoes[documentoentity.StatusFormatando])
	assert.Equal(t, 1, repo.transicoes[documentoentity.StatusFormatado])
	assert.Zero(t, repo.transicoes[documentoentity.StatusFalhou])

	// Resultado do job: chave, slug e versão — e NUNCA conteúdo (regra 7).
	// Os nomes dos campos seguem o precedente de resultadoRenderizarPreview
	// ("chave_preview_pdf"): a spec exige os três dados, não as chaves JSON.
	var corpo struct {
		ChaveFormatado string `json:"chave_formatado"`
		RulesetSlug    string `json:"ruleset_slug"`
		RulesetVersao  int    `json:"ruleset_versao"`
	}
	require.NoError(t, json.Unmarshal(resultado, &corpo))
	assert.Equal(t, chaveEsperada.String(), corpo.ChaveFormatado)
	assert.Equal(t, definicao.Slug, corpo.RulesetSlug)
	assert.Equal(t, definicao.Versao, corpo.RulesetVersao)
}

// ---------------------------------------------------------------------------
// A12 (C3) — invariantes do ADR 0001 no caminho completo
// Abrir -> AplicarPlano -> Salvar -> armazenador.
// ---------------------------------------------------------------------------

func TestExecutarFormatarPreservaTextoEPartesNaoAlvoDoPacoteReal(t *testing.T) {
	t.Parallel()
	// Referência LIDA DO DISCO: comparar o pacote salvo com o que o
	// armazenador devolveu seria comparar o pacote consigo mesmo.
	referencia := lerFixtureExecutor(t, "artigo-real-libreoffice.docx")
	documento := documentoFormatavelExecutor(t, documentoentity.StatusAnalisado, cdmDoDocxExecutor(t, referencia))
	repo := novoRepoFormatarFake(documento)
	armazenador := &armazenadorExecutorFake{conteudo: lerFixtureExecutor(t, "artigo-real-libreoffice.docx")}
	executor := executorFormatarDeTeste(t, repo, armazenador, rulesetsAtivoExecutor(), log.Novo("debug", io.Discard))

	_, err := executor.Executar(context.Background(), jobFormatarExecutor(documento.ID, idRulesetExecutor()))
	require.NoError(t, err)
	require.Len(t, armazenador.salvos, 1)
	salvo := armazenador.salvos[0].conteudo

	documentoReferencia := parteDoZipExecutor(t, referencia, "word/document.xml")
	documentoSalvo := parteDoZipExecutor(t, salvo, "word/document.xml")
	require.NotEqual(t, documentoReferencia, documentoSalvo, "pré-condição: o caso precisa produzir delta")

	assert.Equal(t, textosWTExecutor(t, documentoReferencia), textosWTExecutor(t, documentoSalvo),
		"A12: a sequência de runes dos w:t é idêntica à do arquivo de referência")
	nomes := nomesDoZipExecutor(t, referencia)
	assert.Equal(t, nomes, nomesDoZipExecutor(t, salvo), "A12: a ordem das entradas do ZIP é preservada")
	for _, nome := range nomes {
		if nome == "word/document.xml" {
			continue
		}
		assert.Equalf(t, parteDoZipExecutor(t, referencia, nome), parteDoZipExecutor(t, salvo, nome),
			"A12: %s precisa sair byte a byte igual", nome)
	}
}

// ---------------------------------------------------------------------------
// A8 (C3) — os TRÊS pontos de falha posteriores a IniciarFormatacao
// (AplicarPlano recusa, Salvar erra, ConcluirFormatacao erra depois de
// Salvar), e o único ponto de inconsistência storage-banco do fluxo.
// ---------------------------------------------------------------------------

func TestExecutarFormatarFalhasPosterioresMarcamFalhaPreservandoACausa(t *testing.T) {
	t.Parallel()
	docxSimples := montarDocxMinimoExecutor(t, "parágrafo ordinário do corpo do artigo")
	cdmSimples := cdmDoDocxExecutor(t, docxSimples)

	// Parágrafo 1 com w:numPr e declarado como Paragrafo no CDM: é o
	// preflight de AplicarRecuoPrimeiraLinha que recusa, já depois de a
	// página ter sido aplicada na cópia.
	docxNumPr := montarDocxCorpoExecutor(t,
		`<w:p><w:r><w:t>zero</w:t></w:r></w:p>`+
			`<w:p><w:pPr><w:numPr><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t>um</w:t></w:r></w:p>`+
			`<w:sectPr><w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/></w:sectPr>`)
	cdmNumPr := cdmManualExecutor(t, map[int]cdm.Papel{0: cdm.Paragrafo, 1: cdm.Paragrafo})

	erroSalvar := infraerrors.NovoErroAplicacao("storage indisponível ao salvar")
	erroConcluir := infraerrors.NovoErroAplicacao("banco indisponível ao concluir")

	casos := []struct {
		nome        string
		docx        []byte
		cdm         json.RawMessage
		prepara     func(*repoFormatarFake, *armazenadorExecutorFake, *rulesetsExecutorFake)
		causa       error
		salvaAntes  bool
		exigeFalhou bool
	}{
		{
			nome: "aplicar plano recusa parágrafo com numPr",
			docx: docxNumPr, cdm: cdmNumPr,
			prepara:     func(*repoFormatarFake, *armazenadorExecutorFake, *rulesetsExecutorFake) {},
			exigeFalhou: true,
		},
		{
			nome: "salvar artefato falha",
			docx: docxSimples, cdm: cdmSimples,
			prepara: func(_ *repoFormatarFake, a *armazenadorExecutorFake, _ *rulesetsExecutorFake) {
				a.erroSalvar = erroSalvar
			},
			causa: erroSalvar, exigeFalhou: true,
		},
		// Único ponto de inconsistência storage-banco do fluxo: o artefato
		// órfão é TOLERADO (Reenfileirar zera o Resultado, job.go:171), mas o
		// documento NUNCA pode ficar em formatado.
		{
			nome: "concluir formatação falha depois de salvar",
			docx: docxSimples, cdm: cdmSimples,
			prepara: func(r *repoFormatarFake, _ *armazenadorExecutorFake, _ *rulesetsExecutorFake) {
				r.erros[documentoentity.StatusFormatado] = erroConcluir
			},
			causa: erroConcluir, salvaAntes: true,
		},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()
			documento := documentoFormatavelExecutor(t, documentoentity.StatusAnalisado, caso.cdm)
			repo := novoRepoFormatarFake(documento)
			armazenador := &armazenadorExecutorFake{conteudo: caso.docx}
			rulesets := rulesetsAtivoExecutor()
			caso.prepara(repo, armazenador, rulesets)
			executor := executorFormatarDeTeste(t, repo, armazenador, rulesets, log.Novo("debug", io.Discard))

			resultado, err := executor.Executar(context.Background(), jobFormatarExecutor(documento.ID, idRulesetExecutor()))

			require.Error(t, err)
			assert.Nil(t, resultado)
			if caso.causa != nil {
				assert.True(t, infraerrors.E(err, caso.causa), "A8: o erro preserva a causa")
			}
			assert.NotEqual(t, documentoentity.StatusFormatado, repo.status(documento.ID),
				"A8: o documento NUNCA fica em formatado quando algo falhou")
			if caso.exigeFalhou {
				assert.Equal(t, documentoentity.StatusFalhou, repo.status(documento.ID))
				assert.Empty(t, armazenador.salvos, "A8: nada salvo quando a falha é anterior a Salvar")
			}
			if caso.salvaAntes {
				assert.Len(t, armazenador.salvos, 1, "A8: o artefato órfão é tolerado e declarado")
			}
		})
	}
}

func TestExecutarFormatarPreservaAsDuasCausasQuandoMarcarFalhaFalha(t *testing.T) {
	t.Parallel()
	docx := montarDocxMinimoExecutor(t, "parágrafo ordinário do corpo do artigo")
	documento := documentoFormatavelExecutor(t, documentoentity.StatusAnalisado, cdmDoDocxExecutor(t, docx))
	repo := novoRepoFormatarFake(documento)
	erroOriginal := infraerrors.NovoErroAplicacao("storage indisponível ao salvar")
	erroPersistencia := infraerrors.NovoErroAplicacao("banco indisponível ao marcar falha")
	repo.erros[documentoentity.StatusFalhou] = erroPersistencia
	armazenador := &armazenadorExecutorFake{conteudo: docx, erroSalvar: erroOriginal}
	executor := executorFormatarDeTeste(t, repo, armazenador, rulesetsAtivoExecutor(), log.Novo("debug", io.Discard))

	_, err := executor.Executar(context.Background(), jobFormatarExecutor(documento.ID, idRulesetExecutor()))

	require.Error(t, err)
	var composto *infraerrors.ErroPersistirFalha
	require.True(t, infraerrors.Como(err, &composto), "A8: mesmo ErroPersistirFalha de analisar (:172)")
	require.Len(t, composto.Unwrap(), 2, "A8: as DUAS causas são preservadas")
	assert.True(t, infraerrors.E(err, erroOriginal))
	assert.True(t, infraerrors.E(err, erroPersistencia))
}

// ---------------------------------------------------------------------------
// A9 (C3) — CDM persistido corrompido é falha do SERVIDOR, não do cliente.
// ---------------------------------------------------------------------------

func TestExecutarFormatarReclassificaCDMCorrompidoComoErroDeAplicacao(t *testing.T) {
	t.Parallel()
	docx := montarDocxMinimoExecutor(t, "parágrafo ordinário do corpo do artigo")
	for _, caso := range []struct {
		nome string
		cdm  json.RawMessage
	}{
		{"json inválido", json.RawMessage(`{"versao":1,"blocos":`)},
		{"versão de formato incompatível", json.RawMessage(`{"versao":99,"blocos":[]}`)},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()
			documento := documentoFormatavelExecutor(t, documentoentity.StatusAnalisado, caso.cdm)
			repo := novoRepoFormatarFake(documento)
			armazenador := &armazenadorExecutorFake{conteudo: docx}
			executor := executorFormatarDeTeste(t, repo, armazenador, rulesetsAtivoExecutor(), log.Novo("debug", io.Discard))

			resultado, err := executor.Executar(context.Background(), jobFormatarExecutor(documento.ID, idRulesetExecutor()))

			require.Error(t, err)
			assert.Nil(t, resultado)
			var aplicacao *infraerrors.ErroAplicacao
			assert.True(t, infraerrors.Como(err, &aplicacao), "A9: esperava *ErroAplicacao")
			// cdm.Desserializar devolve ErroValidacao; aqui o dado foi
			// gravado pelo próprio servidor, então a reclassificação de C3
			// passo (5) é obrigatória — sem ela isto viraria 400.
			var validacao *infraerrors.ErroValidacao
			assert.False(t, infraerrors.Como(err, &validacao),
				"A9: nenhum elo da cadeia pode ser *ErroValidacao")
			assert.Equal(t, documentoentity.StatusFalhou, repo.status(documento.ID))
			assert.Empty(t, armazenador.salvos, "A9: nada salvo")
		})
	}
}

// ---------------------------------------------------------------------------
// A10 (C3) — document.xml descomprimido acima do teto dos mutadores.
// ---------------------------------------------------------------------------

// Sem t.Parallel: o caso materializa um document.xml de 33 MiB em memória, de
// propósito. 32 MiB comitados em testdata/ seriam custo permanente por um
// caso de borda.
func TestExecutarFormatarRecusaDocumentXMLAcimaDoTetoDosMutadores(t *testing.T) {
	const tetoXMLMutadores = 32 << 20

	// UM parágrafo com UM w:t gigante: o número de nós fica em uma dezena,
	// muito abaixo de limiteNosPagina (100000). Se a guarda de TAMANHO fosse
	// removida, o parse teria sucesso e o caso passaria a não falhar — é
	// assim que ele fixa QUAL das três guardas disparou, já que a guarda de
	// nós compartilha a mesma mensagem.
	gigante := montarDocxCorpoExecutor(t,
		`<w:p><w:r><w:t>`+strings.Repeat("x", tetoXMLMutadores+1024)+`</w:t></w:r></w:p>`+
			`<w:sectPr><w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/></w:sectPr>`)
	require.Greater(t, len(parteDoZipExecutor(t, gigante, "word/document.xml")), tetoXMLMutadores,
		"pré-condição: o document.xml descomprimido excede o teto dos mutadores")
	require.Less(t, int64(len(gigante)), documentoservice.TamanhoMaximoPadraoBytes,
		"pré-condição: COMPRIMIDO o pacote está sob o teto de download de C4, que portanto não pode ser a guarda que dispara")

	documento := documentoFormatavelExecutor(t, documentoentity.StatusAnalisado,
		cdmManualExecutor(t, map[int]cdm.Papel{0: cdm.Paragrafo}))
	repo := novoRepoFormatarFake(documento)
	armazenador := &armazenadorExecutorFake{conteudo: gigante}
	executor := executorFormatarDeTeste(t, repo, armazenador, rulesetsAtivoExecutor(), log.Novo("debug", io.Discard))

	resultado, err := executor.Executar(context.Background(), jobFormatarExecutor(documento.ID, idRulesetExecutor()))

	require.Error(t, err)
	assert.Nil(t, resultado)
	assert.Contains(t, err.Error(), "word/document.xml inválido ou não suportado",
		"A10: a mensagem é a da guarda de tamanho do XML dos mutadores")
	assert.Equal(t, documentoentity.StatusFalhou, repo.status(documento.ID))
	assert.Empty(t, armazenador.salvos, "A10: NENHUMA chamada a Salvar")
	assert.NotContains(t, err.Error(), strings.Repeat("x", 64), "regra 7: a mensagem é fixa, sem conteúdo do documento")
}

// ---------------------------------------------------------------------------
// A11 (C4) — teto de leitura na função COMPARTILHADA baixarOriginal.
// ---------------------------------------------------------------------------

// leitorZerosExecutor entrega `restante` bytes e CONTA quantos foram lidos.
// Sem o contador, "nenhuma leitura completa em memória" é inobservável: um
// io.ReadAll sem LimitReader seguido de uma checagem de len() passaria.
type leitorZerosExecutor struct {
	restante int64
	lidos    int64
}

func (l *leitorZerosExecutor) Read(p []byte) (int, error) {
	if l.restante <= 0 {
		return 0, io.EOF
	}
	n := int64(len(p))
	if n > l.restante {
		n = l.restante
	}
	for i := range p[:n] {
		p[i] = 0
	}
	l.restante -= n
	l.lidos += n
	return int(n), nil
}

type armazenadorGrandeExecutor struct {
	tamanho int64
	leitor  *leitorZerosExecutor
	salvos  []chamadaSalvarExecutor
}

func (a *armazenadorGrandeExecutor) Obter(context.Context, vo.ChaveStorage) (io.ReadCloser, error) {
	a.leitor = &leitorZerosExecutor{restante: a.tamanho}
	return io.NopCloser(a.leitor), nil
}

func (a *armazenadorGrandeExecutor) Salvar(_ context.Context, chave vo.ChaveStorage, conteudo io.Reader, _ int64, contentType string) error {
	dados, err := io.ReadAll(conteudo)
	if err != nil {
		return err
	}
	a.salvos = append(a.salvos, chamadaSalvarExecutor{chave: chave, conteudo: dados, contentType: contentType})
	return nil
}

var _ ArmazenadorObjetos = (*armazenadorGrandeExecutor)(nil)

func TestBaixarOriginalFixaABordaDoTetoDeLeitura(t *testing.T) {
	limite := documentoservice.TamanhoMaximoPadraoBytes
	documento := documentoDeTesteExecutor(t, documentoentity.StatusAnalisado)

	for _, caso := range []struct {
		nome     string
		tamanho  int64
		recusado bool
	}{
		{"exatamente no teto", limite, false},
		{"um byte acima do teto", limite + 1, true},
		// Objeto MUITO acima do teto: é este caso que torna o contador não
		// vacuoso. Com tamanho = limite+1 o contador nunca poderia passar de
		// limite+1, nem sem LimitReader; aqui, uma leitura completa em
		// memória sugaria quatro vezes o teto do storage.
		{"muito acima do teto", 4 * limite, true},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			armazenador := &armazenadorGrandeExecutor{tamanho: caso.tamanho}
			repo := novoRepoFormatarFake(documento)
			executor := executorFormatarDeTeste(t, repo, armazenador, rulesetsAtivoExecutor(), log.Novo("debug", io.Discard))

			conteudo, err := executor.baixarOriginal(context.Background(), documento.ChaveStorage)

			if caso.recusado {
				require.Error(t, err, "A11: teto+1 FALHA")
				assert.Nil(t, conteudo)
			} else {
				require.NoError(t, err, "A11: exatamente no teto SUCEDE")
				assert.Len(t, conteudo, int(limite))
			}
			require.NotNil(t, armazenador.leitor)
			assert.LessOrEqual(t, armazenador.leitor.lidos, limite+1,
				"A11: no máximo limite+1 bytes saem do storage, nunca o objeto inteiro")
		})
	}
}

func TestOsOutrosDoisChamadoresDeBaixarOriginalSeguemFuncionando(t *testing.T) {
	t.Parallel()
	// A guarda ficou na função COMPARTILHADA, não só no caminho novo: os dois
	// chamadores antigos continuam funcionando dentro do teto.
	for _, tipo := range []entity.TipoJob{entity.TipoAnalisar, entity.TipoRenderizarPreview} {
		t.Run(tipo.String(), func(t *testing.T) {
			t.Parallel()
			documento := documentoDeTesteExecutor(t, documentoentity.StatusRecebido)
			repo := novoRepoInternoFakeExecutor(documento)
			armazenador := &armazenadorExecutorFake{conteudo: lerFixtureExecutor(t, "artigo-real-libreoffice.docx")}
			require.Less(t, int64(len(armazenador.conteudo)), documentoservice.TamanhoMaximoPadraoBytes,
				"pré-condição: a fixture está dentro do teto")
			executor := executorDeTesteExecutor(t, repo, armazenador, &conversorExecutorFake{})

			resultado, err := executor.Executar(context.Background(),
				entity.Job{ID: uuid.New(), DocumentoID: documento.ID, Tipo: tipo, Status: entity.StatusExecutando})

			require.NoError(t, err)
			assert.NotEmpty(t, resultado)
		})
	}
}

// ---------------------------------------------------------------------------
// A13 (C3) — regra 7 em TODOS os caminhos de falha do branch de formatação.
//
// NASCE VERDE: hoje o executor loga só documento_id (:174). Existe para
// derrubar quem acrescentar log de conteúdo depois, e por isso cobre todos os
// caminhos de falha, não um.
// ---------------------------------------------------------------------------

const (
	marcaTextoExecutor = "ZZMARCA-TEXTO-DO-USUARIO-7f3a9b"
	marcaNomeExecutor  = "zzmarca-nome-do-arquivo-7f3a9b"
)

func documentoMarcadoExecutor(t *testing.T, status documentoentity.Status, cdmPersistido json.RawMessage, formato vo.FormatoArquivo) documentoentity.Documento {
	t.Helper()
	dono, err := vo.NovoDonoSessao(uuid.New())
	require.NoError(t, err)
	documento, err := documentoentity.NovoDocumento(dono, marcaNomeExecutor+"."+formato.Extensao(), formato, 4096)
	require.NoError(t, err)
	documento.Status = status
	documento.CDM = cdmPersistido
	return documento
}

func exigirSemMarcaExecutor(t *testing.T, err error, marcas ...string) {
	t.Helper()
	pendentes := []error{err}
	for len(pendentes) > 0 {
		atual := pendentes[len(pendentes)-1]
		pendentes = pendentes[:len(pendentes)-1]
		if atual == nil {
			continue
		}
		for _, marca := range marcas {
			assert.NotContains(t, atual.Error(), marca, "regra 7: elo da cadeia de erro cita conteúdo do documento")
			for _, formato := range []string{"%v", "%+v", "%#v"} {
				assert.NotContains(t, fmt.Sprintf(formato, atual), marca,
					"regra 7: %s sobre o erro não pode expor conteúdo do documento", formato)
			}
		}
		switch desembrulhavel := atual.(type) { //nolint:errorlint // o type switch É o mecanismo de varredura: errors.As pararia no primeiro match e deixaria elos sem conferir.
		case interface{ Unwrap() error }:
			pendentes = append(pendentes, desembrulhavel.Unwrap())
		case interface{ Unwrap() []error }:
			pendentes = append(pendentes, desembrulhavel.Unwrap()...)
		}
	}
}

func TestExecutarFormatarNaoVazaConteudoEmNenhumCaminhoDeFalha(t *testing.T) {
	t.Parallel()
	docxMarcado := montarDocxMinimoExecutor(t, marcaTextoExecutor+" parágrafo confidencial do usuário")
	cdmMarcado := cdmDoDocxExecutor(t, docxMarcado)
	require.Contains(t, string(cdmMarcado), marcaTextoExecutor,
		"pré-condição: o CDM persistido carrega a marca em texto_resumo")

	docxNumPr := montarDocxCorpoExecutor(t,
		`<w:p><w:r><w:t>zero</w:t></w:r></w:p>`+
			`<w:p><w:pPr><w:numPr><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t>`+marcaTextoExecutor+`</w:t></w:r></w:p>`+
			`<w:sectPr><w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/></w:sectPr>`)

	casos := []struct {
		nome    string
		monta   func(*testing.T) (documentoentity.Documento, []byte, *uuid.UUID)
		prepara func(*repoFormatarFake, *armazenadorExecutorFake, *rulesetsExecutorFake)
	}{
		{
			nome: "ruleset do job nulo",
			monta: func(t *testing.T) (documentoentity.Documento, []byte, *uuid.UUID) {
				return documentoMarcadoExecutor(t, documentoentity.StatusAnalisado, cdmMarcado, vo.FormatoDocx), docxMarcado, nil
			},
			prepara: func(*repoFormatarFake, *armazenadorExecutorFake, *rulesetsExecutorFake) {},
		},
		{
			nome: "formato não é docx",
			monta: func(t *testing.T) (documentoentity.Documento, []byte, *uuid.UUID) {
				return documentoMarcadoExecutor(t, documentoentity.StatusAnalisado, cdmMarcado, vo.FormatoPDF), docxMarcado, idRulesetExecutor()
			},
			prepara: func(*repoFormatarFake, *armazenadorExecutorFake, *rulesetsExecutorFake) {},
		},
		{
			nome: "documento ainda em recebido",
			monta: func(t *testing.T) (documentoentity.Documento, []byte, *uuid.UUID) {
				return documentoMarcadoExecutor(t, documentoentity.StatusRecebido, cdmMarcado, vo.FormatoDocx), docxMarcado, idRulesetExecutor()
			},
			prepara: func(*repoFormatarFake, *armazenadorExecutorFake, *rulesetsExecutorFake) {},
		},
		{
			nome: "perfil inativo",
			monta: func(t *testing.T) (documentoentity.Documento, []byte, *uuid.UUID) {
				return documentoMarcadoExecutor(t, documentoentity.StatusAnalisado, cdmMarcado, vo.FormatoDocx), docxMarcado, idRulesetExecutor()
			},
			prepara: func(_ *repoFormatarFake, _ *armazenadorExecutorFake, r *rulesetsExecutorFake) { r.ativo = false },
		},
		{
			nome: "obter perfil falha",
			monta: func(t *testing.T) (documentoentity.Documento, []byte, *uuid.UUID) {
				return documentoMarcadoExecutor(t, documentoentity.StatusAnalisado, cdmMarcado, vo.FormatoDocx), docxMarcado, idRulesetExecutor()
			},
			prepara: func(_ *repoFormatarFake, _ *armazenadorExecutorFake, r *rulesetsExecutorFake) {
				r.erro = infraerrors.NovoErroAplicacao("catálogo indisponível")
			},
		},
		{
			nome: "perde o CAS de iniciar",
			monta: func(t *testing.T) (documentoentity.Documento, []byte, *uuid.UUID) {
				return documentoMarcadoExecutor(t, documentoentity.StatusAnalisado, cdmMarcado, vo.FormatoDocx), docxMarcado, idRulesetExecutor()
			},
			prepara: func(r *repoFormatarFake, _ *armazenadorExecutorFake, _ *rulesetsExecutorFake) {
				r.erros[documentoentity.StatusFormatando] = infraerrors.NovoErroConflito("status do documento mudou")
			},
		},
		{
			nome: "cdm corrompido",
			monta: func(t *testing.T) (documentoentity.Documento, []byte, *uuid.UUID) {
				corrompido := json.RawMessage(`{"versao":1,"blocos":[{"texto_resumo":"` + marcaTextoExecutor + `"`)
				return documentoMarcadoExecutor(t, documentoentity.StatusAnalisado, corrompido, vo.FormatoDocx), docxMarcado, idRulesetExecutor()
			},
			prepara: func(*repoFormatarFake, *armazenadorExecutorFake, *rulesetsExecutorFake) {},
		},
		{
			nome: "baixar original falha",
			monta: func(t *testing.T) (documentoentity.Documento, []byte, *uuid.UUID) {
				return documentoMarcadoExecutor(t, documentoentity.StatusAnalisado, cdmMarcado, vo.FormatoDocx), docxMarcado, idRulesetExecutor()
			},
			prepara: func(_ *repoFormatarFake, a *armazenadorExecutorFake, _ *rulesetsExecutorFake) {
				a.erroObter = infraerrors.NovoErroAplicacao("storage indisponível")
			},
		},
		{
			nome: "bytes que não são docx",
			monta: func(t *testing.T) (documentoentity.Documento, []byte, *uuid.UUID) {
				return documentoMarcadoExecutor(t, documentoentity.StatusAnalisado, cdmMarcado, vo.FormatoDocx),
					[]byte("isto não é um docx, só " + marcaTextoExecutor), idRulesetExecutor()
			},
			prepara: func(*repoFormatarFake, *armazenadorExecutorFake, *rulesetsExecutorFake) {},
		},
		{
			nome: "aplicar plano recusa parágrafo com numPr",
			monta: func(t *testing.T) (documentoentity.Documento, []byte, *uuid.UUID) {
				return documentoMarcadoExecutor(t, documentoentity.StatusAnalisado,
						cdmManualExecutor(t, map[int]cdm.Papel{0: cdm.Paragrafo, 1: cdm.Paragrafo}), vo.FormatoDocx),
					docxNumPr, idRulesetExecutor()
			},
			prepara: func(*repoFormatarFake, *armazenadorExecutorFake, *rulesetsExecutorFake) {},
		},
		{
			nome: "salvar artefato falha",
			monta: func(t *testing.T) (documentoentity.Documento, []byte, *uuid.UUID) {
				return documentoMarcadoExecutor(t, documentoentity.StatusAnalisado, cdmMarcado, vo.FormatoDocx), docxMarcado, idRulesetExecutor()
			},
			prepara: func(_ *repoFormatarFake, a *armazenadorExecutorFake, _ *rulesetsExecutorFake) {
				a.erroSalvar = infraerrors.NovoErroAplicacao("storage indisponível ao salvar")
			},
		},
		{
			nome: "concluir formatação falha",
			monta: func(t *testing.T) (documentoentity.Documento, []byte, *uuid.UUID) {
				return documentoMarcadoExecutor(t, documentoentity.StatusAnalisado, cdmMarcado, vo.FormatoDocx), docxMarcado, idRulesetExecutor()
			},
			prepara: func(r *repoFormatarFake, _ *armazenadorExecutorFake, _ *rulesetsExecutorFake) {
				r.erros[documentoentity.StatusFormatado] = infraerrors.NovoErroAplicacao("banco indisponível")
			},
		},
		{
			nome: "marcar falha também falha",
			monta: func(t *testing.T) (documentoentity.Documento, []byte, *uuid.UUID) {
				return documentoMarcadoExecutor(t, documentoentity.StatusAnalisado, cdmMarcado, vo.FormatoDocx), docxMarcado, idRulesetExecutor()
			},
			prepara: func(r *repoFormatarFake, a *armazenadorExecutorFake, _ *rulesetsExecutorFake) {
				a.erroSalvar = infraerrors.NovoErroAplicacao("storage indisponível ao salvar")
				r.erros[documentoentity.StatusFalhou] = infraerrors.NovoErroAplicacao("banco indisponível")
			},
		},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()
			documento, docx, rulesetID := caso.monta(t)
			var saida bytes.Buffer
			repo := novoRepoFormatarFake(documento)
			armazenador := &armazenadorExecutorFake{conteudo: docx}
			rulesets := rulesetsAtivoExecutor()
			caso.prepara(repo, armazenador, rulesets)
			executor := executorFormatarDeTeste(t, repo, armazenador, rulesets, log.Novo("debug", &saida))

			resultado, err := executor.Executar(context.Background(), jobFormatarExecutor(documento.ID, rulesetID))

			require.Error(t, err, "A13: o caso precisa falhar para haver erro a inspecionar")
			assert.Nil(t, resultado)
			exigirSemMarcaExecutor(t, err, marcaTextoExecutor, marcaNomeExecutor)
			assert.NotContains(t, saida.String(), marcaTextoExecutor,
				"regra 7: nenhuma linha de log do executor cita texto do documento")
			assert.NotContains(t, saida.String(), marcaNomeExecutor,
				"regra 7: nenhuma linha de log do executor cita o nome do arquivo do usuário")
		})
	}
}

// ---------------------------------------------------------------------------
// A18 (C3) — CDM OBSOLETO entre o passo (1) e o passo (4).
//
// O executor lê o documento no passo (1) e transita no passo (4). Entre as
// duas chamadas, um job analisar concorrente pode regravar o CDM e devolver o
// status a analisado: o CAS de IniciarFormatacao não vê corrida alguma,
// porque compara analisado com analisado. Como transitarStatus
// (processamento/servico.go:95) faz leitura FRESCA e DEVOLVE o documento
// atual, o conserto é montar o plano a partir desse retorno — e não do
// documento lido antes da transição.
//
// Observável: QUAIS parágrafos do artefato saíram formatados. É o efeito
// final do plano no arquivo entregue ao usuário, não um espelho da
// implementação; os dois CDMs selecionam ordinais DISJUNTOS, então o
// parágrafo do CDM obsoleto tem de sair sem w:pPr e sem w:rPr.
// ---------------------------------------------------------------------------

func referenciasDoCDMExecutor(t *testing.T, dados json.RawMessage, definicao ruleset.Definicao) []int {
	t.Helper()
	indice, err := cdm.Desserializar(dados)
	require.NoError(t, err)
	plano, err := formatador.Planejar(indice, definicao)
	require.NoError(t, err)
	return plano.ReferenciasCorpo
}

func TestExecutarFormatarMontaPlanoComCDMDevolvidoPorIniciarFormatacao(t *testing.T) {
	t.Parallel()
	docx := montarDocxCorpoExecutor(t,
		`<w:p><w:r><w:t>primeiro bloco do corpo</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>segundo bloco do corpo</w:t></w:r></w:p>`+
			`<w:sectPr><w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/></w:sectPr>`)

	cdmObsoleto := cdmManualExecutor(t, map[int]cdm.Papel{0: cdm.Paragrafo, 1: cdm.Titulo})
	cdmAtual := cdmManualExecutor(t, map[int]cdm.Papel{0: cdm.Titulo, 1: cdm.Paragrafo})

	definicao := definicaoSinteticaExecutor()
	require.Equal(t, []int{0}, referenciasDoCDMExecutor(t, cdmObsoleto, definicao),
		"pré-condição: o CDM obsoleto selecionaria o parágrafo 0")
	require.Equal(t, []int{1}, referenciasDoCDMExecutor(t, cdmAtual, definicao),
		"pré-condição: o CDM atual seleciona o parágrafo 1, disjunto do anterior")

	documento := documentoFormatavelExecutor(t, documentoentity.StatusAnalisado, cdmObsoleto)
	repo := novoRepoFormatarFake(documento)
	// Leitura 1 = passo (1) do executor, com o CDM obsoleto. Leitura 2 = a
	// leitura fresca de transitarStatus dentro de IniciarFormatacao, já com o
	// CDM que o job analisar concorrente regravou.
	repo.cdmNaLeitura = map[int]json.RawMessage{2: cdmAtual}
	armazenador := &armazenadorExecutorFake{conteudo: docx}
	rulesets := rulesetsAtivoExecutor()
	executor := executorFormatarDeTeste(t, repo, armazenador, rulesets, log.Novo("debug", io.Discard))

	resultado, err := executor.Executar(context.Background(), jobFormatarExecutor(documento.ID, idRulesetExecutor()))

	require.NoError(t, err, "A18: a corrida não é um erro — o status é analisado nos dois lados")
	require.NotNil(t, resultado)
	require.GreaterOrEqual(t, repo.leituras, 2,
		"pré-condição: IniciarFormatacao precisa fazer a leitura fresca que troca o CDM")
	require.Len(t, armazenador.salvos, 1, "A18: exatamente um artefato salvo")

	paragrafos := paragrafosPorOrdinalExecutor(t,
		parteDoZipExecutor(t, armazenador.salvos[0].conteudo, "word/document.xml"))
	require.Len(t, paragrafos, 2, "pré-condição: os dois parágrafos do caso sobrevivem no artefato")

	// A ordem é deliberada: os dois asserts do parágrafo OBSOLETO não abortam
	// o teste, então o RED mostra o sintoma inteiro antes de parar no require
	// de atributosUnicosExecutor.
	assert.Nil(t, paragrafos[0].PPr,
		"A18: o parágrafo do CDM OBSOLETO não pode sair formatado — o plano saiu da leitura anterior à transição")
	require.NotEmpty(t, paragrafos[0].Runs, "pré-condição: o parágrafo 0 tem run para a tipografia valer")
	assert.Nil(t, paragrafos[0].Runs[0].RPr,
		"A18: nem a tipografia do parágrafo do CDM obsoleto pode ser aplicada")
	assert.Equal(t, "both", atributosUnicosExecutor(t, paragrafos[1].PPr, "jc")["val"],
		"A18: o parágrafo do CDM devolvido por IniciarFormatacao precisa sair formatado")
}
