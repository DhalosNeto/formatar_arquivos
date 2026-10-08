// Package ooxml abre e salva pacotes DOCX preservando os bytes originais das
// partes que não são mutadas. Ver docs/adr/0001-docx-in-place.md.
//
// A substituição trabalha com bytes opacos para não reserializar XML nem
// alterar namespaces das partes do documento.
package ooxml

import (
	"archive/zip"
	"io"
	"time"

	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// Partes obrigatórias para um pacote ser considerado um DOCX válido.
const (
	parteContentTypes       = "[Content_Types].xml"
	parteDocumentoPrincipal = "word/document.xml"
)

// Documento mantém a ordem das entradas e os bytes das partes intactas.
// Não admite uso concorrente. O ReaderAt original deve permanecer disponível
// e imutável até terminar o uso do documento, inclusive novas tentativas de salvar.
type Documento struct {
	arquivos           []*zip.File
	partesSubstituidas map[string][]byte
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

// Salvar preserva as entradas intactas e regrava as substituídas sem consumir
// as alterações. Em falha, o destino pode ficar parcial e deve ser descartado.
// O writer recebido não é fechado; o documento permite nova tentativa.
func (documento *Documento) Salvar(destino io.Writer) error {
	escritor := zip.NewWriter(destino)

	for _, arquivo := range documento.arquivos {
		if err := documento.gravarParte(escritor, arquivo); err != nil {
			return errors.NovoErroAplicacao("não foi possível gravar pacote docx")
		}
	}

	if err := escritor.Close(); err != nil {
		return errors.NovoErroAplicacao("não foi possível finalizar pacote docx")
	}
	return nil
}

func (documento *Documento) gravarParte(escritor *zip.Writer, arquivo *zip.File) error {
	conteudo, substituida := documento.partesSubstituidas[arquivo.Name]
	if !substituida {
		return escritor.Copy(arquivo)
	}
	cabecalho := arquivo.FileHeader
	cabecalho.Extra = copiarExtraSemZIP64(arquivo.Extra)
	// CreateHeader acrescentaria outro timestamp se Modified permanecesse preenchido.
	cabecalho.Modified = time.Time{}
	cabecalho.CRC32 = 0
	cabecalho.CompressedSize64, cabecalho.UncompressedSize64 = 0, 0
	destino, err := escritor.CreateHeader(&cabecalho)
	if err != nil {
		return err
	}
	_, err = destino.Write(conteudo)
	return err
}
