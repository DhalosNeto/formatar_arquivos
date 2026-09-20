package entity

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

const tamanhoDeTeste int64 = 4096

// Invisíveis que precisam sumir do nome: são exatamente os caracteres que
// existem para disfarçar extensão, esconder separador e sumir em log.
const (
	sobrescritaDireita = "\u202e" // RTLO: inverte a leitura e disfarça a extensão
	larguraZero        = "\u200b" // ZWSP
	marcaDeOrdem       = "\ufeff" // BOM
	separadorLinha     = "\u2028"
	separadorParagrafo = "\u2029"
	proximaLinhaC1     = "\u0085" // NEL, controle C1
)

func donoDeSessaoDeTeste(t *testing.T) vo.Dono {
	t.Helper()

	dono, err := vo.NovoDonoSessao(uuid.New())
	if err != nil {
		t.Fatalf("não esperava erro ao montar dono de sessão, obteve %v", err)
	}
	return dono
}

func donoDeUsuarioDeTeste(t *testing.T) vo.Dono {
	t.Helper()

	dono, err := vo.NovoDonoUsuario(uuid.New())
	if err != nil {
		t.Fatalf("não esperava erro ao montar dono de usuário, obteve %v", err)
	}
	return dono
}

func novoDocumentoValido(t *testing.T) Documento {
	t.Helper()

	documento, err := NovoDocumento(donoDeSessaoDeTeste(t), "artigo.docx", vo.FormatoDocx, tamanhoDeTeste)
	if err != nil {
		t.Fatalf("não esperava erro ao criar documento válido, obteve %v", err)
	}
	return documento
}

func TestNovoDocumentoNasceRecebido(t *testing.T) {
	t.Parallel()

	usuarioID := uuid.New()
	dono, err := vo.NovoDonoUsuario(usuarioID)
	if err != nil {
		t.Fatalf("não esperava erro ao montar o dono, obteve %v", err)
	}

	documento, err := NovoDocumento(dono, "artigo.docx", vo.FormatoDocx, tamanhoDeTeste)
	if err != nil {
		t.Fatalf("não esperava erro, obteve %v", err)
	}

	if documento.Status != StatusRecebido {
		t.Fatalf("status = %q, esperava %q", documento.Status, StatusRecebido)
	}
	if documento.ID == uuid.Nil {
		t.Fatal("esperava um ID gerado, veio uuid.Nil")
	}
	if !documento.Dono.Igual(dono) {
		t.Fatal("esperava que o dono informado fosse preservado")
	}
	if documento.Dono.Especie() != vo.EspecieUsuario {
		t.Fatalf("espécie = %q, esperava %q", documento.Dono.Especie(), vo.EspecieUsuario)
	}
	// O par é lido inteiro, espécie junto com identificador: colapsar o par num
	// "o que não for nil" é exatamente o molde do bug de confusão de identidade.
	guardadoUsuario, guardadoSessao := documento.Dono.ParaColunas()
	if guardadoUsuario == nil || *guardadoUsuario != usuarioID {
		t.Fatal("esperava o identificador na coluna de usuário")
	}
	if guardadoSessao != nil {
		t.Fatal("dono de usuário não pode preencher a coluna de sessão")
	}
	if documento.Formato != vo.FormatoDocx {
		t.Fatalf("formato = %q, esperava %q", documento.Formato, vo.FormatoDocx)
	}
	if documento.TamanhoBytes != tamanhoDeTeste {
		t.Fatalf("tamanho = %d, esperava %d", documento.TamanhoBytes, tamanhoDeTeste)
	}
	if !strings.Contains(documento.ChaveStorage.String(), documento.ID.String()) {
		t.Fatal("esperava que a chave de storage fosse derivada do ID do documento")
	}
	if documento.ChaveStoragePDF != nil {
		t.Fatal("documento novo não deve ter chave de preview")
	}
	if documento.TemPreview() {
		t.Fatal("documento novo não deve ter preview")
	}
	if documento.MIME() != vo.MIMEDocx {
		t.Fatalf("MIME() = %q, esperava %q", documento.MIME(), vo.MIMEDocx)
	}
	if documento.CriadoEm.IsZero() {
		t.Fatal("esperava CriadoEm preenchido")
	}
	if documento.CriadoEm.Location() != time.UTC {
		t.Fatalf("esperava CriadoEm em UTC, obteve %v", documento.CriadoEm.Location())
	}
	if documento.AtualizadoEm.IsZero() {
		t.Fatal("esperava AtualizadoEm preenchido")
	}
	if documento.AtualizadoEm.Location() != time.UTC {
		t.Fatalf("esperava AtualizadoEm em UTC, obteve %v", documento.AtualizadoEm.Location())
	}
	if documento.CriadoEm.After(documento.AtualizadoEm) {
		t.Fatal("CriadoEm não pode ser posterior a AtualizadoEm")
	}
}

func TestNovoDocumentoAceitaDonoDeSessao(t *testing.T) {
	t.Parallel()

	sessaoID := uuid.New()
	dono, err := vo.NovoDonoSessao(sessaoID)
	if err != nil {
		t.Fatalf("não esperava erro ao montar o dono, obteve %v", err)
	}

	documento, err := NovoDocumento(dono, "artigo.docx", vo.FormatoDocx, tamanhoDeTeste)
	if err != nil {
		t.Fatalf("não esperava erro, obteve %v", err)
	}

	if documento.Dono.Vazio() {
		t.Fatal("o dono de sessão não pode ficar vazio")
	}
	if !documento.Dono.Igual(dono) {
		t.Fatal("esperava que o dono de sessão fosse preservado")
	}
	if documento.Dono.Especie() != vo.EspecieSessao {
		t.Fatalf("espécie = %q, esperava %q", documento.Dono.Especie(), vo.EspecieSessao)
	}
	guardadoUsuario, guardadoSessao := documento.Dono.ParaColunas()
	if guardadoUsuario != nil {
		t.Fatal("dono de sessão não pode preencher a coluna de usuário")
	}
	if guardadoSessao == nil || *guardadoSessao != sessaoID {
		t.Fatal("esperava o identificador na coluna de sessão")
	}
}

