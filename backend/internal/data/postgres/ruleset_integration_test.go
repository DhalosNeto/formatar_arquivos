//go:build integration

package postgres_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/daniel-halos/formatador/internal/domain/ruleset"
	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func perfilSeed() ruleset.Definicao {
	return ruleset.Definicao{Slug: "teste-" + uuid.NewString(), Versao: 1, Nome: "Perfil sintético 文 😀 ação", Fonte: "https://example.org/perfil", Pagina: ruleset.Pagina{LarguraCM: 20, AlturaCM: 28}, Corpo: ruleset.Corpo{Fonte: "Fonte Sintética", TamanhoPT: 11, Entrelinha: 1.25, Alinhamento: "justificado"}}
}

type estadoSeed struct {
	ID, Nome, JSON, Checksum, Tupla string
	Ativo                           bool
}

func lerSeed(t *testing.T, perfil ruleset.Definicao) estadoSeed {
	t.Helper()
	var estado estadoSeed
	require.NoError(t, bancoSQL.QueryRowContext(context.Background(),
		"SELECT id, nome, definicao::text, checksum, ativo, xmin::text || ctid::text FROM rulesets WHERE slug=$1 AND versao=$2",
		perfil.Slug, perfil.Versao).Scan(&estado.ID, &estado.Nome, &estado.JSON, &estado.Checksum, &estado.Ativo, &estado.Tupla))
	return estado
}

func contarSeed(t *testing.T, slug string) int {
	t.Helper()
	var quantidade int
	require.NoError(t, bancoSQL.QueryRowContext(context.Background(), "SELECT count(*) FROM rulesets WHERE slug=$1", slug).Scan(&quantidade))
	return quantidade
}

func TestSemearInsercaoRepeticaoENovaVersao(t *testing.T) {
	ctx := context.Background()
	perfil := perfilSeed()
	require.NoError(t, gerente.Rulesets().Semear(ctx, []ruleset.Definicao{perfil}))
	original := lerSeed(t, perfil)
	serializado, err := json.Marshal(perfil)
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("%x", sha256.Sum256(serializado)), original.Checksum)
	assert.JSONEq(t, string(serializado), original.JSON)
	assert.Equal(t, perfil.Nome, original.Nome)
	assert.True(t, original.Ativo)
	_, err = uuid.Parse(original.ID)
	assert.NoError(t, err)
	require.NoError(t, gerente.Rulesets().Semear(ctx, []ruleset.Definicao{perfil, perfil}))
	assert.Equal(t, original, lerSeed(t, perfil), "repetir não pode executar UPDATE, nem mesmo com valores iguais")

	// Alteração administrativa da fixture: o seed não pode reativar nem recalcular hash.
	_, err = bancoSQL.ExecContext(ctx, "UPDATE rulesets SET ativo=false, checksum=$2 WHERE slug=$1", perfil.Slug, "checksum-legado")
	require.NoError(t, err)
	inativo := lerSeed(t, perfil)
	require.NoError(t, gerente.Rulesets().Semear(ctx, []ruleset.Definicao{perfil}))
	assert.Equal(t, inativo, lerSeed(t, perfil))
	nova := perfil
	nova.Versao = 2
	nova.Corpo.TamanhoPT = 15
	require.NoError(t, gerente.Rulesets().Semear(ctx, []ruleset.Definicao{nova}))
	assert.Equal(t, 2, contarSeed(t, perfil.Slug))
	assert.NotEqual(t, original.ID, lerSeed(t, nova).ID)
	assert.Equal(t, inativo, lerSeed(t, perfil))
}

func TestSemearConflitoDesfazInsercaoAnterior(t *testing.T) {
	casos := []struct {
		nome    string
		alterar func(*ruleset.Definicao)
	}{
		{"definicao_diferente", func(d *ruleset.Definicao) { d.Corpo.TamanhoPT = 15 }},
		{"somente_nome_diferente", func(d *ruleset.Definicao) { d.Nome = "Outro perfil" }},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			ctx := context.Background()
			existente := perfilSeed()
			existente.Slug = "z-existente-" + uuid.NewString()
			novo := perfilSeed()
			novo.Slug = "a-novo-" + uuid.NewString()
			require.Less(t, novo.Slug, existente.Slug)
			require.NoError(t, gerente.Rulesets().Semear(ctx, []ruleset.Definicao{existente}))
			original := lerSeed(t, existente)
			alterado := existente
			caso.alterar(&alterado)
			var conflito *errors.ErroConflito
			assert.ErrorAs(t, gerente.Rulesets().Semear(ctx, []ruleset.Definicao{alterado, novo}), &conflito)
			assert.Zero(t, contarSeed(t, novo.Slug))
			assert.Equal(t, original, lerSeed(t, existente))
		})
	}
}

func TestSemearComparaNomeDaColuna(t *testing.T) {
	perfil := perfilSeed()
	ctx := context.Background()
	require.NoError(t, gerente.Rulesets().Semear(ctx, []ruleset.Definicao{perfil}))
	_, err := bancoSQL.ExecContext(ctx, "UPDATE rulesets SET nome=$2 WHERE slug=$1", perfil.Slug, "Nome divergente apenas na coluna")
	require.NoError(t, err)
	original := lerSeed(t, perfil)
	var conflito *errors.ErroConflito
	assert.ErrorAs(t, gerente.Rulesets().Semear(ctx, []ruleset.Definicao{perfil}), &conflito)
	assert.Equal(t, original, lerSeed(t, perfil))
}

