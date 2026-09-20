// Package ooxml testa o round-trip de um pacote DOCX: abrir e salvar sem
// nenhuma mutação. Este é o primeiro teste do módulo, de propósito — o plano
// do backend (F2) e o ADR 0001 exigem essa rede antes de qualquer código que
// mute nó XML. Se o round-trip não segurar, a premissa de "formatar in-place"
// do projeto não se sustenta.
//
// Duas camadas de verificação convivem aqui:
//
//  1. Igualdade byte a byte do ZIP inteiro quando nada é mutado. Confirmado
//     experimentalmente com os dois fixtures deste pacote que
//     zip.Writer.Copy (stdlib, desde Go 1.17) reproduz o arquivo de origem
//     byte a byte ao recopiar cada entrada sem descomprimir — então essa é a
//     implementação esperada para toda parte não tocada, não uma aspiração.
//  2. Igualdade por parte (nome + conteúdo) e integridade de texto (runas de
//     todo w:t), que são as duas invariantes exigidas pelo ADR 0001 e pela
//     skill ooxml-referencia, e que dão diagnóstico mais claro que o diff do
//     ZIP inteiro quando alguma coisa quebra.
package ooxml

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// ---------------------------------------------------------------------------
// Fixtures reais e helpers de montagem de ZIP sintético
// ---------------------------------------------------------------------------

// lerFixture lê um arquivo de backend/testdata/. O pacote roda com cwd em
// backend/internal/infra/ooxml, três níveis abaixo de backend/.
func lerFixture(t *testing.T, nome string) []byte {
	t.Helper()

	caminho := filepath.Join("..", "..", "..", "testdata", nome)
	dados, err := os.ReadFile(caminho)
	require.NoError(t, err, "fixture %q precisa existir em backend/testdata/", nome)
	return dados
}

// entradaZip é um par nome/conteúdo/método usado para montar um ZIP
// determinístico em memória.
type entradaZip struct {
	nome     string
	conteudo []byte
	metodo   uint16
}

// montarZip escreve um ZIP em memória a partir de uma lista ordenada de
// entradas. FileHeader.Modified fica no zero-value deliberadamente: um
// timestamp fixo (não o relógio do teste) é o que torna o ZIP sintético
// reproduzível byte a byte entre execuções — confirmado que zip.Writer
// produz a mesma saída para o mesmo Modified zero em duas chamadas
// independentes.
func montarZip(t *testing.T, entradas []entradaZip) []byte {
	t.Helper()

	var buf bytes.Buffer
	escritor := zip.NewWriter(&buf)
	for _, entrada := range entradas {
		saida, err := escritor.CreateHeader(&zip.FileHeader{Name: entrada.nome, Method: entrada.metodo})
		require.NoError(t, err, "montarZip: CreateHeader(%q)", entrada.nome)
		_, err = saida.Write(entrada.conteudo)
		require.NoError(t, err, "montarZip: Write(%q)", entrada.nome)
	}
	require.NoError(t, escritor.Close(), "montarZip: Close")
	return buf.Bytes()
}

