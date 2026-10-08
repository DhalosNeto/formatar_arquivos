package ooxml

import (
	"bytes"
	"encoding/xml"
	errosPadrao "errors"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/daniel-halos/formatador/internal/infra/errors"
)

const (
	limiteXMLPagina         = 32 << 20
	profundidadeXMLPagina   = 256
	limiteNosPagina         = 100000
	limiteAtributosPagina   = 128
	limiteDeclaracoesPagina = 1024
	namespaceWord           = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
)

type noXMLPagina struct {
	nome                  xml.Name
	qname                 string
	inicio, fim           int
	fechoInicio, fechoFim int
	vazio, historico      bool
	ns                    map[string]string
	pai                   *noXMLPagina
	filhos                []*noXMLPagina
	atributos             []atributoXMLPagina
}

type atributoXMLPagina struct {
	qname, prefixo, local, namespace, valor string
	valorInicio, valorFim                   int
}

type edicaoXMLPagina struct {
	inicio, fim int
	texto       []byte
}

func erroXMLPagina() error {
	return errors.NovoErroValidacao("arquivo", "word/document.xml inválido ou não suportado")
}

func erroAplicarPagina() error {
	return errors.NovoErroAplicacao("não foi possível aplicar as medidas de página")
}

func aplicarPaginaXML(dados []byte, medidas medidasPagina) ([]byte, bool, error) {
	if len(dados) == 0 || len(dados) > limiteXMLPagina || !utf8.Valid(dados) {
		return nil, false, erroXMLPagina()
	}
	if !declaracaoUTF8(dados) {
		return nil, false, erroXMLPagina()
	}
	raiz, nos, err := analisarXMLPagina(dados)
	if err != nil || raiz == nil || raiz.nome.Space != namespaceWord || raiz.nome.Local != "document" {
		return nil, false, erroXMLPagina()
	}
	corpos := filhosW(raiz, "body")
	if len(corpos) != 1 {
		return nil, false, erroXMLPagina()
	}
	body := corpos[0]
	for i, filho := range body.filhos {
		if filho.nome == (xml.Name{Space: namespaceWord, Local: "sectPr"}) && i != len(body.filhos)-1 {
			return nil, false, erroXMLPagina()
		}
	}
	secoes := make([]*noXMLPagina, 0)
	for _, no := range nos {
		if no.nome.Space != namespaceWord || no.nome.Local != "sectPr" || no.historico {
			continue
		}
		if !caminhoSecaoSuportado(no, raiz, body) {
			return nil, false, erroXMLPagina()
		}
		secoes = append(secoes, no)
	}
	if err := validarSecoesPagina(secoes); err != nil {
		return nil, false, err
	}
	edicoes := make([]edicaoXMLPagina, 0, len(secoes)*3+1)
	for _, secao := range secoes {
		edicoesSecao, err := prepararSecaoPagina(dados, secao, medidas)
		if err != nil {
			return nil, false, err
		}
		edicoes = append(edicoes, edicoesSecao...)
	}
	if !temSecaoFinal(body, secoes) {
		edicao, err := criarSecaoFinal(dados, body, medidas)
		if err != nil {
			return nil, false, err
		}
		edicoes = append(edicoes, edicao)
	}
	if len(edicoes) == 0 {
		return dados, false, nil
	}
	bytesSaida, err := aplicarEdicoesPagina(dados, edicoes)
	if err != nil {
		return nil, false, erroAplicarPagina()
	}
	if len(bytesSaida) > limiteXMLPagina {
		return nil, false, erroXMLPagina()
	}
	if _, _, err := analisarXMLPagina(bytesSaida); err != nil {
		return nil, false, erroXMLPagina()
	}
	return bytesSaida, !bytes.Equal(dados, bytesSaida), nil
}

