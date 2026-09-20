package documentos

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/daniel-halos/formatador/internal/application/web/webservices"
	"github.com/daniel-halos/formatador/internal/domain/documento/entity"
	documentoservice "github.com/daniel-halos/formatador/internal/domain/documento/service"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// repositorioMemoria é um fake mínimo de repository.DocumentoRepo: guarda
// tudo em memória e nunca simula falha. Os testes de controlador cuidam de
// comportamento HTTP, não de invariante de domínio — isso já está coberto em
// internal/domain/documento/service e internal/application/web/webservices.
type repositorioMemoria struct {
	documentos map[uuid.UUID]entity.Documento
}

func novoRepositorioMemoria() *repositorioMemoria {
	return &repositorioMemoria{documentos: map[uuid.UUID]entity.Documento{}}
}

func (r *repositorioMemoria) Inserir(_ context.Context, documento entity.Documento) error {
	r.documentos[documento.ID] = documento
	return nil
}

func (r *repositorioMemoria) ObterPorID(_ context.Context, solicitante vo.Dono, id uuid.UUID) (entity.Documento, error) {
	documento, existe := r.documentos[id]
	if !existe || !solicitante.PodeAcessar(documento.Dono) {
		return entity.Documento{}, errors.NovoErroNaoEncontrado("documento")
	}
	return documento, nil
}

func (r *repositorioMemoria) ListarPorDono(_ context.Context, solicitante vo.Dono, _, _ int) ([]entity.Documento, error) {
	var lista []entity.Documento
	for _, documento := range r.documentos {
		if solicitante.PodeAcessar(documento.Dono) {
			lista = append(lista, documento)
		}
	}
	return lista, nil
}

func (r *repositorioMemoria) DefinirChavePreviewPDF(_ context.Context, solicitante vo.Dono, id uuid.UUID, chave vo.ChaveStorage) error {
	documento, existe := r.documentos[id]
	if !existe || !solicitante.PodeAcessar(documento.Dono) {
		return errors.NovoErroNaoEncontrado("documento")
	}
	documento.ChaveStoragePDF = &chave
	r.documentos[id] = documento
	return nil
}

// armazenadorMemoria é um fake mínimo de webservices.ArmazenadorObjetos.
type armazenadorMemoria struct{}

func (armazenadorMemoria) Salvar(_ context.Context, _ vo.ChaveStorage, conteudo io.Reader, _ int64, _ string) error {
	_, err := io.Copy(io.Discard, conteudo)
	return err
}

func (armazenadorMemoria) URLPreAssinada(_ context.Context, chave vo.ChaveStorage, _ time.Duration) (string, error) {
	return "https://storage.exemplo/" + chave.String(), nil
}

// conversorMemoria é um fake mínimo de webservices.ConversorPDF.
type conversorMemoria struct{}

func (conversorMemoria) ConverterParaPDF(context.Context, []byte) ([]byte, error) {
	return []byte("%PDF-1.4 fake"), nil
}

// controladorDeTeste monta o controlador sobre a pilha real de domínio e
// aplicação (documentoservice.Servico + webservices.ServicoDocumento),
// substituindo só as bordas de I/O (storage e conversor) por fakes em
// memória. É a mesma composição que o codador vai fiar em produção.
func controladorDeTeste(t *testing.T) (*Controlador, *repositorioMemoria) {
	t.Helper()
	repo := novoRepositorioMemoria()
	docservico, err := documentoservice.NovoServico(repo, 0)
	exigirSemErroDocumentos(t, err)
	servico, err := webservices.NovoServicoDocumento(docservico, armazenadorMemoria{}, conversorMemoria{})
	exigirSemErroDocumentos(t, err)
	return NovoControlador(servico), repo
}
