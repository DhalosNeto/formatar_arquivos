package service

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/daniel-halos/formatador/internal/domain/documento/entity"
	"github.com/daniel-halos/formatador/internal/domain/documento/repository"
	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// Cada caso tem seu próprio fake e executa chamadas sequenciais, inclusive sob -race.
type repositorioFake struct {
	documentos           map[uuid.UUID]entity.Documento
	adversarial          bool
	erro                 error
	erroPreview          error
	chamadas, escritas   int
	contextos            []context.Context
	donos                []vo.Dono
	limite, deslocamento int
}

var _ repository.DocumentoRepo = (*repositorioFake)(nil)

func (r *repositorioFake) registrarChamada(ctx context.Context, dono vo.Dono) {
	r.chamadas++
	r.contextos = append(r.contextos, ctx)
	r.donos = append(r.donos, dono)
}

func (r *repositorioFake) Inserir(ctx context.Context, documento entity.Documento) error {
	r.registrarChamada(ctx, documento.Dono)
	r.escritas++
	if r.erro != nil {
		return r.erro
	}
	if _, existe := r.documentos[documento.ID]; existe {
		return errors.NovoErroConflito("documento já registrado")
	}
	r.documentos[documento.ID] = documento
	return nil
}

func (r *repositorioFake) ObterPorID(ctx context.Context, solicitante vo.Dono, id uuid.UUID) (entity.Documento, error) {
	r.registrarChamada(ctx, solicitante)
	if r.erro != nil {
		return entity.Documento{}, r.erro
	}
	documento, existe := r.documentos[id]
	if !existe || (!r.adversarial && !solicitante.PodeAcessar(documento.Dono)) {
		return entity.Documento{}, errors.NovoErroNaoEncontrado("documento")
	}
	return documento, nil
}

func (r *repositorioFake) ListarPorDono(ctx context.Context, solicitante vo.Dono, limite, deslocamento int) ([]entity.Documento, error) {
	r.registrarChamada(ctx, solicitante)
	r.limite, r.deslocamento = limite, deslocamento
	if r.erro != nil {
		return nil, r.erro
	}
	var encontrados []entity.Documento
	for _, documento := range r.documentos {
		if r.adversarial || solicitante.PodeAcessar(documento.Dono) {
			encontrados = append(encontrados, documento)
		}
	}
	return encontrados, nil
}

func (r *repositorioFake) DefinirChavePreviewPDF(ctx context.Context, solicitante vo.Dono, id uuid.UUID, chave vo.ChaveStorage) error {
	r.registrarChamada(ctx, solicitante)
	r.escritas++
	if r.erroPreview != nil {
		return r.erroPreview
	}
	if r.erro != nil {
		return r.erro
	}
	documento, existe := r.documentos[id]
	if !existe || (!r.adversarial && !solicitante.PodeAcessar(documento.Dono)) {
		return errors.NovoErroNaoEncontrado("documento")
	}
	documento.ChaveStoragePDF = &chave
	r.documentos[id] = documento
	return nil
}

func exigirSemErro(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
}

func exigirValidacao(t *testing.T, err error, campo string) {
	t.Helper()
	var invalido *errors.ErroValidacao
	if !errors.Como(err, &invalido) {
		t.Fatalf("esperava ErroValidacao, recebeu %T: %v", err, err)
	}
	for _, atual := range invalido.Campos {
		if atual.Campo == campo {
			return
		}
	}
	t.Fatalf("campo %q ausente: %v", campo, invalido.Campos)
}

func exigirNaoEncontrado(t *testing.T, err error) *errors.ErroNaoEncontrado {
	t.Helper()
	var ausente *errors.ErroNaoEncontrado
	if !errors.Como(err, &ausente) {
		t.Fatalf("esperava ErroNaoEncontrado, recebeu %T: %v", err, err)
	}
	return ausente
}

func donosDeTeste(t *testing.T) []vo.Dono {
	t.Helper()
	id := uuid.New()
	sessao, err := vo.NovoDonoSessao(id)
	exigirSemErro(t, err)
	usuario, err := vo.NovoDonoUsuario(id)
	exigirSemErro(t, err)
	outraSessao, err := vo.NovoDonoSessao(uuid.New())
	exigirSemErro(t, err)
	outroUsuario, err := vo.NovoDonoUsuario(uuid.New())
	exigirSemErro(t, err)
	return []vo.Dono{sessao, usuario, outraSessao, outroUsuario}
}