func declaracaoUTF8(dados []byte) bool {
	indice := 0
	if bytes.HasPrefix(dados, []byte{0xef, 0xbb, 0xbf}) {
		indice = 3
	}
	resto := dados[indice:]
	if !bytes.HasPrefix(resto, []byte("<?xml")) {
		return true
	}
	fim := bytes.Index(resto, []byte("?>"))
	if fim < 0 || fim > 512 {
		return false
	}
	declaracao := string(resto[:fim])
	marcador := "encoding"
	posicao := strings.Index(declaracao, marcador)
	if posicao < 0 {
		return true
	}
	declaracao = declaracao[posicao+len(marcador):]
	declaracao = strings.TrimLeft(declaracao, " \t\r\n")
	if !strings.HasPrefix(declaracao, "=") {
		return false
	}
	declaracao = strings.TrimLeft(declaracao[1:], " \t\r\n")
	if len(declaracao) < 2 || (declaracao[0] != '\'' && declaracao[0] != '"') {
		return false
	}
	fimValor := strings.IndexByte(declaracao[1:], declaracao[0])
	if fimValor < 0 {
		return false
	}
	return strings.EqualFold(declaracao[1:1+fimValor], "UTF-8")
}

func analisarXMLPagina(dados []byte) (*noXMLPagina, []*noXMLPagina, error) {
	decodificador := xml.NewDecoder(bytes.NewReader(dados))
	var pilha []*noXMLPagina
	var raiz *noXMLPagina
	var nos []*noXMLPagina
	declaracoes := 0
	for {
		token, err := decodificador.Token()
		if errosPadrao.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		fim := int(decodificador.InputOffset())
		switch valor := token.(type) {
		case xml.CharData:
			if len(pilha) == 0 && strings.TrimSpace(strings.TrimPrefix(string(valor), "\ufeff")) != "" {
				return nil, nil, io.ErrUnexpectedEOF
			}
		case xml.Directive:
			return nil, nil, io.ErrUnexpectedEOF
		case xml.StartElement:
			if len(nos) >= limiteNosPagina || len(valor.Attr) > limiteAtributosPagina {
				return nil, nil, io.ErrUnexpectedEOF
			}
			for _, atributo := range valor.Attr {
				if atributo.Name.Space == "xmlns" || (atributo.Name.Space == "" && atributo.Name.Local == "xmlns") {
					declaracoes++
				}
			}
			if declaracoes > limiteDeclaracoesPagina {
				return nil, nil, io.ErrUnexpectedEOF
			}
			if len(pilha) >= profundidadeXMLPagina {
				return nil, nil, io.ErrUnexpectedEOF
			}
			inicio, ok := inicioTagXML(dados, fim)
			if !ok {
				return nil, nil, io.ErrUnexpectedEOF
			}
			var pai *noXMLPagina
			if len(pilha) > 0 {
				pai = pilha[len(pilha)-1]
			}
			no, ok := lerNoXMLPagina(dados, inicio, fim, valor, pai)
			if !ok {
				return nil, nil, io.ErrUnexpectedEOF
			}
			if pai != nil {
				pai.filhos = append(pai.filhos, no)
			} else if raiz != nil {
				return nil, nil, io.ErrUnexpectedEOF
			} else {
				raiz = no
			}
			nos = append(nos, no)
			pilha = append(pilha, no)
		case xml.EndElement:
			if len(pilha) == 0 {
				return nil, nil, io.ErrUnexpectedEOF
			}
			no := pilha[len(pilha)-1]
			if no.vazio {
				no.fechoInicio, no.fechoFim = no.fim, no.fim
			} else {
				inicio, ok := inicioTagXML(dados, fim)
				if !ok || no.nome != valor.Name || strings.TrimSpace(string(dados[inicio+2:fim-1])) != no.qname {
					return nil, nil, io.ErrUnexpectedEOF
				}
				no.fechoInicio, no.fechoFim = inicio, fim
			}
			pilha = pilha[:len(pilha)-1]
		}
	}
	if raiz == nil || len(pilha) != 0 {
		return nil, nil, io.ErrUnexpectedEOF
	}
	return raiz, nos, nil
}

func inicioTagXML(dados []byte, fim int) (int, bool) {
	if fim <= 0 || fim > len(dados) || dados[fim-1] != '>' {
		return 0, false
	}
	indice := bytes.LastIndexByte(dados[:fim], '<')
	return indice, indice >= 0
}

