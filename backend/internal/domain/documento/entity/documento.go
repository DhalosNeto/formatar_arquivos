package entity

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// TamanhoMaximoNome é o teto de RUNES do nome original guardado. O limite é em
// runes, e não em bytes, para que um nome acentuado não seja cortado no meio de
// um caractere e deixe de ser UTF-8 válido.
const TamanhoMaximoNome = 255

// TamanhoMaximoCDMBytes é o teto de bytes do CDM guardado no agregado. O CDM é
// um índice semântico (papel do bloco + ponteiro para o nó XML), não uma cópia
// do documento, então 5 MiB é folgado até para uma tese com milhares de blocos
// e ainda barra um payload que estouraria a coluna do banco e a memória do
// worker. O valor é uma escolha de capacidade, não de domínio: ajustar depois é
// trocar esta constante.
const TamanhoMaximoCDMBytes = 5 << 20

// Mensagens fixas: nunca ecoam o nome, o CDM nem qualquer conteúdo enviado pelo
// usuário (CLAUDE.md, regra 7).
const (
	mensagemDocumentoInvalido  = "documento inválido"
	mensagemDonoObrigatorio    = "o dono do documento é obrigatório"
	mensagemIDObrigatorio      = "o identificador do documento é obrigatório"
	mensagemNomeObrigatorio    = "o nome do arquivo é obrigatório"
	mensagemNomeNaoHigienizado = "o nome do arquivo não está na forma canônica"
	mensagemTamanhoInvalido    = "o tamanho do arquivo precisa ser maior que zero"
	mensagemFormatoInvalido    = "formato de arquivo não suportado"
	mensagemChaveNaoDerivada   = "a chave de storage não corresponde ao documento"
	mensagemChavePreviewInvali = "chave de preview inválida"
	mensagemStatusInvalido     = "o documento precisa estar recém-recebido"
	mensagemCriadoEmInvalido   = "a data de criação é obrigatória"
	mensagemAtualizadoInvalido = "a data de atualização é obrigatória"
	mensagemTransicaoInvalida  = "a transição de status solicitada não é permitida"
)

// Mensagens fixas de recusa do CDM. Descrevem a regra violada e jamais repetem
// trecho do conteúdo recebido.
const (
	mensagemCDMVazio     = "o cdm é obrigatório"
	mensagemCDMGrande    = "o cdm excede o tamanho máximo permitido"
	mensagemCDMNaoObjeto = "o cdm precisa ser um objeto json"
	mensagemCDMInvalido  = "o cdm precisa ser json válido"
)

// separadoresDeCaminho são os separadores aceitos por clientes unix e windows.
const separadoresDeCaminho = "/\\"

// Separadores de linha Unicode: não são controles nem formatação para o
// unicode, mas quebram linha em log e em nome de arquivo.
const (
	separadorLinhaUnicode     = '\u2028'
	separadorParagrafoUnicode = '\u2029'
)

// Documento é o agregado do arquivo enviado pelo usuário e do seu ciclo de
// vida até a entrega formatada.
type Documento struct {
	ID              uuid.UUID
	Dono            vo.Dono
	NomeOriginal    string
	Formato         vo.FormatoArquivo
	TamanhoBytes    int64
	ChaveStorage    vo.ChaveStorage
	ChaveStoragePDF *vo.ChaveStorage // nil enquanto não há preview gerado
	Status          Status
	CDM             json.RawMessage
	CriadoEm        time.Time
	AtualizadoEm    time.Time
}

// NovoDocumento cria um documento recém-recebido, já higienizado e validado.
func NovoDocumento(
	dono vo.Dono,
	nomeOriginal string,
	formato vo.FormatoArquivo,
	tamanhoBytes int64,
) (Documento, error) {
	nome := HigienizarNomeArquivo(nomeOriginal)

	invalidos := &errors.ErroValidacao{Mensagem: mensagemDocumentoInvalido}
	if dono.Vazio() {
		invalidos.Acrescentar("dono", mensagemDonoObrigatorio)
	}
	if nome == "" {
		invalidos.Acrescentar("nome_original", mensagemNomeObrigatorio)
	}
	if tamanhoBytes <= 0 {
		invalidos.Acrescentar("tamanho_bytes", mensagemTamanhoInvalido)
	}
	if !formato.Valido() {
		invalidos.Acrescentar("formato", mensagemFormatoInvalido)
	}
	if invalidos.TemCampos() {
		return Documento{}, invalidos
	}

	documentoID := uuid.New()

	// A chave deriva do ID e do formato, jamais do nome enviado: o nome é
	// entrada do usuário e não pode alcançar o espaço de chaves do storage.
	chave, err := vo.NovaChaveOriginal(documentoID, formato)
	if err != nil {
		return Documento{}, errors.Envolver(err, "montar chave do arquivo original")
	}

	agora := time.Now().UTC()
	return Documento{
		ID:           documentoID,
		Dono:         dono,
		NomeOriginal: nome,
		Formato:      formato,
		TamanhoBytes: tamanhoBytes,
		ChaveStorage: chave,
		Status:       StatusRecebido,
		CriadoEm:     agora,
		AtualizadoEm: agora,
	}, nil
}

