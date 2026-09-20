package vo

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// Identificadores fixos: nenhum teste deste arquivo pode depender de relógio,
// de rede ou de aleatoriedade.
const (
	idDonoA = "1f5f3c22-0c9a-4a1f-9c2e-7d4b8a6f1e30"
	idDonoB = "2a6e4d33-1dab-4b20-8d3f-8e5c9b7a2f41"
)

// idDeDono recupera o identificador do dono pelo único caminho autorizado pelo
// contrato: as colunas de persistência. O contrato NÃO expõe o uuid cru, porque
// expor habilitaria `donoA.ID() == donoB.ID()` — comparação sem espécie, que é
// exatamente a colisão que TestSessaoEUsuarioComMesmoUUIDNaoSeConfundem impede.
func idDeDono(t *testing.T, dono Dono) uuid.UUID {
	t.Helper()

	usuarioID, sessaoID := dono.ParaColunas()
	switch {
	case usuarioID != nil && sessaoID != nil:
		t.Fatal("ParaColunas não pode devolver as duas colunas preenchidas")
	case usuarioID != nil:
		return *usuarioID
	case sessaoID != nil:
		return *sessaoID
	}
	return uuid.Nil
}

// TestDonoNaoExpoeIdentificadorCru trava a remoção de ID() do contrato.
//
// Um acessor do uuid cru convida à comparação sem espécie — `a.ID() == b.ID()`
// dá true para uma sessão e um usuário que colidam de identificador, que é a
// confusão de identidade que o discriminador existe para impedir. A persistência
// já é atendida por ParaColunas, que devolve o par de colunas com a espécie
// embutida na escolha da coluna. Este teste falha (não compila a intenção) se o
// método voltar.
func TestDonoNaoExpoeIdentificadorCru(t *testing.T) {
	t.Parallel()

	sessao, err := NovoDonoSessao(uuid.MustParse(idDonoA))
	if err != nil {
		t.Fatalf("pré-condição: não esperava erro, obteve %v", err)
	}

	var qualquer any = sessao
	if _, expoe := qualquer.(interface{ ID() uuid.UUID }); expoe {
		t.Fatal("Dono não pode expor ID(): comparação por uuid cru ignora a espécie")
	}
}

// TestConstrutoresDeDonoRejeitamUUIDNulo fixa que um dono nunca nasce com
// identidade nula: o uuid.Nil é o zero value do uuid e entraria silenciosamente
// por qualquer caminho que esquecesse de preencher o identificador.
func TestConstrutoresDeDonoRejeitamUUIDNulo(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome      string
		construir func() (Dono, error)
	}{
		{
			nome:      "dono de sessão com uuid nulo",
			construir: func() (Dono, error) { return NovoDonoSessao(uuid.Nil) },
		},
		{
			nome:      "dono de usuário com uuid nulo",
			construir: func() (Dono, error) { return NovoDonoUsuario(uuid.Nil) },
		},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			dono, err := caso.construir()
			if err == nil {
				t.Fatal("esperava erro de validação para uuid nulo")
			}

			var invalido *errors.ErroValidacao
			if !errors.Como(err, &invalido) {
				t.Fatalf("esperava *ErroValidacao, obteve %T", err)
			}
			exigirCampo(t, invalido, "dono")

			// O dono devolvido junto do erro tem que ser inerte, porque um
			// chamador descuidado pode ignorar o err.
			if !dono.Vazio() {
				t.Fatal("o dono devolvido junto com o erro tem que ser vazio")
			}
		})
	}
}