// documentoXMLMinimo monta um word/document.xml de um parágrafo só, com o
// texto informado já escapado.
func documentoXMLMinimo(texto string) []byte {
	return []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>%s</w:t></w:r></w:p></w:body></w:document>`, texto))
}

// partesBaseDocx devolve as três partes mínimas de um pacote DOCX válido, na
// ordem que o LibreOffice espera: [Content_Types].xml, _rels/.rels e
// word/document.xml.
func partesBaseDocx(documentoXML []byte) []entradaZip {
	contentTypes := []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`)
	rels := []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`)

	return []entradaZip{
		{nome: "[Content_Types].xml", conteudo: contentTypes, metodo: zip.Deflate},
		{nome: "_rels/.rels", conteudo: rels, metodo: zip.Deflate},
		{nome: "word/document.xml", conteudo: documentoXML, metodo: zip.Deflate},
	}
}

// docxSintetico monta um pacote DOCX mínimo e válido com o document.xml
// informado.
func docxSintetico(t *testing.T, documentoXML []byte) []byte {
	t.Helper()
	return montarZip(t, partesBaseDocx(documentoXML))
}

// listarPartes devolve os nomes das entradas do ZIP, na ordem em que
// aparecem no pacote.
func listarPartes(t *testing.T, dados []byte) []string {
	t.Helper()

	leitor, err := zip.NewReader(bytes.NewReader(dados), int64(len(dados)))
	require.NoError(t, err, "listarPartes: ZIP inválido")

	nomes := make([]string, 0, len(leitor.File))
	for _, arquivo := range leitor.File {
		nomes = append(nomes, arquivo.Name)
	}
	return nomes
}

// lerParte devolve o conteúdo descomprimido de uma entrada do ZIP. Falha o
// teste se a entrada não existir — nenhum caso deste arquivo espera isso.
func lerParte(t *testing.T, dados []byte, nome string) []byte {
	t.Helper()

	leitor, err := zip.NewReader(bytes.NewReader(dados), int64(len(dados)))
	require.NoError(t, err, "lerParte: ZIP inválido")

	for _, arquivo := range leitor.File {
		if arquivo.Name != nome {
			continue
		}
		leitorParte, err := arquivo.Open()
		require.NoError(t, err, "lerParte: abrir %q", nome)
		defer leitorParte.Close()

		conteudo, err := io.ReadAll(leitorParte)
		require.NoError(t, err, "lerParte: ler %q", nome)
		return conteudo
	}
	t.Fatalf("lerParte: parte %q não encontrada no pacote", nome)
	return nil
}

// lerMetodo devolve o método de compressão declarado para uma entrada.
func lerMetodo(t *testing.T, dados []byte, nome string) uint16 {
	t.Helper()

	leitor, err := zip.NewReader(bytes.NewReader(dados), int64(len(dados)))
	require.NoError(t, err, "lerMetodo: ZIP inválido")

	for _, arquivo := range leitor.File {
		if arquivo.Name == nome {
			return arquivo.Method
		}
	}
	t.Fatalf("lerMetodo: parte %q não encontrada no pacote", nome)
	return 0
}

// extrairTextosWT devolve o texto de cada elemento w:t de document.xml, na
// ordem em que aparecem. Usado tanto para conferir integridade de texto
// quanto para conferir sobrevivência de xml:space="preserve" e caracteres
// fora do BMP.
func extrairTextosWT(t *testing.T, documentoXML []byte) []string {
	t.Helper()

	decodificador := xml.NewDecoder(bytes.NewReader(documentoXML))
	var textos []string
	for {
		tok, err := decodificador.Token()
		if errors.E(err, io.EOF) {
			break
		}
		require.NoError(t, err, "extrairTextosWT: XML inválido")

		inicio, ok := tok.(xml.StartElement)
		if !ok || inicio.Name.Local != "t" {
			continue
		}
		textos = append(textos, lerTextoAteFechar(t, decodificador))
	}
	return textos
}

func lerTextoAteFechar(t *testing.T, decodificador *xml.Decoder) string {
	t.Helper()

	var construido strings.Builder
	for {
		tok, err := decodificador.Token()
		require.NoError(t, err, "lerTextoAteFechar")

		switch v := tok.(type) {
		case xml.CharData:
			construido.Write(v)
		case xml.EndElement:
			return construido.String()
		}
	}
}

// abrirSemPanic chama Abrir capturando qualquer panic e convertendo em falha
// de teste explícita, para que "arquivo corrompido não pode derrubar o
// processo" seja uma asserção, não uma esperança.
func abrirSemPanic(t *testing.T, r io.ReaderAt, tamanho int64) (doc *Documento, err error) {
	t.Helper()
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("Abrir entrou em panic para entrada inválida em vez de devolver erro: %v", rec)
		}
	}()
	return Abrir(r, tamanho)
}

// abrirESalvar é o caminho feliz completo: abre, salva, devolve os bytes de
// saída. Falha o teste (via require) se qualquer passo falhar — é a
// pré-condição dos testes de preservação, não o que eles verificam.
func abrirESalvar(t *testing.T, dados []byte) []byte {
	t.Helper()

	doc, err := Abrir(bytes.NewReader(dados), int64(len(dados)))
	require.NoError(t, err)
	require.NotNil(t, doc)

	var saida bytes.Buffer
	require.NoError(t, doc.Salvar(&saida))
	return saida.Bytes()
}

// ---------------------------------------------------------------------------
// Round-trip dos fixtures reais — o teste que sustenta o ADR 0001
// ---------------------------------------------------------------------------

func TestAbrirSalvarRoundTripFixturesReais(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nome    string
		fixture string
	}{
		{nome: "artigo real gerado pelo LibreOffice, dez partes", fixture: "artigo-real-libreoffice.docx"},
		{nome: "artigo sintético mínimo, cinco partes", fixture: "artigo-desformatado.docx"},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			original := lerFixture(t, caso.fixture)

			doc, err := Abrir(bytes.NewReader(original), int64(len(original)))
			require.NoError(t, err)
			require.NotNil(t, doc)

			var saida bytes.Buffer
			require.NoError(t, doc.Salvar(&saida))

			// Camada 1: lista de partes, na mesma ordem — o que o ADR 0001
			// pede literalmente.
			assert.Equal(t, listarPartes(t, original), listarPartes(t, saida.Bytes()),
				"a lista de partes do ZIP, na mesma ordem, precisa sair igual")

			// Camada 2: conteúdo de cada parte — diagnóstico direto de qual
			// parte especificamente divergiu.
			for _, parte := range listarPartes(t, original) {
				assert.Equal(t, lerParte(t, original, parte), lerParte(t, saida.Bytes(), parte),
					"parte %q divergiu depois do round-trip", parte)
			}

			// Camada 3: o ZIP inteiro, byte a byte. Cobre também o que as
			// camadas acima não enxergam sozinhas: timestamp por entrada,
			// método de compressão, metadados do cabeçalho local e da
			// central directory. Sem mutação nenhuma, este é o alvo: se
			// falhar, o pacote não está saindo "byte a byte igual" como o
			// ADR promete, mesmo que o conteúdo pareça igual.
			assert.Equal(t, original, saida.Bytes(),
				"sem nenhuma mutação, o pacote inteiro tem que sair byte a byte idêntico ao original")
		})
	}
}

// ---------------------------------------------------------------------------
// Integridade de texto — a outra invariante do ADR 0001
// ---------------------------------------------------------------------------

func TestIntegridadeDeTextoWTFixturesReais(t *testing.T) {
	t.Parallel()

	for _, fixture := range []string{"artigo-real-libreoffice.docx", "artigo-desformatado.docx"} {
		t.Run(fixture, func(t *testing.T) {
			t.Parallel()

			original := lerFixture(t, fixture)
			textosAntes := extrairTextosWT(t, lerParte(t, original, "word/document.xml"))
			require.NotEmpty(t, textosAntes, "fixture sem nenhum w:t não prova integridade de texto")

			saida := abrirESalvar(t, original)
			textosDepois := extrairTextosWT(t, lerParte(t, saida, "word/document.xml"))

			assert.Equal(t, textosAntes, textosDepois,
				"a sequência de runas de todo w:t precisa sair idêntica, sem nenhuma mutação declarada")
		})
	}
}

// ---------------------------------------------------------------------------
// Armadilhas de namespace, encoding e forma do XML
// ---------------------------------------------------------------------------

func TestSalvarPreservaNamespacePrefixoNaoPadrao(t *testing.T) {
	t.Parallel()

	// O encoding/xml da stdlib reescreve declarações de namespace ao
	// serializar, e é capaz de trocar o prefixo por um gerado. Um prefixo
	// incomum (não "w") é o jeito mais direto de flagrar isso: se o
	// round-trip parsear e remarshalizar em vez de preservar bytes crus, o
	// prefixo muda.
	documentoXML := []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<doc:document xmlns:doc="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><doc:body><doc:p><doc:r><doc:t>prefixo incomum sobrevive</doc:t></doc:r></doc:p></doc:body></doc:document>`)

	saida := abrirESalvar(t, docxSintetico(t, documentoXML))

	assert.Equal(t, documentoXML, lerParte(t, saida, "word/document.xml"))
}