func lerNoXMLPagina(dados []byte, inicio, fim int, token xml.StartElement, pai *noXMLPagina) (*noXMLPagina, bool) {
	texto := dados[inicio+1 : fim-1]
	indice := 0
	pularEspacosXML(texto, &indice)
	iniNome := indice
	for indice < len(texto) && !espacoXML(texto[indice]) && texto[indice] != '/' {
		indice++
	}
	if iniNome == indice {
		return nil, false
	}
	qname := string(texto[iniNome:indice])
	no := &noXMLPagina{nome: token.Name, qname: qname, inicio: inicio, fim: fim, pai: pai}
	if pai != nil {
		no.ns = pai.ns
		no.historico = pai.historico || (token.Name.Space == namespaceWord && (token.Name.Local == "sectPrChange" || token.Name.Local == "pPrChange"))
	} else {
		no.ns = map[string]string{"xml": "http://www.w3.org/XML/1998/namespace"}
	}
	// Escopos são imutáveis: copiar somente quando este elemento declara prefixos.
	for _, atributo := range token.Attr {
		if pai != nil && (atributo.Name.Space == "xmlns" || (atributo.Name.Space == "" && atributo.Name.Local == "xmlns")) {
			copia := make(map[string]string, len(no.ns)+1)
			for prefixo, uri := range no.ns {
				copia[prefixo] = uri
			}
			no.ns = copia
			break
		}
	}
	for _, atributo := range token.Attr {
		if atributo.Name.Space == "xmlns" {
			if atributo.Name.Local == "xmlns" || atributo.Value == "" ||
				(atributo.Name.Local == "xml" && atributo.Value != no.ns["xml"]) {
				return nil, false
			}
			no.ns[atributo.Name.Local] = atributo.Value
		} else if atributo.Name.Space == "" && atributo.Name.Local == "xmlns" {
			no.ns[""] = atributo.Value
		}
	}
	prefixoNo, localNo := separarQName(qname)
	if strings.Count(qname, ":") > 1 || localNo == "" ||
		(prefixoNo != "" && no.ns[prefixoNo] == "") || no.ns[prefixoNo] != token.Name.Space {
		return nil, false
	}
	atributosDecodificados := make(map[string]xml.Attr)
	for _, atributo := range token.Attr {
		chave := atributo.Name.Space + "\x00" + atributo.Name.Local
		if _, duplicado := atributosDecodificados[chave]; duplicado {
			return nil, false
		}
		atributosDecodificados[chave] = atributo
	}
	for indice < len(texto) {
		pularEspacosXML(texto, &indice)
		if indice >= len(texto) || texto[indice] == '/' {
			break
		}
		iniAtributo := indice
		for indice < len(texto) && !espacoXML(texto[indice]) && texto[indice] != '=' && texto[indice] != '/' {
			indice++
		}
		if iniAtributo == indice {
			return nil, false
		}
		nomeAtributo := string(texto[iniAtributo:indice])
		pularEspacosXML(texto, &indice)
		if indice >= len(texto) || texto[indice] != '=' {
			return nil, false
		}
		indice++
		pularEspacosXML(texto, &indice)
		if indice >= len(texto) || (texto[indice] != '\'' && texto[indice] != '"') {
			return nil, false
		}
		aspas := texto[indice]
		indice++
		iniValor := indice
		for indice < len(texto) && texto[indice] != aspas {
			indice++
		}
		if indice >= len(texto) {
			return nil, false
		}
		fimValor := indice
		indice++
		prefixo, local := separarQName(nomeAtributo)
		if strings.Count(nomeAtributo, ":") > 1 || local == "" ||
			(prefixo != "" && prefixo != "xmlns" && no.ns[prefixo] == "") {
			return nil, false
		}
		chave := "\x00" + local
		namespace := ""
		if prefixo == "xmlns" || (prefixo == "" && local == "xmlns") {
			if prefixo == "xmlns" {
				chave = "xmlns\x00" + local
			} else {
				chave = "\x00xmlns"
			}
			namespace = "xmlns"
		} else if prefixo != "" {
			namespace = no.ns[prefixo]
			chave = namespace + "\x00" + local
		}
		decodificado, existe := atributosDecodificados[chave]
		if !existe {
			// encoding/xml representa xmlns default com Name.Space vazio.
			if prefixo == "" && local == "xmlns" {
				decodificado, existe = atributosDecodificados["\x00xmlns"]
			}
		}
		valor := string(texto[iniValor:fimValor])
		if existe {
			valor = decodificado.Value
			if decodificado.Name.Space != "xmlns" && decodificado.Name.Local != "xmlns" {
				namespace = decodificado.Name.Space
			}
		}
		no.atributos = append(no.atributos, atributoXMLPagina{
			qname: nomeAtributo, prefixo: prefixo, local: local, namespace: namespace,
			valor: valor, valorInicio: inicio + 1 + iniValor, valorFim: inicio + 1 + fimValor,
		})
	}
	if len(no.atributos) != len(token.Attr) || !atributosUnicosPagina(no) {
		return nil, false
	}
	no.vazio = textoTagVazia(texto)
	return no, true
}