// TestConstrutoresDeDonoPreenchemEspecieEID confere o caso feliz dos dois construtores.
func TestConstrutoresDeDonoPreenchemEspecieEID(t *testing.T) {
	t.Parallel()

	sessaoID := uuid.MustParse(idDonoA)
	usuarioID := uuid.MustParse(idDonoB)

	casos := []struct {
		nome            string
		construir       func() (Dono, error)
		especieEsperada EspecieDono
		idEsperado      uuid.UUID
	}{
		{
			nome:            "dono de sessão",
			construir:       func() (Dono, error) { return NovoDonoSessao(sessaoID) },
			especieEsperada: EspecieSessao,
			idEsperado:      sessaoID,
		},
		{
			nome:            "dono de usuário",
			construir:       func() (Dono, error) { return NovoDonoUsuario(usuarioID) },
			especieEsperada: EspecieUsuario,
			idEsperado:      usuarioID,
		},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			dono, err := caso.construir()
			if err != nil {
				t.Fatalf("não esperava erro, obteve %v", err)
			}
			if dono.Especie() != caso.especieEsperada {
				t.Fatalf("espécie = %q, esperava %q", dono.Especie(), caso.especieEsperada)
			}
			if obtido := idDeDono(t, dono); obtido != caso.idEsperado {
				t.Fatalf("identificador persistido = %v, esperava %v", obtido, caso.idEsperado)
			}
			if dono.Vazio() {
				t.Fatal("um dono construído com sucesso não pode ser vazio")
			}
		})
	}
}

// TestDonoVazioEInerte é o teste mais importante do arquivo.
//
// O zero value de Dono é alcançável sem passar por construtor: basta declarar
// `var d vo.Dono`, esquecer de preencher um campo de struct, ou ignorar o erro
// de ParaDono. Se PodeAcessar devolvesse true para o dono vazio — em especial
// contra outro dono vazio, que é o caso traiçoeiro, porque `Dono{} == Dono{}` é
// verdade em Go — o zero value viraria chave-mestra: qualquer requisição sem
// identidade acessaria qualquer recurso sem identidade, e um recurso gravado
// com dono vazio por um bug de persistência ficaria aberto a todo mundo.
// Por isso a verificação de identidade tem que rejeitar o vazio ANTES de
// comparar, e não confiar na igualdade estrutural.
func TestDonoVazioEInerte(t *testing.T) {
	t.Parallel()

	vazio := Dono{}

	if !vazio.Vazio() {
		t.Fatal("o zero value de Dono tem que ser reconhecido como vazio")
	}
	if vazio.Especie() != EspecieDono("") {
		t.Fatalf("espécie do dono vazio = %q, esperava vazia", vazio.Especie())
	}
	if usuarioID, sessaoID := vazio.ParaColunas(); usuarioID != nil || sessaoID != nil {
		t.Fatal("o dono vazio não pode produzir coluna preenchida alguma")
	}

	sessao, err := NovoDonoSessao(uuid.MustParse(idDonoA))
	if err != nil {
		t.Fatalf("pré-condição: não esperava erro, obteve %v", err)
	}
	usuario, err := NovoDonoUsuario(uuid.MustParse(idDonoB))
	if err != nil {
		t.Fatalf("pré-condição: não esperava erro, obteve %v", err)
	}

	casos := []struct {
		nome     string
		sujeito  Dono
		recurso  Dono
		esperado bool
	}{
		{nome: "vazio não acessa recurso de sessão", sujeito: vazio, recurso: sessao},
		{nome: "vazio não acessa recurso de usuário", sujeito: vazio, recurso: usuario},
		{nome: "vazio não acessa recurso vazio", sujeito: vazio, recurso: Dono{}},
		{nome: "sessão não acessa recurso vazio", sujeito: sessao, recurso: vazio},
		{nome: "usuário não acessa recurso vazio", sujeito: usuario, recurso: vazio},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			if obtido := caso.sujeito.PodeAcessar(caso.recurso); obtido != caso.esperado {
				t.Fatalf("PodeAcessar = %v, esperava %v", obtido, caso.esperado)
			}
		})
	}

	// Igual também não pode transformar o vazio em identidade válida.
	if vazio.Igual(Dono{}) {
		t.Fatal("dois donos vazios não podem ser considerados iguais: vazio não é identidade")
	}
}