func TestSalvarPreservaDeclaracaoXMLeBOM(t *testing.T) {
	t.Parallel()

	bom := []byte{0xEF, 0xBB, 0xBF}
	base := []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>documento com declaração</w:t></w:r></w:p></w:body></w:document>`)

	casos := []struct {
		nome         string
		documentoXML []byte
	}{
		{nome: "sem BOM", documentoXML: base},
		{nome: "com BOM", documentoXML: append(append([]byte{}, bom...), base...)},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			saida := abrirESalvar(t, docxSintetico(t, caso.documentoXML))
			assert.Equal(t, caso.documentoXML, lerParte(t, saida, "word/document.xml"),
				"declaração XML e BOM têm que sair exatamente como entraram")
		})
	}
}

func TestSalvarPreservaXMLSpacePreserve(t *testing.T) {
	t.Parallel()

	documentoXML := []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t xml:space="preserve">  espaço nas duas pontas  </w:t></w:r></w:p></w:body></w:document>`)

	saida := abrirESalvar(t, docxSintetico(t, documentoXML))

	assert.Equal(t, documentoXML, lerParte(t, saida, "word/document.xml"))

	textos := extrairTextosWT(t, lerParte(t, saida, "word/document.xml"))
	require.Len(t, textos, 1)
	assert.Equal(t, "  espaço nas duas pontas  ", textos[0],
		"perder xml:space=\"preserve\" come o espaço do texto do usuário")
}

