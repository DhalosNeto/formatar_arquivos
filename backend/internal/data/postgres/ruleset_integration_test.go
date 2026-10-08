//go:build integration

package postgres_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/daniel-halos/formatador/internal/domain/ruleset"
	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

// ---------------------------------------------------------------------------
// Recorte f3-leitura-ruleset-por-id — C3 ((*RepositorioRuleset).ObterPorID),
// critérios A8-A14. Exigem Postgres real (testcontainers).
// ---------------------------------------------------------------------------

func semearEObterID(t *testing.T, perfil ruleset.Definicao) uuid.UUID {
	t.Helper()
	require.NoError(t, perfil.Validar(), "pré-condição: o perfil semeado tem de ser válido")
	require.NoError(t, gerente.Rulesets().Semear(context.Background(), []ruleset.Definicao{perfil}))
	id, err := uuid.Parse(lerSeed(t, perfil).ID)
	require.NoError(t, err)
	return id
}

// inserirRulesetDireto grava por SQL direto, sem passar pela porta de seed: é o
// caminho que ruleset.go:66-67 reconhece como fora da garantia de imutabilidade
// e o único jeito de produzir linha corrompida.
func inserirRulesetDireto(t *testing.T, slug string, versao int, nome string, definicao []byte) uuid.UUID {
	t.Helper()
	var id string
	require.NoError(t, bancoSQL.QueryRowContext(context.Background(),
		"INSERT INTO rulesets (slug,versao,nome,definicao,checksum) VALUES ($1,$2,$3,$4::jsonb,$5) RETURNING id",
		slug, versao, nome, string(definicao), fmt.Sprintf("%x", sha256.Sum256(definicao))).Scan(&id))
	convertido, err := uuid.Parse(id)
	require.NoError(t, err)
	return convertido
}

func alterarUmaLinha(t *testing.T, comando string, argumentos ...any) {
	t.Helper()
	resultado, err := bancoSQL.ExecContext(context.Background(), comando, argumentos...)
	require.NoError(t, err)
	afetadas, err := resultado.RowsAffected()
	require.NoError(t, err)
	require.Equal(t, int64(1), afetadas, "pré-condição: a fixture tinha de alterar exatamente uma linha")
}

func definicaoGravada(t *testing.T, id uuid.UUID) ruleset.Definicao {
	t.Helper()
	var bruto string
	require.NoError(t, bancoSQL.QueryRowContext(context.Background(),
		"SELECT definicao::text FROM rulesets WHERE id=$1::uuid", id.String()).Scan(&bruto))
	var definicao ruleset.Definicao
	require.NoError(t, json.Unmarshal([]byte(bruto), &definicao))
	return definicao
}

func octetosGravados(t *testing.T, id uuid.UUID) int {
	t.Helper()
	var octetos int
	require.NoError(t, bancoSQL.QueryRowContext(context.Background(),
		"SELECT octet_length(definicao::text) FROM rulesets WHERE id=$1::uuid", id.String()).Scan(&octetos))
	return octetos
}

// perfilSeedCompleto preenche margens e espaçamentos, que perfilSeed deixa em
// zero: sem isso o Equal de A8 passaria mesmo perdendo campos aninhados.
func perfilSeedCompleto() ruleset.Definicao {
	perfil := perfilSeed()
	perfil.Pagina.Margens = ruleset.Margens{SuperiorCM: 3, InferiorCM: 2, EsquerdaCM: 3, DireitaCM: 2}
	perfil.Corpo.RecuoCM = 1.25
	perfil.Corpo.EspacoAntesPT = 6
	perfil.Corpo.EspacoDepoisPT = 12
	return perfil
}

// A8
func TestObterRulesetPorIDDevolvePerfilSemeado(t *testing.T) {
	perfil := perfilSeedCompleto()
	id := semearEObterID(t, perfil)

	obtida, ativo, err := gerente.RulesetsConsulta().ObterPorID(context.Background(), id)

	require.NoError(t, err)
	assert.Equal(t, perfil, obtida)
	assert.Equal(t, perfil.Pagina.Margens, obtida.Pagina.Margens)
	assert.Equal(t, perfil.Corpo, obtida.Corpo)
	assert.True(t, ativo, "linha recém-semeada tem ativo=true por DEFAULT")
}