func TestNovoDocumentoRecusaDonoVazio(t *testing.T) {
	t.Parallel()

	_, err := NovoDocumento(vo.Dono{}, "artigo.docx", vo.FormatoDocx, tamanhoDeTeste)
	if err == nil {
		t.Fatal("esperava erro de validação para dono vazio")
	}

	var invalido *errors.ErroValidacao
	if !errors.Como(err, &invalido) {
		t.Fatalf("esperava *ErroValidacao, obteve %T", err)
	}
	exigirCampo(t, invalido, "dono")
	if len(invalido.Campos) != 1 {
		t.Fatalf("esperava 1 campo reprovado, obteve %d", len(invalido.Campos))
	}
}

func TestNovoDocumentoDonosDiferentesNaoSeMisturam(t *testing.T) {
	t.Parallel()

	dono := donoDeSessaoDeTeste(t)
	outro := donoDeSessaoDeTeste(t)

	documento, err := NovoDocumento(dono, "artigo.docx", vo.FormatoDocx, tamanhoDeTeste)
	if err != nil {
		t.Fatalf("não esperava erro, obteve %v", err)
	}

	if documento.Dono.Igual(outro) {
		t.Fatal("o documento não pode pertencer a outro dono")
	}
	if !dono.PodeAcessar(documento.Dono) {
		t.Fatal("o dono do documento precisa poder acessá-lo")
	}
	if outro.PodeAcessar(documento.Dono) {
		t.Fatal("um dono estranho não pode acessar o documento")
	}
	if (vo.Dono{}).PodeAcessar(documento.Dono) {
		t.Fatal("dono vazio não pode acessar documento algum")
	}
}

func TestNovoDocumentoHigienizaNome(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome     string
		entrada  string
		esperado string
	}{
		{nome: "caminho unix", entrada: "../../../etc/passwd", esperado: "passwd"},
		{nome: "caminho windows", entrada: "C:\\Users\\ana\\artigo.docx", esperado: "artigo.docx"},
		{nome: "espaços nas bordas", entrada: "   artigo.docx  ", esperado: "artigo.docx"},
		{nome: "caracteres de controle", entrada: "arti\x00go\x1b.docx", esperado: "artigo.docx"},
		{nome: "quebra de linha embutida", entrada: "artigo\n.docx", esperado: "artigo.docx"},
		{nome: "nome acentuado preservado", entrada: "ação-econômica.docx", esperado: "ação-econômica.docx"},
		{nome: "caminho misto", entrada: "pasta\\sub/artigo final.docx", esperado: "artigo final.docx"},
		{
			nome:     "sobrescrita de direção disfarçando a extensão",
			entrada:  "arti" + sobrescritaDireita + "go.docx",
			esperado: "artigo.docx",
		},
		{
			nome:     "espaço de largura zero",
			entrada:  "artigo" + larguraZero + ".docx",
			esperado: "artigo.docx",
		},
		{
			nome:     "marca de ordem de bytes no início",
			entrada:  marcaDeOrdem + "artigo.docx",
			esperado: "artigo.docx",
		},
		{
			nome:     "separador de linha unicode",
			entrada:  "artigo" + separadorLinha + ".docx",
			esperado: "artigo.docx",
		},
		{
			nome:     "separador de parágrafo unicode",
			entrada:  "artigo" + separadorParagrafo + ".docx",
			esperado: "artigo.docx",
		},
		{
			nome:     "controle c1 de próxima linha",
			entrada:  "artigo" + proximaLinhaC1 + ".docx",
			esperado: "artigo.docx",
		},
		{
			nome:     "aspas duplas sobrevivem íntegras",
			entrada:  `relatório "final".docx`,
			esperado: `relatório "final".docx`,
		},
		{
			nome:     "aspas simples sobrevivem íntegras",
			entrada:  "o'brien - artigo.docx",
			esperado: "o'brien - artigo.docx",
		},
		{
			nome:     "invisível antes da barra some junto com o caminho",
			entrada:  "pasta" + larguraZero + "/artigo.docx",
			esperado: "artigo.docx",
		},
		{
			nome:     "invisível depois da barra não sobra no nome",
			entrada:  "pasta/" + sobrescritaDireita + "artigo.docx",
			esperado: "artigo.docx",
		},
		{
			// Prova a ordem: o invisível é removido ANTES do corte final de
			// espaços. Aparar primeiro deixaria o espaço encoberto pelo ZWSP
			// no fim do nome, porque ZWSP não é espaço para o TrimSpace.
			nome:     "invisível encobrindo espaço na borda",
			entrada:  "artigo.docx " + larguraZero,
			esperado: "artigo.docx",
		},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			documento, err := NovoDocumento(donoDeSessaoDeTeste(t), caso.entrada, vo.FormatoDocx, tamanhoDeTeste)
			if err != nil {
				t.Fatalf("não esperava erro, obteve %v", err)
			}
			if documento.NomeOriginal != caso.esperado {
				t.Fatalf("nome higienizado = %q, esperava %q", documento.NomeOriginal, caso.esperado)
			}
		})
	}
}