func TestSalvarPreservaCaractereForaDoBMP(t *testing.T) {
	t.Parallel()

	// 😀 é U+1F600, fora do BMP e par substituto em UTF-16 (o modelo interno
	// do Word). 你好世界 é CJK. Nenhum dos dois é contagem de rune = contagem
	// de byte = contagem de unidade UTF-16.
	textoOriginal := "emoji 😀 e CJK 你好世界"
	var escapado bytes.Buffer
	require.NoError(t, xml.EscapeText(&escapado, []byte(textoOriginal)))
	documentoXML := documentoXMLMinimo(escapado.String())

	saida := abrirESalvar(t, docxSintetico(t, documentoXML))

	assert.Equal(t, documentoXML, lerParte(t, saida, "word/document.xml"))

	textos := extrairTextosWT(t, lerParte(t, saida, "word/document.xml"))
	require.Len(t, textos, 1)
	assert.Equal(t, []rune(textoOriginal), []rune(textos[0]),
		"caractere fora do BMP sobrevive: comparação em runas, não em bytes nem em unidades UTF-16")
}

func TestSalvarNaoNormalizaAcentuacaoNFD(t *testing.T) {
	t.Parallel()

	// "café" com o "é" decomposto: 'e' (U+0065) seguido do acento agudo
	// combinante (U+0301), em vez do code point precomposto 'é' (U+00E9).
	// Misturado com "açúcar", que fica precomposto — o caso real é mesmo
	// texto com as duas formas convivendo.
	textoNFD := "café com açúcar em NFD"
	require.NotEqual(t, "café com açúcar em NFD", textoNFD,
		"pré-condição do caso: os bytes precisam ser realmente NFD, não a forma precomposta")

	documentoXML := documentoXMLMinimo(textoNFD)

	saida := abrirESalvar(t, docxSintetico(t, documentoXML))

	assert.Equal(t, documentoXML, lerParte(t, saida, "word/document.xml"))

	textos := extrairTextosWT(t, lerParte(t, saida, "word/document.xml"))
	require.Len(t, textos, 1)
	assert.Equal(t, textoNFD, textos[0],
		"normalizar NFD para NFC (ou vice-versa) altera os bytes do texto do usuário sem que ele tenha pedido")
}

