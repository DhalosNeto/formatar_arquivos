// Package ooxml abre e salva pacotes DOCX preservando os bytes originais das
// partes que não são mutadas. Ver docs/adr/0001-docx-in-place.md.
//
// Nesta fase (F2), nenhuma parte é desserializada: Abrir só confere a forma
// mínima de um DOCX válido e Salvar recopia cada entrada crua do ZIP via
// zip.Writer.Copy, sem descomprimir nem reescrever XML. A desserialização
// entra quando a mutação de um nó exigir, não antes — parsear sem necessidade
// só cria superfície para o encoding/xml reescrever namespace e corromper o
// pacote.
package ooxml

import (
	"archive/zip"
	"io"

	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// Partes obrigatórias para um pacote ser considerado um DOCX válido.
const (
	parteContentTypes       = "[Content_Types].xml"
	parteDocumentoPrincipal = "word/document.xml"
)

// Documento é um pacote DOCX aberto, pronto para ser salvo. Guarda as
// entradas do ZIP de origem na ordem em que apareceram, para que Salvar as
// recopie sem reordenar nem recomprimir.
type Documento struct {
	arquivos []*zip.File
}

// leitorRastreado envolve um io.ReaderAt e lembra o último erro de I/O
// devolvido pela origem (que não seja io.EOF). Serve para distinguir, depois
// que zip.NewReader falha, se a causa foi o meio de leitura (storage caiu) ou
// a estrutura do arquivo em si (não é um ZIP válido) — os dois viram tipos de
// erro diferentes.
type leitorRastreado struct {
	origem io.ReaderAt
	erro   error
}

func (l *leitorRastreado) ReadAt(p []byte, off int64) (int, error) {
	n, err := l.origem.ReadAt(p, off)
	if err != nil && err != io.EOF {
		l.erro = err
	}
	return n, err
}

// Abrir lê um pacote DOCX a partir de um io.ReaderAt e confere sua forma
// mínima: ZIP íntegro contendo [Content_Types].xml e word/document.xml.
//
// Falha de estrutura do arquivo (não é ZIP, truncado, faltam partes
// obrigatórias) devolve *errors.ErroValidacao. Falha do meio de leitura (o
// io.ReaderAt devolve erro) devolve *errors.ErroAplicacao — não é culpa do
// cliente.
func Abrir(r io.ReaderAt, tamanho int64) (*Documento, error) {
	rastreado := &leitorRastreado{origem: r}

	leitor, err := zip.NewReader(rastreado, tamanho)
	if err != nil {
		if rastreado.erro != nil {
			return nil, errors.NovoErroAplicacao("ler pacote docx: " + rastreado.erro.Error())
		}
		return nil, errors.NovoErroValidacao("arquivo", "o arquivo não é um pacote ZIP válido")
	}

	if err := conferirPartesObrigatorias(leitor.File); err != nil {
		return nil, err
	}

	return &Documento{arquivos: leitor.File}, nil
}

// conferirPartesObrigatorias exige as duas partes sem as quais um pacote não
// pode ser tratado como DOCX. Mesma exigência de vo.ConferirPacoteDocx —
// aqui não a reusamos porque aquela função recebe []byte, e forçar a chamada
// obrigaria bufferizar o io.ReaderAt inteiro antes de abrir.
func conferirPartesObrigatorias(arquivos []*zip.File) error {
	var temContentTypes, temDocumentoPrincipal bool
	for _, arquivo := range arquivos {
		switch arquivo.Name {
		case parteContentTypes:
			temContentTypes = true
		case parteDocumentoPrincipal:
			temDocumentoPrincipal = true
		}
	}
	if temContentTypes && temDocumentoPrincipal {
		return nil
	}
	return errors.NovoErroValidacao("arquivo", "o arquivo não é um pacote DOCX válido: faltam partes obrigatórias")
}

// Salvar grava o pacote no destino informado. Nenhuma parte é mutada nesta
// fase: cada entrada é recopiada crua com zip.Writer.Copy (stdlib, desde Go
// 1.17), que preserva cabeçalho, método de compressão e bytes comprimidos tal
// como vieram — é isso que reproduz o ZIP de origem byte a byte.
//
// ponytail: Copy não descomprime nada, então o limite de zip bomb não se
// aplica a este caminho (quem grava já validou com vo.ConferirPacoteDocx no
// upload). O teto entra quando uma mutação futura precisar descomprimir uma
// parte de verdade.
func (d *Documento) Salvar(w io.Writer) error {
	escritor := zip.NewWriter(w)

	for _, arquivo := range d.arquivos {
		if err := escritor.Copy(arquivo); err != nil {
			return errors.NovoErroAplicacao("gravar pacote docx: " + err.Error())
		}
	}

	if err := escritor.Close(); err != nil {
		return errors.NovoErroAplicacao("finalizar pacote docx: " + err.Error())
	}
	return nil
}
