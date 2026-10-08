package ooxml

import (
	"bytes"
	"encoding/xml"
	"io"
	"strconv"
	"strings"

	"github.com/daniel-halos/formatador/internal/domain/ruleset"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

const (
	namespaceOPC      = "http://schemas.openxmlformats.org/package/2006/relationships"
	tipoRelacaoStyles = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles"
)

// AplicarRecuoPrimeiraLinha valida herança e grava w:firstLine nos parágrafos indicados.
func (documento *Documento) AplicarRecuoPrimeiraLinha(referencias []int, centimetros float64) error {
	if documento == nil {
		return errors.NovoErroArgumentoNulo("documento")
	}
	if err := ruleset.ValidarRecuoCM(centimetros); err != nil {
		return err
	}
	twips, err := vo.CentimetrosParaTwips(centimetros)
	if err != nil {
		return err
	}
	preflight := novoPreflightRecuo(documento)
	return documento.aplicarPropriedadeParagrafosComPreflight(referencias, "ind", map[string]string{"firstLine": formatarInteiroXML(twips)}, func(dados []byte, paragrafo *noXMLPagina) error {
		return preflight.validar(dados, paragrafo)
	})
}

func formatarInteiroXML(valor int) string { return strconv.Itoa(valor) }

type preflightRecuo struct {
	documento      *Documento
	carregouStyles bool
	styles         *noXMLPagina
	stylesDados    []byte
	porID          map[string][]*noXMLPagina
	erro           error
	semStyles      bool
}

func novoPreflightRecuo(documento *Documento) *preflightRecuo {
	return &preflightRecuo{documento: documento}
}

func (p *preflightRecuo) validar(dados []byte, paragrafo *noXMLPagina) error {
	for _, filho := range paragrafo.filhos {
		if filho.nome.Space == "http://schemas.openxmlformats.org/markup-compatibility/2006" && filho.nome.Local == "AlternateContent" {
			return erroXMLPagina()
		}
	}
	pprs := filhosW(paragrafo, "pPr")
	if len(pprs) > 1 {
		return erroXMLPagina()
	}
	var estiloID string
	if len(pprs) == 1 {
		if err := validarPPrRecuo(dados, pprs[0]); err != nil {
			return err
		}
		valores, err := atributosFilhoW(pprs[0], "pStyle", "val")
		if err != nil {
			return err
		}
		if len(valores) > 1 || (len(filhosW(pprs[0], "pStyle")) == 1 && len(valores) != 1) {
			return erroXMLPagina()
		}
		if len(valores) == 1 {
			estiloID = valores[0]
		}
	}
	if err := p.carregarStyles(); err != nil {
		return err
	}
	if p.semStyles {
		if estiloID != "" {
			return erroXMLPagina()
		}
		return nil
	}
	defaults := filhosW(p.styles, "docDefaults")
	if len(defaults) > 1 {
		return erroXMLPagina()
	}
	if len(defaults) == 1 {
		if contemAlternateContentDireto(defaults[0]) {
			return erroXMLPagina()
		}
		pprDefaults := filhosW(defaults[0], "pPrDefault")
		if len(pprDefaults) > 1 {
			return erroXMLPagina()
		}
		if len(pprDefaults) == 1 {
			if contemAlternateContentDireto(pprDefaults[0]) {
				return erroXMLPagina()
			}
			pprsDefault := filhosW(pprDefaults[0], "pPr")
			if len(pprsDefault) > 1 {
				return erroXMLPagina()
			}
			if len(pprsDefault) == 1 {
				if err := validarPPrRecuo(p.stylesDados, pprsDefault[0]); err != nil {
					return err
				}
			}
		}
	}
	if estiloID == "" {
		var err error
		estiloID, err = p.estiloPadrao()
		if err != nil {
			return err
		}
	}
	return p.validarCadeia(estiloID)
}

func (p *preflightRecuo) carregarStyles() error {
	if p.carregouStyles {
		return p.erro
	}
	p.carregouStyles = true
	rels, encontrouRels, err := p.lerParte("word/_rels/document.xml.rels")
	if err != nil {
		p.erro = err
		return err
	}
	stylesParte, stylesDuplicada := p.temParte("word/styles.xml")
	if stylesDuplicada {
		p.erro = erroXMLPagina()
		return p.erro
	}
	if !encontrouRels {
		if stylesParte {
			p.erro = erroXMLPagina()
			return p.erro
		}
		p.semStyles = true
		return nil
	}
	raiz, _, parseErr := analisarXMLPagina(rels)
	if parseErr != nil || raiz.nome != (xml.Name{Space: namespaceOPC, Local: "Relationships"}) {
		p.erro = erroXMLPagina()
		return p.erro
	}
	var relStyles []*noXMLPagina
	ids := map[string]bool{}
	for _, rel := range raiz.filhos {
		if rel.nome != (xml.Name{Space: namespaceOPC, Local: "Relationship"}) {
			p.erro = erroXMLPagina()
			return p.erro
		}
		id, okID := atributoNaoQualificado(rel, "Id")
		tipo, okType := atributoNaoQualificado(rel, "Type")
		target, okTarget := atributoNaoQualificado(rel, "Target")
		modo, okMode := atributoNaoQualificado(rel, "TargetMode")
		if !okID || !okType || !okTarget || id == "" || tipo == "" || target == "" || ids[id] {
			p.erro = erroXMLPagina()
			return p.erro
		}
		ids[id] = true
		if atributoQualificadoHomologo(rel, "TargetMode") {
			p.erro = erroXMLPagina()
			return p.erro
		}
		if okMode && modo != "Internal" && modo != "External" {
			p.erro = erroXMLPagina()
			return p.erro
		}
		if tipo == tipoRelacaoStyles {
			relStyles = append(relStyles, rel)
		}
	}
	if len(relStyles) > 1 {
		p.erro = erroXMLPagina()
		return p.erro
	}
	if len(relStyles) == 0 {
		if stylesParte {
			p.erro = erroXMLPagina()
			return p.erro
		}
		p.semStyles = true
		return nil
	}
	rel := relStyles[0]
	modo, temModo := atributoNaoQualificado(rel, "TargetMode")
	target, _ := atributoNaoQualificado(rel, "Target")
	if (temModo && modo != "Internal") || target != "styles.xml" || !stylesParte {
		p.erro = erroXMLPagina()
		return p.erro
	}
	conteudo, achou, err := p.lerParte("word/styles.xml")
	if err != nil {
		p.erro = err
		return err
	}
	if !achou {
		p.erro = erroXMLPagina()
		return p.erro
	}
	styles, _, err := analisarXMLPagina(conteudo)
	if err != nil || styles.nome != (xml.Name{Space: namespaceWord, Local: "styles"}) {
		p.erro = erroXMLPagina()
		return p.erro
	}
	p.styles = styles
	p.stylesDados = conteudo
	p.porID = make(map[string][]*noXMLPagina)
	for _, estilo := range filhosW(styles, "style") {
		ids := atributosDeNo(estilo, "styleId")
		if len(ids) == 1 {
			id, ok := valorWordOuNaoQualificado(ids[0], "styleId")
			if ok && id != "" {
				p.porID[id] = append(p.porID[id], estilo)
			}
		}
	}
	return nil
}

func (p *preflightRecuo) temParte(nome string) (bool, bool) {
	contagem := 0
	for _, f := range p.documento.arquivos {
		if f.Name == nome {
			contagem++
		}
	}
	return contagem == 1, contagem > 1
}

func (p *preflightRecuo) lerParte(nome string) ([]byte, bool, error) {
	if valor, ok := p.documento.partesSubstituidas[nome]; ok {
		return bytes.Clone(valor), true, nil
	}
	arquivo := p.documento.parte(nome)
	if arquivo == nil {
		return nil, false, nil
	}
	if _, duplicada := p.temParte(nome); duplicada {
		return nil, false, erroXMLPagina()
	}
	leitor, err := arquivo.Open()
	if err != nil {
		return nil, false, errors.NovoErroAplicacao("não foi possível ler parte do documento")
	}
	conteudo, readErr := io.ReadAll(io.LimitReader(leitor, limiteXMLPagina+1))
	closeErr := leitor.Close()
	if readErr != nil || closeErr != nil {
		return nil, false, errors.NovoErroAplicacao("não foi possível ler parte do documento")
	}
	if len(conteudo) == 0 || len(conteudo) > limiteXMLPagina || !declaracaoUTF8(conteudo) {
		return nil, false, erroXMLPagina()
	}
	return conteudo, true, nil
}

func (p *preflightRecuo) estiloPadrao() (string, error) {
	var padroes []string
	for _, estilo := range filhosW(p.styles, "style") {
		tipos := atributosDeNo(estilo, "type")
		if len(tipos) != 1 {
			continue
		}
		tipo, ok := valorWordOuNaoQualificado(tipos[0], "type")
		if !ok || tipo != "paragraph" {
			continue
		}
		flags := atributosDeNo(estilo, "default")
		if len(flags) > 1 {
			return "", erroXMLPagina()
		}
		if len(flags) == 0 {
			continue
		}
		valor, ok := valorWordOuNaoQualificado(flags[0], "default")
		if !ok {
			return "", erroXMLPagina()
		}
		switch valor {
		case "true", "1", "on":
			id, ok := atributoWordOuNaoQualificado(estilo, "styleId")
			if !ok || id == "" {
				return "", erroXMLPagina()
			}
			padroes = append(padroes, id)
		case "false", "0", "off":
		default:
			return "", erroXMLPagina()
		}
	}
	if len(padroes) > 1 {
		return "", erroXMLPagina()
	}
	if len(padroes) == 0 {
		return "", nil
	}
	return padroes[0], nil
}

func (p *preflightRecuo) validarCadeia(id string) error {
	if id == "" {
		return nil
	}
	vistos := map[string]bool{}
	for profundidade := 0; id != ""; profundidade++ {
		if profundidade >= 32 || vistos[id] {
			return erroXMLPagina()
		}
		vistos[id] = true
		estilos := p.porID[id]
		if len(estilos) != 1 {
			return erroXMLPagina()
		}
		estilo := estilos[0]
		tipos := atributosDeNo(estilo, "type")
		if len(tipos) != 1 {
			return erroXMLPagina()
		}
		tipo, ok := valorWordOuNaoQualificado(tipos[0], "type")
		if !ok || tipo != "paragraph" {
			return erroXMLPagina()
		}
		if contemAlternateContentDireto(estilo) {
			return erroXMLPagina()
		}
		pprs := filhosW(estilo, "pPr")
		if len(pprs) > 1 {
			return erroXMLPagina()
		}
		if len(pprs) == 1 {
			if err := validarPPrRecuo(p.stylesDados, pprs[0]); err != nil {
				return err
			}
		}
		based, err := atributosFilhoW(estilo, "basedOn", "val")
		if err != nil || len(based) > 1 {
			return erroXMLPagina()
		}
		if len(based) == 0 {
			return nil
		}
		id = based[0]
	}
	return nil
}

func contemAlternateContentDireto(no *noXMLPagina) bool {
	for _, filho := range no.filhos {
		if filho.nome.Space == "http://schemas.openxmlformats.org/markup-compatibility/2006" && filho.nome.Local == "AlternateContent" {
			return true
		}
	}
	return false
}

func atributosDeNo(no *noXMLPagina, nome string) []atributoXMLPagina {
	var encontrados []atributoXMLPagina
	for _, a := range no.atributos {
		if a.local == nome {
			encontrados = append(encontrados, a)
		}
	}
	return encontrados
}
func valorWordOuNaoQualificado(a atributoXMLPagina, nome string) (string, bool) {
	return a.valor, a.local == nome && (a.namespace == namespaceWord || a.namespace == "")
}
func atributoWordOuNaoQualificado(no *noXMLPagina, nome string) (string, bool) {
	for _, a := range no.atributos {
		if (a.namespace == namespaceWord || a.namespace == "") && a.local == nome {
			return a.valor, true
		}
	}
	return "", false
}
func atributoNaoQualificado(no *noXMLPagina, nome string) (string, bool) {
	for _, a := range no.atributos {
		if a.namespace == "" && a.local == nome {
			return a.valor, true
		}
	}
	return "", false
}
func atributoQualificadoHomologo(no *noXMLPagina, nome string) bool {
	for _, a := range no.atributos {
		if a.local == nome && a.namespace != "" {
			return true
		}
	}
	return false
}
func atributosFilhoW(pai *noXMLPagina, filho, atributo string) ([]string, error) {
	filhos := filhosW(pai, filho)
	if len(filhos) > 1 {
		return nil, erroXMLPagina()
	}
	if len(filhos) == 0 {
		return nil, nil
	}
	var valores []string
	for _, a := range filhos[0].atributos {
		if a.local == atributo && a.namespace == "" {
			return nil, erroXMLPagina()
		}
		if a.local == atributo && a.namespace == namespaceWord {
			valores = append(valores, a.valor)
		}
	}
	if len(valores) != 1 || valores[0] == "" {
		return nil, erroXMLPagina()
	}
	return valores, nil
}

func validarPPrRecuo(dados []byte, ppr *noXMLPagina) error {
	if ppr.nome.Space != namespaceWord || ppr.nome.Local != "pPr" {
		return erroXMLPagina()
	}
	ultimo := -1
	for _, filho := range ppr.filhos {
		rank := ordemPropriedadeParagrafo(filho)
		if rank < 0 || rank <= ultimo {
			return erroXMLPagina()
		}
		ultimo = rank
		if filho.nome.Space == namespaceWord && filho.nome.Local == "numPr" {
			return erroXMLPagina()
		}
		if filho.nome.Space == "http://schemas.openxmlformats.org/markup-compatibility/2006" && filho.nome.Local == "AlternateContent" {
			return erroXMLPagina()
		}
		if filho.nome.Space == namespaceWord && filho.nome.Local == "ind" {
			if len(filho.filhos) != 0 || (!filho.vazio && strings.Trim(string(dados[filho.fim:filho.fechoInicio]), " \t\r\n") != "") {
				return erroXMLPagina()
			}
			for _, a := range filho.atributos {
				if a.namespace == "" && (a.local == "firstLine" || a.local == "hanging" || a.local == "hangingChars" || a.local == "firstLineChars") {
					return erroXMLPagina()
				}
				if a.namespace == namespaceWord && (a.local == "hanging" || a.local == "hangingChars" || a.local == "firstLineChars") {
					return erroXMLPagina()
				}
			}
		}
	}
	return nil
}
