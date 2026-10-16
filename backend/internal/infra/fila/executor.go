package fila

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"

	"github.com/google/uuid"

	"github.com/daniel-halos/formatador/internal/domain/cdm"
	documentoentity "github.com/daniel-halos/formatador/internal/domain/documento/entity"
	"github.com/daniel-halos/formatador/internal/domain/documento/processamento"
	documentoservice "github.com/daniel-halos/formatador/internal/domain/documento/service"
	"github.com/daniel-halos/formatador/internal/domain/formatador"
	"github.com/daniel-halos/formatador/internal/domain/job/entity"
	"github.com/daniel-halos/formatador/internal/domain/ruleset"
	rulesetrepo "github.com/daniel-halos/formatador/internal/domain/ruleset/repository"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/daniel-halos/formatador/internal/infra/ooxml"
)

// ArmazenadorObjetos é a porta de storage usada pelo executor de documento.
// Implementada em produção por infra/storage.ClienteS3.
type ArmazenadorObjetos interface {
	Obter(ctx context.Context, chave vo.ChaveStorage) (io.ReadCloser, error)
	Salvar(ctx context.Context, chave vo.ChaveStorage, conteudo io.Reader, tamanho int64, contentType string) error
}

// ConversorPDF é a porta de conversão DOCX->PDF usada pelo executor de
// documento. Implementada em produção por infra/pdfconv.Cliente.
type ConversorPDF interface {
	ConverterParaPDF(ctx context.Context, docx []byte) ([]byte, error)
}

// resultadoRenderizarPreview é o único dado persistido no job para o tipo
// renderizar_preview: a chave do preview gerado, nunca o conteúdo do
// documento (CLAUDE.md, regra 7).
type resultadoRenderizarPreview struct {
	ChavePreviewPDF string `json:"chave_preview_pdf"`
}

// resultadoAnalisar é o que fica em jobs.resultado depois de uma análise:
// quantos blocos foram classificados, e nada além disso. O CDM completo —
// que carrega trecho do documento em TextoResumo — é gravado no DOCUMENTO
// por ConcluirAnalise, onde a regra de acesso por dono se aplica. Repeti-lo
// aqui o espalharia para uma tabela com outra fronteira de acesso.
type resultadoAnalisar struct {
	Blocos int `json:"blocos"`
}

// resultadoFormatar é o que fica em jobs.resultado depois de uma formatação:
// onde o artefato ficou e sob qual perfil, nunca conteúdo do documento
// (CLAUDE.md, regra 7). Os nomes seguem o precedente de "chave_preview_pdf".
type resultadoFormatar struct {
	ChaveFormatado string `json:"chave_formatado"`
	RulesetSlug    string `json:"ruleset_slug"`
	RulesetVersao  int    `json:"ruleset_versao"`
}

// Mensagens fixas das recusas do branch de formatação: nunca citam conteúdo
// nem metadado do documento do usuário (CLAUDE.md, regra 7).
const (
	msgRulesetObrigatorio   = "job de formatação exige um ruleset"
	msgFormatoNaoFormatavel = "só documento docx pode ser formatado"
	msgStatusNaoFormatav    = "documento precisa estar analisado ou formatado para ser formatado"
	msgCDMAusente           = "documento precisa ter cdm para ser formatado"
	msgPerfilInativo        = "perfil de formatação fora de circulação"
	msgCDMCorrompido        = "cdm do documento ilegível"
	msgOriginalAcimaDoTeto  = "arquivo original acima do tamanho máximo"
)

// ExecutorDocumento roda o trabalho de fato dos jobs de documento para o
// laço da fila. Sabe renderizar_preview (DOCX -> PDF), analisar (DOCX ->
// CDM) e formatar (DOCX -> DOCX formatado); qualquer outro tipo falha com
// erro claro, nunca é ignorado silenciosamente.
type ExecutorDocumento struct {
	documentos  *processamento.ServicoInterno
	armazenador ArmazenadorObjetos
	conversor   ConversorPDF
	rulesets    rulesetrepo.ConsultaRulesetRepo
	registrador *slog.Logger
	fallback    *cdm.Fallback
}