// TestHigienizarNomeArquivoEFonteUnicaDaRegra exercita a função exportada
// diretamente: é ela que o serviço passa a usar, em vez de manter um critério
// próprio de strings.TrimSpace que diverge da entidade.
func TestHigienizarNomeArquivoEFonteUnicaDaRegra(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome     string
		entrada  string
		esperado string
	}{
		{nome: "nome simples atravessa intacto", entrada: "artigo.docx", esperado: "artigo.docx"},
		{nome: "vazio continua vazio", entrada: "", esperado: ""},
		{nome: "só espaços vira vazio", entrada: "    ", esperado: ""},
		// Divergência que o serviço aprovava e a entidade reprovava.
		{nome: "só um controle c0 vira vazio", entrada: "\x01", esperado: ""},
		{nome: "pasta com barra final vira vazio", entrada: "pasta/", esperado: ""},
		{nome: "barra final windows vira vazio", entrada: "pasta\\", esperado: ""},
		{nome: "só separadores vira vazio", entrada: "///", esperado: ""},
		{nome: "só invisíveis vira vazio", entrada: larguraZero + marcaDeOrdem + sobrescritaDireita, esperado: ""},
		{nome: "del isolado vira vazio", entrada: "\x7f", esperado: ""},
		{nome: "travessia de diretório", entrada: "../../etc/passwd", esperado: "passwd"},
		{nome: "caminho absoluto", entrada: "/var/tmp/artigo.docx", esperado: "artigo.docx"},
		{nome: "aspas preservadas", entrada: `"artigo".docx`, esperado: `"artigo".docx`},
		{nome: "acento preservado", entrada: "resumé.docx", esperado: "resumé.docx"},
		{nome: "espaço interno preservado", entrada: "artigo final.docx", esperado: "artigo final.docx"},
		{nome: "sobrescrita de direção removida", entrada: "arti" + sobrescritaDireita + "go.docx", esperado: "artigo.docx"},
		{nome: "largura zero removida", entrada: "artigo" + larguraZero + ".docx", esperado: "artigo.docx"},
		{nome: "marca de ordem removida", entrada: marcaDeOrdem + "artigo.docx", esperado: "artigo.docx"},
		{nome: "separador de linha removido", entrada: "artigo" + separadorLinha + ".docx", esperado: "artigo.docx"},
		{nome: "separador de parágrafo removido", entrada: "artigo" + separadorParagrafo + ".docx", esperado: "artigo.docx"},
		{nome: "próxima linha c1 removida", entrada: "artigo" + proximaLinhaC1 + ".docx", esperado: "artigo.docx"},
		{nome: "tabulação removida", entrada: "arti\tgo.docx", esperado: "artigo.docx"},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			if obtido := HigienizarNomeArquivo(caso.entrada); obtido != caso.esperado {
				t.Fatalf("HigienizarNomeArquivo = %q, esperava %q", obtido, caso.esperado)
			}
		})
	}
}

func TestHigienizarNomeArquivoEIdempotente(t *testing.T) {
	t.Parallel()

	entradas := []string{
		"  pasta/arti" + larguraZero + "go.docx  ",
		"C:\\Users\\ana\\" + marcaDeOrdem + "artigo.docx",
		strings.Repeat("é", 400),
	}

	for _, entrada := range entradas {
		uma := HigienizarNomeArquivo(entrada)
		duas := HigienizarNomeArquivo(uma)
		if uma != duas {
			t.Fatalf("higienizar duas vezes mudou o resultado: %q -> %q", uma, duas)
		}
	}
}

func TestHigienizarNomeArquivoTruncaEm255Runes(t *testing.T) {
	t.Parallel()

	obtido := HigienizarNomeArquivo(strings.Repeat("é", 400))

	if contagem := utf8.RuneCountInString(obtido); contagem != TamanhoMaximoNome {
		t.Fatalf("esperava %d runes, obteve %d", TamanhoMaximoNome, contagem)
	}
	if !utf8.ValidString(obtido) {
		t.Fatal("o nome truncado precisa continuar sendo UTF-8 válido")
	}
}

func TestNovoDocumentoNaoDeixaNomeVazarParaAChave(t *testing.T) {
	t.Parallel()

	documento, err := NovoDocumento(donoDeSessaoDeTeste(t), "../../../etc/passwd", vo.FormatoDocx, tamanhoDeTeste)
	if err != nil {
		t.Fatalf("não esperava erro, obteve %v", err)
	}

	chave := documento.ChaveStorage.String()
	if strings.Contains(chave, "..") {
		t.Fatal("a chave de storage não pode conter travessia de diretório")
	}
	if strings.Contains(chave, "passwd") {
		t.Fatal("a chave de storage não pode conter o nome enviado pelo usuário")
	}
}

func TestNovoDocumentoTruncaNomeEm255Runes(t *testing.T) {
	t.Parallel()

	longo := strings.Repeat("é", 400)

	documento, err := NovoDocumento(donoDeSessaoDeTeste(t), longo, vo.FormatoDocx, tamanhoDeTeste)
	if err != nil {
		t.Fatalf("não esperava erro, obteve %v", err)
	}

	if contagem := utf8.RuneCountInString(documento.NomeOriginal); contagem != 255 {
		t.Fatalf("esperava 255 runes, obteve %d", contagem)
	}
	if !utf8.ValidString(documento.NomeOriginal) {
		t.Fatal("o nome truncado precisa continuar sendo UTF-8 válido")
	}
}

func TestNovoDocumentoReprovaEntradaInvalida(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome     string
		entrada  string
		formato  vo.FormatoArquivo
		tamanho  int64
		campo    string
		qtdCampo int
	}{
		{nome: "nome vazio", entrada: "", formato: vo.FormatoDocx, tamanho: tamanhoDeTeste, campo: "nome_original", qtdCampo: 1},
		{nome: "nome só com espaços", entrada: "     ", formato: vo.FormatoDocx, tamanho: tamanhoDeTeste, campo: "nome_original", qtdCampo: 1},
		{nome: "nome só com separadores", entrada: "///", formato: vo.FormatoDocx, tamanho: tamanhoDeTeste, campo: "nome_original", qtdCampo: 1},
		{nome: "nome só com invisíveis", entrada: larguraZero + marcaDeOrdem, formato: vo.FormatoDocx, tamanho: tamanhoDeTeste, campo: "nome_original", qtdCampo: 1},
		{nome: "nome só com controle", entrada: "\x01", formato: vo.FormatoDocx, tamanho: tamanhoDeTeste, campo: "nome_original", qtdCampo: 1},
		{nome: "nome só pasta", entrada: "pasta/", formato: vo.FormatoDocx, tamanho: tamanhoDeTeste, campo: "nome_original", qtdCampo: 1},
		{nome: "tamanho zero", entrada: "artigo.docx", formato: vo.FormatoDocx, tamanho: 0, campo: "tamanho_bytes", qtdCampo: 1},
		{nome: "tamanho negativo", entrada: "artigo.docx", formato: vo.FormatoDocx, tamanho: -1, campo: "tamanho_bytes", qtdCampo: 1},
		{nome: "formato desconhecido", entrada: "artigo.docx", formato: vo.FormatoArquivo("exe"), tamanho: tamanhoDeTeste, campo: "formato", qtdCampo: 1},
		{nome: "formato vazio", entrada: "artigo.docx", formato: vo.FormatoArquivo(""), tamanho: tamanhoDeTeste, campo: "formato", qtdCampo: 1},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			_, err := NovoDocumento(donoDeSessaoDeTeste(t), caso.entrada, caso.formato, caso.tamanho)
			if err == nil {
				t.Fatal("esperava erro de validação")
			}

			var invalido *errors.ErroValidacao
			if !errors.Como(err, &invalido) {
				t.Fatalf("esperava *ErroValidacao, obteve %T", err)
			}
			exigirCampo(t, invalido, caso.campo)
			if len(invalido.Campos) != caso.qtdCampo {
				t.Fatalf("esperava %d campo(s) reprovado(s), obteve %d", caso.qtdCampo, len(invalido.Campos))
			}
		})
	}
}

