//go:build integration

package postgres_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/daniel-halos/formatador/internal/application/web/webmodel"
	"github.com/daniel-halos/formatador/internal/application/web/webservices"
	"github.com/daniel-halos/formatador/internal/domain/cdm"
	"github.com/daniel-halos/formatador/internal/domain/documento/entity"
	documentoservice "github.com/daniel-halos/formatador/internal/domain/documento/service"
	"github.com/daniel-halos/formatador/internal/domain/job/consulta"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/rotas"
	"github.com/daniel-halos/formatador/internal/rotas/root/webrotas/documentos"
	"github.com/daniel-halos/formatador/internal/rotas/root/webrotas/jobs"
	"github.com/daniel-halos/formatador/internal/rotas/sessao"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

func TestF2CorrecaoEConsultaJobHTTPComPostgres(t *testing.T) {
	ctx := context.Background()
	sessaoID := uuid.New()
	dono, err := vo.NovoDonoSessao(sessaoID)
	require.NoError(t, err)
	documento := inserirDocumento(ctx, t, dono, 100)
	bloco, err := cdm.NovoBloco(cdm.Paragrafo, "Texto que deve ser preservado", 0.4, cdm.OrigemHeuristica, 0)
	require.NoError(t, err)
	dados, err := cdm.NovoIndice([]cdm.Bloco{bloco}).Serializar()
	require.NoError(t, err)
	require.NoError(t, gerente.DocumentosInternos().DefinirCDM(ctx, documento.ID, dados, entity.StatusRecebido, entity.StatusAnalisado))
	idJob := inserirJobFixtureSQL(ctx, t, documento.ID)
	_, err = bancoSQL.ExecContext(ctx, `UPDATE jobs SET resultado='{"chave":"PRIVADO-STORAGE"}'::jsonb WHERE id=$1`, idJob)
	require.NoError(t, err)
	dominio, err := documentoservice.NovoServicoEstrutura(gerente.Estruturas())
	require.NoError(t, err)
	aplicacao, err := webservices.NovoServicoEstrutura(dominio)
	require.NoError(t, err)
	servicoDocumento, err := documentoservice.NovoServico(gerente.Documentos(), 0)
	require.NoError(t, err)
	consultas, err := consulta.NovoServico(gerente.JobsConsulta(), servicoDocumento)
	require.NoError(t, err)
	servicoJob, err := webservices.NovoServicoJob(consultas)
	require.NoError(t, err)
	servidor := echo.New()
	rotas.AplicarEmEcho(servidor, documentos.Roteador(documentos.NovoControlador(nil, nil, aplicacao), 1<<20), "/v1")
	rotas.AplicarEmEcho(servidor, jobs.Roteador(jobs.NovoControlador(servicoJob)), "/v1")
	chamar := func(metodo, caminho, corpo, cookie string) *httptest.ResponseRecorder {
		requisicao := httptest.NewRequest(metodo, caminho, strings.NewReader(corpo))
		requisicao.Header.Set("Content-Type", "application/json")
		if cookie != "" {
			requisicao.AddCookie(&http.Cookie{Name: sessao.NomeCookie, Value: cookie})
		}
		resposta := httptest.NewRecorder()
		servidor.ServeHTTP(resposta, requisicao)
		return resposta
	}
	caminho := "/v1/documentos/" + documento.ID.String() + "/estrutura"
	resposta := chamar(http.MethodPatch, caminho, `{"ref_xml":0,"papel":"titulo"}`, sessaoID.String())
	require.Equal(t, 200, resposta.Code, resposta.Body.String())
	var publico webmodel.EstruturaResposta
	require.NoError(t, json.Unmarshal(resposta.Body.Bytes(), &publico))
	require.Equal(t, "usuario", publico.Blocos[0].Origem)
	salvo, err := gerente.Documentos().ObterPorID(ctx, dono, documento.ID)
	require.NoError(t, err)
	indice, err := cdm.Desserializar(salvo.CDM)
	require.NoError(t, err)
	require.Equal(t, bloco.TextoResumo, indice.Blocos[0].TextoResumo)
	reclassificado, err := indice.Blocos[0].Reclassificar(cdm.Paragrafo, 0.8, cdm.OrigemHeuristica)
	require.NoError(t, err)
	require.Equal(t, indice.Blocos[0], reclassificado)
	for _, cookie := range []string{"", uuid.New().String()} {
		require.Equal(t, 404, chamar(http.MethodPatch, caminho, `{"ref_xml":0,"papel":"resumo"}`, cookie).Code)
		alheio := chamar(http.MethodGet, "/v1/jobs/"+idJob.String(), "", cookie)
		ausente := chamar(http.MethodGet, "/v1/jobs/"+uuid.New().String(), "", cookie)
		require.Equal(t, 404, alheio.Code)
		require.Equal(t, ausente.Body.String(), alheio.Body.String())
	}
	resposta = chamar(http.MethodGet, "/v1/jobs/"+idJob.String(), "", sessaoID.String())
	require.Equal(t, 200, resposta.Code, resposta.Body.String())
	require.NotContains(t, resposta.Body.String(), "PRIVADO-STORAGE")
	require.NotContains(t, resposta.Body.String(), "resultado")
}