// TestDonoParcialmentePreenchidoEInerte cobre os estados intermediários que
// NENHUM construtor consegue produzir: espécie sem identificador e identificador
// sem espécie. Eles só são montáveis por literal aqui dentro do pacote vo, e é
// por isso que o teste existe — Vazio() e copiaIDDaEspecie testam dois termos
// num OR, e sem estes casos o segundo termo de cada um nunca é exercitado.
// Cobertura de statements dá 100% sem eles; cobertura de condição, não. Se
// alguém simplificar `especie == "" || id == uuid.Nil` para só o primeiro termo,
// ou `d.especie != especie || d.id == uuid.Nil` para só a comparação de espécie,
// estes casos caem — que é o alarme desejado, porque um dono com identificador
// nulo mas espécie preenchida voltaria a ser tratado como identidade válida.
func TestDonoParcialmentePreenchidoEInerte(t *testing.T) {
	t.Parallel()

	valido, err := NovoDonoSessao(uuid.MustParse(idDonoA))
	if err != nil {
		t.Fatalf("pré-condição: não esperava erro, obteve %v", err)
	}

	casos := []struct {
		nome    string
		forjado Dono
	}{
		{nome: "espécie de sessão sem identificador", forjado: Dono{especie: EspecieSessao}},
		{nome: "espécie de usuário sem identificador", forjado: Dono{especie: EspecieUsuario}},
		{nome: "identificador sem espécie", forjado: Dono{id: uuid.MustParse(idDonoA)}},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			if !caso.forjado.Vazio() {
				t.Fatal("dono sem espécie ou sem identificador tem que ser vazio")
			}
			if caso.forjado.PodeAcessar(valido) {
				t.Fatal("dono parcialmente preenchido não pode acessar recurso algum")
			}
			if valido.PodeAcessar(caso.forjado) {
				t.Fatal("nada pode acessar recurso de dono parcialmente preenchido")
			}
			if caso.forjado.PodeAcessar(caso.forjado) {
				t.Fatal("dono parcialmente preenchido não pode acessar nem o próprio recurso")
			}
			if caso.forjado.Igual(caso.forjado) || caso.forjado.Igual(valido) {
				t.Fatal("dono parcialmente preenchido não é identidade e não pode ser igual a nada")
			}
			usuarioID, sessaoID := caso.forjado.ParaColunas()
			if usuarioID != nil || sessaoID != nil {
				t.Fatal("ParaColunas tem que devolver (nil, nil) para dono parcialmente preenchido")
			}
		})
	}
}

// TestSessaoEUsuarioComMesmoUUIDNaoSeConfundem justifica a existência do
// discriminador de espécie. sessao_id e usuario_id vêm de tabelas diferentes e
// nada impede que um valor colida com o outro — por acaso ou por escolha de um
// atacante que controle o próprio identificador de sessão. Sem a espécie, essa
// colisão viraria acesso ao documento de outra pessoa.
func TestSessaoEUsuarioComMesmoUUIDNaoSeConfundem(t *testing.T) {
	t.Parallel()

	mesmoID := uuid.MustParse(idDonoA)

	sessao, err := NovoDonoSessao(mesmoID)
	if err != nil {
		t.Fatalf("pré-condição: não esperava erro, obteve %v", err)
	}
	usuario, err := NovoDonoUsuario(mesmoID)
	if err != nil {
		t.Fatalf("pré-condição: não esperava erro, obteve %v", err)
	}

	if sessao.PodeAcessar(usuario) {
		t.Fatal("dono de sessão não pode acessar recurso de usuário com o mesmo uuid")
	}
	if usuario.PodeAcessar(sessao) {
		t.Fatal("dono de usuário não pode acessar recurso de sessão com o mesmo uuid")
	}
	if sessao.Igual(usuario) || usuario.Igual(sessao) {
		t.Fatal("espécies diferentes com o mesmo uuid não podem ser iguais")
	}
	if sessao == usuario {
		t.Fatal("a espécie precisa fazer parte do valor comparado com ==")
	}
}