// NovoExecutorDocumento monta o executor de jobs de documento.
//
// rulesets é a porta do DOMÍNIO, e não contracts.ConsultaRulesetRepo: nenhum
// pacote de infra importa data/contracts em produção. Como o contrato é só um
// alias, cmd/worker segue passando gerenciador.RulesetsConsulta().
func NovoExecutorDocumento(
	documentos *processamento.ServicoInterno,
	armazenador ArmazenadorObjetos,
	conversor ConversorPDF,
	rulesets rulesetrepo.ConsultaRulesetRepo,
	registrador *slog.Logger,
	fallbacks ...*cdm.Fallback,
) (*ExecutorDocumento, error) {
	if len(fallbacks) > 1 {
		return nil, errors.NovoErroValidacao("fallback", "no máximo um fallback pode ser configurado")
	}
	if documentos == nil {
		return nil, errors.NovoErroArgumentoNulo("documentos")
	}
	if armazenador == nil {
		return nil, errors.NovoErroArgumentoNulo("armazenador")
	}
	if conversor == nil {
		return nil, errors.NovoErroArgumentoNulo("conversor")
	}
	if rulesets == nil {
		return nil, errors.NovoErroArgumentoNulo("rulesets")
	}
	if registrador == nil {
		return nil, errors.NovoErroArgumentoNulo("registrador")
	}
	var fallback *cdm.Fallback
	if len(fallbacks) == 1 {
		fallback = fallbacks[0]
	}
	return &ExecutorDocumento{
		documentos:  documentos,
		armazenador: armazenador,
		conversor:   conversor,
		rulesets:    rulesets,
		registrador: registrador,
		fallback:    fallback,
	}, nil
}

// Executar despacha o job pelo tipo. Tipo desconhecido ou ainda não
// suportado pelo worker devolve erro, nunca sucesso silencioso.
func (e *ExecutorDocumento) Executar(ctx context.Context, job entity.Job) (json.RawMessage, error) {
	switch job.Tipo {
	case entity.TipoRenderizarPreview:
		return e.renderizarPreview(ctx, job)
	case entity.TipoAnalisar:
		return e.analisar(ctx, job)
	case entity.TipoFormatar:
		return e.formatar(ctx, job)
	}
	return nil, errors.NovoErroAplicacao("tipo de job não suportado pelo worker: " + job.Tipo.String())
}

// analisar extrai a estrutura do DOCX e grava o CDM no documento.
//
// Após iniciar a análise, tenta persistir o estado de falha quando a extração
// falha. Se a persistência também falhar, o documento pode ficar em analisando;
// o erro retornado preserva ambas as causas.
func (e *ExecutorDocumento) analisar(ctx context.Context, job entity.Job) (json.RawMessage, error) {
	if _, err := e.documentos.IniciarAnalise(ctx, job.DocumentoID); err != nil {
		// Ainda não saiu de `recebido`: não há do que marcar falha, e
		// MarcarFalha aqui mascararia uma disputa entre dois workers.
		return nil, errors.Envolver(err, "iniciar análise do documento")
	}

	resultado, err := e.extrairCDM(ctx, job)
	if err != nil {
		return nil, e.marcarFalha(ctx, job.DocumentoID, err)
	}
	return resultado, nil
}

// extrairCDM é o caminho feliz isolado, para que analisar tenha um único
// ponto de tratamento de falha em vez de repetir marcarFalha em cada etapa.
func (e *ExecutorDocumento) extrairCDM(ctx context.Context, job entity.Job) (json.RawMessage, error) {
	documento, err := e.documentos.ObterPorIDInterno(ctx, job.DocumentoID)
	if err != nil {
		return nil, errors.Envolver(err, "obter documento para analisar")
	}

	conteudo, err := e.baixarOriginal(ctx, documento.ChaveStorage)
	if err != nil {
		return nil, err
	}

	blocos, err := ooxml.AnalisarEstrutura(conteudo)
	if err != nil {
		// Sem Envolver: a mensagem de ooxml é fixa e não cita conteúdo, mas
		// encadear contexto aqui também não acrescentaria nada útil ao log.
		return nil, errors.Envolver(err, "analisar estrutura do documento")
	}
	if e.fallback != nil {
		indice, err := e.fallback.Aplicar(ctx, blocos)
		if err != nil {
			return nil, errors.Envolver(err, "aplicar fallback de estrutura")
		}
		indiceJSON, err := indice.Serializar()
		if err != nil {
			return nil, errors.Envolver(err, "serializar cdm com fallback")
		}
		return e.persistirIndiceAnalisado(ctx, job, indice, indiceJSON)
	}

	indice, err := cdm.NovoIndice(blocos).Serializar()
	if err != nil {
		return nil, errors.Envolver(err, "serializar cdm do documento")
	}

	return e.persistirIndiceAnalisado(ctx, job, cdm.NovoIndice(blocos), indice)
}