func TestNovoDocumentoAcumulaTodosOsCamposInvalidos(t *testing.T) {
	t.Parallel()

	_, err := NovoDocumento(vo.Dono{}, "   ", vo.FormatoArquivo("exe"), 0)
	if err == nil {
		t.Fatal("esperava erro de validação")
	}

	var invalido *errors.ErroValidacao
	if !errors.Como(err, &invalido) {
		t.Fatalf("esperava *ErroValidacao, obteve %T", err)
	}
	if len(invalido.Campos) != 4 {
		t.Fatalf("esperava 4 campos reprovados num único erro, obteve %d", len(invalido.Campos))
	}
	for _, campo := range []string{"dono", "nome_original", "tamanho_bytes", "formato"} {
		exigirCampo(t, invalido, campo)
	}
}

func TestNovoDocumentoNaoEcoaNomeNaMensagemDeErro(t *testing.T) {
	t.Parallel()

	const segredo = "tese-sigilosa-do-usuario"

	_, err := NovoDocumento(vo.Dono{}, segredo+"/", vo.FormatoArquivo("exe"), 0)
	if err == nil {
		t.Fatal("esperava erro de validação")
	}
	if strings.Contains(err.Error(), segredo) {
		t.Fatal("a mensagem de erro não pode ecoar o nome enviado pelo usuário")
	}
}

func TestValidarCDM(t *testing.T) {
	t.Parallel()

	noLimite := objetoJSONComTamanho(t, TamanhoMaximoCDMBytes)
	umByteAcima := objetoJSONComTamanho(t, TamanhoMaximoCDMBytes+1)

	casos := []struct {
		nome    string
		entrada json.RawMessage
		erro    bool
	}{
		{nome: "objeto vazio é aceito", entrada: json.RawMessage(`{}`), erro: false},
		{nome: "objeto com blocos é aceito", entrada: json.RawMessage(`{"blocos":[{"papel":"titulo"}]}`), erro: false},
		{nome: "objeto com espaço antes é aceito", entrada: json.RawMessage("  \n\t{\"blocos\":[]}"), erro: false},
		{nome: "objeto no limite exato é aceito", entrada: noLimite, erro: false},
		{nome: "objeto um byte acima do limite é recusado", entrada: umByteAcima, erro: true},
		{nome: "cdm nulo é recusado", entrada: nil, erro: true},
		{nome: "cdm vazio é recusado", entrada: json.RawMessage(``), erro: true},
		{nome: "só espaços é recusado", entrada: json.RawMessage("   \n  "), erro: true},
		{nome: "texto solto não é json", entrada: json.RawMessage(`isto não é json`), erro: true},
		{nome: "json truncado é recusado", entrada: json.RawMessage(`{"blocos":`), erro: true},
		{nome: "abre chave sozinha é recusada", entrada: json.RawMessage(`{`), erro: true},
		{nome: "lixo depois do objeto é recusado", entrada: json.RawMessage(`{} {}`), erro: true},
		// Os quatro abaixo são JSON válido e nenhum deles é um CDM: json.Valid
		// sozinho aprovaria os quatro.
		{nome: "null é json válido mas não é cdm", entrada: json.RawMessage(`null`), erro: true},
		{nome: "número é json válido mas não é cdm", entrada: json.RawMessage(`123`), erro: true},
		{nome: "string é json válido mas não é cdm", entrada: json.RawMessage(`"texto"`), erro: true},
		{nome: "booleano é json válido mas não é cdm", entrada: json.RawMessage(`true`), erro: true},
		{nome: "array é json válido mas não é cdm", entrada: json.RawMessage(`[]`), erro: true},
		{nome: "array de blocos ainda não é cdm", entrada: json.RawMessage(`[{"papel":"titulo"}]`), erro: true},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			err := ValidarCDM(caso.entrada)
			if !caso.erro {
				if err != nil {
					t.Fatalf("não esperava erro, obteve %v", err)
				}
				return
			}

			if err == nil {
				t.Fatal("esperava erro de validação")
			}
			var invalido *errors.ErroValidacao
			if !errors.Como(err, &invalido) {
				t.Fatalf("esperava *ErroValidacao, obteve %T", err)
			}
			exigirCampo(t, invalido, "cdm")
		})
	}
}

func TestValidarCDMNaoEcoaConteudoRecebido(t *testing.T) {
	t.Parallel()

	const segredo = "conclusao-confidencial-do-artigo"

	entradas := []json.RawMessage{
		json.RawMessage(segredo),
		json.RawMessage(`"` + segredo + `"`),
		json.RawMessage(`[{"texto":"` + segredo + `"}]`),
	}

	for _, entrada := range entradas {
		err := ValidarCDM(entrada)
		if err == nil {
			t.Fatalf("esperava erro de validação para %d bytes de entrada", len(entrada))
		}
		if strings.Contains(err.Error(), segredo) {
			t.Fatal("a mensagem de erro não pode ecoar o conteúdo do documento")
		}
	}
}

func TestCaminhoFelizDeTransicoes(t *testing.T) {
	t.Parallel()

	documento := novoDocumentoValido(t)

	if err := documento.IniciarAnalise(); err != nil {
		t.Fatalf("IniciarAnalise: não esperava erro, obteve %v", err)
	}
	if documento.Status != StatusAnalisando {
		t.Fatalf("status = %q, esperava %q", documento.Status, StatusAnalisando)
	}

	if err := documento.ConcluirAnalise(json.RawMessage(`{"blocos":[]}`)); err != nil {
		t.Fatalf("ConcluirAnalise: não esperava erro, obteve %v", err)
	}
	if documento.Status != StatusAnalisado {
		t.Fatalf("status = %q, esperava %q", documento.Status, StatusAnalisado)
	}

	if err := documento.IniciarFormatacao(); err != nil {
		t.Fatalf("IniciarFormatacao: não esperava erro, obteve %v", err)
	}
	if documento.Status != StatusFormatando {
		t.Fatalf("status = %q, esperava %q", documento.Status, StatusFormatando)
	}

	if err := documento.ConcluirFormatacao(); err != nil {
		t.Fatalf("ConcluirFormatacao: não esperava erro, obteve %v", err)
	}
	if documento.Status != StatusFormatado {
		t.Fatalf("status = %q, esperava %q", documento.Status, StatusFormatado)
	}
}