// HigienizarNomeArquivo normaliza o nome recebido do cliente e é a fonte única
// da regra: quem precisar decidir se um nome é aceitável compara com o retorno
// desta função, em vez de manter um critério próprio que diverge da entidade.
//
// A ordem das etapas é parte da regra: remover os invisíveis ANTES de cortar o
// caminho impede que um caractere escondido mude qual é o último separador da
// string, e removê-los antes de aparar as bordas impede que um zero-width
// encubra um espaço final, que o TrimSpace então não enxergaria.
func HigienizarNomeArquivo(bruto string) string {
	semInvisiveis := removerInvisiveis(bruto)

	somenteArquivo := semInvisiveis
	if corte := strings.LastIndexAny(somenteArquivo, separadoresDeCaminho); corte >= 0 {
		somenteArquivo = somenteArquivo[corte+1:]
	}

	return truncarEmRunes(strings.TrimSpace(somenteArquivo), TamanhoMaximoNome)
}

// removerInvisiveis descarta o que não aparece em log nem em gerenciador de
// arquivos: controles C0, C1 e DEL (unicode.Cc), formatação como BOM,
// zero-width e override de direção (unicode.Cf) e os separadores de linha e de
// parágrafo do Unicode. Letra acentuada é categoria L e é preservada, assim
// como o espaço interno.
func removerInvisiveis(valor string) string {
	return strings.Map(func(caractere rune) rune {
		switch {
		case unicode.IsControl(caractere),
			unicode.Is(unicode.Cf, caractere),
			caractere == separadorLinhaUnicode,
			caractere == separadorParagrafoUnicode:
			return -1
		default:
			return caractere
		}
	}, valor)
}

// truncarEmRunes corta o texto em no máximo o número de runes indicado,
// preservando a validade UTF-8 do resultado.
func truncarEmRunes(valor string, maximo int) string {
	runes := []rune(valor)
	if len(runes) <= maximo {
		return valor
	}
	return string(runes[:maximo])
}

// ValidarCDM aprova apenas um objeto JSON: json.Valid sozinho aceitaria null,
// número, string, booleano e array, e nenhum deles é um índice semântico.
func ValidarCDM(cdm json.RawMessage) error {
	if len(cdm) == 0 {
		return errors.NovoErroValidacao("cdm", mensagemCDMVazio)
	}
	if len(cdm) > TamanhoMaximoCDMBytes {
		return errors.NovoErroValidacao("cdm", mensagemCDMGrande)
	}

	aparado := bytes.TrimSpace(cdm)
	if len(aparado) == 0 || aparado[0] != '{' {
		return errors.NovoErroValidacao("cdm", mensagemCDMNaoObjeto)
	}
	if !json.Valid(cdm) {
		return errors.NovoErroValidacao("cdm", mensagemCDMInvalido)
	}
	return nil
}

// Validar revalida os invariantes de um documento recém-registrado. Todos os
// campos do agregado são exportados, então um literal montado à mão chega até a
// persistência sem passar pelo construtor: este método é a barreira. Acumula
// todos os campos reprovados num único erro, em vez de parar no primeiro.
func (d Documento) Validar() error {
	invalidos := &errors.ErroValidacao{Mensagem: mensagemDocumentoInvalido}

	if d.ID == uuid.Nil {
		invalidos.Acrescentar("id", mensagemIDObrigatorio)
	}
	if d.Dono.Vazio() {
		invalidos.Acrescentar("dono", mensagemDonoObrigatorio)
	}
	d.validarNome(invalidos)
	if !d.Formato.Valido() {
		invalidos.Acrescentar("formato", mensagemFormatoInvalido)
	}
	if d.TamanhoBytes <= 0 {
		invalidos.Acrescentar("tamanho_bytes", mensagemTamanhoInvalido)
	}
	d.validarChaveStorage(invalidos)
	// A revalidação cobre o instante do registro, que é sempre o estado inicial;
	// documento em qualquer outra etapa do pipeline não passa por aqui.
	if d.Status != StatusRecebido {
		invalidos.Acrescentar("status", mensagemStatusInvalido)
	}
	if d.CriadoEm.IsZero() {
		invalidos.Acrescentar("criado_em", mensagemCriadoEmInvalido)
	}
	if d.AtualizadoEm.IsZero() {
		invalidos.Acrescentar("atualizado_em", mensagemAtualizadoInvalido)
	}
	// CDM ausente é o estado normal de um documento recém-recebido: só há o que
	// validar quando ele já foi preenchido.
	if d.CDM != nil {
		if ValidarCDM(d.CDM) != nil {
			invalidos.Acrescentar("cdm", mensagemCDMInvalido)
		}
	}

	if invalidos.TemCampos() {
		return invalidos
	}
	return nil
}