// A9
func TestObterRulesetPorIDInexistente(t *testing.T) {
	for _, caso := range []struct {
		nome string
		id   uuid.UUID
	}{
		{"uuid_aleatorio", uuid.New()},
		{"uuid_nil", uuid.Nil},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			definicao, ativo, err := gerente.RulesetsConsulta().ObterPorID(context.Background(), caso.id)

			require.Error(t, err)
			var naoEncontrado *errors.ErroNaoEncontrado
			require.True(t, errors.Como(err, &naoEncontrado), "esperado *ErroNaoEncontrado, obtido %T", err)
			assert.Equal(t, "ruleset", naoEncontrado.Recurso)
			var validacao *errors.ErroValidacao
			assert.False(t, errors.Como(err, &validacao), "id inexistente não é erro de quem fez a requisição")
			assert.False(t, errors.E(err, pgx.ErrNoRows), "erro cru do driver não pode atravessar a porta")
			assert.Zero(t, definicao)
			assert.False(t, ativo)
		})
	}
}

// A10: falha de DOIS jeitos — se alguém acrescentar `AND ativo` ao WHERE, ou se
// o flag não for propagado.
func TestObterRulesetPorIDDevolveFlagInativoSemFiltrar(t *testing.T) {
	perfil := perfilSeedCompleto()
	id := semearEObterID(t, perfil)
	alterarUmaLinha(t, "UPDATE rulesets SET ativo=false WHERE id=$1::uuid", id.String())

	obtida, ativo, err := gerente.RulesetsConsulta().ObterPorID(context.Background(), id)

	require.NoError(t, err, "`AND ativo` no WHERE quebraria a reprodutibilidade de um job já existente")
	assert.Equal(t, perfil, obtida)
	assert.False(t, ativo, "sem o flag propagado o gate futuro na criação de job custaria segunda query")
}

// A11: prova que a revalidação de C2 está no caminho real da leitura, não só na
// função pura. Colunas slug/versao batem com o JSON, então só Validar pode falhar.
func TestObterRulesetPorIDRecusaDefinicaoInvalidaNoBanco(t *testing.T) {
	perfil := perfilSeedCompleto()
	perfil.Slug = "Slug_Invalido-" + uuid.NewString()
	require.Error(t, perfil.Validar(), "pré-condição: slug fora do kebab-case tem de reprovar")
	dados, err := json.Marshal(perfil)
	require.NoError(t, err)
	id := inserirRulesetDireto(t, perfil.Slug, perfil.Versao, perfil.Nome, dados)

	definicao, _, err := gerente.RulesetsConsulta().ObterPorID(context.Background(), id)

	require.Error(t, err)
	var aplicacao *errors.ErroAplicacao
	require.True(t, errors.Como(err, &aplicacao), "corrupção de servidor é 500, obtido %T", err)
	var validacao *errors.ErroValidacao
	assert.False(t, errors.Como(err, &validacao), "dado gravado pelo próprio servidor não é culpa do cliente")
	assert.Zero(t, definicao)
}

// A12: procedência. A UNIQUE (slug,versao) vive nas COLUNAS; se o JSON divergir,
// o documento registraria procedência que não corresponde à linha lida e o
// próximo job com o mesmo UUID aplicaria outro perfil sob o mesmo nome. A decisão
// é composta (slug OU versao), e cada termo tem caso dos dois lados.
func TestObterRulesetPorIDRecusaProcedenciaDivergente(t *testing.T) {
	casos := []struct {
		nome    string
		comando string
		extra   []any
	}{
		{"jsonb_slug_divergente", "UPDATE rulesets SET definicao=jsonb_set(definicao,'{slug}',to_jsonb($2::text)) WHERE id=$1::uuid", []any{"outro-slug-" + uuid.NewString()}},
		{"coluna_slug_divergente", "UPDATE rulesets SET slug=$2 WHERE id=$1::uuid", []any{"outro-slug-" + uuid.NewString()}},
		{"jsonb_versao_divergente", "UPDATE rulesets SET definicao=jsonb_set(definicao,'{versao}',to_jsonb(9::int)) WHERE id=$1::uuid", nil},
		{"coluna_versao_divergente", "UPDATE rulesets SET versao=9 WHERE id=$1::uuid", nil},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			id := semearEObterID(t, perfilSeedCompleto())
			alterarUmaLinha(t, caso.comando, append([]any{id.String()}, caso.extra...)...)
			require.NoError(t, definicaoGravada(t, id).Validar(),
				"pré-condição: o JSON tem de continuar VÁLIDO, senão o caso provaria A11 e não a procedência")

			definicao, _, err := gerente.RulesetsConsulta().ObterPorID(context.Background(), id)

			require.Error(t, err)
			var aplicacao *errors.ErroAplicacao
			require.True(t, errors.Como(err, &aplicacao), "divergência coluna/JSON é corrupção (500), obtido %T", err)
			var validacao *errors.ErroValidacao
			assert.False(t, errors.Como(err, &validacao))
			var naoEncontrado *errors.ErroNaoEncontrado
			assert.False(t, errors.Como(err, &naoEncontrado), "a linha existe: divergência não é 404")
			assert.Zero(t, definicao)
		})
	}
}