func TestTransicoesInvalidasNaoAlteramEstado(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome    string
		preparo func(t *testing.T, documento *Documento)
		acao    func(documento *Documento) error
	}{
		{
			nome:    "formatar direto de recebido",
			preparo: func(*testing.T, *Documento) {},
			acao:    func(documento *Documento) error { return documento.IniciarFormatacao() },
		},
		{
			nome:    "concluir formatação de recebido",
			preparo: func(*testing.T, *Documento) {},
			acao:    func(documento *Documento) error { return documento.ConcluirFormatacao() },
		},
		{
			nome:    "concluir análise de recebido",
			preparo: func(*testing.T, *Documento) {},
			acao:    func(documento *Documento) error { return documento.ConcluirAnalise(json.RawMessage(`{}`)) },
		},
		{
			nome: "formatar durante a análise",
			preparo: func(t *testing.T, documento *Documento) {
				exigirSemErro(t, documento.IniciarAnalise())
			},
			acao: func(documento *Documento) error { return documento.IniciarFormatacao() },
		},
		{
			nome: "iniciar análise durante a análise",
			preparo: func(t *testing.T, documento *Documento) {
				exigirSemErro(t, documento.IniciarAnalise())
			},
			acao: func(documento *Documento) error { return documento.IniciarAnalise() },
		},
		{
			nome: "concluir formatação a partir de analisado",
			preparo: func(t *testing.T, documento *Documento) {
				exigirSemErro(t, documento.IniciarAnalise())
				exigirSemErro(t, documento.ConcluirAnalise(json.RawMessage(`{}`)))
			},
			acao: func(documento *Documento) error { return documento.ConcluirFormatacao() },
		},
		{
			nome: "concluir análise durante a formatação",
			preparo: func(t *testing.T, documento *Documento) {
				exigirSemErro(t, documento.IniciarAnalise())
				exigirSemErro(t, documento.ConcluirAnalise(json.RawMessage(`{}`)))
				exigirSemErro(t, documento.IniciarFormatacao())
			},
			acao: func(documento *Documento) error { return documento.ConcluirAnalise(json.RawMessage(`{}`)) },
		},
		{
			nome: "marcar falha duas vezes",
			preparo: func(t *testing.T, documento *Documento) {
				exigirSemErro(t, documento.MarcarFalha())
			},
			acao: func(documento *Documento) error { return documento.MarcarFalha() },
		},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			documento := novoDocumentoValido(t)
			caso.preparo(t, &documento)

			antes := documento
			err := caso.acao(&documento)
			if err == nil {
				t.Fatal("esperava erro de conflito")
			}

			var conflito *errors.ErroConflito
			if !errors.Como(err, &conflito) {
				t.Fatalf("esperava *ErroConflito, obteve %T", err)
			}
			if documento.Status != antes.Status {
				t.Fatalf("estado alterado: status = %q, esperava %q", documento.Status, antes.Status)
			}
			if !documento.AtualizadoEm.Equal(antes.AtualizadoEm) {
				t.Fatal("AtualizadoEm não pode mudar numa transição rejeitada")
			}
			if string(documento.CDM) != string(antes.CDM) {
				t.Fatal("o CDM não pode mudar numa transição rejeitada")
			}
		})
	}
}

func TestMarcarFalhaAPartirDeCadaEstadoNaoTerminal(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome    string
		preparo func(t *testing.T, documento *Documento)
	}{
		{nome: "de recebido", preparo: func(*testing.T, *Documento) {}},
		{
			nome: "de formatando",
			preparo: func(t *testing.T, documento *Documento) {
				exigirSemErro(t, documento.IniciarAnalise())
				exigirSemErro(t, documento.ConcluirAnalise(json.RawMessage(`{}`)))
				exigirSemErro(t, documento.IniciarFormatacao())
			},
		},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			documento := novoDocumentoValido(t)
			caso.preparo(t, &documento)

			if err := documento.MarcarFalha(); err != nil {
				t.Fatalf("não esperava erro, obteve %v", err)
			}
			if documento.Status != StatusFalhou {
				t.Fatalf("status = %q, esperava %q", documento.Status, StatusFalhou)
			}
		})
	}
}

func TestReprocessamentoAPartirDeFalhou(t *testing.T) {
	t.Parallel()

	documento := novoDocumentoValido(t)
	exigirSemErro(t, documento.MarcarFalha())

	if err := documento.IniciarAnalise(); err != nil {
		t.Fatalf("esperava reprocessar a análise a partir de falhou, obteve %v", err)
	}
	if documento.Status != StatusAnalisando {
		t.Fatalf("status = %q, esperava %q", documento.Status, StatusAnalisando)
	}
}

func TestDefinirPreviewPDFNaoAlteraStatus(t *testing.T) {
	t.Parallel()

	documento := novoDocumentoValido(t)
	chave, err := vo.NovaChavePreviewPDF(documento.ID)
	if err != nil {
		t.Fatalf("não esperava erro ao montar a chave, obteve %v", err)
	}

	if err := documento.DefinirPreviewPDF(chave); err != nil {
		t.Fatalf("não esperava erro, obteve %v", err)
	}
	if documento.Status != StatusRecebido {
		t.Fatalf("status = %q, esperava %q", documento.Status, StatusRecebido)
	}
	if !documento.TemPreview() {
		t.Fatal("esperava TemPreview() verdadeiro")
	}
	if documento.ChaveStoragePDF == nil || *documento.ChaveStoragePDF != chave {
		t.Fatal("esperava que a chave de preview fosse guardada")
	}
}