// validarNome exige a forma canônica: qualquer nome que a higienização mudaria
// é reprovado como está, em vez de ser silenciosamente corrigido aqui.
func (d Documento) validarNome(invalidos *errors.ErroValidacao) {
	if d.NomeOriginal == "" {
		invalidos.Acrescentar("nome_original", mensagemNomeObrigatorio)
		return
	}
	if d.NomeOriginal != HigienizarNomeArquivo(d.NomeOriginal) {
		invalidos.Acrescentar("nome_original", mensagemNomeNaoHigienizado)
	}
}

// validarChaveStorage recomputa a chave a partir do próprio ID e formato: a
// gramática canônica aceita o UUID de qualquer documento, então conferir só a
// boa formação deixaria um registro apontar para o arquivo de outro.
func (d Documento) validarChaveStorage(invalidos *errors.ErroValidacao) {
	esperada, err := vo.NovaChaveOriginal(d.ID, d.Formato)
	if err != nil || d.ChaveStorage != esperada {
		invalidos.Acrescentar("chave_storage", mensagemChaveNaoDerivada)
	}
}

// MIME devolve o tipo de conteúdo do documento.
func (d Documento) MIME() string { return d.Formato.MIME() }

// TemPreview informa se já existe PDF de pré-visualização associado.
func (d Documento) TemPreview() bool { return d.ChaveStoragePDF != nil }

// IniciarAnalise move o documento para a extração de estrutura.
func (d *Documento) IniciarAnalise() error { return d.transitarPara(StatusAnalisando) }

// ConcluirAnalise grava o CDM produzido e marca o documento como analisado.
//
// O estado é conferido ANTES do conteúdo e nada é mutado antes das duas
// verificações passarem: fora de ordem, quem chamou precisa ouvir "conflito de
// estado", e não um erro de validação que o faria reenviar outro CDM para
// sempre.
func (d *Documento) ConcluirAnalise(cdm json.RawMessage) error {
	if !d.Status.PodeTransitarPara(StatusAnalisado) {
		return errors.NovoErroConflito(mensagemTransicaoInvalida)
	}
	if err := ValidarCDM(cdm); err != nil {
		return err
	}

	d.Status = StatusAnalisado
	d.AtualizadoEm = time.Now().UTC()
	// Cópia defensiva: o chamador continua dono do slice que passou.
	d.CDM = append(json.RawMessage(nil), cdm...)
	return nil
}

// IniciarFormatacao move o documento para a aplicação do ruleset.
func (d *Documento) IniciarFormatacao() error { return d.transitarPara(StatusFormatando) }

// ConcluirFormatacao marca o documento como formatado e pronto para download.
func (d *Documento) ConcluirFormatacao() error { return d.transitarPara(StatusFormatado) }

// MarcarFalha interrompe o processamento por erro, preservando o reprocessamento.
func (d *Documento) MarcarFalha() error { return d.transitarPara(StatusFalhou) }

// transitarPara aplica a mudança de status. Quando a transição é recusada,
// nada é mutado: o documento continua exatamente como estava.
func (d *Documento) transitarPara(novo Status) error {
	if !d.Status.PodeTransitarPara(novo) {
		return errors.NovoErroConflito(mensagemTransicaoInvalida)
	}
	d.Status = novo
	d.AtualizadoEm = time.Now().UTC()
	return nil
}

// DefinirPreviewPDF associa o PDF de pré-visualização ao documento. Gerar
// preview não é etapa do ciclo de vida, então o status permanece inalterado.
//
// A chave precisa ser exatamente a derivada deste documento: a gramática
// canônica aceita o UUID de qualquer um, e validar só a boa formação deixaria
// um documento apontar para o preview de outro.
func (d *Documento) DefinirPreviewPDF(chave vo.ChaveStorage) error {
	esperada, err := vo.NovaChavePreviewPDF(d.ID)
	if err != nil || chave != esperada {
		return errors.NovoErroValidacao("chave_storage_pdf", mensagemChavePreviewInvali)
	}
	d.ChaveStoragePDF = &chave
	d.AtualizadoEm = time.Now().UTC()
	return nil
}
