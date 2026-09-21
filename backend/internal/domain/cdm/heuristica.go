package cdm

import (
	"regexp"
	"strings"
)

// Confiança da camada 2. A faixa fica deliberadamente abaixo da camada 1
// (estilo nomeado, 0,95): evidência textual é mais fraca que o próprio
// documento declarar o papel do bloco.
//
// Regra específica (prefixo literal de palavras-chave, legenda ou numeração
// de seção) vale mais que inferência por posição: "está depois do rótulo
// RESUMO" é um palpite sobre a região, não uma marca no texto do bloco.
const (
	confiancaRegraTextual = 0.8
	confiancaRegiao       = 0.6
)

// Rótulos que abrem uma região. A comparação é feita sobre o texto em caixa
// alta e sem espaço nas pontas — variantes acentuadas entram como entrada
// própria, de propósito: dobrar uma dependência de normalização Unicode
// (golang.org/x/text) para cinco palavras não se paga.
var regioes = map[string]Papel{
	"RESUMO":      Resumo,
	"ABSTRACT":    Resumo,
	"REFERÊNCIAS": Referencia,
	"REFERENCIAS": Referencia,
	"REFERENCES":  Referencia,
}

// Prefixos de palavras-chave, comparados em caixa alta.
var prefixosPalavrasChave = []string{"PALAVRAS-CHAVE", "PALAVRAS CHAVE", "KEYWORDS"}

// Prefixos de legenda que exigem um número logo depois ("Tabela 1"), e o
// prefixo de fonte, que não exige.
var (
	prefixosLegendaNumerada = []string{"TABELA ", "FIGURA ", "QUADRO "}
	prefixoFonte            = "FONTE:"
)

// numeracaoDeSecao casa "1 ", "2.1 ", "3.2.4 " no início do bloco. O espaço
// obrigatório separa numeração de seção de um parágrafo que só começa com
// número: "1.Introdução" e "1Introdução" não são cabeçalho.
var numeracaoDeSecao = regexp.MustCompile(`^(\d+(?:\.\d+)*) `)

// AplicarHeuristica é a camada 2 da classificação: reclassifica blocos já
// passados pela camada 1 usando evidência textual e posicional, numa única
// passada na ordem do documento.
//
// Devolve sempre uma fatia nova do mesmo tamanho; a de entrada nunca é
// mutada. Texto e posição de cada bloco saem intactos — só a classificação
// semântica muda.
//
// Bloco corrigido pelo usuário sai idêntico: quem garante isso é
// Bloco.Reclassificar, não um desvio aqui. Mas o TEXTO dele continua valendo
// como evidência para os vizinhos: proteger o papel de um bloco não apaga o
// que está escrito nele.
func AplicarHeuristica(blocos []Bloco) ([]Bloco, error) {
	saida := make([]Bloco, len(blocos))

	// Papel zero-value é inválido por construção e serve de "nenhuma região
	// aberta" — não existe Papel válido que possa ser confundido com ele.
	var regiaoAberta Papel

	for i, bloco := range blocos {
		texto := strings.TrimSpace(bloco.TextoResumo)
		emCaixaAlta := strings.ToUpper(texto)

		// Regra 1 e 2 — o rótulo abre a região e não é reclassificado: ele é
		// o cabeçalho da seção, não o conteúdo dela.
		if papelDaRegiao, ehRotulo := regioes[emCaixaAlta]; ehRotulo {
			regiaoAberta = papelDaRegiao
			saida[i] = bloco
			continue
		}

		// Regras 3, 4 e 5 — evidência no próprio texto do bloco, que vence a
		// inferência por região.
		if papel, casou := papelPorRegraTextual(texto, emCaixaAlta); casou {
			// Só a seção numerada é cabeçalho: legenda e palavras-chave
			// aparecem DENTRO de uma região sem encerrá-la.
			if ehCabecalho(papel) {
				regiaoAberta = Papel{}
			}
			novo, err := reclassificar(bloco, papel, confiancaRegraTextual)
			if err != nil {
				return nil, err
			}
			saida[i] = novo
			continue
		}

		// Cabeçalho que a camada 1 já reconheceu pelo estilo nomeado encerra
		// a região sem precisar de numeração no texto.
		if ehCabecalho(bloco.Papel) {
			regiaoAberta = Papel{}
			saida[i] = bloco
			continue
		}

		if regiaoAberta.Valido() {
			novo, err := reclassificar(bloco, regiaoAberta, confiancaRegiao)
			if err != nil {
				return nil, err
			}
			saida[i] = novo
			continue
		}

		saida[i] = bloco
	}

	return saida, nil
}

