package ooxml

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"strings"

	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// SubstituirParte retém uma cópia opaca para uma entrada existente e única.
// Nil e vazio mantêm a entrada com zero bytes; uma rejeição preserva o estado.
func (documento *Documento) SubstituirParte(nome string, conteudo []byte) error {
	if documento == nil {
		return errors.NovoErroArgumentoNulo("documento")
	}
	if !nomeParteSeguro(nome) {
		return errors.NovoErroValidacao("parte", "nome de parte inválido")
	}
	if err := documento.validarAlvoSubstituicao(nome); err != nil {
		return err
	}
	total := uint64(len(conteudo))
	if total > vo.TamanhoDescomprimidoMaximoBytes {
		return errors.NovoErroValidacao("parte", "conteúdo excede o limite de partes substituídas")
	}
	for nomeRetido, conteudoRetido := range documento.partesSubstituidas {
		if nomeRetido == nome {
			continue
		}
		if uint64(len(conteudoRetido)) > vo.TamanhoDescomprimidoMaximoBytes-total {
			return errors.NovoErroValidacao("parte", "conteúdo excede o limite de partes substituídas")
		}
		total += uint64(len(conteudoRetido))
	}
	copia := bytes.Clone(conteudo)
	if documento.partesSubstituidas == nil {
		documento.partesSubstituidas = make(map[string][]byte)
	}
	documento.partesSubstituidas[nome] = copia
	return nil
}

func nomeParteSeguro(nome string) bool {
	if nome == "" || strings.ContainsAny(nome, "\\\x00") {
		return false
	}
	if len(nome) >= 2 && nome[1] == ':' && ((nome[0] >= 'A' && nome[0] <= 'Z') || (nome[0] >= 'a' && nome[0] <= 'z')) {
		return false
	}
	for _, segmento := range strings.Split(nome, "/") {
		if segmento == "" || segmento == "." || segmento == ".." {
			return false
		}
	}
	return true
}

func (documento *Documento) validarAlvoSubstituicao(nome string) error {
	var alvo *zip.File
	for _, arquivo := range documento.arquivos {
		if arquivo.Name != nome {
			continue
		}
		if alvo != nil {
			return errors.NovoErroValidacao("parte", "parte duplicada no pacote")
		}
		alvo = arquivo
	}
	if alvo == nil || alvo.FileInfo().IsDir() {
		return errors.NovoErroValidacao("parte", "parte ausente ou diretório")
	}
	if alvo.Method != zip.Store && alvo.Method != zip.Deflate {
		return errors.NovoErroValidacao("parte", "método de compressão não suportado")
	}
	for extra := alvo.Extra; len(extra) > 0; {
		if len(extra) < 4 {
			return errors.NovoErroValidacao("parte", "metadados extras inválidos")
		}
		tamanho := 4 + int(binary.LittleEndian.Uint16(extra[2:4]))
		if tamanho > len(extra) {
			return errors.NovoErroValidacao("parte", "metadados extras inválidos")
		}
		extra = extra[tamanho:]
	}
	return nil
}

func copiarExtraSemZIP64(extra []byte) []byte {
	copia := make([]byte, 0, len(extra))
	for len(extra) > 0 {
		tamanho := 4 + int(binary.LittleEndian.Uint16(extra[2:4]))
		if binary.LittleEndian.Uint16(extra[:2]) != 0x0001 {
			copia = append(copia, extra[:tamanho]...)
		}
		extra = extra[tamanho:]
	}
	return copia
}