// TestPodeAcessarEIgualEntreDonosValidos cobre o caso feliz e a recusa entre
// donos distintos da mesma espécie.
//
// Este teste NÃO afirma que Igual e PodeAcessar coincidem, nem que PodeAcessar é
// simétrico: isso é a implementação de hoje, não o contrato. Na F7, quando uma
// sessão anônima for promovida a usuário, o usuário passará a acessar o recurso
// da sessão que ele era sem que a sessão acesse o recurso do usuário — relação
// assimétrica, e Igual continuará sendo igualdade de valor. Fixar a coincidência
// aqui obrigaria a apagar a spec para evoluir, que é acoplamento. Só a simetria
// de Igual é afirmada, porque igualdade de valor é simétrica por definição.
func TestPodeAcessarEIgualEntreDonosValidos(t *testing.T) {
	t.Parallel()

	sessaoA, err := NovoDonoSessao(uuid.MustParse(idDonoA))
	if err != nil {
		t.Fatalf("pré-condição: não esperava erro, obteve %v", err)
	}
	outraSessaoA, err := NovoDonoSessao(uuid.MustParse(idDonoA))
	if err != nil {
		t.Fatalf("pré-condição: não esperava erro, obteve %v", err)
	}
	sessaoB, err := NovoDonoSessao(uuid.MustParse(idDonoB))
	if err != nil {
		t.Fatalf("pré-condição: não esperava erro, obteve %v", err)
	}
	usuarioA, err := NovoDonoUsuario(uuid.MustParse(idDonoA))
	if err != nil {
		t.Fatalf("pré-condição: não esperava erro, obteve %v", err)
	}
	usuarioB, err := NovoDonoUsuario(uuid.MustParse(idDonoB))
	if err != nil {
		t.Fatalf("pré-condição: não esperava erro, obteve %v", err)
	}

	casos := []struct {
		nome              string
		sujeito           Dono
		recurso           Dono
		acessoEsperado    bool
		igualdadeEsperada bool
	}{
		{nome: "mesma sessão acessa o próprio recurso", sujeito: sessaoA, recurso: sessaoA, acessoEsperado: true, igualdadeEsperada: true},
		{nome: "sessão construída duas vezes acessa o próprio recurso", sujeito: sessaoA, recurso: outraSessaoA, acessoEsperado: true, igualdadeEsperada: true},
		{nome: "mesmo usuário acessa o próprio recurso", sujeito: usuarioA, recurso: usuarioA, acessoEsperado: true, igualdadeEsperada: true},
		{nome: "sessões diferentes não se acessam", sujeito: sessaoA, recurso: sessaoB, acessoEsperado: false, igualdadeEsperada: false},
		{nome: "usuários diferentes não se acessam", sujeito: usuarioA, recurso: usuarioB, acessoEsperado: false, igualdadeEsperada: false},
		{nome: "usuário não acessa recurso de sessão alheia", sujeito: usuarioA, recurso: sessaoB, acessoEsperado: false, igualdadeEsperada: false},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			if obtido := caso.sujeito.PodeAcessar(caso.recurso); obtido != caso.acessoEsperado {
				t.Fatalf("PodeAcessar = %v, esperava %v", obtido, caso.acessoEsperado)
			}
			if obtido := caso.sujeito.Igual(caso.recurso); obtido != caso.igualdadeEsperada {
				t.Fatalf("Igual = %v, esperava %v", obtido, caso.igualdadeEsperada)
			}
			// Só Igual é simétrico por definição.
			if obtido := caso.recurso.Igual(caso.sujeito); obtido != caso.igualdadeEsperada {
				t.Fatalf("Igual invertido = %v, esperava %v: igualdade de valor é simétrica", obtido, caso.igualdadeEsperada)
			}
		})
	}
}

// TestParaDonoRejeitaColunasInvalidas cobre o que o banco pode devolver de
// errado. Dono ambíguo (as duas colunas preenchidas) é recusa deliberada: seria
// o motor escolher em silêncio qual identidade vale.
func TestParaDonoRejeitaColunasInvalidas(t *testing.T) {
	t.Parallel()

	idA := uuid.MustParse(idDonoA)
	idB := uuid.MustParse(idDonoB)
	nulo := uuid.Nil

	casos := []struct {
		nome      string
		usuarioID *uuid.UUID
		sessaoID  *uuid.UUID
	}{
		{nome: "ambas as colunas nulas", usuarioID: nil, sessaoID: nil},
		{nome: "ambas as colunas preenchidas", usuarioID: &idA, sessaoID: &idB},
		{nome: "ambas as colunas preenchidas com o mesmo uuid", usuarioID: &idA, sessaoID: &idA},
		{nome: "usuário apontando para uuid nulo", usuarioID: &nulo, sessaoID: nil},
		{nome: "sessão apontando para uuid nulo", usuarioID: nil, sessaoID: &nulo},
		{nome: "ambas apontando para uuid nulo", usuarioID: &nulo, sessaoID: &nulo},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			dono, err := ParaDono(caso.usuarioID, caso.sessaoID)
			if err == nil {
				t.Fatal("esperava erro de validação")
			}

			var invalido *errors.ErroValidacao
			if !errors.Como(err, &invalido) {
				t.Fatalf("esperava *ErroValidacao, obteve %T", err)
			}
			exigirCampo(t, invalido, "dono")

			if !dono.Vazio() {
				t.Fatal("o dono devolvido junto com o erro tem que ser vazio")
			}
		})
	}
}