func dadosValidos(dono vo.Dono) DadosIngestao {
	return DadosIngestao{Dono: dono, NomeOriginal: "artigo.docx", TamanhoBytes: 4096, Prefixo: []byte{0x50, 0x4B, 0x03, 0x04, 0x14, 0, 6, 0}}
}

func novoServicoDeTeste(t *testing.T) (*Servico, *repositorioFake) {
	t.Helper()
	repositorio := &repositorioFake{documentos: make(map[uuid.UUID]entity.Documento)}
	servico, err := NovoServico(repositorio, 0)
	exigirSemErro(t, err)
	return servico, repositorio
}

func documentoDeTeste(t *testing.T, dono vo.Dono) entity.Documento {
	t.Helper()
	documento, err := entity.NovoDocumento(dono, "artigo.docx", vo.FormatoDocx, 4096)
	exigirSemErro(t, err)
	return documento
}

func TestNovoServico(t *testing.T) {
	t.Parallel()
	_, err := NovoServico(nil, 0)
	var nulo *errors.ErroArgumentoNulo
	if !errors.Como(err, &nulo) || nulo.Argumento != "repositorio" {
		t.Fatalf("esperava repositório obrigatório: %v", err)
	}
	if TamanhoMaximoPadraoBytes != 25<<20 {
		t.Fatal("teto padrão deve ser 25 MiB")
	}
	for _, caso := range []struct {
		nome            string
		maximo, tamanho int64
		invalido        bool
	}{
		{"padrão aceita teto", 0, 25 << 20, false},
		{"negativo usa padrão", -1, 25 << 20, false},
		{"padrão rejeita excesso", 0, (25 << 20) + 1, true},
		{"explícito aceita teto", 1024, 1024, false},
		{"explícito rejeita excesso", 1024, 1025, true},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			_, repo := novoServicoDeTeste(t)
			servico, err := NovoServico(repo, caso.maximo)
			exigirSemErro(t, err)
			dados := dadosValidos(donosDeTeste(t)[0])
			dados.TamanhoBytes = caso.tamanho
			_, err = servico.ValidarIngestao(dados)
			if caso.invalido {
				exigirValidacao(t, err, "tamanho_bytes")
			} else {
				exigirSemErro(t, err)
			}
		})
	}
}

func TestIngestaoValidaAntesDePreparar(t *testing.T) {
	t.Parallel()
	for _, caso := range []struct {
		nome, campo string
		alterar     func(*DadosIngestao)
	}{
		{"dono vazio", "dono", func(d *DadosIngestao) { d.Dono = vo.Dono{} }},
		{"pdf", "arquivo", func(d *DadosIngestao) { d.Prefixo = []byte("%PDF-1.7") }},
		{"outro formato", "arquivo", func(d *DadosIngestao) { d.Prefixo = []byte("{\\rtf1\\ansi") }},
		{"prefixo curto", "arquivo", func(d *DadosIngestao) { d.Prefixo = []byte{0x50, 0x4B, 3} }},
		{"prefixo nulo", "arquivo", func(d *DadosIngestao) { d.Prefixo = nil }},
		{"tamanho zero", "tamanho_bytes", func(d *DadosIngestao) { d.TamanhoBytes = 0 }},
		{"tamanho negativo", "tamanho_bytes", func(d *DadosIngestao) { d.TamanhoBytes = -1 }},
		{"nome vazio", "nome_original", func(d *DadosIngestao) { d.NomeOriginal = "" }},
		{"nome branco", "nome_original", func(d *DadosIngestao) { d.NomeOriginal = "  " }},
		{"nome esvazia na higienização", "nome_original", func(d *DadosIngestao) { d.NomeOriginal = "\u200b\u202e\x00\u2028\u2029" }},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			servico, repo := novoServicoDeTeste(t)
			dados := dadosValidos(donosDeTeste(t)[0])
			caso.alterar(&dados)
			_, err := servico.ValidarIngestao(dados)
			exigirValidacao(t, err, caso.campo)
			_, err = servico.PrepararIngestao(dados)
			exigirValidacao(t, err, caso.campo)
			if repo.chamadas != 0 {
				t.Fatal("ingestão inválida fez I/O")
			}
		})
	}
}