// reclassificar aplica a nova classificação apenas quando ela DIFERE da que o
// bloco já tem.
//
// Concordar com a camada 1 não pode enfraquecer o resultado: se o estilo
// nomeado já disse Secao(1) com confiança 0,95 e a numeração do texto diz a
// mesma coisa, reescrever o bloco trocaria OrigemEstiloDocx por
// OrigemHeuristica e derrubaria a confiança — duas evidências independentes
// concordando sairiam valendo menos que uma sozinha.
func reclassificar(bloco Bloco, papel Papel, confianca float64) (Bloco, error) {
	if bloco.Papel == papel {
		return bloco, nil
	}
	return bloco.Reclassificar(papel, confianca, OrigemHeuristica)
}

func ehCabecalho(papel Papel) bool { return papel.nome == nomePapelSecao }

// papelPorRegraTextual aplica, nesta precedência, palavras-chave > legenda >
// seção numerada. Os três prefixos são mutuamente exclusivos na prática; a
// ordem existe para o comportamento ser determinado, não sorteado.
func papelPorRegraTextual(texto, emCaixaAlta string) (Papel, bool) {
	if temAlgumPrefixo(emCaixaAlta, prefixosPalavrasChave) {
		return PalavrasChave, true
	}
	if ehLegenda(emCaixaAlta) {
		return Legenda, true
	}
	if nivel, ok := nivelPelaNumeracao(texto); ok {
		return Secao(nivel), true
	}
	return Papel{}, false
}

func temAlgumPrefixo(texto string, prefixos []string) bool {
	for _, prefixo := range prefixos {
		if strings.HasPrefix(texto, prefixo) {
			return true
		}
	}
	return false
}

// ehLegenda exige o número logo depois de "Tabela"/"Figura"/"Quadro": sem
// isso, "Tabela mostra os resultados" — uma frase comum no corpo do texto —
// viraria legenda. O espaço no prefixo também descarta o plural ("Tabelas 1 e
// 2 mostram..."), que é referência no texto, não legenda.
func ehLegenda(emCaixaAlta string) bool {
	if strings.HasPrefix(emCaixaAlta, prefixoFonte) {
		return true
	}
	for _, prefixo := range prefixosLegendaNumerada {
		if !strings.HasPrefix(emCaixaAlta, prefixo) {
			continue
		}
		resto := emCaixaAlta[len(prefixo):]
		if resto != "" && resto[0] >= '0' && resto[0] <= '9' {
			return true
		}
	}
	return false
}

// nivelPelaNumeracao devolve quantos níveis a numeração declara: "2.1" são
// dois. Acima de NivelSecaoMaximo devolve false — Secao(7) seria um bloco que
// NovoBloco recusa, e inventar um cabeçalho inválido é pior que não
// classificar.
func nivelPelaNumeracao(texto string) (int, bool) {
	casamento := numeracaoDeSecao.FindStringSubmatch(texto)
	if casamento == nil {
		return 0, false
	}
	nivel := strings.Count(casamento[1], ".") + 1
	if nivel > NivelSecaoMaximo {
		return 0, false
	}
	return nivel, true
}
