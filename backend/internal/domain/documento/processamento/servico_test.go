package processamento

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/daniel-halos/formatador/internal/domain/documento/entity"
	"github.com/daniel-halos/formatador/internal/domain/documento/repository"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

type repositorioInternoFake struct {
	documentos                       map[uuid.UUID]entity.Documento
	contextos                        []context.Context
	leituras, escritas, gravacoesCDM int
	anterior, novo                   entity.Status
	erroLeitura, erroEscrita         error
	// Simula outra escrita depois da leitura e antes do CAS, sem goroutines ou relógio.
	statusConcorrente entity.Status
}

var _ repository.DocumentoInternoRepo = (*repositorioInternoFake)(nil)

func (r *repositorioInternoFake) ObterPorIDInterno(ctx context.Context, id uuid.UUID) (entity.Documento, error) {
	r.contextos = append(r.contextos, ctx)
	r.leituras++
	if r.erroLeitura != nil {
		return entity.Documento{}, r.erroLeitura
	}
	documento, existe := r.documentos[id]
	if !existe {
		return entity.Documento{}, errors.NovoErroNaoEncontrado("documento")
	}
	documento.CDM = append(json.RawMessage(nil), documento.CDM...)
	return documento, nil
}

func (r *repositorioInternoFake) AtualizarStatus(ctx context.Context, id uuid.UUID, statusAtual, novoStatus entity.Status) error {
	r.contextos = append(r.contextos, ctx)
	r.escritas++
	r.anterior, r.novo = statusAtual, novoStatus
	if r.erroEscrita != nil {
		return r.erroEscrita
	}
	documento, existe := r.documentos[id]
	if !existe {
		return errors.NovoErroNaoEncontrado("documento")
	}
	if r.statusConcorrente != "" {
		documento.Status = r.statusConcorrente
		r.documentos[id] = documento
	}
	if documento.Status != statusAtual {
		return errors.NovoErroConflito("status do documento mudou")
	}
	documento.Status = novoStatus
	r.documentos[id] = documento
	return nil
}

func (r *repositorioInternoFake) DefinirCDM(ctx context.Context, id uuid.UUID, cdm json.RawMessage, statusAtual, novoStatus entity.Status) error {
	r.gravacoesCDM++
	if err := r.AtualizarStatus(ctx, id, statusAtual, novoStatus); err != nil {
		return err
	}
	documento := r.documentos[id]
	documento.CDM = append(json.RawMessage(nil), cdm...)
	r.documentos[id] = documento
	return nil
}

func exigirSemErro(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
}

func novoCenario(t *testing.T, status entity.Status) (*ServicoInterno, *repositorioInternoFake, entity.Documento) {
	t.Helper()
	dono, err := vo.NovoDonoSessao(uuid.New())
	exigirSemErro(t, err)
	documento, err := entity.NovoDocumento(dono, "artigo.docx", vo.FormatoDocx, 4096)
	exigirSemErro(t, err)
	documento.Status = status
	repo := &repositorioInternoFake{documentos: map[uuid.UUID]entity.Documento{documento.ID: documento}}
	servico, err := NovoServicoInterno(repo)
	exigirSemErro(t, err)
	return servico, repo, documento
}

func chamarOperacao(ctx context.Context, servico *ServicoInterno, operacao string, id uuid.UUID) (entity.Documento, error) {
	switch operacao {
	case "iniciar":
		return servico.IniciarAnalise(ctx, id)
	case "concluir":
		return servico.ConcluirAnalise(ctx, id, json.RawMessage(` {"blocos":[]} `))
	default:
		return servico.MarcarFalha(ctx, id)
	}
}

func TestNovoServicoInternoExigeRepositorio(t *testing.T) {
	t.Parallel()
	_, err := NovoServicoInterno(nil)
	var nulo *errors.ErroArgumentoNulo
	if !errors.Como(err, &nulo) || nulo.Argumento != "repositorio" {
		t.Fatalf("esperava repositório obrigatório: %v", err)
	}
}