func TestPrepararRegistrarObterEPreviewPreservamDono(t *testing.T) {
	t.Parallel()
	for _, dono := range donosDeTeste(t)[:2] {
		t.Run(string(dono.Especie()), func(t *testing.T) {
			servico, repo := novoServicoDeTeste(t)
			ctx, cancelar := context.WithCancel(context.Background())
			defer cancelar()
			dados := dadosValidos(dono)
			dados.NomeOriginal = "C:\\pasta\\ \u202eartigo\x00\x7f\u200b\u2028\u2029 'ação'.pdf "
			formato, err := servico.ValidarIngestao(dados)
			exigirSemErro(t, err)
			if formato != vo.FormatoDocx {
				t.Fatal("formato deve vir da assinatura, não da extensão")
			}
			documento, err := servico.PrepararIngestao(dados)
			exigirSemErro(t, err)
			if repo.chamadas != 0 {
				t.Fatal("preparação fez I/O")
			}
			exigirSemErro(t, documento.Validar())
			if !documento.Dono.Igual(dono) || documento.NomeOriginal != "artigo 'ação'.pdf" || documento.CDM != nil || documento.TemPreview() {
				t.Fatal("preparação não preservou o contrato")
			}
			exigirSemErro(t, servico.Registrar(ctx, dono, documento))
			obtido, err := servico.Obter(ctx, dono, documento.ID)
			exigirSemErro(t, err)
			if !reflect.DeepEqual(documento, obtido) {
				t.Fatal("registro/leitura alterou documento")
			}
			exigirSemErro(t, servico.RegistrarPreviewPDF(ctx, dono, documento.ID))
			chave, err := vo.NovaChavePreviewPDF(documento.ID)
			exigirSemErro(t, err)
			persistido := repo.documentos[documento.ID]
			if persistido.ChaveStoragePDF == nil || *persistido.ChaveStoragePDF != chave {
				t.Fatal("preview não derivou a chave do ID")
			}
			persistido.ChaveStoragePDF = nil
			persistido.AtualizadoEm = documento.AtualizadoEm
			if !reflect.DeepEqual(documento, persistido) {
				t.Fatal("preview alterou campos do original")
			}
			for i, chamado := range repo.contextos {
				if chamado != ctx || !repo.donos[i].Igual(dono) {
					t.Fatal("contexto ou dono não propagado")
				}
			}
			err = servico.Registrar(ctx, dono, documento)
			var conflito *errors.ErroConflito
			if !errors.Como(err, &conflito) {
				t.Fatalf("registro duplicado deve ser conflito: %v", err)
			}
		})
	}
}

func TestRegistrarRevalidaCadaInvarianteAntesDeIO(t *testing.T) {
	t.Parallel()
	for _, caso := range []struct {
		nome, campo string
		alterar     func(*entity.Documento)
	}{
		{"id vazio", "id", func(d *entity.Documento) { d.ID = uuid.Nil }},
		{"dono vazio", "dono", func(d *entity.Documento) { d.Dono = vo.Dono{} }},
		{"nome vazio", "nome_original", func(d *entity.Documento) { d.NomeOriginal = "" }},
		{"nome não canônico", "nome_original", func(d *entity.Documento) { d.NomeOriginal = "../artigo.docx" }},
		{"formato inválido", "formato", func(d *entity.Documento) { d.Formato = "invalido" }},
		{"tamanho zero", "tamanho_bytes", func(d *entity.Documento) { d.TamanhoBytes = 0 }},
		{"chave vazia", "chave_storage", func(d *entity.Documento) { d.ChaveStorage = "" }},
		{"chave de terceiro", "chave_storage", func(d *entity.Documento) {
			d.ChaveStorage = vo.ChaveStorage("documentos/" + uuid.NewString() + "/original.docx")
		}},
		{"status avançado", "status", func(d *entity.Documento) { d.Status = entity.StatusAnalisando }},
		{"criação vazia", "criado_em", func(d *entity.Documento) { d.CriadoEm = time.Time{} }},
		{"atualização vazia", "atualizado_em", func(d *entity.Documento) { d.AtualizadoEm = time.Time{} }},
		{"preview previamente preenchido", "chave_storage_pdf", func(d *entity.Documento) {
			chave, err := vo.NovaChavePreviewPDF(d.ID)
			exigirSemErro(t, err)
			d.ChaveStoragePDF = &chave
		}},
		{"preview vazio mas presente", "chave_storage_pdf", func(d *entity.Documento) { chave := vo.ChaveStorage(""); d.ChaveStoragePDF = &chave }},
		{"cdm válido mas antecipado", "cdm", func(d *entity.Documento) { d.CDM = json.RawMessage(`{}`) }},
		{"cdm vazio mas presente", "cdm", func(d *entity.Documento) { d.CDM = json.RawMessage{} }},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			servico, repo := novoServicoDeTeste(t)
			dono := donosDeTeste(t)[0]
			documento := documentoDeTeste(t, dono)
			caso.alterar(&documento)
			exigirValidacao(t, servico.Registrar(context.Background(), dono, documento), caso.campo)
			if repo.chamadas != 0 {
				t.Fatal("documento inválido chegou ao repositório")
			}
		})
	}
}