func (e *ExecutorDocumento) persistirIndiceAnalisado(ctx context.Context, job entity.Job, indice cdm.Indice, dados []byte) (json.RawMessage, error) {
	if _, err := e.documentos.ConcluirAnalise(ctx, job.DocumentoID, dados); err != nil {
		return nil, errors.Envolver(err, "gravar cdm do documento")
	}
	resultado, err := json.Marshal(resultadoAnalisar{Blocos: len(indice.Blocos)})
	if err != nil {
		return nil, errors.Envolver(err, "montar resultado do job de análise")
	}
	return resultado, nil
}

func (e *ExecutorDocumento) marcarFalha(ctx context.Context, documentoID uuid.UUID, original error) error {
	if _, err := e.documentos.MarcarFalha(ctx, documentoID); err != nil {
		e.registrador.ErrorContext(ctx, "falha ao persistir estado de falha do documento", "documento_id", documentoID.String())
		return errors.NovoErroPersistirFalha(original, err)
	}
	return original
}

// baixarOriginal lê o arquivo original do documento do storage, com TETO: o
// limite do upload é aplicado na ingestão, nunca na leitura, e um objeto maior
// por qualquer motivo — limite alterado, gravação fora da porta, bucket
// compartilhado — viraria OOM do worker. A guarda fica aqui, na função
// compartilhada pelos três chamadores (analisar, renderizarPreview, formatar),
// em vez de uma cópia por chamador.
//
// ACOPLAMENTO: documentoservice.TamanhoMaximoPadraoBytes é um DEFAULT, e
// documento/service.NovoServico aceita limite próprio. Se a ingestão for
// configurada com teto MAIOR, este worker passa a recusar objeto VÁLIDO, e
// derruba analisar e renderizarPreview junto com formatar. É o preço de
// consertar na raiz, e precisa ficar escrito.
func (e *ExecutorDocumento) baixarOriginal(ctx context.Context, chave vo.ChaveStorage) ([]byte, error) {
	original, err := e.armazenador.Obter(ctx, chave)
	if err != nil {
		return nil, errors.Envolver(err, "obter arquivo original do documento")
	}
	defer func() { _ = original.Close() }()

	limite := documentoservice.TamanhoMaximoPadraoBytes
	conteudo, err := io.ReadAll(io.LimitReader(original, limite+1))
	if err != nil {
		return nil, errors.Envolver(err, "ler arquivo original do documento")
	}
	if int64(len(conteudo)) > limite {
		return nil, errors.NovoErroValidacao("arquivo", msgOriginalAcimaDoTeto)
	}
	return conteudo, nil
}