func separarQName(nome string) (string, string) {
	if indice := strings.IndexByte(nome, ':'); indice >= 0 {
		return nome[:indice], nome[indice+1:]
	}
	return "", nome
}

func espacoXML(b byte) bool { return b == ' ' || b == '\t' || b == '\r' || b == '\n' }

func pularEspacosXML(texto []byte, indice *int) {
	for *indice < len(texto) && espacoXML(texto[*indice]) {
		*indice++
	}
}

func textoTagVazia(texto []byte) bool {
	indice := len(texto) - 1
	for indice >= 0 && espacoXML(texto[indice]) {
		indice--
	}
	return indice >= 0 && texto[indice] == '/'
}

func filhosW(no *noXMLPagina, local string) []*noXMLPagina {
	var encontrados []*noXMLPagina
	for _, filho := range no.filhos {
		if filho.nome.Space == namespaceWord && filho.nome.Local == local {
			encontrados = append(encontrados, filho)
		}
	}
	return encontrados
}

func caminhoSecaoSuportado(secao, raiz, body *noXMLPagina) bool {
	if secao.pai == body {
		return body.pai == raiz
	}
	paragrafoPropriedades := secao.pai
	paragrafo := (*noXMLPagina)(nil)
	if paragrafoPropriedades != nil && paragrafoPropriedades.nome.Space == namespaceWord && paragrafoPropriedades.nome.Local == "pPr" {
		paragrafo = paragrafoPropriedades.pai
	}
	return paragrafo != nil && paragrafo.nome.Space == namespaceWord && paragrafo.nome.Local == "p" && paragrafo.pai == body && body.pai == raiz
}

func validarSecoesPagina(secoes []*noXMLPagina) error {
	pais := make(map[*noXMLPagina]bool)
	for _, secao := range secoes {
		if pais[secao.pai] {
			return erroXMLPagina()
		}
		pais[secao.pai] = true
		ultimoRank := -1
		quantidadeTamanho, quantidadeMargem := 0, 0
		for _, filho := range secao.filhos {
			if !filhoSectPrConhecido(filho) {
				continue
			}
			rank, conhecido := rankSectPrPagina(filho.nome.Local)
			if conhecido {
				if rank < ultimoRank {
					return erroXMLPagina()
				}
				ultimoRank = rank
			}
			switch filho.nome.Local {
			case "pgSz":
				quantidadeTamanho++
				if !atributosUnicosPagina(filho) {
					return erroXMLPagina()
				}
			case "pgMar":
				quantidadeMargem++
				if !atributosUnicosPagina(filho) {
					return erroXMLPagina()
				}
			case "cols":
				if colunasComLargura(filho) {
					return erroXMLPagina()
				}
			}
		}
		if quantidadeTamanho > 1 || quantidadeMargem > 1 {
			return erroXMLPagina()
		}
	}
	return nil
}

// Ordem dos filhos de CT_SectPr; referências de cabeçalho/rodapé são uma choice.
func rankSectPrPagina(local string) (int, bool) {
	switch local {
	case "headerReference", "footerReference":
		return 0, true
	case "footnotePr":
		return 2, true
	case "endnotePr":
		return 3, true
	case "type":
		return 4, true
	case "pgSz":
		return 5, true
	case "pgMar":
		return 6, true
	case "paperSrc":
		return 7, true
	case "pgBorders":
		return 8, true
	case "lnNumType":
		return 9, true
	case "pgNumType":
		return 10, true
	case "cols":
		return 11, true
	case "formProt":
		return 12, true
	case "vAlign":
		return 13, true
	case "noEndnote":
		return 14, true
	case "titlePg":
		return 15, true
	case "textDirection":
		return 16, true
	case "bidi":
		return 17, true
	case "rtlGutter":
		return 18, true
	case "docGrid":
		return 19, true
	case "printerSettings":
		return 20, true
	case "footnoteColumns":
		return 21, true
	case "sectPrChange":
		return 22, true
	default:
		return 0, false
	}
}