func TestDonoVazioRecusadoAntesDeIO(t *testing.T) {
	t.Parallel()
	servico, repo := novoServicoDeTeste(t)
	documento := documentoDeTeste(t, donosDeTeste(t)[0])
	ctx := context.Background()
	for nome, chamar := range map[string]func() error{
		"registrar": func() error { return servico.Registrar(ctx, vo.Dono{}, documento) },
		"registrar ambos vazios": func() error {
			copia := documento
			copia.Dono = vo.Dono{}
			return servico.Registrar(ctx, vo.Dono{}, copia)
		},
		"obter":   func() error { _, err := servico.Obter(ctx, vo.Dono{}, documento.ID); return err },
		"listar":  func() error { _, err := servico.ListarDoDono(ctx, vo.Dono{}, 20, 0); return err },
		"preview": func() error { return servico.RegistrarPreviewPDF(ctx, vo.Dono{}, documento.ID) },
	} {
		t.Run(nome, func(t *testing.T) { exigirValidacao(t, chamar(), "dono") })
	}
	if repo.chamadas != 0 {
		t.Fatal("dono vazio chegou ao repositório")
	}
}

func TestIDVazioRecusadoAntesDeIO(t *testing.T) {
	t.Parallel()
	servico, repo := novoServicoDeTeste(t)
	dono := donosDeTeste(t)[0]
	_, err := servico.Obter(context.Background(), dono, uuid.Nil)
	exigirValidacao(t, err, "id")
	exigirValidacao(t, servico.RegistrarPreviewPDF(context.Background(), dono, uuid.Nil), "id")
	if repo.chamadas != 0 {
		t.Fatal("ID vazio chegou ao repositório")
	}
}

func TestAcessoCruzadoNaoRevelaDocumento(t *testing.T) {
	t.Parallel()
	donos := donosDeTeste(t)
	for i, solicitante := range donos {
		for j, dono := range donos {
			if i == j {
				continue
			}
			for _, adversarial := range []bool{false, true} {
				servico, repo := novoServicoDeTeste(t)
				repo.adversarial = adversarial
				documento := documentoDeTeste(t, dono)
				repo.documentos[documento.ID] = documento
				ctx := context.Background()
				for _, preview := range []bool{false, true} {
					chamar := func(id uuid.UUID) error {
						if preview {
							return servico.RegistrarPreviewPDF(ctx, solicitante, id)
						}
						obtido, err := servico.Obter(ctx, solicitante, id)
						if !reflect.DeepEqual(obtido, entity.Documento{}) {
							t.Fatal("recusa vazou documento")
						}
						return err
					}
					terceiro := exigirNaoEncontrado(t, chamar(documento.ID))
					inexistente := exigirNaoEncontrado(t, chamar(uuid.New()))
					if !reflect.DeepEqual(terceiro, inexistente) || terceiro.Error() != inexistente.Error() {
						t.Fatal("erro público distingue terceiro de inexistente")
					}
				}
				if repo.escritas != 0 {
					t.Fatal("preview de terceiro chegou à escrita")
				}
				exigirNaoEncontrado(t, servico.Registrar(ctx, solicitante, documento))
				if repo.escritas != 0 {
					t.Fatal("registro com dono alheio chegou à escrita")
				}
			}
		}
	}
}

func TestListarDoDonoNormalizaPaginacao(t *testing.T) {
	t.Parallel()
	for _, caso := range []struct {
		nome                                                       string
		limite, deslocamento, esperadoLimite, esperadoDeslocamento int
	}{
		{"zero", 0, 0, 20, 0}, {"negativo", -1, 0, 20, 0}, {"offset negativo", 7, -1, 7, 0},
		{"excesso", 101, 0, 100, 0}, {"teto", 100, 0, 100, 0}, {"preservado", 7, 14, 7, 14},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			servico, repo := novoServicoDeTeste(t)
			dono := donosDeTeste(t)[0]
			ctx, cancelar := context.WithCancel(context.Background())
			defer cancelar()
			_, err := servico.ListarDoDono(ctx, dono, caso.limite, caso.deslocamento)
			exigirSemErro(t, err)
			if repo.limite != caso.esperadoLimite || repo.deslocamento != caso.esperadoDeslocamento || len(repo.donos) != 1 || !repo.donos[0].Igual(dono) || repo.contextos[0] != ctx {
				t.Fatal("paginação, contexto ou dono incorreto")
			}
		})
	}
}