func TestSemearDuplicatasDivergentes(t *testing.T) {
	perfil := perfilSeed()
	alterado := perfil
	alterado.Corpo.TamanhoPT = 15
	var conflito *errors.ErroConflito
	assert.ErrorAs(t, gerente.Rulesets().Semear(context.Background(), []ruleset.Definicao{perfil, alterado}), &conflito)
	assert.Zero(t, contarSeed(t, perfil.Slug))
}

func TestSemearLoteInvalidoECancelado(t *testing.T) {
	t.Run("valido_seguido_de_invalido", func(t *testing.T) {
		perfil := perfilSeed()
		var validacao *errors.ErroValidacao
		assert.ErrorAs(t, gerente.Rulesets().Semear(context.Background(), []ruleset.Definicao{perfil, {}}), &validacao)
		assert.Zero(t, contarSeed(t, perfil.Slug))
	})
	t.Run("contexto_cancelado", func(t *testing.T) {
		perfil := perfilSeed()
		ctx, cancelar := context.WithCancel(context.Background())
		cancelar()
		assert.Error(t, gerente.Rulesets().Semear(ctx, []ruleset.Definicao{perfil}))
		assert.Zero(t, contarSeed(t, perfil.Slug))
	})
	for _, caso := range []struct {
		nome string
		lote []ruleset.Definicao
	}{
		{"lote_nil", nil}, {"lote_vazio", []ruleset.Definicao{}},
	} {
		t.Run(caso.nome, func(t *testing.T) { assert.NoError(t, gerente.Rulesets().Semear(context.Background(), caso.lote)) })
	}
}

func TestSemearLimite4096(t *testing.T) {
	perfil := perfilSeed()
	lote := make([]ruleset.Definicao, 4096)
	for i := range lote {
		lote[i] = perfil
		lote[i].Versao = i + 1
	}
	require.NoError(t, gerente.Rulesets().Semear(context.Background(), lote))
	assert.Equal(t, 4096, contarSeed(t, perfil.Slug))
}

func TestSemearConcorrenteOrdemInversa(t *testing.T) {
	// Canais sincronizam a largada; o prazo é apenas proteção contra deadlock.
	ctx, cancelar := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelar()
	primeiro, segundo := perfilSeed(), perfilSeed()
	primeiro.Slug = "a-" + uuid.NewString()
	segundo.Slug = "z-" + uuid.NewString()
	versao2 := primeiro
	versao2.Versao = 2
	inicio := make(chan struct{})
	resultados := make(chan error, 8)
	for i := range 8 {
		lote := []ruleset.Definicao{primeiro, versao2, segundo}
		if i%2 != 0 {
			lote = []ruleset.Definicao{segundo, versao2, primeiro}
		}
		go func() {
			<-inicio
			resultados <- gerente.Rulesets().Semear(ctx, lote)
		}()
	}
	close(inicio)
	for range 8 {
		assert.NoError(t, <-resultados)
	}
	assert.Equal(t, 2, contarSeed(t, primeiro.Slug))
	assert.Equal(t, 1, contarSeed(t, segundo.Slug))
}

func TestSemearConcorrenteDivergente(t *testing.T) {
	ctx, cancelar := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelar()
	primeiro := perfilSeed()
	segundo := primeiro
	segundo.Corpo.TamanhoPT = 15
	inicio := make(chan struct{})
	type resultado struct {
		perfil ruleset.Definicao
		err    error
	}
	resultados := make(chan resultado, 2)
	for _, perfil := range []ruleset.Definicao{primeiro, segundo} {
		go func() {
			<-inicio
			resultados <- resultado{perfil, gerente.Rulesets().Semear(ctx, []ruleset.Definicao{perfil})}
		}()
	}
	close(inicio)
	sucessos, conflitos := 0, 0
	for range 2 {
		resultado := <-resultados
		if resultado.err == nil {
			sucessos++
			serializado, err := json.Marshal(resultado.perfil)
			require.NoError(t, err)
			gravado := lerSeed(t, resultado.perfil)
			assert.JSONEq(t, string(serializado), gravado.JSON)
			assert.Equal(t, fmt.Sprintf("%x", sha256.Sum256(serializado)), gravado.Checksum)
		} else {
			var conflito *errors.ErroConflito
			if assert.ErrorAs(t, resultado.err, &conflito) {
				conflitos++
			}
		}
	}
	assert.Equal(t, 1, sucessos)
	assert.Equal(t, 1, conflitos)
	assert.Equal(t, 1, contarSeed(t, primeiro.Slug))
}

func TestSemearPrazoDuranteLock(t *testing.T) {
	perfil := perfilSeed()
	require.NoError(t, gerente.Rulesets().Semear(context.Background(), []ruleset.Definicao{perfil}))
	transacao, err := bancoSQL.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	defer func() {
		if transacao != nil {
			assert.NoError(t, transacao.Rollback())
		}
	}()
	var id string
	require.NoError(t, transacao.QueryRowContext(context.Background(),
		"SELECT id FROM rulesets WHERE slug=$1 AND versao=$2 FOR UPDATE", perfil.Slug, perfil.Versao).Scan(&id))
	ctx, cancelar := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancelar()
	assert.ErrorIs(t, gerente.Rulesets().Semear(ctx, []ruleset.Definicao{perfil}), context.DeadlineExceeded)
	require.NoError(t, transacao.Rollback())
	transacao = nil
	assert.Equal(t, id, lerSeed(t, perfil).ID)
}