// TestDefinirPreviewPDFExigeAChaveDoProprioDocumento fecha o furo original: a
// gramática canônica aceita o UUID de QUALQUER documento, então validar só
// "chave bem formada" deixaria um documento apontar para o preview de outro.
func TestDefinirPreviewPDFExigeAChaveDoProprioDocumento(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome  string
		chave func(t *testing.T, documento Documento) vo.ChaveStorage
	}{
		{
			nome:  "chave vazia",
			chave: func(*testing.T, Documento) vo.ChaveStorage { return vo.ChaveStorage("") },
		},
		{
			nome: "preview de outro documento",
			chave: func(t *testing.T, _ Documento) vo.ChaveStorage {
				t.Helper()
				outra, err := vo.NovaChavePreviewPDF(uuid.New())
				if err != nil {
					t.Fatalf("não esperava erro ao montar a chave do outro, obteve %v", err)
				}
				return outra
			},
		},
		{
			nome: "original do próprio documento",
			chave: func(t *testing.T, documento Documento) vo.ChaveStorage {
				t.Helper()
				original, err := vo.NovaChaveOriginal(documento.ID, documento.Formato)
				if err != nil {
					t.Fatalf("não esperava erro ao montar a chave original, obteve %v", err)
				}
				return original
			},
		},
		{
			nome: "caminho arbitrário fora do espaço de chaves",
			chave: func(*testing.T, Documento) vo.ChaveStorage {
				return vo.ChaveStorage("documentos/../../etc/passwd")
			},
		},
		{
			nome: "chave com travessia depois do id",
			chave: func(_ *testing.T, documento Documento) vo.ChaveStorage {
				return vo.ChaveStorage("documentos/" + documento.ID.String() + "/../preview.pdf")
			},
		},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			documento := novoDocumentoValido(t)
			antes := documento

			err := documento.DefinirPreviewPDF(caso.chave(t, documento))
			if err == nil {
				t.Fatal("esperava erro de validação")
			}

			var invalido *errors.ErroValidacao
			if !errors.Como(err, &invalido) {
				t.Fatalf("esperava *ErroValidacao, obteve %T", err)
			}
			if documento.TemPreview() {
				t.Fatal("uma chave recusada não pode marcar o documento como tendo preview")
			}
			if documento.ChaveStoragePDF != nil {
				t.Fatal("uma chave recusada não pode ser guardada")
			}
			if !documento.AtualizadoEm.Equal(antes.AtualizadoEm) {
				t.Fatal("AtualizadoEm não pode mudar quando a chave é recusada")
			}
		})
	}
}

func TestConcluirAnaliseGravaCDMEAvancaAtualizadoEm(t *testing.T) {
	t.Parallel()

	documento := novoDocumentoValido(t)
	exigirSemErro(t, documento.IniciarAnalise())

	antes := documento.AtualizadoEm
	cdm := json.RawMessage(`{"blocos":[{"papel":"titulo"}]}`)

	if err := documento.ConcluirAnalise(cdm); err != nil {
		t.Fatalf("não esperava erro, obteve %v", err)
	}
	if string(documento.CDM) != string(cdm) {
		t.Fatal("o CDM gravado difere do informado")
	}
	if documento.AtualizadoEm.Before(antes) {
		t.Fatal("AtualizadoEm não pode retroceder")
	}
	if !documento.CriadoEm.Equal(documento.CriadoEm.UTC()) {
		t.Fatal("CriadoEm precisa permanecer em UTC")
	}
}

func TestConcluirAnaliseFazCopiaDefensivaDoCDM(t *testing.T) {
	t.Parallel()

	documento := novoDocumentoValido(t)
	exigirSemErro(t, documento.IniciarAnalise())

	cdm := json.RawMessage(`{"blocos":[]}`)
	exigirSemErro(t, documento.ConcluirAnalise(cdm))

	guardado := string(documento.CDM)
	for indice := range cdm {
		cdm[indice] = 'X'
	}

	if string(documento.CDM) != guardado {
		t.Fatal("mutar o slice original não pode alterar o CDM da entidade")
	}
}

func TestConcluirAnaliseRecusaCDMInvalidoSemMutarNada(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome    string
		entrada json.RawMessage
	}{
		{nome: "cdm nulo", entrada: nil},
		{nome: "cdm vazio", entrada: json.RawMessage(``)},
		{nome: "não é json", entrada: json.RawMessage(`isto não é json`)},
		{nome: "null", entrada: json.RawMessage(`null`)},
		{nome: "número", entrada: json.RawMessage(`123`)},
		{nome: "string", entrada: json.RawMessage(`"texto"`)},
		{nome: "array", entrada: json.RawMessage(`[]`)},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			documento := novoDocumentoValido(t)
			exigirSemErro(t, documento.IniciarAnalise())
			antes := documento

			err := documento.ConcluirAnalise(caso.entrada)
			if err == nil {
				t.Fatal("esperava erro de validação")
			}

			var invalido *errors.ErroValidacao
			if !errors.Como(err, &invalido) {
				t.Fatalf("esperava *ErroValidacao, obteve %T", err)
			}
			exigirCampo(t, invalido, "cdm")
			if documento.Status != antes.Status {
				t.Fatalf("status = %q, esperava %q", documento.Status, antes.Status)
			}
			if documento.CDM != nil {
				t.Fatal("um CDM recusado não pode ser gravado")
			}
			if !documento.AtualizadoEm.Equal(antes.AtualizadoEm) {
				t.Fatal("AtualizadoEm não pode mudar quando o CDM é recusado")
			}
		})
	}
}

// TestConcluirAnaliseDaPrecedenciaAoEstadoSobreOConteudo trava a ordem das duas
// verificações: fora de ordem, o chamador precisa ouvir "conflito de estado",
// e não um erro de validação que o faria reenviar outro CDM para sempre.
func TestConcluirAnaliseDaPrecedenciaAoEstadoSobreOConteudo(t *testing.T) {
	t.Parallel()

	documento := novoDocumentoValido(t) // status recebido: ConcluirAnalise é inválido aqui
	antes := documento

	err := documento.ConcluirAnalise(json.RawMessage(`123`))
	if err == nil {
		t.Fatal("esperava erro")
	}

	var conflito *errors.ErroConflito
	if !errors.Como(err, &conflito) {
		t.Fatalf("esperava *ErroConflito, obteve %T", err)
	}
	var invalido *errors.ErroValidacao
	if errors.Como(err, &invalido) {
		t.Fatal("estado tem precedência: não pode devolver *ErroValidacao fora de ordem")
	}
	if documento.Status != antes.Status || documento.CDM != nil {
		t.Fatal("nada pode ser mutado quando a chamada é recusada")
	}
}