// TestParaDonoAceitaColunasValidas confere a conversão vinda do banco.
func TestParaDonoAceitaColunasValidas(t *testing.T) {
	t.Parallel()

	idA := uuid.MustParse(idDonoA)
	idB := uuid.MustParse(idDonoB)

	casos := []struct {
		nome            string
		usuarioID       *uuid.UUID
		sessaoID        *uuid.UUID
		especieEsperada EspecieDono
		idEsperado      uuid.UUID
	}{
		{
			nome:            "só a coluna de usuário preenchida",
			usuarioID:       &idA,
			especieEsperada: EspecieUsuario,
			idEsperado:      idA,
		},
		{
			nome:            "só a coluna de sessão preenchida",
			sessaoID:        &idB,
			especieEsperada: EspecieSessao,
			idEsperado:      idB,
		},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			dono, err := ParaDono(caso.usuarioID, caso.sessaoID)
			if err != nil {
				t.Fatalf("não esperava erro, obteve %v", err)
			}
			if dono.Vazio() {
				t.Fatal("o dono convertido não pode ser vazio")
			}
			if dono.Especie() != caso.especieEsperada {
				t.Fatalf("espécie = %q, esperava %q", dono.Especie(), caso.especieEsperada)
			}
			if obtido := idDeDono(t, dono); obtido != caso.idEsperado {
				t.Fatalf("identificador persistido = %v, esperava %v", obtido, caso.idEsperado)
			}
		})
	}
}

// TestIdaEVoltaDonoColunas garante que gravar e reler não muda a identidade, e
// que a coluna da espécie errada sai nula — se as duas saíssem preenchidas, o
// próprio ParaDono recusaria o valor na volta.
func TestIdaEVoltaDonoColunas(t *testing.T) {
	t.Parallel()

	idSessao := uuid.MustParse(idDonoA)
	idUsuario := uuid.MustParse(idDonoB)

	sessao, err := NovoDonoSessao(idSessao)
	if err != nil {
		t.Fatalf("pré-condição: não esperava erro, obteve %v", err)
	}
	usuario, err := NovoDonoUsuario(idUsuario)
	if err != nil {
		t.Fatalf("pré-condição: não esperava erro, obteve %v", err)
	}

	casos := []struct {
		nome              string
		dono              Dono
		idEsperado        uuid.UUID
		esperaUsuarioNulo bool
		esperaSessaoNula  bool
	}{
		{nome: "dono de sessão", dono: sessao, idEsperado: idSessao, esperaUsuarioNulo: true},
		{nome: "dono de usuário", dono: usuario, idEsperado: idUsuario, esperaSessaoNula: true},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			usuarioID, sessaoID := caso.dono.ParaColunas()

			if caso.esperaUsuarioNulo && usuarioID != nil {
				t.Fatal("esperava a coluna usuario_id nula para dono de sessão")
			}
			if caso.esperaSessaoNula && sessaoID != nil {
				t.Fatal("esperava a coluna sessao_id nula para dono de usuário")
			}
			if !caso.esperaUsuarioNulo && (usuarioID == nil || *usuarioID != caso.idEsperado) {
				t.Fatal("esperava a coluna usuario_id com o id do dono")
			}
			if !caso.esperaSessaoNula && (sessaoID == nil || *sessaoID != caso.idEsperado) {
				t.Fatal("esperava a coluna sessao_id com o id do dono")
			}

			devolta, err := ParaDono(usuarioID, sessaoID)
			if err != nil {
				t.Fatalf("não esperava erro na volta, obteve %v", err)
			}
			if !devolta.Igual(caso.dono) {
				t.Fatalf("a ida e volta mudou a identidade: %v, esperava %v", devolta.Especie(), caso.dono.Especie())
			}
			if devolta != caso.dono {
				t.Fatal("a ida e volta tem que devolver um valor idêntico sob ==")
			}
		})
	}

	// As colunas devolvidas não podem compartilhar memória com o dono nem entre
	// chamadas: mutar o ponteiro recebido não pode alterar o dono nem contaminar
	// a chamada seguinte.
	_, primeiraSessaoID := sessao.ParaColunas()
	if primeiraSessaoID == nil {
		t.Fatal("pré-condição: esperava a coluna sessao_id preenchida")
	}
	_, segundaSessaoID := sessao.ParaColunas()
	if segundaSessaoID == nil {
		t.Fatal("pré-condição: esperava a coluna sessao_id preenchida na segunda chamada")
	}
	if primeiraSessaoID == segundaSessaoID {
		t.Fatal("duas chamadas de ParaColunas não podem devolver o mesmo ponteiro")
	}

	*primeiraSessaoID = idUsuario
	if *segundaSessaoID != idSessao {
		t.Fatal("mutar o ponteiro de uma chamada não pode alterar o de outra")
	}
	if _, sessaoID := sessao.ParaColunas(); sessaoID == nil || *sessaoID != idSessao {
		t.Fatal("mutar o ponteiro devolvido por ParaColunas não pode alterar o dono")
	}
}