func TestSalvarPreservaElementoSelfClosingEParAbertoFechado(t *testing.T) {
	t.Parallel()

	// <w:b/> e <w:b></w:b> são equivalentes para o Word, mas bytes
	// diferentes. O Marshal do encoding/xml da stdlib nunca emite a forma
	// self-closing: reconstruir o nó em vez de preservar bytes crus troca a
	// forma silenciosamente.
	documentoXML := []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:pPr><w:rPr></w:rPr></w:pPr><w:r><w:rPr><w:b/><w:i></w:i></w:rPr><w:t>negrito self-closing, itálico em par</w:t></w:r></w:p></w:body></w:document>`)

	saida := abrirESalvar(t, docxSintetico(t, documentoXML))

	conteudo := lerParte(t, saida, "word/document.xml")
	assert.Equal(t, documentoXML, conteudo)
	assert.Contains(t, string(conteudo), "<w:b/>", "forma self-closing precisa sobreviver exatamente como veio")
	assert.Contains(t, string(conteudo), "<w:i></w:i>", "par aberto/fechado não pode virar self-closing")
	assert.Contains(t, string(conteudo), "<w:rPr></w:rPr>",
		"w:rPr vazio explícito no original não pode ganhar nem perder forma")
}

// ---------------------------------------------------------------------------
// Pacote ZIP: parte desconhecida e método de compressão misto
// ---------------------------------------------------------------------------

func TestSalvarPreservaParteDesconhecida(t *testing.T) {
	t.Parallel()

	// Simula um embed binário (ex.: imagem) que o ooxml não sabe nem precisa
	// interpretar: só precisa sair byte a byte igual. O nome termina em
	// .png, não em .xml — nada aqui pode tentar decodificar isto como XML.
	binario := append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A},
		[]byte("conteúdo binário opaco, não é XML")...)

	entradas := append(partesBaseDocx(documentoXMLMinimo("com parte desconhecida")), entradaZip{
		nome: "word/media/image1.png", conteudo: binario, metodo: zip.Deflate,
	})
	dados := montarZip(t, entradas)

	saida := abrirESalvar(t, dados)

	assert.Equal(t, binario, lerParte(t, saida, "word/media/image1.png"))
	assert.Equal(t, listarPartes(t, dados), listarPartes(t, saida))
	assert.Equal(t, dados, saida, "nenhuma parte foi mutada: o pacote inteiro tem que sair idêntico")
}

func TestSalvarPreservaMetodoDeCompressaoMisto(t *testing.T) {
	t.Parallel()

	base := partesBaseDocx(documentoXMLMinimo("método misto"))
	base[0].metodo = zip.Store // [Content_Types].xml sem compressão
	base[2].metodo = zip.Deflate

	dados := montarZip(t, base)
	saida := abrirESalvar(t, dados)

	assert.Equal(t, zip.Store, lerMetodo(t, saida, "[Content_Types].xml"),
		"método de compressão por entrada faz parte dos bytes; trocar STORE por DEFLATE produz ZIP diferente")
	assert.Equal(t, zip.Deflate, lerMetodo(t, saida, "word/document.xml"))
	assert.Equal(t, dados, saida)
}

// ---------------------------------------------------------------------------
// Entrada inválida: erro claro, nunca panic
// ---------------------------------------------------------------------------

func TestAbrirRejeitaArquivoCorrompidoOuInvalido(t *testing.T) {
	t.Parallel()

	docxValido := docxSintetico(t, documentoXMLMinimo("válido"))

	semPartesObrigatorias := montarZip(t, []entradaZip{
		{nome: "readme.txt", conteudo: []byte("isto é um zip qualquer, não um docx"), metodo: zip.Deflate},
	})

	casos := []struct {
		nome  string
		dados []byte
	}{
		{nome: "vazio", dados: []byte{}},
		{nome: "bytes aleatórios, não é zip", dados: []byte("isto não é um zip nem um docx, só texto solto")},
		{nome: "zip truncado no meio", dados: docxValido[:len(docxValido)/2]},
		{nome: "zip válido mas sem Content_Types nem document.xml", dados: semPartesObrigatorias},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			doc, err := abrirSemPanic(t, bytes.NewReader(caso.dados), int64(len(caso.dados)))

			require.Error(t, err)
			assert.Nil(t, doc)

			var validacao *errors.ErroValidacao
			assert.True(t, errors.Como(err, &validacao),
				"esperava *errors.ErroValidacao (culpa é do arquivo, HTTP 400), obteve %T (%v)", err, err)
		})
	}
}

// TestAbrirNaoResolveEntidadeExternaXXE prova que um DOCTYPE com ENTITY
// SYSTEM apontando para um arquivo local não vaza o conteúdo desse arquivo
// para o texto do documento. Rejeitar o pacote é uma resposta aceitável;
// resolver a entidade não é, em nenhuma das duas saídas possíveis (Abrir ou
// Salvar).
func TestAbrirNaoResolveEntidadeExternaXXE(t *testing.T) {
	t.Parallel()

	segredo := "SEGREDO-XXE-nao-pode-vazar-3f9a"
	arquivoSegredo := filepath.Join(t.TempDir(), "segredo.txt")
	require.NoError(t, os.WriteFile(arquivoSegredo, []byte(segredo), 0o600))

	documentoXML := []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<!DOCTYPE w:document [<!ENTITY xxe SYSTEM "file://%s">]>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>&xxe;</w:t></w:r></w:p></w:body></w:document>`, arquivoSegredo))

	dados := docxSintetico(t, documentoXML)

	doc, err := abrirSemPanic(t, bytes.NewReader(dados), int64(len(dados)))
	if err != nil {
		assert.NotContains(t, err.Error(), segredo, "o conteúdo do arquivo local não pode aparecer nem na mensagem de erro")
		return
	}
	require.NotNil(t, doc)

	var saida bytes.Buffer
	err = doc.Salvar(&saida)
	if err != nil {
		assert.NotContains(t, err.Error(), segredo)
		return
	}
	assert.NotContains(t, saida.String(), segredo,
		"entidade externa foi resolvida: o conteúdo do arquivo local vazou para dentro do documento salvo")
}

