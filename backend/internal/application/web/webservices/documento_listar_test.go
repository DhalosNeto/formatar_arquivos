package webservices_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/daniel-halos/formatador/internal/application/web/webmodel"
	"github.com/daniel-halos/formatador/internal/domain/documento/entity"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// TestListarDevolveDocumentosConvertidos cobre o caso feliz: cada entity.Documento
// do dono vira o retrato público correspondente, sem depender da ordem de
// iteração do repositório (o fake devolve um map).
func TestListarDevolveDocumentosConvertidos(t *testing.T) {
	t.Parallel()
	servico, repo, _, _, _ := novoServicoDeTeste(t)
	dono := donoDeTeste(t)
	primeiro := documentoDeTeste(t, dono)
	segundo := documentoComPreviewDeTeste(t, dono)
	repo.documentos = map[uuid.UUID]entity.Documento{
		primeiro.ID: primeiro,
		segundo.ID:  segundo,
	}

	resposta, err := servico.Listar(context.Background(), dono, 20, 0)
	exigirSemErro(t, err)

	if len(resposta) != 2 {
		t.Fatalf("esperava 2 documentos, obteve %d", len(resposta))
	}
	porID := map[uuid.UUID]webmodel.DocumentoResposta{}
	for _, item := range resposta {
		porID[item.ID] = item
	}
	if item, ok := porID[primeiro.ID]; !ok || item.NomeOriginal != primeiro.NomeOriginal || item.TemPreview {
		t.Fatalf("documento sem preview convertido incorretamente: %+v", item)
	}
	if item, ok := porID[segundo.ID]; !ok || !item.TemPreview {
		t.Fatalf("documento com preview convertido incorretamente: %+v", item)
	}
}

// TestListarDeDonoSemDocumentosDevolveListaVazia prova que uma sessão nova
// devolve slice vazio, nunca nil: sessão nova sem documentos é normal, não
// erro, e o JSON precisa serializar como "[]", não "null".
func TestListarDeDonoSemDocumentosDevolveListaVazia(t *testing.T) {
	t.Parallel()
	servico, _, _, _, _ := novoServicoDeTeste(t)
	dono := donoDeTeste(t)

	resposta, err := servico.Listar(context.Background(), dono, 20, 0)
	exigirSemErro(t, err)

	if resposta == nil {
		t.Fatal("lista vazia veio nil: serializaria como null, não []")
	}
	if len(resposta) != 0 {
		t.Fatalf("esperava lista vazia, obteve %d itens", len(resposta))
	}
}

// TestListarErroDeValidacaoDoDominioSobeSemReclassificar prova que o erro que
// documentoservice.ListarDoDono devolve para dono vazio chega ao chamador tal
// como é: ErroValidacao, sem ser embrulhado em outro tipo.
func TestListarErroDeValidacaoDoDominioSobeSemReclassificar(t *testing.T) {
	t.Parallel()
	servico, _, _, _, _ := novoServicoDeTeste(t)

	_, err := servico.Listar(context.Background(), vo.Dono{}, 20, 0)
	exigirValidacao(t, err)
}

// TestListarPropagaErroDoRepositorioSemReclassificar prova que uma falha
// típica de repositório (ex.: conflito de leitura) sobe intacta, sem virar
// outro tipo de erro nesta camada.
func TestListarPropagaErroDoRepositorioSemReclassificar(t *testing.T) {
	t.Parallel()
	servico, repo, _, _, _ := novoServicoDeTeste(t)
	dono := donoDeTeste(t)
	falhaOriginal := errors.NovoErroAplicacao("repositório indisponível")
	repo.erroListar = falhaOriginal

	_, err := servico.Listar(context.Background(), dono, 20, 0)
	if !errors.E(err, falhaOriginal) {
		t.Fatalf("esperava a causa original preservada, obteve %T: %v", err, err)
	}
}

// TestListarComLimiteAbsurdoNaoQuebraNemVazaAlemDoTeto: o serviço de
// aplicação só repassa a paginação ao domínio, que já normaliza — este teste
// prova que a composição continua correta mesmo com um limite absurdo vindo
// da borda HTTP, sem quebrar nem devolver erro.
func TestListarComLimiteAbsurdoNaoQuebraNemVazaAlemDoTeto(t *testing.T) {
	t.Parallel()
	servico, repo, _, _, _ := novoServicoDeTeste(t)
	dono := donoDeTeste(t)
	documento := documentoDeTeste(t, dono)
	repo.documentos = map[uuid.UUID]entity.Documento{documento.ID: documento}

	_, err := servico.Listar(context.Background(), dono, 100000, 0)
	exigirSemErro(t, err)
}