// TestStringDeDonoNaoVazaIdentificador aplica a regra 7 do CLAUDE.md ao dono.
// Um Dono cai em log, em mensagem de erro e em atributo de trace por descuido —
// via %v numa struct que o contenha, por exemplo. Se o String() carregasse o
// uuid, o log viraria um correlacionador de sessão: bastaria cruzar linhas para
// reconstruir tudo o que uma pessoa não autenticada fez.
func TestStringDeDonoNaoVazaIdentificador(t *testing.T) {
	t.Parallel()

	idSessao := uuid.MustParse(idDonoA)
	idUsuario := uuid.MustParse(idDonoB)

	sessao, err := NovoDonoSessao(idSessao)
	if err != nil {
		t.Fatalf("pré-condição: não esperava erro, obteve %v", err)
	}
	usuario, err := NovoDonoUsuario(idUsuario)
	if err != nil {
		t.Fatalf("pré-condição: não esperava erro, obteve %v", err)
	}

	casos := []struct {
		nome            string
		dono            Dono
		trechoEsperado  string
		idQueNaoPodeVir uuid.UUID
	}{
		{nome: "dono de sessão", dono: sessao, trechoEsperado: string(EspecieSessao), idQueNaoPodeVir: idSessao},
		{nome: "dono de usuário", dono: usuario, trechoEsperado: string(EspecieUsuario), idQueNaoPodeVir: idUsuario},
		{nome: "dono vazio", dono: Dono{}, trechoEsperado: "vazio"},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			texto := caso.dono.String()
			if texto == "" {
				t.Fatal("String() não pode ser vazio: some do log sem deixar rastro da espécie")
			}
			if !strings.Contains(texto, caso.trechoEsperado) {
				t.Fatalf("String() = %q, esperava conter %q", texto, caso.trechoEsperado)
			}
			if caso.idQueNaoPodeVir != uuid.Nil {
				exigirSemIdentificador(t, "String()", texto, caso.idQueNaoPodeVir)
			}
		})
	}
}

// registroDeLogComDono reproduz o descuido real: uma struct de log, de erro ou
// de atributo de trace que carrega o Dono como campo e é impressa inteira.
type registroDeLogComDono struct {
	Acao string
	Dono Dono
}