// ---------------------------------------------------------------------------
// Classificação de erro: I/O de infraestrutura não é culpa do cliente
// ---------------------------------------------------------------------------

var errFalhaLeituraSimulada = errors.Novo("falha simulada de armazenamento ao ler o pacote")

// leitorComFalha implementa io.ReaderAt e falha a partir de um certo offset,
// simulando um storage que cai no meio da leitura — não um arquivo inválido.
type leitorComFalha struct {
	dados     []byte
	falhaApos int64
}

func (l *leitorComFalha) ReadAt(p []byte, off int64) (int, error) {
	if off+int64(len(p)) > l.falhaApos {
		return 0, errFalhaLeituraSimulada
	}
	return copy(p, l.dados[off:off+int64(len(p))]), nil
}

func TestAbrirDevolveErroAplicacaoQuandoLeituraFalha(t *testing.T) {
	t.Parallel()

	dados := lerFixture(t, "artigo-desformatado.docx")
	leitor := &leitorComFalha{dados: dados, falhaApos: int64(len(dados)) / 2}

	doc, err := abrirSemPanic(t, leitor, int64(len(dados)))

	require.Error(t, err)
	assert.Nil(t, doc)

	var aplicacao *errors.ErroAplicacao
	assert.True(t, errors.Como(err, &aplicacao),
		"falha do meio de armazenamento é *errors.ErroAplicacao (HTTP 500), não culpa do cliente; obteve %T (%v)", err, err)

	var validacao *errors.ErroValidacao
	assert.False(t, errors.Como(err, &validacao),
		"erro de I/O não pode virar *errors.ErroValidacao: errors.Envolver não reclassifica, e isso culparia o cliente por uma falha do servidor")
}

var errFalhaEscritaSimulada = errors.Novo("falha simulada de armazenamento ao gravar o pacote")

// escritorComFalha implementa io.Writer e falha depois de aceitar
// falhaApos bytes, simulando um destino (storage, resposta HTTP) que cai no
// meio da gravação.
type escritorComFalha struct {
	falhaApos int
	escrito   int
}

func (e *escritorComFalha) Write(p []byte) (int, error) {
	restante := e.falhaApos - e.escrito
	if restante <= 0 {
		return 0, errFalhaEscritaSimulada
	}
	n := len(p)
	if n > restante {
		n = restante
	}
	e.escrito += n
	if n < len(p) {
		return n, errFalhaEscritaSimulada
	}
	return n, nil
}

func TestSalvarDevolveErroAplicacaoQuandoEscritaFalha(t *testing.T) {
	t.Parallel()

	dados := lerFixture(t, "artigo-desformatado.docx")
	doc, err := Abrir(bytes.NewReader(dados), int64(len(dados)))
	require.NoError(t, err)
	require.NotNil(t, doc)

	escritor := &escritorComFalha{falhaApos: 50}

	var erroSalvar error
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				t.Fatalf("Salvar entrou em panic quando a escrita falhou: %v", rec)
			}
		}()
		erroSalvar = doc.Salvar(escritor)
	}()

	require.Error(t, erroSalvar)

	var aplicacao *errors.ErroAplicacao
	assert.True(t, errors.Como(erroSalvar, &aplicacao),
		"falha ao gravar no destino é *errors.ErroAplicacao (HTTP 500); obteve %T (%v)", erroSalvar, erroSalvar)

	var validacao *errors.ErroValidacao
	assert.False(t, errors.Como(erroSalvar, &validacao),
		"falha de escrita não é culpa do cliente: não pode virar *errors.ErroValidacao")
}