// A13: regra 3 — o erro de contexto tem precedência e não vira 404.
func TestObterRulesetPorIDContextoCancelado(t *testing.T) {
	id := semearEObterID(t, perfilSeedCompleto())
	ctx, cancelar := context.WithCancel(context.Background())
	cancelar()

	_, _, err := gerente.RulesetsConsulta().ObterPorID(ctx, id)

	require.Error(t, err)
	assert.True(t, errors.E(err, context.Canceled), "contexto cancelado tem de ser identificável, obtido %v", err)
	var naoEncontrado *errors.ErroNaoEncontrado
	assert.False(t, errors.Como(err, &naoEncontrado), "ctx cancelado não pode virar ErroNaoEncontrado (regra 3)")
}

// A14: o teto no SQL, a única guarda que protege a memória do WORKER de uma
// linha gigante gravada por SQL direto ou por caminho de seed que não passe por
// infra/ruleset/carregar.go. O preenchimento vai num campo desconhecido, que A4
// garante ser ignorado, então acima/abaixo do teto só difere em TAMANHO.
//
// LIMITE DESTE TESTE: ele prova a RECUSA, não a não materialização do payload no
// processo — isso exigiria instrumentar o transporte do pgx e não é observável
// pela porta.
func TestObterRulesetPorIDTetoDeTamanhoNoSQL(t *testing.T) {
	const teto = 65536
	montar := func(t *testing.T, perfil ruleset.Definicao, enchimento int) []byte {
		t.Helper()
		base, err := json.Marshal(perfil)
		require.NoError(t, err)
		require.Positive(t, enchimento)
		return []byte(string(base[:len(base)-1]) + `,"preenchimento":"` + strings.Repeat("a", enchimento) + `"}`)
	}
	// O jsonb normaliza o texto, então o tamanho exato só é conhecido depois de
	// gravar: cada caractere ASCII de preenchimento vale 1 byte, logo uma única
	// correção acerta o alvo, e o require confirma.
	gravarComTamanhoExato := func(t *testing.T, perfil ruleset.Definicao, alvo int) uuid.UUID {
		t.Helper()
		enchimento := 1024
		id := inserirRulesetDireto(t, perfil.Slug, perfil.Versao, perfil.Nome, montar(t, perfil, enchimento))
		enchimento += alvo - octetosGravados(t, id)
		alterarUmaLinha(t, "UPDATE rulesets SET definicao=$2::jsonb WHERE id=$1::uuid", id.String(), string(montar(t, perfil, enchimento)))
		require.Equal(t, alvo, octetosGravados(t, id))
		return id
	}

	t.Run("exatamente_no_teto_e_lido", func(t *testing.T) {
		perfil := perfilSeedCompleto()
		id := gravarComTamanhoExato(t, perfil, teto)

		obtida, ativo, err := gerente.RulesetsConsulta().ObterPorID(context.Background(), id)

		require.NoError(t, err, "o teto é `acima de`, não `a partir de`")
		assert.Equal(t, perfil, obtida)
		assert.True(t, ativo)
	})

	t.Run("um_byte_acima_do_teto_recusado", func(t *testing.T) {
		perfil := perfilSeedCompleto()
		id := gravarComTamanhoExato(t, perfil, teto+1)
		require.NoError(t, definicaoGravada(t, id).Validar(),
			"pré-condição: a linha é válida e tem procedência coerente; só o tamanho a reprova")

		definicao, _, err := gerente.RulesetsConsulta().ObterPorID(context.Background(), id)

		require.Error(t, err)
		var aplicacao *errors.ErroAplicacao
		require.True(t, errors.Como(err, &aplicacao), "linha acima do teto é corrupção (500), obtido %T", err)
		var validacao *errors.ErroValidacao
		assert.False(t, errors.Como(err, &validacao))
		assert.Zero(t, definicao, "payload acima do teto não pode voltar pela porta")
	})
}