// TestFormatacaoIndiretaDeDonoNaoVazaIdentificador é a versão da regra 7 que a
// spec anterior deixou passar: ela chamava String() direto, mas a ameaça
// declarada é o Dono caindo em log DENTRO de outra struct, por um verbo de
// formatação qualquer.
//
// O fmt honra Stringer em %v, %+v, %s e %q — mas NÃO em %#v, que procura
// GoStringer e, não achando, imprime os campos privados, despejando o uuid em
// bytes hexadecimais. Como o sessao_id é o conteúdo do cookie de sessão, isso é
// um bearer vivo indo parar no log. Por isso a verificação normaliza a saída
// (tira "0x", vírgulas, espaços e hífens) antes de procurar o identificador:
// "0x1f, 0x5f, 0x3c, 0x22" é o mesmo vazamento que "1f5f3c22", só disfarçado.
func TestFormatacaoIndiretaDeDonoNaoVazaIdentificador(t *testing.T) {
	t.Parallel()

	idSessao := uuid.MustParse(idDonoA)
	idUsuario := uuid.MustParse(idDonoB)

	sessao, err := NovoDonoSessao(idSessao)
	if err != nil {
		t.Fatalf("pré-condição: não esperava erro, obteve %v", err)
	}
	usuario, err := NovoDonoUsuario(idUsuario)
	if err != nil {
		t.Fatalf("pré-condição: não esperava erro, obteve %v", err)
	}

	verbos := []string{"%v", "%+v", "%#v", "%s", "%q"}

	casos := []struct {
		nome            string
		dono            Dono
		trechoEsperado  string
		idQueNaoPodeVir uuid.UUID
	}{
		{nome: "dono de sessão", dono: sessao, trechoEsperado: string(EspecieSessao), idQueNaoPodeVir: idSessao},
		{nome: "dono de usuário", dono: usuario, trechoEsperado: string(EspecieUsuario), idQueNaoPodeVir: idUsuario},
	}

	for _, caso := range casos {
		for _, verbo := range verbos {
			t.Run(caso.nome+" direto com "+verbo, func(t *testing.T) {
				t.Parallel()

				texto := fmt.Sprintf(verbo, caso.dono)
				if !strings.Contains(texto, caso.trechoEsperado) {
					t.Fatalf("%s = %q, esperava conter a espécie %q", verbo, texto, caso.trechoEsperado)
				}
				exigirSemIdentificador(t, verbo, texto, caso.idQueNaoPodeVir)
			})

			t.Run(caso.nome+" dentro de struct com "+verbo, func(t *testing.T) {
				t.Parallel()

				registro := registroDeLogComDono{Acao: "formatar", Dono: caso.dono}
				texto := fmt.Sprintf(verbo, registro)
				if !strings.Contains(texto, caso.trechoEsperado) {
					t.Fatalf("%s de struct = %q, esperava conter a espécie %q", verbo, texto, caso.trechoEsperado)
				}
				exigirSemIdentificador(t, verbo+" de struct", texto, caso.idQueNaoPodeVir)
			})
		}
	}
}

// exigirSemIdentificador recusa o identificador em qualquer disfarce: na forma
// canônica com hífens, em prefixo de 8 caracteres (meio uuid já correlaciona) e
// na forma de bytes hexadecimais que %#v produz.
func exigirSemIdentificador(t *testing.T, origem, texto string, id uuid.UUID) {
	t.Helper()

	canonico := id.String()
	hexadecimal := strings.ReplaceAll(canonico, "-", "")
	normalizado := strings.NewReplacer("0x", "", ",", "", " ", "", "-", "").Replace(texto)

	if strings.Contains(texto, canonico) {
		t.Fatalf("%s não pode conter o identificador do dono", origem)
	}
	if strings.Contains(texto, canonico[:8]) {
		t.Fatalf("%s não pode conter nem prefixo do identificador do dono", origem)
	}
	if strings.Contains(normalizado, hexadecimal) {
		t.Fatalf("%s vazou o identificador em bytes hexadecimais (saída = %q)", origem, texto)
	}
	if strings.Contains(normalizado, hexadecimal[:8]) {
		t.Fatalf("%s vazou prefixo do identificador em bytes hexadecimais (saída = %q)", origem, texto)
	}
}