func filhoSectPrConhecido(no *noXMLPagina) bool {
	return no.nome.Space == namespaceWord ||
		(no.nome.Space == "http://schemas.microsoft.com/office/word/2012/wordml" && no.nome.Local == "footnoteColumns")
}

func atributosUnicosPagina(no *noXMLPagina) bool {
	vistos := make(map[string]bool, len(no.atributos))
	for _, atributo := range no.atributos {
		chave := atributo.namespace + "\x00" + atributo.local
		if vistos[chave] {
			return false
		}
		vistos[chave] = true
	}
	return true
}

func colunasComLargura(colunas *noXMLPagina) bool {
	for _, no := range descendentesXML(colunas) {
		if no.nome.Space != namespaceWord || no.nome.Local != "col" {
			continue
		}
		for _, atributo := range no.atributos {
			if atributo.namespace == namespaceWord && atributo.local == "w" {
				return true
			}
		}
	}
	return false
}

func descendentesXML(no *noXMLPagina) []*noXMLPagina {
	resultado := make([]*noXMLPagina, 0)
	for _, filho := range no.filhos {
		resultado = append(resultado, filho)
		resultado = append(resultado, descendentesXML(filho)...)
	}
	return resultado
}

func temSecaoFinal(body *noXMLPagina, secoes []*noXMLPagina) bool {
	for _, secao := range secoes {
		if secao.pai == body {
			return true
		}
	}
	return false
}

func prepararSecaoPagina(dados []byte, secao *noXMLPagina, medidas medidasPagina) ([]edicaoXMLPagina, error) {
	tamanhos, margens := filhosW(secao, "pgSz"), filhosW(secao, "pgMar")
	var tamanho, margem *noXMLPagina
	if len(tamanhos) == 1 {
		tamanho = tamanhos[0]
	}
	if len(margens) == 1 {
		margem = margens[0]
	}
	medidaMedianiz := medidas.medianiz
	if margem != nil {
		for _, nome := range []string{"header", "footer", "gutter"} {
			if existente := atributoPaginaW(margem, nome); existente != nil {
				valor, err := strconv.Atoi(existente.valor)
				if err != nil || valor < 0 || valor > limitePaginaTwips {
					return nil, erroXMLPagina()
				}
			}
		}
		if existente := atributoPaginaW(margem, "gutter"); existente != nil {
			valor, err := strconv.Atoi(existente.valor)
			if err != nil || valor < 0 || valor > limitePaginaTwips {
				return nil, erroXMLPagina()
			}
			medidaMedianiz = valor
		}
	}
	if int64(medidas.esquerda)+int64(medidas.direita)+int64(medidaMedianiz) >= int64(medidas.largura) {
		return nil, erroParametrosPagina()
	}
	edicoes := make([]edicaoXMLPagina, 0, 3)
	if tamanho != nil {
		edicoes = append(edicoes, edicaoXMLPagina{inicio: tamanho.inicio, fim: tamanho.fim, texto: nil})
		texto, err := editarTagPagina(dados, tamanho, map[string]string{
			"w": strconv.Itoa(medidas.largura), "h": strconv.Itoa(medidas.altura), "orient": orientacaoPagina(medidas),
		})
		if err != nil {
			return nil, err
		}
		edicoes[len(edicoes)-1].texto = texto
	}
	if margem != nil {
		valores := map[string]string{
			"top": strconv.Itoa(medidas.superior), "bottom": strconv.Itoa(medidas.inferior),
			"left": strconv.Itoa(medidas.esquerda), "right": strconv.Itoa(medidas.direita),
		}
		if atributoPaginaW(margem, "header") == nil {
			valores["header"] = strconv.Itoa(medidas.cabecalho)
		}
		if atributoPaginaW(margem, "footer") == nil {
			valores["footer"] = strconv.Itoa(medidas.rodape)
		}
		if atributoPaginaW(margem, "gutter") == nil {
			valores["gutter"] = strconv.Itoa(medidas.medianiz)
		}
		texto, err := editarTagPagina(dados, margem, valores)
		if err != nil {
			return nil, err
		}
		edicoes = append(edicoes, edicaoXMLPagina{inicio: margem.inicio, fim: margem.fim, texto: texto})
	}
	var novos []string
	if tamanho == nil {
		novos = append(novos, elementoPagina(dados, secao, "pgSz", map[string]string{"w": strconv.Itoa(medidas.largura), "h": strconv.Itoa(medidas.altura), "orient": orientacaoPagina(medidas)}))
	}
	if margem == nil {
		novos = append(novos, elementoPagina(dados, secao, "pgMar", map[string]string{"top": strconv.Itoa(medidas.superior), "right": strconv.Itoa(medidas.direita), "bottom": strconv.Itoa(medidas.inferior), "left": strconv.Itoa(medidas.esquerda), "header": strconv.Itoa(medidas.cabecalho), "footer": strconv.Itoa(medidas.rodape), "gutter": strconv.Itoa(medidas.medianiz)}))
	}
	if len(novos) > 0 {
		if secao.vazio {
			agora := dados[secao.inicio:secao.fim]
			abertura := bytes.TrimSuffix(agora, []byte("/>"))
			if bytes.Equal(agora, abertura) {
				return nil, erroXMLPagina()
			}
			fechamento := []byte("</" + secao.qname + ">")
			conteudo := append(append(append([]byte{}, abertura...), '>'), []byte(strings.Join(novos, ""))...)
			conteudo = append(conteudo, fechamento...)
			edicoes = append(edicoes, edicaoXMLPagina{inicio: secao.inicio, fim: secao.fim, texto: conteudo})
		} else {
			rank := 5
			if tamanho != nil {
				rank = 6
			}
			indice := pontoInsercaoFilho(secao, rank)
			edicoes = append(edicoes, edicaoXMLPagina{inicio: indice, fim: indice, texto: []byte(strings.Join(novos, ""))})
		}
	}
	return edicoes, nil
}