func TestTransicoesInternasUsamCAS(t *testing.T) {
	t.Parallel()
	for _, caso := range []struct {
		nome, operacao string
		anterior, novo entity.Status
	}{
		{"iniciar recebido", "iniciar", entity.StatusRecebido, entity.StatusAnalisando},
		{"reiniciar falhou", "iniciar", entity.StatusFalhou, entity.StatusAnalisando},
		{"concluir análise", "concluir", entity.StatusAnalisando, entity.StatusAnalisado},
		{"falhar recebido", "falhar", entity.StatusRecebido, entity.StatusFalhou},
		{"falhar analisando", "falhar", entity.StatusAnalisando, entity.StatusFalhou},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			servico, repo, documento := novoCenario(t, caso.anterior)
			ctx, cancelar := context.WithCancel(context.Background())
			defer cancelar()
			obtido, err := chamarOperacao(ctx, servico, caso.operacao, documento.ID)
			exigirSemErro(t, err)
			persistido := repo.documentos[documento.ID]
			if obtido.Status != caso.novo || persistido.Status != caso.novo || repo.anterior != caso.anterior || repo.novo != caso.novo || repo.escritas != 1 || repo.leituras != 1 {
				t.Fatal("transição não usou o status lido no CAS")
			}
			for _, atual := range []entity.Documento{obtido, persistido} {
				copia := atual
				copia.Status = documento.Status
				copia.AtualizadoEm = documento.AtualizadoEm
				copia.CDM = documento.CDM
				if !reflect.DeepEqual(copia, documento) {
					t.Fatal("transição alterou identidade ou metadados do original")
				}
			}
			if caso.operacao == "concluir" {
				if repo.gravacoesCDM != 1 || string(obtido.CDM) != ` {"blocos":[]} ` || !reflect.DeepEqual(obtido.CDM, persistido.CDM) {
					t.Fatal("CDM não persistido junto ao status")
				}
			} else if repo.gravacoesCDM != 0 {
				t.Fatal("transição simples escreveu CDM")
			}
			for _, contexto := range repo.contextos {
				if contexto != ctx {
					t.Fatal("contexto não propagado")
				}
			}
			_, err = chamarOperacao(ctx, servico, caso.operacao, documento.ID)
			var conflito *errors.ErroConflito
			if !errors.Como(err, &conflito) || repo.escritas != 1 {
				t.Fatalf("repetição deve falhar antes de escrever: %v", err)
			}
		})
	}
}

func TestConcluirAnaliseValidaCDMNaEntidadeExistente(t *testing.T) {
	t.Parallel()
	for _, caso := range []struct {
		nome string
		cdm  json.RawMessage
	}{
		{"nulo", nil}, {"vazio", json.RawMessage{}}, {"branco", json.RawMessage(" \n\t")},
		{"json null", json.RawMessage(`null`)}, {"array", json.RawMessage(`[]`)},
		{"string", json.RawMessage(`"conteudo_privado"`)}, {"número", json.RawMessage(`42`)},
		{"booleano", json.RawMessage(`true`)}, {"objeto malformado", json.RawMessage(`{"conteudo_privado":`)},
		{"acima do teto", json.RawMessage(`{"texto":"` + strings.Repeat("x", entity.TamanhoMaximoCDMBytes) + `"}`)},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			servico, repo, documento := novoCenario(t, entity.StatusAnalisando)
			obtido, err := servico.ConcluirAnalise(context.Background(), documento.ID, caso.cdm)
			var invalido *errors.ErroValidacao
			if !errors.Como(err, &invalido) {
				t.Fatalf("esperava ErroValidacao: %v", err)
			}
			if len(invalido.Campos) == 0 || invalido.Campos[0].Campo != "cdm" {
				t.Fatal("validação deve apontar CDM")
			}
			if strings.Contains(err.Error(), "conteudo_privado") {
				t.Fatal("erro expôs conteúdo")
			}
			if repo.escritas != 0 || !reflect.DeepEqual(repo.documentos[documento.ID], documento) || !reflect.DeepEqual(obtido, entity.Documento{}) {
				t.Fatal("CDM inválido alterou documento ou foi devolvido")
			}
		})
	}
	// A ordem de validação vem da entidade: recebido + CDM inválido é conflito.
	servico, repo, documento := novoCenario(t, entity.StatusRecebido)
	_, err := servico.ConcluirAnalise(context.Background(), documento.ID, nil)
	var conflito *errors.ErroConflito
	if !errors.Como(err, &conflito) || repo.escritas != 0 {
		t.Fatalf("estado deve preceder validação de CDM: %v", err)
	}
}