// TestSerializacaoJSONDeDonoNaoVazaIdentificador fixa o contrato de hoje: Dono
// só tem campos privados e não implementa MarshalJSON, então json.Marshal
// produz "{}". Não vaza, mas vaza-ou-não é acidente da visibilidade dos campos,
// e acidente não é contrato — daí este teste.
//
// Se um dia for preciso serializar o dono, adicionar MarshalJSON tem que ser
// decisão consciente, e o resultado NÃO pode passar a incluir o identificador:
// um DTO de resposta ou um payload de fila com o sessao_id dentro reproduz, em
// outro meio, exatamente o vazamento que String() evita no log (CLAUDE.md,
// regra 7). Quem mudar isto vai ter que reescrever este teste de propósito.
func TestSerializacaoJSONDeDonoNaoVazaIdentificador(t *testing.T) {
	t.Parallel()

	idSessao := uuid.MustParse(idDonoA)
	idUsuario := uuid.MustParse(idDonoB)

	sessao, err := NovoDonoSessao(idSessao)
	if err != nil {
		t.Fatalf("pré-condição: não esperava erro, obteve %v", err)
	}
	usuario, err := NovoDonoUsuario(idUsuario)
	if err != nil {
		t.Fatalf("pré-condição: não esperava erro, obteve %v", err)
	}

	casos := []struct {
		nome            string
		dono            Dono
		jsonEsperado    string
		idQueNaoPodeVir uuid.UUID
	}{
		{nome: "dono de sessão", dono: sessao, jsonEsperado: "{}", idQueNaoPodeVir: idSessao},
		{nome: "dono de usuário", dono: usuario, jsonEsperado: "{}", idQueNaoPodeVir: idUsuario},
		{nome: "dono vazio", dono: Dono{}, jsonEsperado: "{}"},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			// A ausência de campos exportados e de MarshalJSON em Dono é o
			// contrato sob teste, não um descuido: é ela que garante o "{}".
			bruto, err := json.Marshal(caso.dono) //nolint:staticcheck // SA9005: serializar struct sem campos exportados é justamente a asserção aqui.
			if err != nil {
				t.Fatalf("não esperava erro de serialização, obteve %v", err)
			}
			if string(bruto) != caso.jsonEsperado {
				t.Fatalf("json.Marshal = %s, esperava %s", bruto, caso.jsonEsperado)
			}
			if caso.idQueNaoPodeVir != uuid.Nil {
				exigirSemIdentificador(t, "json.Marshal", string(bruto), caso.idQueNaoPodeVir)
			}
		})

		t.Run(caso.nome+" dentro de struct", func(t *testing.T) {
			t.Parallel()

			bruto, err := json.Marshal(registroDeLogComDono{Acao: "formatar", Dono: caso.dono})
			if err != nil {
				t.Fatalf("não esperava erro de serialização, obteve %v", err)
			}
			if esperado := `{"Acao":"formatar","Dono":{}}`; string(bruto) != esperado {
				t.Fatalf("json.Marshal de struct = %s, esperava %s", bruto, esperado)
			}
			if caso.idQueNaoPodeVir != uuid.Nil {
				exigirSemIdentificador(t, "json.Marshal de struct", string(bruto), caso.idQueNaoPodeVir)
			}
		})
	}
}

// TestDonoPermaneceComparavel trava a forma da struct. Dono é usado como valor:
// comparado com ==, guardado em mapa e copiado sem cerimônia. Se alguém puser
// slice, mapa, função ou ponteiro mutável dentro dele, este teste para de
// compilar (ou passa a comparar endereços), que é exatamente o alarme desejado.
func TestDonoPermaneceComparavel(t *testing.T) {
	t.Parallel()

	sessao, err := NovoDonoSessao(uuid.MustParse(idDonoA))
	if err != nil {
		t.Fatalf("pré-condição: não esperava erro, obteve %v", err)
	}
	mesmaSessao, err := NovoDonoSessao(uuid.MustParse(idDonoA))
	if err != nil {
		t.Fatalf("pré-condição: não esperava erro, obteve %v", err)
	}
	usuario, err := NovoDonoUsuario(uuid.MustParse(idDonoA))
	if err != nil {
		t.Fatalf("pré-condição: não esperava erro, obteve %v", err)
	}

	// Não compila se Dono deixar de ser comparável.
	contagem := map[Dono]int{}
	contagem[sessao]++
	contagem[mesmaSessao]++
	contagem[usuario]++
	contagem[Dono{}]++

	if len(contagem) != 3 {
		t.Fatalf("esperava 3 chaves distintas no mapa, obteve %d", len(contagem))
	}
	if contagem[sessao] != 2 {
		t.Fatalf("esperava que donos equivalentes colidissem na mesma chave, obteve %d", contagem[sessao])
	}
	if sessao != mesmaSessao {
		t.Fatal("donos construídos com a mesma espécie e o mesmo id têm que ser == entre si")
	}
}