// TestValidarAprovaDocumentoRecemCriado garante que a revalidação não briga com
// o próprio construtor.
func TestValidarAprovaDocumentoRecemCriado(t *testing.T) {
	t.Parallel()

	documento := novoDocumentoValido(t)

	if err := documento.Validar(); err != nil {
		t.Fatalf("não esperava erro, obteve %v", err)
	}

	comCDM := novoDocumentoValido(t)
	comCDM.CDM = json.RawMessage(`{"blocos":[]}`)
	if err := comCDM.Validar(); err != nil {
		t.Fatalf("CDM válido presente não pode reprovar, obteve %v", err)
	}

	deUsuario, err := NovoDocumento(donoDeUsuarioDeTeste(t), "artigo.pdf", vo.FormatoPDF, 1)
	if err != nil {
		t.Fatalf("não esperava erro, obteve %v", err)
	}
	if err := deUsuario.Validar(); err != nil {
		t.Fatalf("não esperava erro, obteve %v", err)
	}
}

// TestValidarReprovaDocumentoMontadoAMao cobre UM TERMO POR CASO: todos os
// campos de Documento são exportados, então Registrar receberia qualquer
// literal montado à mão se a revalidação deixasse algum termo sem exercício.
func TestValidarReprovaDocumentoMontadoAMao(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome   string
		mutar  func(t *testing.T, documento *Documento)
		campos []string
		exato  bool
	}{
		{
			nome:   "id nulo",
			mutar:  func(_ *testing.T, documento *Documento) { documento.ID = uuid.Nil },
			campos: []string{"id"},
		},
		{
			nome:   "dono vazio",
			mutar:  func(_ *testing.T, documento *Documento) { documento.Dono = vo.Dono{} },
			campos: []string{"dono"},
			exato:  true,
		},
		{
			nome:   "nome vazio",
			mutar:  func(_ *testing.T, documento *Documento) { documento.NomeOriginal = "" },
			campos: []string{"nome_original"},
			exato:  true,
		},
		{
			nome:   "nome só com espaços",
			mutar:  func(_ *testing.T, documento *Documento) { documento.NomeOriginal = "   " },
			campos: []string{"nome_original"},
			exato:  true,
		},
		{
			nome:   "nome com caminho",
			mutar:  func(_ *testing.T, documento *Documento) { documento.NomeOriginal = "pasta/" },
			campos: []string{"nome_original"},
			exato:  true,
		},
		{
			nome:   "nome com barra no meio",
			mutar:  func(_ *testing.T, documento *Documento) { documento.NomeOriginal = "pasta/artigo.docx" },
			campos: []string{"nome_original"},
			exato:  true,
		},
		{
			nome:   "nome com controle",
			mutar:  func(_ *testing.T, documento *Documento) { documento.NomeOriginal = "\x01" },
			campos: []string{"nome_original"},
			exato:  true,
		},
		{
			nome: "nome com invisível",
			mutar: func(_ *testing.T, documento *Documento) {
				documento.NomeOriginal = "arti" + sobrescritaDireita + "go.docx"
			},
			campos: []string{"nome_original"},
			exato:  true,
		},
		{
			nome:   "nome com espaço na borda",
			mutar:  func(_ *testing.T, documento *Documento) { documento.NomeOriginal = " artigo.docx" },
			campos: []string{"nome_original"},
			exato:  true,
		},
		{
			nome: "nome acima do teto de runes",
			mutar: func(_ *testing.T, documento *Documento) {
				documento.NomeOriginal = strings.Repeat("é", TamanhoMaximoNome+1)
			},
			campos: []string{"nome_original"},
			exato:  true,
		},
		{
			nome:   "formato desconhecido",
			mutar:  func(_ *testing.T, documento *Documento) { documento.Formato = vo.FormatoArquivo("exe") },
			campos: []string{"formato"},
		},
		{
			nome:   "formato vazio",
			mutar:  func(_ *testing.T, documento *Documento) { documento.Formato = vo.FormatoArquivo("") },
			campos: []string{"formato"},
		},
		{
			nome:   "tamanho zero",
			mutar:  func(_ *testing.T, documento *Documento) { documento.TamanhoBytes = 0 },
			campos: []string{"tamanho_bytes"},
			exato:  true,
		},
		{
			nome:   "tamanho negativo",
			mutar:  func(_ *testing.T, documento *Documento) { documento.TamanhoBytes = -1 },
			campos: []string{"tamanho_bytes"},
			exato:  true,
		},
		{
			nome:   "chave de storage vazia",
			mutar:  func(_ *testing.T, documento *Documento) { documento.ChaveStorage = vo.ChaveStorage("") },
			campos: []string{"chave_storage"},
			exato:  true,
		},
		{
			nome: "chave de storage de outro documento",
			mutar: func(t *testing.T, documento *Documento) {
				t.Helper()
				alheia, err := vo.NovaChaveOriginal(uuid.New(), documento.Formato)
				if err != nil {
					t.Fatalf("não esperava erro ao montar a chave alheia, obteve %v", err)
				}
				documento.ChaveStorage = alheia
			},
			campos: []string{"chave_storage"},
			exato:  true,
		},
		{
			nome: "chave de storage de outro formato",
			mutar: func(t *testing.T, documento *Documento) {
				t.Helper()
				outra, err := vo.NovaChaveOriginal(documento.ID, vo.FormatoPDF)
				if err != nil {
					t.Fatalf("não esperava erro ao montar a chave, obteve %v", err)
				}
				documento.ChaveStorage = outra
			},
			campos: []string{"chave_storage"},
			exato:  true,
		},
		{
			nome: "chave de storage apontando para o preview",
			mutar: func(t *testing.T, documento *Documento) {
				t.Helper()
				preview, err := vo.NovaChavePreviewPDF(documento.ID)
				if err != nil {
					t.Fatalf("não esperava erro ao montar a chave de preview, obteve %v", err)
				}
				documento.ChaveStorage = preview
			},
			campos: []string{"chave_storage"},
			exato:  true,
		},
		{
			nome:   "chave de storage fora do espaço de chaves",
			mutar:  func(_ *testing.T, documento *Documento) { documento.ChaveStorage = vo.ChaveStorage("../../etc/passwd") },
			campos: []string{"chave_storage"},
			exato:  true,
		},
		{
			nome:   "status vazio",
			mutar:  func(_ *testing.T, documento *Documento) { documento.Status = Status("") },
			campos: []string{"status"},
			exato:  true,
		},
		{
			nome:   "status desconhecido",
			mutar:  func(_ *testing.T, documento *Documento) { documento.Status = Status("arquivado") },
			campos: []string{"status"},
			exato:  true,
		},
		{
			nome:   "status válido porém adiantado",
			mutar:  func(_ *testing.T, documento *Documento) { documento.Status = StatusAnalisando },
			campos: []string{"status"},
			exato:  true,
		},
		{
			nome:   "status formatado num documento recém-recebido",
			mutar:  func(_ *testing.T, documento *Documento) { documento.Status = StatusFormatado },
			campos: []string{"status"},
			exato:  true,
		},
		{
			nome:   "criado em zerado",
			mutar:  func(_ *testing.T, documento *Documento) { documento.CriadoEm = time.Time{} },
			campos: []string{"criado_em"},
			exato:  true,
		},
		{
			nome:   "atualizado em zerado",
			mutar:  func(_ *testing.T, documento *Documento) { documento.AtualizadoEm = time.Time{} },
			campos: []string{"atualizado_em"},
			exato:  true,
		},
		{
			nome:   "cdm presente e não é json",
			mutar:  func(_ *testing.T, documento *Documento) { documento.CDM = json.RawMessage(`isto não é json`) },
			campos: []string{"cdm"},
			exato:  true,
		},
		{
			nome:   "cdm presente e é null",
			mutar:  func(_ *testing.T, documento *Documento) { documento.CDM = json.RawMessage(`null`) },
			campos: []string{"cdm"},
			exato:  true,
		},
		{
			nome:   "cdm presente e é array",
			mutar:  func(_ *testing.T, documento *Documento) { documento.CDM = json.RawMessage(`[]`) },
			campos: []string{"cdm"},
			exato:  true,
		},
		{
			nome: "cdm presente e acima do teto",
			mutar: func(t *testing.T, documento *Documento) {
				t.Helper()
				documento.CDM = objetoJSONComTamanho(t, TamanhoMaximoCDMBytes+1)
			},
			campos: []string{"cdm"},
			exato:  true,
		},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			documento := novoDocumentoValido(t)
			caso.mutar(t, &documento)

			err := documento.Validar()
			if err == nil {
				t.Fatal("esperava erro de validação")
			}

			var invalido *errors.ErroValidacao
			if !errors.Como(err, &invalido) {
				t.Fatalf("esperava *ErroValidacao, obteve %T", err)
			}
			for _, campo := range caso.campos {
				exigirCampo(t, invalido, campo)
			}
			if caso.exato && len(invalido.Campos) != len(caso.campos) {
				t.Fatalf("esperava %d campo(s) reprovado(s), obteve %d", len(caso.campos), len(invalido.Campos))
			}
		})
	}
}