func TestOperacoesInternasRejeitamIDVazioEInexistente(t *testing.T) {
	t.Parallel()
	for _, operacao := range []string{"iniciar", "concluir", "falhar"} {
		t.Run(operacao, func(t *testing.T) {
			servico, repo, _ := novoCenario(t, entity.StatusAnalisando)
			_, err := chamarOperacao(context.Background(), servico, operacao, uuid.Nil)
			var invalido *errors.ErroValidacao
			if !errors.Como(err, &invalido) || repo.leituras != 0 || repo.escritas != 0 {
				t.Fatalf("ID vazio deve falhar antes de I/O: %v", err)
			}
			obtido, err := chamarOperacao(context.Background(), servico, operacao, uuid.New())
			var ausente *errors.ErroNaoEncontrado
			if !errors.Como(err, &ausente) || repo.escritas != 0 || !reflect.DeepEqual(obtido, entity.Documento{}) {
				t.Fatalf("inexistente deve preservar ErroNaoEncontrado: %v", err)
			}
		})
	}
}

func TestCASConcorrenteNaoSobrescreveStatusNemCDM(t *testing.T) {
	t.Parallel()
	for _, operacao := range []string{"iniciar", "concluir", "falhar"} {
		t.Run(operacao, func(t *testing.T) {
			status := entity.StatusRecebido
			if operacao == "concluir" {
				status = entity.StatusAnalisando
			}
			servico, repo, documento := novoCenario(t, status)
			documento.CDM = json.RawMessage(`{"anterior":true}`)
			repo.documentos[documento.ID] = documento
			repo.statusConcorrente = entity.StatusFormatado
			obtido, err := chamarOperacao(context.Background(), servico, operacao, documento.ID)
			var conflito *errors.ErroConflito
			if !errors.Como(err, &conflito) {
				t.Fatalf("CAS deve propagar ErroConflito: %v", err)
			}
			esperado := documento
			esperado.Status = entity.StatusFormatado
			if repo.escritas != 1 || repo.anterior != status || !reflect.DeepEqual(repo.documentos[documento.ID], esperado) || !reflect.DeepEqual(obtido, entity.Documento{}) {
				t.Fatal("CAS perdido sobrescreveu documento ou anunciou sucesso")
			}
		})
	}
}

func TestServicoInternoPreservaFalhasTipadas(t *testing.T) {
	t.Parallel()
	for _, operacao := range []string{"iniciar", "concluir", "falhar"} {
		for _, escrita := range []bool{false, true} {
			for _, falha := range []error{errors.NovoErroConflito("corrida"), errors.NovoErroNaoEncontrado("documento"), errors.NovoErroAplicacao("indisponível"), context.Canceled} {
				status := entity.StatusRecebido
				if operacao == "concluir" {
					status = entity.StatusAnalisando
				}
				servico, repo, documento := novoCenario(t, status)
				if escrita {
					repo.erroEscrita = falha
				} else {
					repo.erroLeitura = falha
				}
				obtido, err := chamarOperacao(context.Background(), servico, operacao, documento.ID)
				if !errors.E(err, falha) {
					t.Fatalf("%s perdeu causa %T: %v", operacao, falha, err)
				}
				if !escrita && repo.escritas != 0 {
					t.Fatal("escrita após falha de leitura")
				}
				if !reflect.DeepEqual(repo.documentos[documento.ID], documento) || !reflect.DeepEqual(obtido, entity.Documento{}) {
					t.Fatal("falha alterou documento ou retornou sucesso parcial")
				}
			}
		}
	}
}
