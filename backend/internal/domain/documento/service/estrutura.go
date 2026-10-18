package service

import (
	"context"
	"github.com/daniel-halos/formatador/internal/domain/cdm"
	"github.com/daniel-halos/formatador/internal/domain/documento/entity"
	"github.com/daniel-halos/formatador/internal/domain/documento/repository"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/google/uuid"
)

// ServicoEstrutura corrige a classificação manual de um bloco analisado.
type ServicoEstrutura struct{ repositorio repository.EstruturaRepo }

// NovoServicoEstrutura monta o serviço de correção de estrutura.
func NovoServicoEstrutura(repo repository.EstruturaRepo) (*ServicoEstrutura, error) {
	if repo == nil {
		return nil, errors.NovoErroArgumentoNulo("repositorio")
	}
	return &ServicoEstrutura{repositorio: repo}, nil
}

// Corrigir troca o papel de um bloco do CDM por decisão do usuário.
//
// A correção grava OrigemUsuario com confiança 1: a partir daí Reclassificar
// bloqueia qualquer camada automática de sobrescrever esse bloco. É a única
// entrada do sistema que produz essa origem.
//
// Exige o documento em StatusAnalisado — corrigir estrutura de documento que
// ainda está sendo analisado disputaria a escrita com o worker.
func (s *ServicoEstrutura) Corrigir(ctx context.Context, dono vo.Dono, id uuid.UUID, refXML int, papel cdm.Papel) (cdm.Indice, error) {
	if err := validarDono(dono); err != nil {
		return cdm.Indice{}, err
	}
	if id == uuid.Nil {
		return cdm.Indice{}, errors.NovoErroValidacao(campoID, mensagemIDObrigatorio)
	}
	if refXML < 0 {
		return cdm.Indice{}, errors.NovoErroValidacao("ref_xml", "a referência ao bloco não pode ser negativa")
	}
	if !papel.Valido() {
		return cdm.Indice{}, errors.NovoErroValidacao("papel", "papel desconhecido ou seção fora da faixa 1..6")
	}
	// Transição PARA Tabela, recusada ANTES de qualquer I/O: cdm.Tabela
	// significa "este bloco é um w:tbl", e nenhum escritor de Papel o produz
	// para outra forma de nó — invariante sustentada por construção, não
	// verificada aqui. Marcar o ordinal de um w:p como Tabela tiraria o bloco
	// de ReferenciasCorpo e da formatação, em silêncio.
	if papel == cdm.Tabela {
		return cdm.Indice{}, erroPapelTabela()
	}
	documento, err := s.repositorio.ObterPorID(ctx, dono, id)
	if err != nil {
		return cdm.Indice{}, errors.Envolver(err, "obter documento para corrigir estrutura")
	}
	if documento.ID != id || !dono.PodeAcessar(documento.Dono) {
		return cdm.Indice{}, errors.NovoErroNaoEncontrado("documento")
	}
	if documento.Status != entity.StatusAnalisado {
		return cdm.Indice{}, errors.NovoErroConflito("o documento precisa estar analisado para corrigir a estrutura")
	}
	if len(documento.CDM) == 0 {
		return cdm.Indice{}, errors.NovoErroConflito("o documento ainda não possui estrutura")
	}
	indice, err := cdm.Desserializar(documento.CDM)
	if err != nil {
		return cdm.Indice{}, cdm.ErroIndiceCorrompido(err)
	}
	referencias := make(map[int]struct{}, len(indice.Blocos))
	alvo := -1
	for posicao, bloco := range indice.Blocos {
		if _, existe := referencias[bloco.RefXML]; existe {
			return cdm.Indice{}, cdm.ErroIndiceCorrompido(cdm.ErroRefXMLDuplicado)
		}
		referencias[bloco.RefXML] = struct{}{}
		if bloco.RefXML == refXML {
			alvo = posicao
		}
	}
	if alvo < 0 {
		return cdm.Indice{}, errors.NovoErroValidacao("ref_xml", "a referência não corresponde a um bloco da estrutura")
	}
	// Transição DE Tabela, recusada depois de localizar o alvo porque é o
	// bloco gravado que decide, não o pedido. Tirar o papel Tabela de um w:tbl
	// o levaria a ReferenciasCorpo, e o primeiro mutador recusaria o alvo que
	// não é w:p — falhando o job e travando o par (documento, perfil).
	if indice.Blocos[alvo].Papel == cdm.Tabela {
		return cdm.Indice{}, erroPapelTabela()
	}
	indice.Blocos[alvo], err = indice.Blocos[alvo].Reclassificar(papel, 1, cdm.OrigemUsuario)
	if err != nil {
		return cdm.Indice{}, errors.Envolver(err, "reclassificar bloco")
	}
	revisoes := indice.Revisoes[:0]
	for _, revisao := range indice.Revisoes {
		if revisao.RefXML != refXML {
			revisoes = append(revisoes, revisao)
		}
	}
	indice.Revisoes = revisoes
	novo, err := indice.Serializar()
	if err != nil {
		return cdm.Indice{}, errors.Envolver(err, "serializar estrutura corrigida")
	}
	if err = s.repositorio.SalvarEstrutura(ctx, dono, id, documento.CDM, novo, documento.Status); err != nil {
		return cdm.Indice{}, errors.Envolver(err, "salvar estrutura corrigida")
	}
	return indice, nil
}

// erroPapelTabela é a recusa das duas transições de C5. Uma função só porque a
// mensagem é a mesma e o campo precisa continuar sendo "papel" nos dois lados.
func erroPapelTabela() error {
	return errors.NovoErroValidacao("papel", "o papel tabela acompanha a forma do bloco e não pode ser corrigido")
}
