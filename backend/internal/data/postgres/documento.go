package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	documentoentity "github.com/daniel-halos/formatador/internal/domain/documento/entity"
	documentorepo "github.com/daniel-halos/formatador/internal/domain/documento/repository"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// colunasDocumento é a lista de colunas em comum a todas as leituras de
// documento, na ordem esperada por scanDocumento.
const colunasDocumento = "id, usuario_id, sessao_id, nome_original, mime, tamanho_bytes, " +
	"chave_storage, chave_storage_pdf, status, cdm, criado_em, atualizado_em"

// RepositorioDocumento implementa documentorepo.DocumentoRepo e
// documentorepo.DocumentoInternoRepo sobre o mesmo pool: a distinção entre as
// duas portas é só de autorização, decidida em cada query, não de tipo Go.
type RepositorioDocumento struct {
	pool *pgxpool.Pool
}

// NovoRepositorioDocumento cria o repositório de documentos.
func NovoRepositorioDocumento(pool *pgxpool.Pool) *RepositorioDocumento {
	return &RepositorioDocumento{pool: pool}
}

// Inserir grava um documento recém-recebido.
func (r *RepositorioDocumento) Inserir(ctx context.Context, documento documentoentity.Documento) error {
	usuarioID, sessaoID := documento.Dono.ParaColunas()
	var chaveStoragePDF *string
	if documento.ChaveStoragePDF != nil {
		valor := documento.ChaveStoragePDF.String()
		chaveStoragePDF = &valor
	}

	_, err := r.pool.Exec(ctx, `INSERT INTO documentos (
		id, usuario_id, sessao_id, nome_original, mime, tamanho_bytes,
		chave_storage, chave_storage_pdf, status, cdm, criado_em, atualizado_em
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		documento.ID, usuarioID, sessaoID, documento.NomeOriginal, documento.Formato.MIME(), documento.TamanhoBytes,
		documento.ChaveStorage.String(), chaveStoragePDF, string(documento.Status), cdmOuNulo(documento.CDM),
		documento.CriadoEm, documento.AtualizadoEm)
	if err != nil {
		return envolverPostgres(err, "inserir documento")
	}
	return nil
}

// ObterPorID restringe a consulta ao solicitante, sem distinguir terceiros de ausentes.
func (r *RepositorioDocumento) ObterPorID(ctx context.Context, solicitante vo.Dono, id uuid.UUID) (documentoentity.Documento, error) {
	usuarioID, sessaoID := solicitante.ParaColunas()
	linha := r.pool.QueryRow(ctx,
		`SELECT `+colunasDocumento+` FROM documentos WHERE id = $1 AND (usuario_id = $2 OR sessao_id = $3)`,
		id, usuarioID, sessaoID)
	documento, err := scanDocumento(linha)
	if err != nil {
		if errors.E(err, pgx.ErrNoRows) {
			return documentoentity.Documento{}, errors.NovoErroNaoEncontrado("documento")
		}
		return documentoentity.Documento{}, err
	}
	return documento, nil
}

// ListarPorDono devolve apenas os documentos do solicitante, dos mais recentes aos antigos.
func (r *RepositorioDocumento) ListarPorDono(ctx context.Context, solicitante vo.Dono, limite, deslocamento int) ([]documentoentity.Documento, error) {
	usuarioID, sessaoID := solicitante.ParaColunas()
	linhas, err := r.pool.Query(ctx,
		`SELECT `+colunasDocumento+` FROM documentos WHERE usuario_id = $1 OR sessao_id = $2 ORDER BY criado_em DESC, id DESC LIMIT $3 OFFSET $4`,
		usuarioID, sessaoID, limite, deslocamento)
	if err != nil {
		return nil, envolverPostgres(err, "listar documentos por dono")
	}
	defer linhas.Close()

	documentos := make([]documentoentity.Documento, 0, limite)
	for linhas.Next() {
		documento, err := scanDocumento(linhas)
		if err != nil {
			return nil, err
		}
		documentos = append(documentos, documento)
	}
	if err := linhas.Err(); err != nil {
		return nil, envolverPostgres(err, "listar documentos por dono")
	}
	return documentos, nil
}

// DefinirChavePreviewPDF restringe a atualização ao solicitante.
func (r *RepositorioDocumento) DefinirChavePreviewPDF(ctx context.Context, solicitante vo.Dono, id uuid.UUID, chave vo.ChaveStorage) error {
	usuarioID, sessaoID := solicitante.ParaColunas()
	marca, err := r.pool.Exec(ctx,
		`UPDATE documentos SET chave_storage_pdf = $4, atualizado_em = now() WHERE id = $1 AND (usuario_id = $2 OR sessao_id = $3)`,
		id, usuarioID, sessaoID, chave.String())
	if err != nil {
		return envolverPostgres(err, "definir chave de preview")
	}
	if marca.RowsAffected() == 0 {
		return errors.NovoErroNaoEncontrado("documento")
	}
	return nil
}

// ObterPorIDInterno é a porta exclusiva do worker: acha documento de
// qualquer dono, sem restrição de autorização.
func (r *RepositorioDocumento) ObterPorIDInterno(ctx context.Context, id uuid.UUID) (documentoentity.Documento, error) {
	linha := r.pool.QueryRow(ctx, `SELECT `+colunasDocumento+` FROM documentos WHERE id = $1`, id)
	documento, err := scanDocumento(linha)
	if err != nil {
		if errors.E(err, pgx.ErrNoRows) {
			return documentoentity.Documento{}, errors.NovoErroNaoEncontrado("documento")
		}
		return documentoentity.Documento{}, err
	}
	return documento, nil
}

// AtualizarStatus muda o status apenas se o documento ainda estiver em
// statusAtual, comparando e escrevendo na mesma instrução para que dois
// workers concorrentes não avancem o mesmo documento duas vezes.
func (r *RepositorioDocumento) AtualizarStatus(ctx context.Context, id uuid.UUID, statusAtual, novoStatus documentoentity.Status) error {
	if !statusAtual.Valido() || !novoStatus.Valido() {
		return errors.NovoErroValidacao("status", "status inválido")
	}
	marca, err := r.pool.Exec(ctx,
		`UPDATE documentos SET status = $3, atualizado_em = now() WHERE id = $1 AND status = $2`,
		id, string(statusAtual), string(novoStatus))
	if err != nil {
		return envolverPostgres(err, "atualizar status do documento")
	}
	if marca.RowsAffected() == 0 {
		return errors.NovoErroConflito("o documento não está mais no status esperado")
	}
	return nil
}

// DefinirCDM grava CDM e status atomicamente, somente se statusAtual ainda confere.
func (r *RepositorioDocumento) DefinirCDM(ctx context.Context, id uuid.UUID, cdm json.RawMessage, statusAtual, novoStatus documentoentity.Status) error {
	if !statusAtual.Valido() || !novoStatus.Valido() {
		return errors.NovoErroValidacao("status", "status inválido")
	}
	if err := documentoentity.ValidarCDM(cdm); err != nil {
		return err
	}
	marca, err := r.pool.Exec(ctx,
		`UPDATE documentos SET cdm = $3, status = $4, atualizado_em = now() WHERE id = $1 AND status = $2`,
		id, string(statusAtual), cdmOuNulo(cdm), string(novoStatus))
	if err != nil {
		return envolverPostgres(err, "definir cdm do documento")
	}
	if marca.RowsAffected() == 0 {
		return errors.NovoErroConflito("o documento não está mais no status esperado")
	}
	return nil
}

// cdmOuNulo converte json.RawMessage(nil) em nil de interface: passar o
// json.RawMessage vazio direto ao pgx gravaria o literal JSON `null` numa
// coluna jsonb, em vez de SQL NULL.
func cdmOuNulo(cdm json.RawMessage) any {
	if len(cdm) == 0 {
		return nil
	}
	return cdm
}

// scanDocumento lê uma linha nas colunas de colunasDocumento e monta o
// agregado. Qualquer inconsistência encontrada aqui (dono ambíguo, formato ou
// status desconhecido, chave malformada) é corrupção de dado: o CHECK do
// banco já garante o dono, então esses erros nunca deveriam acontecer, e por
// isso são envolvidos como falha, nunca traduzidos para "não encontrado" —
// isso esconderia a corrupção como um 404 comum.
// erroLinhaCorrompida classifica falha de conversão de valor LIDO do banco.
//
// Os VOs devolvem *errors.ErroValidacao, que é a classificação certa para dado
// que chegou do cliente e errada para dado que já estava gravado: preservá-lo no
// encadeamento faz rotasutil.TratarErro responder 400 e culpar quem só fez um
// GET, escondendo corrupção do servidor atrás de "requisição inválida". Linha
// corrompida é falha de aplicação, não do chamador.
//
// A mensagem do VO entra no texto (descreve o formato esperado, não o valor
// lido), mas o *ErroValidacao NÃO entra no encadeamento — é justamente o tipo
// que precisa sumir daqui.
func erroLinhaCorrompida(err error, contexto string) error {
	return errors.NovoErroAplicacao(contexto + ": " + err.Error())
}

func scanDocumento(linha pgx.Row) (documentoentity.Documento, error) {
	var (
		id                     uuid.UUID
		usuarioID, sessaoID    *uuid.UUID
		nomeOriginal, mime     string
		tamanhoBytes           int64
		chaveStorage           string
		chaveStoragePDF        *string
		status                 string
		cdm                    json.RawMessage
		criadoEm, atualizadoEm time.Time
	)
	if err := linha.Scan(&id, &usuarioID, &sessaoID, &nomeOriginal, &mime, &tamanhoBytes,
		&chaveStorage, &chaveStoragePDF, &status, &cdm, &criadoEm, &atualizadoEm); err != nil {
		return documentoentity.Documento{}, err
	}

	dono, err := vo.ParaDono(usuarioID, sessaoID)
	if err != nil {
		return documentoentity.Documento{}, erroLinhaCorrompida(err, "montar dono do documento lido do banco")
	}
	formato, err := vo.FormatoPorMIME(mime)
	if err != nil {
		return documentoentity.Documento{}, erroLinhaCorrompida(err, "converter mime do documento lido do banco")
	}
	chave, err := vo.ParaChaveStorage(chaveStorage)
	if err != nil {
		return documentoentity.Documento{}, erroLinhaCorrompida(err, "converter chave de storage do documento lido do banco")
	}
	var chavePreview *vo.ChaveStorage
	if chaveStoragePDF != nil {
		convertida, err := vo.ParaChaveStorage(*chaveStoragePDF)
		if err != nil {
			return documentoentity.Documento{}, erroLinhaCorrompida(err, "converter chave de preview do documento lido do banco")
		}
		chavePreview = &convertida
	}
	statusDocumento := documentoentity.Status(status)
	if !statusDocumento.Valido() {
		return documentoentity.Documento{}, errors.NovoErroAplicacao("converter status do documento lido do banco: status desconhecido")
	}

	return documentoentity.Documento{
		ID:              id,
		Dono:            dono,
		NomeOriginal:    nomeOriginal,
		Formato:         formato,
		TamanhoBytes:    tamanhoBytes,
		ChaveStorage:    chave,
		ChaveStoragePDF: chavePreview,
		Status:          statusDocumento,
		CDM:             cdm,
		CriadoEm:        criadoEm.UTC(),
		AtualizadoEm:    atualizadoEm.UTC(),
	}, nil
}

var (
	_ documentorepo.DocumentoRepo        = (*RepositorioDocumento)(nil)
	_ documentorepo.DocumentoInternoRepo = (*RepositorioDocumento)(nil)
)