// formatar aplica o perfil do job ao DOCX original e grava o artefato.
//
// PRINCÍPIO DE ATRIBUIÇÃO DE FALHA, que é o que determina a ordem dos passos:
// falha atribuível ao DOCUMENTO leva o documento a falhou; falha atribuível ao
// PERFIL ou ao PEDIDO não transita o documento.
//
// INFRAESTRUTURA depende de ONDE ela falha, e não é neutra: antes da transição
// do passo (4) — passos (2) e (3) — nada transita, porque o documento ainda
// está em analisado/formatado. DEPOIS dela, infra TRANSITA: um blip de storage
// em baixarOriginal, ou falha de banco em ConcluirFormatacao, cai no
// marcarFalha abaixo e leva o documento a falhou. É deliberado — a essa altura
// o documento já está em formatando, e deixá-lo preso lá seria pior.
//
// AGRAVANTE a registrar: sair de falhou exige REANÁLISE. A entidade permitiria
// falhou -> formatando (entity/status.go:48), mas
// recusarPreCondicoesDeFormatacao só aceita analisado/formatado, então um blip
// de storage custa ao usuário um ciclo de análise inteiro.
func (e *ExecutorDocumento) formatar(ctx context.Context, job entity.Job) (json.RawMessage, error) {
	documento, err := e.documentos.ObterPorIDInterno(ctx, job.DocumentoID)
	if err != nil {
		return nil, errors.Envolver(err, "obter documento para formatar")
	}
	if err := recusarPreCondicoesDeFormatacao(job, documento); err != nil {
		return nil, err
	}

	// Passo (3): ANTES de qualquer transição. Perfil retirado de circulação e
	// catálogo indisponível não são atribuíveis ao documento, então nenhum dos
	// dois pode levá-lo a falhou — senão retirar um perfil do catálogo
	// derrubaria em lote todo documento com job pendente.
	definicao, ativo, err := e.rulesets.ObterPorID(ctx, *job.RulesetID)
	if err != nil {
		return nil, errors.Envolver(err, "obter perfil do job de formatação")
	}
	if !ativo {
		return nil, errors.NovoErroConflito(msgPerfilInativo)
	}

	// O retorno de IniciarFormatacao SUBSTITUI o documento lido no passo (1):
	// transitarStatus relê o agregado antes de transitar, então este é o CDM
	// vigente. Descartá-lo abriria a janela em que um job analisar concorrente
	// conclui entre as duas leituras, regrava o CDM e devolve o status a
	// analisado — o CAS não vê diferença alguma (analisado dos dois lados) e o
	// artefato sairia do CDM velho, sobrescrevendo a chave canônica do perfil.
	//
	// LACUNA CONHECIDA, não coberta por teste: aplicarFormatacao também usa
	// documento.ChaveStorage e documento.ID. Hoje nenhum dos dois muda depois
	// da criação, logo não há cenário observável; se algum dia existir
	// re-upload que regrave ChaveStorage, o defeito ressurge por esse caminho e
	// o teste de CDM obsoleto não o pega.
	documento, err = e.documentos.IniciarFormatacao(ctx, job.DocumentoID)
	if err != nil {
		// Sem MarcarFalha, como em analisar: formatando -> falhou É transição
		// válida, então um segundo worker que perdesse o CAS mataria a
		// execução viva do primeiro, cujo ConcluirFormatacao falharia depois,
		// deixando o documento em falhou COM o artefato já salvo.
		return nil, errors.Envolver(err, "iniciar formatação do documento")
	}

	resultado, err := e.aplicarFormatacao(ctx, job, documento, definicao)
	if err != nil {
		return nil, e.marcarFalha(ctx, job.DocumentoID, err)
	}
	return resultado, nil
}

// recusarPreCondicoesDeFormatacao é o passo (2): recusa SEM transitar o
// documento. Aceitar StatusFormatado é deliberado — transicoesValidas o
// permite, e exigir só analisado obrigaria reanálise para trocar de perfil.
//
// A guarda de RulesetID nil é defesa em profundidade e NÃO é alcançável em
// produção, porque o CHECK jobs_ruleset_obrigatorio_para_formatar já a impede.
// Declarada para ninguém a remover por "inalcançável".
func recusarPreCondicoesDeFormatacao(job entity.Job, documento documentoentity.Documento) error {
	if job.RulesetID == nil {
		return errors.NovoErroValidacao("ruleset_id", msgRulesetObrigatorio)
	}
	// Sem esta guarda, um documento pdf ou latex em analisado produziria uma
	// chave formatado-*.docx com bytes que não são docx.
	if documento.Formato != vo.FormatoDocx {
		return errors.NovoErroValidacao("formato", msgFormatoNaoFormatavel)
	}
	if documento.Status != documentoentity.StatusAnalisado && documento.Status != documentoentity.StatusFormatado {
		return errors.NovoErroConflito(msgStatusNaoFormatav)
	}
	if len(documento.CDM) == 0 {
		return errors.NovoErroConflito(msgCDMAusente)
	}
	return nil
}

