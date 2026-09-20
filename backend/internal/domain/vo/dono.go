package vo

import (
	"github.com/google/uuid"

	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// EspecieDono discrimina de que tabela vem a identidade do dono. sessao_id e
// usuario_id são gerados de forma independente e nada impede que colidam; sem o
// discriminador, a colisão viraria acesso ao documento de outra pessoa.
type EspecieDono string

const (
	// EspecieSessao identifica quem usa o sistema sem conta, por sessão anônima.
	EspecieSessao EspecieDono = "sessao"
	// EspecieUsuario identifica quem usa o sistema autenticado.
	EspecieUsuario EspecieDono = "usuario"
)

// Mensagens fixas: nunca carregam o identificador recebido (CLAUDE.md, regra 7).
const (
	mensagemDonoInvalido   = "dono inválido"
	mensagemDonoIdentidade = "o dono precisa de espécie conhecida e de identificador não nulo"
	mensagemDonoAmbiguo    = "o dono não pode ser de sessão e de usuário ao mesmo tempo"
	mensagemDonoAusente    = "o dono precisa ser de sessão ou de usuário"
)

// Valida informa se a espécie é uma das reconhecidas pelo domínio.
//
// A lista é fechada de propósito: dentro do pacote vo é possível montar
// `Dono{especie: "sistema", id: x}` por literal, e sem esta verificação esse
// valor seria não-vazio e acessaria a si mesmo — um dono-curinga entrando pela
// porta dos fundos, sem construtor e sem alarme. Espécie desconhecida é inerte.
func (e EspecieDono) Valida() bool {
	return e == EspecieSessao || e == EspecieUsuario
}

// Dono é a identidade a quem um recurso pertence.
//
// Os campos são privados de propósito: fora deste arquivo só se constrói Dono
// por um construtor, e todo construtor recusa uuid.Nil. Isso mantém o zero value
// — alcançável por `var d Dono`, por campo de struct esquecido ou por erro de
// ParaDono ignorado — inerte, em vez de utilizável como identidade.
//
// O tipo é comparável com == de propósito (só uma string e um [16]byte dentro),
// para servir de chave de mapa e de valor copiável. Mas == NÃO É AUTORIZAÇÃO:
// `Dono{} == Dono{}` é verdade em Go, e autorizar por igualdade estrutural
// transformaria o dono vazio em chave-mestra de todo recurso sem identidade.
// A única forma de decidir acesso é PodeAcessar, que rejeita o vazio antes de
// comparar.
//
// O identificador nunca sai do tipo em forma crua: quem persiste usa
// ParaColunas, que embute a espécie na escolha da coluna, e quem registra em log
// usa String ou GoString, que só descrevem a espécie.
type Dono struct {
	especie EspecieDono
	id      uuid.UUID
}

// NovoDonoSessao cria a identidade de quem usa o sistema sem conta.
func NovoDonoSessao(sessaoID uuid.UUID) (Dono, error) {
	return novoDono(EspecieSessao, sessaoID)
}

// NovoDonoUsuario cria a identidade de quem usa o sistema autenticado.
func NovoDonoUsuario(usuarioID uuid.UUID) (Dono, error) {
	return novoDono(EspecieUsuario, usuarioID)
}

func novoDono(especie EspecieDono, id uuid.UUID) (Dono, error) {
	if !especie.Valida() || id == uuid.Nil {
		return Dono{}, errors.NovoErroValidacaoCampos(
			mensagemDonoInvalido,
			errors.CampoInvalido{Campo: "dono", Mensagem: mensagemDonoIdentidade},
		)
	}
	return Dono{especie: especie, id: id}, nil
}

// ParaDono converte o par de colunas usuario_id/sessao_id numa identidade.
//
// Exige exatamente uma das duas preenchida: as duas nulas não dizem de quem é o
// recurso, e as duas preenchidas fariam o motor escolher em silêncio qual
// identidade vale. Nos dois casos a recusa é deliberada.
//
// Diferente de ParaChaveStorage, aqui o motivo da recusa é revelado: a entrada
// vem do banco, não do cliente, então distinguir "ambíguo" de "ausente" ajuda a
// depurar dado corrompido sem virar oráculo de validação para quem sonda a API.
func ParaDono(usuarioID, sessaoID *uuid.UUID) (Dono, error) {
	switch {
	case usuarioID != nil && sessaoID != nil:
		return Dono{}, errors.NovoErroValidacaoCampos(
			mensagemDonoInvalido,
			errors.CampoInvalido{Campo: "dono", Mensagem: mensagemDonoAmbiguo},
		)
	case usuarioID != nil:
		return NovoDonoUsuario(*usuarioID)
	case sessaoID != nil:
		return NovoDonoSessao(*sessaoID)
	default:
		return Dono{}, errors.NovoErroValidacaoCampos(
			mensagemDonoInvalido,
			errors.CampoInvalido{Campo: "dono", Mensagem: mensagemDonoAusente},
		)
	}
}

// Vazio informa que o dono não carrega identidade alguma: espécie desconhecida
// ou identificador nulo bastam para torná-lo inerte.
func (d Dono) Vazio() bool { return !d.especie.Valida() || d.id == uuid.Nil }

// PodeAcessar é a única forma de decidir acesso a um recurso.
//
// Dono vazio não acessa nada e nada acessa recurso de dono vazio: ausência de
// identidade não é identidade. A verificação vem ANTES da comparação justamente
// porque == daria true entre dois vazios.
func (d Dono) PodeAcessar(recurso Dono) bool {
	if d.Vazio() || recurso.Vazio() {
		return false
	}
	return d == recurso
}

// Igual compara duas identidades como VALOR: serve para chave de mapa,
// deduplicação e asserção de teste. Vale a mesma ressalva de PodeAcessar: dois
// donos vazios não são iguais, porque vazio não é identidade.
//
// Igual NUNCA decide acesso. Autorização é só PodeAcessar. Os dois corpos
// coincidem hoje, mas vão divergir: na F7, quando uma sessão anônima for
// promovida a usuário, PodeAcessar passará a aceitar o usuário sobre os recursos
// da sessão que ele era, e Igual continuará recusando — a divergência cai na
// direção errada para quem escolheu o método errado, e `if !dono.Igual(x)` vira
// recusa indevida (ou, invertido, bypass silencioso) sem quebrar teste algum.
func (d Dono) Igual(outro Dono) bool {
	if d.Vazio() || outro.Vazio() {
		return false
	}
	return d == outro
}

// Especie devolve o discriminador do dono, ou vazio quando não há identidade.
func (d Dono) Especie() EspecieDono { return d.especie }

// ParaColunas devolve o par usuario_id/sessao_id para persistência, com a coluna
// da outra espécie nula. É o único caminho pelo qual o identificador sai do tipo.
func (d Dono) ParaColunas() (usuarioID *uuid.UUID, sessaoID *uuid.UUID) {
	return d.copiaIDDaEspecie(EspecieUsuario), d.copiaIDDaEspecie(EspecieSessao)
}

// copiaIDDaEspecie aponta para uma CÓPIA local do identificador: devolver o
// endereço do campo deixaria o chamador reescrever a identidade do dono.
func (d Dono) copiaIDDaEspecie(especie EspecieDono) *uuid.UUID {
	if d.especie != especie || d.id == uuid.Nil {
		return nil
	}
	identificador := d.id
	return &identificador
}

// String descreve o dono só pela espécie. O identificador fica de fora de
// propósito (CLAUDE.md, regra 7): um Dono cai em log por %v de qualquer struct
// que o contenha, e o uuid no log viraria correlacionador de sessão.
func (d Dono) String() string {
	if d.Vazio() {
		return "dono(vazio)"
	}
	return "dono(" + string(d.especie) + ")"
}

// GoString repete a descrição de String porque o fmt honra Stringer em %v, %+v,
// %s e %q, mas em %#v procura GoStringer e, não achando, imprime os campos
// privados — despejando o uuid em bytes hexadecimais. Como o sessao_id é o
// conteúdo do cookie de sessão, um %#v num log futuro entregaria um bearer vivo.
func (d Dono) GoString() string { return d.String() }