func TestListagemIsolaDonosERecusaRepositorioAdversarial(t *testing.T) {
	t.Parallel()
	donos := donosDeTeste(t)
	for _, solicitante := range donos {
		servico, repo := novoServicoDeTeste(t)
		for _, dono := range donos {
			documento := documentoDeTeste(t, dono)
			repo.documentos[documento.ID] = documento
		}
		documentos, err := servico.ListarDoDono(context.Background(), solicitante, 20, 0)
		exigirSemErro(t, err)
		if len(documentos) != 1 || !documentos[0].Dono.Igual(solicitante) {
			t.Fatal("listagem misturou identidades")
		}
		repo.adversarial = true
		documentos, err = servico.ListarDoDono(context.Background(), solicitante, 20, 0)
		exigirNaoEncontrado(t, err)
		if len(documentos) != 0 {
			t.Fatal("listagem adversarial vazou resultados parciais")
		}
	}
}

func TestRepositorioRetornaDocumentoSemDono(t *testing.T) {
	t.Parallel()
	servico, repo := novoServicoDeTeste(t)
	dono := donosDeTeste(t)[0]
	documento := documentoDeTeste(t, dono)
	documento.Dono = vo.Dono{}
	repo.documentos[documento.ID] = documento
	repo.adversarial = true
	obtido, err := servico.Obter(context.Background(), dono, documento.ID)
	exigirNaoEncontrado(t, err)
	if !reflect.DeepEqual(obtido, entity.Documento{}) {
		t.Fatal("obter vazou documento sem dono")
	}
	exigirNaoEncontrado(t, servico.RegistrarPreviewPDF(context.Background(), dono, documento.ID))
	lista, err := servico.ListarDoDono(context.Background(), dono, 20, 0)
	exigirNaoEncontrado(t, err)
	if len(lista) != 0 || repo.escritas != 0 {
		t.Fatal("documento sem dono foi exposto ou alterado")
	}
}

func TestFalhasTipadasDoRepositorioSaoPreservadas(t *testing.T) {
	t.Parallel()
	for _, falha := range []error{errors.NovoErroConflito("corrida"), errors.NovoErroAplicacao("indisponível"), context.Canceled} {
		servico, repo := novoServicoDeTeste(t)
		dono := donosDeTeste(t)[0]
		documento := documentoDeTeste(t, dono)
		repo.documentos[documento.ID] = documento
		repo.erro = falha
		ctx := context.Background()
		for nome, chamar := range map[string]func() error{
			"registrar": func() error { return servico.Registrar(ctx, dono, documento) },
			"obter":     func() error { _, err := servico.Obter(ctx, dono, documento.ID); return err },
			"listar":    func() error { _, err := servico.ListarDoDono(ctx, dono, 20, 0); return err },
			"preview":   func() error { return servico.RegistrarPreviewPDF(ctx, dono, documento.ID) },
		} {
			if err := chamar(); !errors.E(err, falha) {
				t.Fatalf("%s perdeu causa %T: %v", nome, falha, err)
			}
		}
	}
}

func TestServicoPublicoNaoExpoeTransicoes(t *testing.T) {
	t.Parallel()
	tipo := reflect.TypeOf((*Servico)(nil))
	for _, nome := range strings.Fields("IniciarAnalise ConcluirAnalise MarcarFalha") {
		if _, existe := tipo.MethodByName(nome); existe {
			t.Errorf("%s deve existir apenas no serviço interno", nome)
		}
	}
}

func TestPreviewPreservaFalhaTipadaDaEscrita(t *testing.T) {
	t.Parallel()
	for _, falha := range []error{errors.NovoErroConflito("corrida"), errors.NovoErroNaoEncontrado("documento"), errors.NovoErroAplicacao("indisponível"), context.Canceled} {
		servico, repo := novoServicoDeTeste(t)
		dono := donosDeTeste(t)[0]
		documento := documentoDeTeste(t, dono)
		repo.documentos[documento.ID] = documento
		repo.erroPreview = falha
		err := servico.RegistrarPreviewPDF(context.Background(), dono, documento.ID)
		if !errors.E(err, falha) || repo.escritas != 1 {
			t.Fatalf("preview perdeu falha de escrita %T: %v", falha, err)
		}
		if !reflect.DeepEqual(repo.documentos[documento.ID], documento) {
			t.Fatal("falha de preview alterou documento")
		}
	}
}