// aplicarFormatacao é o caminho feliz isolado, para que formatar tenha um
// único ponto de tratamento de falha — o mesmo arranjo de analisar/extrairCDM.
func (e *ExecutorDocumento) aplicarFormatacao(
	ctx context.Context,
	job entity.Job,
	documento documentoentity.Documento,
	definicao ruleset.Definicao,
) (json.RawMessage, error) {
	indice, err := cdm.Desserializar(documento.CDM)
	if err != nil {
		// RECLASSIFICAÇÃO sem encadear: o CDM foi gravado pelo próprio
		// servidor, então corrupção aqui é 500, não 400. errors.Envolver não
		// reclassifica e errors.Como acharia o *ErroValidacao de Desserializar.
		return nil, errors.NovoErroAplicacao(msgCDMCorrompido)
	}
	plano, err := formatador.Planejar(indice, definicao)
	if err != nil {
		return nil, errors.Envolver(err, "planejar formatação do documento")
	}

	conteudo, err := e.baixarOriginal(ctx, documento.ChaveStorage)
	if err != nil {
		return nil, err
	}

	pacote, err := ooxml.Abrir(bytes.NewReader(conteudo), int64(len(conteudo)))
	if err != nil {
		return nil, errors.Envolver(err, "abrir pacote docx do documento")
	}
	// MargensComplementares ZERO de propósito: ruleset.Pagina não tem
	// cabeçalho, rodapé nem medianiz, e inventar medida de cabeçalho seria
	// publicar valor normativo sem fonte.
	//
	// Em documento que JÁ tem pgMar o zero é inerte: AplicarPagina só usa esses
	// valores quando o atributo não existe. Mas quando pgMar — ou o sectPr
	// inteiro — está AUSENTE, o mutador MATERIALIZA header="0" footer="0"
	// gutter="0" (ooxml/pagina_xml.go:631 e :770). Nesse caso o zero é, sim,
	// valor inventado: o DOCX sai com cabeçalho e rodapé na borda do papel,
	// onde Word e LibreOffice escrevem ~708-720 twips.
	//
	// Mantido assim: a spec fixou o zero e CT_PageMar exige os atributos, logo
	// um pacote sem pgMar já é não conformidade antes de passar por aqui.
	if err := pacote.AplicarPlano(plano, ooxml.MargensComplementares{}); err != nil {
		return nil, errors.Envolver(err, "aplicar plano de formatação")
	}
	var formatado bytes.Buffer
	if err := pacote.Salvar(&formatado); err != nil {
		return nil, errors.Envolver(err, "gravar pacote docx formatado")
	}

	chave, err := vo.NovaChaveFormatado(documento.ID, *job.RulesetID)
	if err != nil {
		return nil, errors.Envolver(err, "montar chave do documento formatado")
	}
	if err := e.armazenador.Salvar(ctx, chave, bytes.NewReader(formatado.Bytes()), int64(formatado.Len()), vo.MIMEDocx); err != nil {
		return nil, errors.Envolver(err, "salvar documento formatado")
	}

	// Único ponto de inconsistência storage-banco do fluxo: se concluir
	// falhar, o artefato fica órfão no storage (Reenfileirar zera o Resultado
	// do job), mas o documento NUNCA fica em formatado.
	if _, err := e.documentos.ConcluirFormatacao(ctx, job.DocumentoID); err != nil {
		return nil, errors.Envolver(err, "concluir formatação do documento")
	}

	resultado, err := json.Marshal(resultadoFormatar{
		ChaveFormatado: chave.String(),
		RulesetSlug:    definicao.Slug,
		RulesetVersao:  definicao.Versao,
	})
	if err != nil {
		return nil, errors.Envolver(err, "montar resultado do job de formatação")
	}
	return resultado, nil
}

// renderizarPreview baixa o original do storage, converte para PDF e grava o
// preview de volta no storage. Só a chave do resultado vai para o job — a
// gravação da chave no agregado de documento fica para um passo seguinte,
// fora do escopo desta entrega (ver relato do codador).
func (e *ExecutorDocumento) renderizarPreview(ctx context.Context, job entity.Job) (json.RawMessage, error) {
	documento, err := e.documentos.ObterPorIDInterno(ctx, job.DocumentoID)
	if err != nil {
		return nil, errors.Envolver(err, "obter documento para renderizar preview")
	}

	conteudo, err := e.baixarOriginal(ctx, documento.ChaveStorage)
	if err != nil {
		return nil, err
	}

	pdf, err := e.conversor.ConverterParaPDF(ctx, conteudo)
	if err != nil {
		return nil, errors.Envolver(err, "converter documento em pdf")
	}

	chave, err := vo.NovaChavePreviewPDF(documento.ID)
	if err != nil {
		return nil, errors.Envolver(err, "montar chave do preview")
	}

	if err := e.armazenador.Salvar(ctx, chave, bytes.NewReader(pdf), int64(len(pdf)), vo.MIMEPDF); err != nil {
		return nil, errors.Envolver(err, "salvar preview em pdf")
	}

	resultado, err := json.Marshal(resultadoRenderizarPreview{ChavePreviewPDF: chave.String()})
	if err != nil {
		return nil, errors.Envolver(err, "montar resultado do job de preview")
	}
	return resultado, nil
}

var _ Executor = (*ExecutorDocumento)(nil)