func atributoPaginaW(no *noXMLPagina, local string) *atributoXMLPagina {
	for i := range no.atributos {
		if no.atributos[i].namespace == namespaceWord && no.atributos[i].local == local {
			return &no.atributos[i]
		}
	}
	return nil
}

func orientacaoPagina(medidas medidasPagina) string {
	if medidas.largura > medidas.altura {
		return "landscape"
	}
	return "portrait"
}

func editarTagPagina(dados []byte, no *noXMLPagina, valores map[string]string) ([]byte, error) {
	prefixo, declaracao := prefixoAtributoW(no)
	substituicoes := make([]edicaoXMLPagina, 0, len(valores)+1)
	var faltantes []string
	locais := make([]string, 0, len(valores))
	for local := range valores {
		locais = append(locais, local)
	}
	sort.Strings(locais)
	for _, local := range locais {
		valor := valores[local]
		if atributo := atributoPaginaW(no, local); atributo != nil {
			substituicoes = append(substituicoes, edicaoXMLPagina{inicio: atributo.valorInicio - no.inicio, fim: atributo.valorFim - no.inicio, texto: []byte(valor)})
		} else {
			faltantes = append(faltantes, prefixo+":"+local+"=\""+valor+"\"")
		}
	}
	if len(faltantes) > 0 {
		if declaracao != "" {
			faltantes = append(faltantes, declaracao)
		}
		indice := no.fim - no.inicio - 1
		if no.vazio {
			indice--
		}
		substituicoes = append(substituicoes, edicaoXMLPagina{inicio: indice, fim: indice, texto: []byte(" " + strings.Join(faltantes, " "))})
	}
	resultado, err := aplicarEdicoesPagina(dados[no.inicio:no.fim], substituicoes)
	if err != nil {
		return nil, erroAplicarPagina()
	}
	return resultado, nil
}

