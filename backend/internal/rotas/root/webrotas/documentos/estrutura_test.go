package documentos

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/daniel-halos/formatador/internal/application/web/webservices"
	"github.com/daniel-halos/formatador/internal/domain/documento/entity"
	dominioservice "github.com/daniel-halos/formatador/internal/domain/documento/service"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/daniel-halos/formatador/internal/rotas"
	"github.com/daniel-halos/formatador/internal/rotas/sessao"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

type estruturaRepoHTTP struct {
	documento entity.Documento
	escritas  int
}

func (repo *estruturaRepoHTTP) ObterPorID(_ context.Context, dono vo.Dono, id uuid.UUID) (entity.Documento, error) {
	if id != repo.documento.ID || !dono.PodeAcessar(repo.documento.Dono) {
		return entity.Documento{}, errors.NovoErroNaoEncontrado("documento")
	}
	return repo.documento, nil
}

func (repo *estruturaRepoHTTP) SalvarEstrutura(_ context.Context, dono vo.Dono, id uuid.UUID, anterior, novo json.RawMessage, status entity.Status) error {
	if id != repo.documento.ID || !dono.PodeAcessar(repo.documento.Dono) {
		return errors.NovoErroNaoEncontrado("documento")
	}
	if string(anterior) != string(repo.documento.CDM) || status != repo.documento.Status {
		return errors.NovoErroConflito("estrutura alterada")
	}
	repo.escritas++
	repo.documento.CDM = novo
	return nil
}

func TestCorrecaoEstruturaHTTP(t *testing.T) {
	for _, caso := range []struct {
		nome, corpo, cookie string
		status              int
	}{
		{"corrige", `{"ref_xml":0,"papel":"secao","nivel":2}`, "dono", 200},
		{"sem cookie", `{"ref_xml":0,"papel":"titulo"}`, "", 404},
		{"terceiro", `{"ref_xml":0,"papel":"titulo"}`, "terceiro", 404},
		{"referencia ausente", `{"papel":"titulo"}`, "dono", 400},
		{"referencia nula", `{"ref_xml":null,"papel":"titulo"}`, "dono", 400},
		{"referencia inexistente", `{"ref_xml":999,"papel":"titulo"}`, "dono", 400},
		{"origem injetada", `{"ref_xml":0,"papel":"titulo","origem":"llm"}`, "dono", 400},
		{"texto injetado", `{"ref_xml":0,"papel":"titulo","texto_resumo":"SEGREDO"}`, "dono", 400},
		{"nivel invalido", `{"ref_xml":0,"papel":"secao","nivel":7}`, "dono", 400},
		{"papel desconhecido", `{"ref_xml":0,"papel":"SEGREDO"}`, "dono", 400},
		{"segundo objeto", `{"ref_xml":0,"papel":"titulo"} {}`, "dono", 400},
		{"campo duplicado", `{"ref_xml":0,"ref_xml":1,"papel":"titulo"}`, "dono", 400},
		{"nivel nulo", `{"ref_xml":0,"papel":"titulo","nivel":null}`, "dono", 400},
		{"objeto nulo", `null`, "dono", 400},
		{"papel ausente", `{"ref_xml":0}`, "dono", 400},
		{"corpo grande", strings.Repeat(" ", 4097), "dono", 400},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			sessaoID := uuid.New()
			dono, err := vo.NovoDonoSessao(sessaoID)
			require.NoError(t, err)
			documento, err := entity.NovoDocumento(dono, "artigo.docx", vo.FormatoDocx, 100)
			require.NoError(t, err)
			documento.Status = entity.StatusAnalisado
			documento.CDM = cdmValidoDeTeste(t)
			repo := &estruturaRepoHTTP{documento: documento}
			dominio, err := dominioservice.NovoServicoEstrutura(repo)
			require.NoError(t, err)
			aplicacao, err := webservices.NovoServicoEstrutura(dominio)
			require.NoError(t, err)
			servidor := echo.New()
			rotas.AplicarEmEcho(servidor, Roteador(NovoControlador(nil, nil, aplicacao), 1<<20), "/v1")
			requisicao := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/v1/documentos/"+documento.ID.String()+"/estrutura", strings.NewReader(caso.corpo))
			requisicao.Header.Set("Content-Type", "application/json")
			if caso.cookie != "" {
				valor := sessaoID.String()
				if caso.cookie == "terceiro" {
					valor = uuid.New().String()
				}
				requisicao.AddCookie(&http.Cookie{Name: sessao.NomeCookie, Value: valor})
			}
			resposta := httptest.NewRecorder()
			servidor.ServeHTTP(resposta, requisicao)
			require.Equal(t, caso.status, resposta.Code, resposta.Body.String())
			if caso.status == 200 {
				require.Equal(t, 1, repo.escritas)
				require.Contains(t, resposta.Body.String(), `"origem":"usuario"`)
				require.Contains(t, resposta.Body.String(), `"nivel":2`)
			} else {
				require.Zero(t, repo.escritas)
				require.NotContains(t, resposta.Body.String(), "SEGREDO")
			}
		})
	}
}