func TestValidarAcumulaTodosOsCamposNumUnicoErro(t *testing.T) {
	t.Parallel()

	var documento Documento // literal montado à mão, o cenário que Registrar precisa barrar

	err := documento.Validar()
	if err == nil {
		t.Fatal("esperava erro de validação para o documento zerado")
	}

	var invalido *errors.ErroValidacao
	if !errors.Como(err, &invalido) {
		t.Fatalf("esperava *ErroValidacao, obteve %T", err)
	}

	esperados := []string{
		"id", "dono", "nome_original", "formato",
		"tamanho_bytes", "chave_storage", "status", "criado_em", "atualizado_em",
	}
	for _, campo := range esperados {
		exigirCampo(t, invalido, campo)
	}
	if len(invalido.Campos) != len(esperados) {
		t.Fatalf("esperava %d campos num único erro, obteve %d", len(esperados), len(invalido.Campos))
	}
}

func TestValidarNaoEcoaConteudoDoUsuario(t *testing.T) {
	t.Parallel()

	const segredo = "titulo-secreto-da-tese"

	documento := novoDocumentoValido(t)
	documento.NomeOriginal = segredo + "/"
	documento.CDM = json.RawMessage(`"` + segredo + `"`)

	err := documento.Validar()
	if err == nil {
		t.Fatal("esperava erro de validação")
	}
	if strings.Contains(err.Error(), segredo) {
		t.Fatal("a mensagem de erro não pode ecoar conteúdo enviado pelo usuário")
	}
}

// objetoJSONComTamanho monta um objeto JSON válido com exatamente o número de
// bytes pedido, para exercitar o limite e o primeiro byte além dele.
func objetoJSONComTamanho(t *testing.T, bytes int) json.RawMessage {
	t.Helper()

	const moldura = len(`{"a":""}`)
	if bytes < moldura {
		t.Fatalf("tamanho %d é menor que a moldura mínima de %d bytes", bytes, moldura)
	}

	bruto := `{"a":"` + strings.Repeat("x", bytes-moldura) + `"}`
	if len(bruto) != bytes {
		t.Fatalf("montou %d bytes, esperava %d", len(bruto), bytes)
	}
	if !json.Valid([]byte(bruto)) {
		t.Fatal("o objeto montado precisa ser JSON válido")
	}
	return json.RawMessage(bruto)
}

func exigirSemErro(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Fatalf("não esperava erro, obteve %v", err)
	}
}

func exigirCampo(t *testing.T, erro *errors.ErroValidacao, campo string) {
	t.Helper()

	for _, invalido := range erro.Campos {
		if invalido.Campo == campo {
			return
		}
	}

	nomes := make([]string, 0, len(erro.Campos))
	for _, invalido := range erro.Campos {
		nomes = append(nomes, invalido.Campo)
	}
	t.Fatalf("esperava o campo %q entre %v", campo, nomes)
}