func prefixoAtributoW(no *noXMLPagina) (string, string) {
	for _, atributo := range no.atributos {
		if atributo.namespace == namespaceWord && atributo.prefixo != "" {
			return atributo.prefixo, ""
		}
	}
	var prefixos []string
	for prefixo, uri := range no.ns {
		if prefixo != "" && uri == namespaceWord {
			prefixos = append(prefixos, prefixo)
		}
	}
	sort.Strings(prefixos)
	if len(prefixos) > 0 {
		return prefixos[0], ""
	}
	for n := 0; ; n++ {
		prefixo := "w"
		if n > 0 {
			prefixo += strconv.Itoa(n)
		}
		if _, existe := no.ns[prefixo]; !existe {
			return prefixo, "xmlns:" + prefixo + "=\"" + namespaceWord + "\""
		}
	}
}

func elementoPagina(dados []byte, secao *noXMLPagina, local string, valores map[string]string) string {
	prefixoElemento, _ := separarQName(secao.qname)
	qname := local
	if prefixoElemento != "" {
		qname = prefixoElemento + ":" + local
	}
	prefixoAtributo, declaracao := prefixoAtributoW(secao)
	atributos := make([]string, 0, len(valores)+1)
	if declaracao != "" {
		atributos = append(atributos, declaracao)
	}
	for _, par := range []string{"w", "h", "top", "right", "bottom", "left", "header", "footer", "gutter", "orient"} {
		if valor, ok := valores[par]; ok {
			atributos = append(atributos, prefixoAtributo+":"+par+"=\""+valor+"\"")
		}
	}
	_ = dados
	return "<" + qname + " " + strings.Join(atributos, " ") + "/>"
}

func pontoInsercaoFilho(secao *noXMLPagina, rankAlvo int) int {
	for _, no := range secao.filhos {
		if filhoSectPrConhecido(no) {
			if rank, ok := rankSectPrPagina(no.nome.Local); ok && rank > rankAlvo {
				return no.inicio
			}
		}
	}
	return secao.fechoInicio
}

func criarSecaoFinal(dados []byte, body *noXMLPagina, medidas medidasPagina) (edicaoXMLPagina, error) {
	if medidas.esquerda+medidas.direita+medidas.medianiz >= medidas.largura {
		return edicaoXMLPagina{}, erroParametrosPagina()
	}
	secao := "<" + body.qname[:len(body.qname)-len("body")] + "sectPr>" +
		elementoPagina(dados, body, "pgSz", map[string]string{"w": strconv.Itoa(medidas.largura), "h": strconv.Itoa(medidas.altura), "orient": orientacaoPagina(medidas)}) +
		elementoPagina(dados, body, "pgMar", map[string]string{"top": strconv.Itoa(medidas.superior), "right": strconv.Itoa(medidas.direita), "bottom": strconv.Itoa(medidas.inferior), "left": strconv.Itoa(medidas.esquerda), "header": strconv.Itoa(medidas.cabecalho), "footer": strconv.Itoa(medidas.rodape), "gutter": strconv.Itoa(medidas.medianiz)}) +
		"</" + body.qname[:len(body.qname)-len("body")] + "sectPr>"
	if body.vazio {
		original := dados[body.inicio:body.fim]
		abertura := append([]byte(nil), original...)
		abertura = bytes.TrimSuffix(abertura, []byte("/>"))
		if bytes.Equal(abertura, original) {
			return edicaoXMLPagina{}, erroXMLPagina()
		}
		return edicaoXMLPagina{inicio: body.inicio, fim: body.fim, texto: append(append(append(abertura, '>'), []byte(secao)...), []byte("</"+body.qname+">")...)}, nil
	}
	return edicaoXMLPagina{inicio: body.fechoInicio, fim: body.fechoInicio, texto: []byte(secao)}, nil
}

func aplicarEdicoesPagina(dados []byte, edicoes []edicaoXMLPagina) ([]byte, error) {
	sort.SliceStable(edicoes, func(i, j int) bool {
		if edicoes[i].inicio == edicoes[j].inicio {
			return edicoes[i].fim < edicoes[j].fim
		}
		return edicoes[i].inicio < edicoes[j].inicio
	})
	var saida bytes.Buffer
	posicao := 0
	for _, edicao := range edicoes {
		if edicao.inicio < posicao || edicao.fim < edicao.inicio || edicao.fim > len(dados) {
			return nil, io.ErrUnexpectedEOF
		}
		saida.Write(dados[posicao:edicao.inicio])
		saida.Write(edicao.texto)
		posicao = edicao.fim
	}
	saida.Write(dados[posicao:])
	return saida.Bytes(), nil
}
