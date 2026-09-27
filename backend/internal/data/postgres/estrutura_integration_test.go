//go:build integration

package postgres_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/daniel-halos/formatador/internal/domain/documento/entity"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestEstruturaCAS(t *testing.T) {
	ctx := context.Background()
	identidade := criarUsuario(t)
	usuario, err := vo.NovoDonoUsuario(identidade)
	require.NoError(t, err)
	sessao, err := vo.NovoDonoSessao(identidade)
	require.NoError(t, err)
	anterior := json.RawMessage(`{"versao":1,"blocos":[]}`)
	equivalente := json.RawMessage(`{ "blocos": [], "versao": 1 }`)
	novo := json.RawMessage(`{"versao":1,"blocos":[],"marca":1}`)
	for _, dono := range []vo.Dono{usuario, sessao} {
		t.Run(string(dono.Especie()), func(t *testing.T) {
			documento := inserirDocumento(ctx, t, dono, 10)
			require.NoError(t, gerente.DocumentosInternos().DefinirCDM(ctx, documento.ID, anterior, entity.StatusRecebido, entity.StatusAnalisado))
			intruso := usuario
			if dono.Especie() == vo.EspecieUsuario {
				intruso = sessao
			}
			var ausente *errors.ErroNaoEncontrado
			require.ErrorAs(t, gerente.Estruturas().SalvarEstrutura(ctx, intruso, documento.ID, anterior, novo, entity.StatusAnalisado), &ausente)
			require.ErrorAs(t, gerente.Estruturas().SalvarEstrutura(ctx, dono, uuid.New(), anterior, novo, entity.StatusAnalisado), &ausente)
			require.NoError(t, gerente.Estruturas().SalvarEstrutura(ctx, dono, documento.ID, equivalente, novo, entity.StatusAnalisado))
			var conflito *errors.ErroConflito
			require.ErrorAs(t, gerente.Estruturas().SalvarEstrutura(ctx, dono, documento.ID, anterior, novo, entity.StatusAnalisado), &conflito)
			require.NoError(t, gerente.DocumentosInternos().AtualizarStatus(ctx, documento.ID, entity.StatusAnalisado, entity.StatusFormatando))
			require.ErrorAs(t, gerente.Estruturas().SalvarEstrutura(ctx, dono, documento.ID, novo, anterior, entity.StatusAnalisado), &conflito)
			salvo, err := gerente.Estruturas().ObterPorID(ctx, dono, documento.ID)
			require.NoError(t, err)
			require.JSONEq(t, string(novo), string(salvo.CDM))
			require.Equal(t, entity.StatusFormatando, salvo.Status)
		})
	}
	t.Run("concorrencia real", func(t *testing.T) {
		documento := inserirDocumento(ctx, t, sessao, 10)
		require.NoError(t, gerente.DocumentosInternos().DefinirCDM(ctx, documento.ID, anterior, entity.StatusRecebido, entity.StatusAnalisado))
		resultados := make(chan error, 2)
		inicio := make(chan struct{})
		var grupo sync.WaitGroup
		for _, proximo := range []json.RawMessage{novo, json.RawMessage(`{"versao":1,"blocos":[],"marca":2}`)} {
			grupo.Add(1)
			go func(proximo json.RawMessage) {
				defer grupo.Done()
				<-inicio
				resultados <- gerente.Estruturas().SalvarEstrutura(ctx, sessao, documento.ID, anterior, proximo, entity.StatusAnalisado)
			}(proximo)
		}
		close(inicio)
		grupo.Wait()
		close(resultados)
		sucessos, conflitos := 0, 0
		for err := range resultados {
			if err == nil {
				sucessos++
			} else {
				var conflito *errors.ErroConflito
				require.ErrorAs(t, err, &conflito)
				conflitos++
			}
		}
		require.Equal(t, 1, sucessos)
		require.Equal(t, 1, conflitos)
	})
}
