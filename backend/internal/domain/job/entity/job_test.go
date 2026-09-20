package entity

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/google/uuid"
)

func TestNovoJobReprovaEntradasInvalidas(t *testing.T) {
	t.Parallel()

	documentoID := uuid.New()
	rulesetID := uuid.New()
	rulesetNulo := uuid.Nil

	casos := []struct {
		nome            string
		documentoID     uuid.UUID
		tipo            TipoJob
		rulesetID       *uuid.UUID
		camposEsperados []string
	}{
		{
			nome:            "formatar sem ruleset",
			documentoID:     documentoID,
			tipo:            TipoFormatar,
			rulesetID:       nil,
			camposEsperados: []string{"ruleset_id"},
		},
		{
			nome:            "formatar com ruleset nulo",
			documentoID:     documentoID,
			tipo:            TipoFormatar,
			rulesetID:       &rulesetNulo,
			camposEsperados: []string{"ruleset_id"},
		},
		{
			nome:            "renderizar preview com ruleset indevido",
			documentoID:     documentoID,
			tipo:            TipoRenderizarPreview,
			rulesetID:       &rulesetID,
			camposEsperados: []string{"ruleset_id"},
		},
		{
			nome:            "analisar com ruleset indevido",
			documentoID:     documentoID,
			tipo:            TipoAnalisar,
			rulesetID:       &rulesetID,
			camposEsperados: []string{"ruleset_id"},
		},
		{
			nome:            "documento nulo",
			documentoID:     uuid.Nil,
			tipo:            TipoAnalisar,
			rulesetID:       nil,
			camposEsperados: []string{"documento_id"},
		},
		{
			nome:            "tipo desconhecido",
			documentoID:     documentoID,
			tipo:            TipoJob("compilar"),
			rulesetID:       nil,
			camposEsperados: []string{"tipo"},
		},
		{
			nome:            "tipo vazio",
			documentoID:     documentoID,
			tipo:            TipoJob(""),
			rulesetID:       nil,
			camposEsperados: []string{"tipo"},
		},
		{
			nome:            "documento nulo e tipo desconhecido acumulam num único erro",
			documentoID:     uuid.Nil,
			tipo:            TipoJob("compilar"),
			rulesetID:       &rulesetID,
			camposEsperados: []string{"documento_id", "tipo"},
		},
		{
			nome:            "documento nulo e formatar sem ruleset acumulam num único erro",
			documentoID:     uuid.Nil,
			tipo:            TipoFormatar,
			rulesetID:       nil,
			camposEsperados: []string{"documento_id", "ruleset_id"},
		},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			job, err := NovoJob(caso.documentoID, caso.tipo, caso.rulesetID)
			if err == nil {
				t.Fatal("esperava erro de validação")
			}

			var invalido *errors.ErroValidacao
			if !errors.Como(err, &invalido) {
				t.Fatalf("esperava *ErroValidacao, obteve %T", err)
			}
			for _, campo := range caso.camposEsperados {
				if !contemCampoDeValidacao(invalido, campo) {
					t.Fatalf("esperava o campo %q entre %v", campo, invalido.Campos)
				}
			}
			if len(invalido.Campos) != len(caso.camposEsperados) {
				t.Fatalf("esperava exatamente %d campo(s) reprovado(s), obteve %v",
					len(caso.camposEsperados), invalido.Campos)
			}
			if !reflect.DeepEqual(job, Job{}) {
				t.Fatal("esperava job zerado quando a validação falha")
			}
		})
	}
}

func TestNovoJobCriaJobPendente(t *testing.T) {
	t.Parallel()

	documentoID := uuid.New()
	antes := time.Now().UTC().Add(-time.Second)

	job, err := NovoJob(documentoID, TipoRenderizarPreview, nil)
	if err != nil {
		t.Fatalf("não esperava erro, obteve %v", err)
	}

	if job.ID == uuid.Nil {
		t.Fatal("esperava ID gerado")
	}
	if job.DocumentoID != documentoID {
		t.Fatalf("esperava documento %v, obteve %v", documentoID, job.DocumentoID)
	}
	if job.RulesetID != nil {
		t.Fatal("esperava ruleset nulo para renderizar preview")
	}
	if job.Tipo != TipoRenderizarPreview {
		t.Fatalf("esperava tipo %q, obteve %q", TipoRenderizarPreview, job.Tipo)
	}
	if job.Status != StatusPendente {
		t.Fatalf("esperava status pendente, obteve %q", job.Status)
	}
	if job.Tentativas != 0 {
		t.Fatalf("esperava 0 tentativas, obteve %d", job.Tentativas)
	}
	if job.Progresso != ProgressoMinimo {
		t.Fatalf("esperava progresso %d, obteve %d", ProgressoMinimo, job.Progresso)
	}
	if job.Erro != "" {
		t.Fatal("esperava erro vazio no job recém-criado")
	}
	if job.Resultado != nil {
		t.Fatal("esperava resultado nulo no job recém-criado")
	}
	if job.CriadoEm.IsZero() {
		t.Fatal("esperava CriadoEm preenchido")
	}
	if job.CriadoEm.Location() != time.UTC {
		t.Fatalf("esperava CriadoEm em UTC, obteve %v", job.CriadoEm.Location())
	}
	if job.CriadoEm.Before(antes) {
		t.Fatalf("esperava CriadoEm recente, obteve %v", job.CriadoEm)
	}
	if job.IniciadoEm != nil {
		t.Fatal("esperava IniciadoEm nulo")
	}
	if job.FinalizadoEm != nil {
		t.Fatal("esperava FinalizadoEm nulo")
	}
	if job.Terminal() {
		t.Fatal("job pendente não é terminal")
	}
}

func TestNovoJobFormatarComRulesetValido(t *testing.T) {
	t.Parallel()

	rulesetID := uuid.New()

	job, err := NovoJob(uuid.New(), TipoFormatar, &rulesetID)
	if err != nil {
		t.Fatalf("não esperava erro, obteve %v", err)
	}
	if job.RulesetID == nil {
		t.Fatal("esperava ruleset preenchido")
	}
	if *job.RulesetID != rulesetID {
		t.Fatalf("esperava ruleset %v, obteve %v", rulesetID, *job.RulesetID)
	}
	original := rulesetID
	rulesetID = uuid.New()
	if *job.RulesetID != original {
		t.Fatal("esperava cópia de RulesetID imune à mutação externa")
	}
	*job.RulesetID = uuid.New()
	if rulesetID == *job.RulesetID {
		t.Fatal("esperava RulesetID sem compartilhar ponteiro com a entrada")
	}
	if job.Status != StatusPendente {
		t.Fatalf("esperava status pendente, obteve %q", job.Status)
	}
}

func TestJobIniciar(t *testing.T) {
	t.Parallel()

	t.Run("pendente passa para executando", func(t *testing.T) {
		t.Parallel()

		job := jobPendente(t, TipoAnalisar)
		antes := time.Now().UTC().Add(-time.Second)

		if err := job.Iniciar(); err != nil {
			t.Fatalf("não esperava erro, obteve %v", err)
		}
		if job.Status != StatusExecutando {
			t.Fatalf("esperava status executando, obteve %q", job.Status)
		}
		if job.Tentativas != 1 {
			t.Fatalf("esperava 1 tentativa, obteve %d", job.Tentativas)
		}
		if job.IniciadoEm == nil {
			t.Fatal("esperava IniciadoEm preenchido")
		}
		if job.IniciadoEm.Location() != time.UTC {
			t.Fatalf("esperava IniciadoEm em UTC, obteve %v", job.IniciadoEm.Location())
		}
		if job.IniciadoEm.Before(antes) {
			t.Fatalf("esperava IniciadoEm recente, obteve %v", *job.IniciadoEm)
		}
		if job.FinalizadoEm != nil {
			t.Fatal("esperava FinalizadoEm ainda nulo")
		}
	})

	t.Run("iniciar duas vezes gera conflito e não altera o estado", func(t *testing.T) {
		t.Parallel()

		job := jobPendente(t, TipoAnalisar)
		if err := job.Iniciar(); err != nil {
			t.Fatalf("não esperava erro no primeiro início, obteve %v", err)
		}
		anterior := *job
		defer exigirJobInalterado(t, job)()

		err := job.Iniciar()
		if err == nil {
			t.Fatal("esperava conflito ao iniciar job já em execução")
		}

		var conflito *errors.ErroConflito
		if !errors.Como(err, &conflito) {
			t.Fatalf("esperava *ErroConflito, obteve %T", err)
		}
		if job.Tentativas != anterior.Tentativas {
			t.Fatalf("esperava tentativas inalteradas (%d), obteve %d", anterior.Tentativas, job.Tentativas)
		}
		if job.Status != anterior.Status {
			t.Fatalf("esperava status inalterado (%q), obteve %q", anterior.Status, job.Status)
		}
		if !job.IniciadoEm.Equal(*anterior.IniciadoEm) {
			t.Fatal("esperava IniciadoEm inalterado")
		}
	})

	t.Run("iniciar job concluído gera conflito", func(t *testing.T) {
		t.Parallel()

		job := jobExecutando(t, TipoAnalisar)
		if err := job.Concluir(nil); err != nil {
			t.Fatalf("não esperava erro ao concluir, obteve %v", err)
		}

		exigirConflito(t, job.Iniciar())
		if job.Status != StatusConcluido {
			t.Fatalf("esperava status concluido preservado, obteve %q", job.Status)
		}
	})
}

func TestJobDefinirProgresso(t *testing.T) {
	t.Parallel()

	t.Run("job pendente rejeita progresso com conflito", func(t *testing.T) {
		t.Parallel()

		job := jobPendente(t, TipoAnalisar)
		exigirConflito(t, job.DefinirProgresso(50))
		if job.Progresso != ProgressoMinimo {
			t.Fatalf("esperava progresso inalterado, obteve %d", job.Progresso)
		}
	})

	t.Run("progresso fora da faixa é inválido", func(t *testing.T) {
		t.Parallel()

		casos := []struct {
			nome  string
			valor int
		}{
			{nome: "abaixo do mínimo", valor: -1},
			{nome: "acima do máximo", valor: 101},
			{nome: "muito acima do máximo", valor: 1000},
		}

		for _, caso := range casos {
			t.Run(caso.nome, func(t *testing.T) {
				t.Parallel()

				job := jobExecutando(t, TipoAnalisar)
				defer exigirJobInalterado(t, job)()
				err := job.DefinirProgresso(caso.valor)
				exigirCampoInvalido(t, err, "progresso")
				if job.Progresso != ProgressoMinimo {
					t.Fatalf("esperava progresso inalterado, obteve %d", job.Progresso)
				}
			})
		}
	})

	t.Run("progresso nos limites da faixa é aceito", func(t *testing.T) {
		t.Parallel()

		job := jobExecutando(t, TipoAnalisar)
		if err := job.DefinirProgresso(ProgressoMinimo); err != nil {
			t.Fatalf("não esperava erro no mínimo, obteve %v", err)
		}
		if err := job.DefinirProgresso(ProgressoMaximo); err != nil {
			t.Fatalf("não esperava erro no máximo, obteve %v", err)
		}
		if job.Progresso != ProgressoMaximo {
			t.Fatalf("esperava progresso %d, obteve %d", ProgressoMaximo, job.Progresso)
		}
	})

	t.Run("progresso repetido é idempotente e retroceder é inválido", func(t *testing.T) {
		t.Parallel()

		job := jobExecutando(t, TipoAnalisar)
		if err := job.DefinirProgresso(60); err != nil {
			t.Fatalf("não esperava erro, obteve %v", err)
		}
		if err := job.DefinirProgresso(60); err != nil {
			t.Fatalf("esperava idempotência, obteve %v", err)
		}
		if job.Progresso != 60 {
			t.Fatalf("esperava progresso 60, obteve %d", job.Progresso)
		}

		defer exigirJobInalterado(t, job)()
		err := job.DefinirProgresso(59)
		exigirCampoInvalido(t, err, "progresso")
		if job.Progresso != 60 {
			t.Fatalf("esperava progresso preservado em 60, obteve %d", job.Progresso)
		}
	})
}

func TestJobConcluir(t *testing.T) {
	t.Parallel()

	t.Run("executando conclui com progresso máximo", func(t *testing.T) {
		t.Parallel()

		job := jobExecutando(t, TipoAnalisar)
		if err := job.DefinirProgresso(30); err != nil {
			t.Fatalf("não esperava erro, obteve %v", err)
		}

		if err := job.Concluir(nil); err != nil {
			t.Fatalf("não esperava erro, obteve %v", err)
		}
		if job.Resultado != nil {
			t.Fatal("esperava ausência de resultado para Concluir(nil)")
		}
		if job.Status != StatusConcluido {
			t.Fatalf("esperava status concluido, obteve %q", job.Status)
		}
		if job.Progresso != ProgressoMaximo {
			t.Fatalf("esperava progresso %d, obteve %d", ProgressoMaximo, job.Progresso)
		}
		if job.FinalizadoEm == nil {
			t.Fatal("esperava FinalizadoEm preenchido")
		}
		if job.FinalizadoEm.Location() != time.UTC {
			t.Fatalf("esperava FinalizadoEm em UTC, obteve %v", job.FinalizadoEm.Location())
		}
		if !job.Terminal() {
			t.Fatal("job concluído é terminal")
		}
	})

	t.Run("concluir job pendente gera conflito", func(t *testing.T) {
		t.Parallel()

		job := jobPendente(t, TipoAnalisar)
		exigirConflito(t, job.Concluir(nil))
		if job.Status != StatusPendente {
			t.Fatalf("esperava status pendente preservado, obteve %q", job.Status)
		}
		if job.FinalizadoEm != nil {
			t.Fatal("esperava FinalizadoEm nulo após conflito")
		}
	})

	t.Run("concluir duas vezes gera conflito", func(t *testing.T) {
		t.Parallel()

		job := jobExecutando(t, TipoAnalisar)
		if err := job.Concluir(nil); err != nil {
			t.Fatalf("não esperava erro, obteve %v", err)
		}
		exigirConflito(t, job.Concluir(nil))
	})

	t.Run("resultado é copiado em profundidade", func(t *testing.T) {
		t.Parallel()

		job := jobExecutando(t, TipoAnalisar)
		resultado := json.RawMessage(`{"blocos":3}`)

		if err := job.Concluir(resultado); err != nil {
			t.Fatalf("não esperava erro, obteve %v", err)
		}
		guardado := string(job.Resultado)

		resultado[0] = 'X'

		if string(job.Resultado) != guardado {
			t.Fatalf("esperava resultado imune à mutação externa (%q), obteve %q", guardado, string(job.Resultado))
		}
		if guardado != `{"blocos":3}` {
			t.Fatalf("esperava resultado preservado, obteve %q", guardado)
		}
	})
}

func TestJobConcluirValidaResultado(t *testing.T) {
	t.Parallel()

	if TamanhoMaximoResultadoBytes != 65536 {
		t.Fatal("esperava limite de 64 KiB em bytes brutos")
	}
	casos := []struct {
		nome      string
		resultado json.RawMessage
		invalido  bool
	}{
		{"ausente", nil, false},
		{"objeto vazio", json.RawMessage(`{}`), false},
		{"objeto com espaços externos", json.RawMessage(" \n{\"valor\":\"ação 😀\"}\t "), false},
		{"objeto aninhado", json.RawMessage(`{"lista":[null,1,{"ok":true}]}`), false},
		{"vazio não nil", json.RawMessage{}, true},
		{"branco", json.RawMessage(" \t\n"), true},
		{"null", json.RawMessage(`null`), true},
		{"array", json.RawMessage(`[]`), true},
		{"string", json.RawMessage(`"texto"`), true},
		{"número", json.RawMessage(`42`), true},
		{"booleano", json.RawMessage(`true`), true},
		{"JSON incompleto", json.RawMessage(`{"valor":`), true},
		{"vírgula inválida", json.RawMessage(`{"valor":1,}`), true},
		{"dois objetos", json.RawMessage(`{} {}`), true},
		{"lixo após objeto", json.RawMessage(`{} lixo`), true},
		{"UTF8 inválido dentro de string", json.RawMessage("{\"valor\":\"\xff\"}"), true},
		{"65536 bytes", json.RawMessage(`{"v":"` + strings.Repeat("é", 32764) + `"}`), false},
		{"65537 bytes", json.RawMessage(`{"v":"` + strings.Repeat("é", 32764) + `a"}`), true},
		{"65536 bytes incluindo espaços", json.RawMessage(`{}` + strings.Repeat(" ", 65534)), false},
		{"65537 bytes incluindo espaços", json.RawMessage(`{}` + strings.Repeat(" ", 65535)), true},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()
			job := jobExecutando(t, TipoFormatar)
			if err := job.DefinirProgresso(40); err != nil {
				t.Fatalf("pré-condição: %v", err)
			}
			if caso.invalido {
				job.Resultado = json.RawMessage(`{"anterior":true}`)
				defer exigirJobInalterado(t, job)()
				exigirCampoInvalido(t, job.Concluir(caso.resultado), "resultado")
				return
			}
			if err := job.Concluir(caso.resultado); err != nil {
				t.Fatalf("não esperava erro, obteve %v", err)
			}
			if !reflect.DeepEqual(job.Resultado, caso.resultado) {
				t.Fatal("esperava resultado bruto preservado")
			}
			if job.Status != StatusConcluido || job.Progresso != 100 {
				t.Fatal("esperava conclusão com progresso máximo")
			}
		})
	}
}

func TestJobConflitosNaoMutamEntidadeAntesDeValidarPayload(t *testing.T) {
	t.Parallel()

	operacoes := []struct {
		nome       string
		permitidos []StatusJob
		executar   func(*Job) error
	}{
		{"iniciar", []StatusJob{StatusPendente}, (*Job).Iniciar},
		{"cancelar", []StatusJob{StatusPendente, StatusExecutando}, (*Job).Cancelar},
		{"reenfileirar", []StatusJob{StatusFalhou}, (*Job).Reenfileirar},
		{"concluir sem resultado", []StatusJob{StatusExecutando}, func(j *Job) error { return j.Concluir(nil) }},
		{"concluir resultado inválido", []StatusJob{StatusExecutando}, func(j *Job) error { return j.Concluir(json.RawMessage(`{`)) }},
		{"concluir resultado excedente", []StatusJob{StatusExecutando}, func(j *Job) error {
			return j.Concluir(json.RawMessage(`{}` + strings.Repeat(" ", 65535)))
		}},
		{"falhar motivo válido", []StatusJob{StatusExecutando}, func(j *Job) error { return j.Falhar("falha técnica") }},
		{"falhar motivo branco", []StatusJob{StatusExecutando}, func(j *Job) error { return j.Falhar(" \t") }},
		{"progresso válido", []StatusJob{StatusExecutando}, func(j *Job) error { return j.DefinirProgresso(70) }},
		{"progresso inválido", []StatusJob{StatusExecutando}, func(j *Job) error { return j.DefinirProgresso(101) }},
	}
	for _, status := range statusConhecidos {
		for _, operacao := range operacoes {
			permitido := false
			for _, origem := range operacao.permitidos {
				permitido = permitido || origem == status
			}
			if permitido {
				continue
			}
			t.Run(string(status)+"/"+operacao.nome, func(t *testing.T) {
				t.Parallel()
				job := jobPendente(t, TipoFormatar)
				inicio := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
				fim := inicio.Add(time.Minute)
				job.Status = status
				job.Tentativas = 2
				job.Progresso = 40
				job.IniciadoEm, job.FinalizadoEm = &inicio, &fim
				job.Erro = "Não foi possível processar o documento."
				job.Resultado = json.RawMessage(`{"anterior":true}`)
				defer exigirJobInalterado(t, job)()
				exigirConflito(t, operacao.executar(job))
			})
		}
	}
}

func TestJobFalhar(t *testing.T) {
	t.Parallel()

	t.Run("motivo vazio é inválido", func(t *testing.T) {
		t.Parallel()

		casos := []struct {
			nome   string
			motivo string
		}{
			{nome: "string vazia", motivo: ""},
			{nome: "somente espaços", motivo: "   "},
			{nome: "somente tabulação e quebra de linha", motivo: "\t\n "},
			{nome: "somente espaços Unicode", motivo: "\u00a0\u2003"},
		}

		for _, caso := range casos {
			t.Run(caso.nome, func(t *testing.T) {
				t.Parallel()

				job := jobExecutando(t, TipoAnalisar)
				defer exigirJobInalterado(t, job)()
				exigirCampoInvalido(t, job.Falhar(caso.motivo), "erro")
				if job.Status != StatusExecutando {
					t.Fatalf("esperava status executando preservado, obteve %q", job.Status)
				}
			})
		}
	})

	t.Run("motivos técnicos nunca são persistidos", func(t *testing.T) {
		t.Parallel()

		for _, caso := range []struct{ nome, motivo string }{
			{"motivo longo", strings.Repeat("é", 65537)},
			{"segredo e conteúdo", "token=segredo; trecho privado do documento"},
			{"motivo cercado de espaços", " \t erro técnico \n"},
			{"UTF8 inválido não branco", string([]byte{0xff})},
		} {
			t.Run(caso.nome, func(t *testing.T) {
				t.Parallel()
				job := jobExecutando(t, TipoAnalisar)
				if err := job.Falhar(caso.motivo); err != nil {
					t.Fatalf("não esperava erro, obteve %v", err)
				}
				if job.Erro != "Não foi possível processar o documento." {
					t.Fatal("esperava somente mensagem fixa, sem motivo técnico")
				}
			})
		}
	})

	t.Run("mensagem fixa é armazenada e o progresso não é alterado", func(t *testing.T) {
		t.Parallel()

		job := jobExecutando(t, TipoFormatar)
		if err := job.DefinirProgresso(40); err != nil {
			t.Fatalf("não esperava erro, obteve %v", err)
		}

		if err := job.Falhar("conversor indisponível"); err != nil {
			t.Fatalf("não esperava erro, obteve %v", err)
		}
		if job.Status != StatusFalhou {
			t.Fatalf("esperava status falhou, obteve %q", job.Status)
		}
		if job.Erro != "Não foi possível processar o documento." {
			t.Fatal("esperava somente a mensagem fixa do contrato")
		}
		if job.Progresso != 40 {
			t.Fatalf("esperava progresso 40 preservado, obteve %d", job.Progresso)
		}
		if job.FinalizadoEm == nil {
			t.Fatal("esperava FinalizadoEm preenchido")
		}
		if !job.Terminal() {
			t.Fatal("job que falhou é terminal")
		}
	})

	t.Run("falhar job pendente gera conflito", func(t *testing.T) {
		t.Parallel()

		job := jobPendente(t, TipoAnalisar)
		exigirConflito(t, job.Falhar("motivo qualquer"))
		if job.Erro != "" {
			t.Fatal("esperava erro vazio após conflito")
		}
	})
}

func TestJobCancelar(t *testing.T) {
	t.Parallel()

	t.Run("cancelar job pendente", func(t *testing.T) {
		t.Parallel()

		job := jobPendente(t, TipoAnalisar)
		if err := job.Cancelar(); err != nil {
			t.Fatalf("não esperava erro, obteve %v", err)
		}
		if job.Status != StatusCancelado {
			t.Fatalf("esperava status cancelado, obteve %q", job.Status)
		}
		if job.FinalizadoEm == nil {
			t.Fatal("esperava FinalizadoEm preenchido")
		}
		if !job.Terminal() {
			t.Fatal("job cancelado é terminal")
		}
	})

	t.Run("cancelar job executando", func(t *testing.T) {
		t.Parallel()

		job := jobExecutando(t, TipoAnalisar)
		if err := job.Cancelar(); err != nil {
			t.Fatalf("não esperava erro, obteve %v", err)
		}
		if job.Status != StatusCancelado {
			t.Fatalf("esperava status cancelado, obteve %q", job.Status)
		}
	})

	t.Run("cancelar job concluído gera conflito", func(t *testing.T) {
		t.Parallel()

		job := jobExecutando(t, TipoAnalisar)
		if err := job.Concluir(nil); err != nil {
			t.Fatalf("não esperava erro, obteve %v", err)
		}

		exigirConflito(t, job.Cancelar())
		if job.Status != StatusConcluido {
			t.Fatalf("esperava status concluido preservado, obteve %q", job.Status)
		}
	})

	t.Run("cancelar job já cancelado gera conflito", func(t *testing.T) {
		t.Parallel()

		job := jobPendente(t, TipoAnalisar)
		if err := job.Cancelar(); err != nil {
			t.Fatalf("não esperava erro, obteve %v", err)
		}
		exigirConflito(t, job.Cancelar())
	})
}

func TestJobReenfileirar(t *testing.T) {
	t.Parallel()

	t.Run("job que falhou volta para pendente preservando tentativas", func(t *testing.T) {
		t.Parallel()

		job := jobExecutando(t, TipoAnalisar)
		if err := job.DefinirProgresso(70); err != nil {
			t.Fatalf("não esperava erro, obteve %v", err)
		}
		if err := job.Falhar("falha temporária"); err != nil {
			t.Fatalf("não esperava erro, obteve %v", err)
		}
		tentativas := job.Tentativas
		id, criadoEm := job.ID, job.CriadoEm
		job.Resultado = json.RawMessage(`{"tentativa_anterior":true}`)

		if err := job.Reenfileirar(); err != nil {
			t.Fatalf("não esperava erro, obteve %v", err)
		}
		if job.Status != StatusPendente {
			t.Fatalf("esperava status pendente, obteve %q", job.Status)
		}
		if job.Erro != "" {
			t.Fatalf("esperava erro zerado, obteve %q", job.Erro)
		}
		if job.Resultado != nil {
			t.Fatal("esperava resultado limpo após retry")
		}
		if job.ID != id || !job.CriadoEm.Equal(criadoEm) {
			t.Fatal("esperava ID e criação preservados após retry")
		}
		if job.Progresso != ProgressoMinimo {
			t.Fatalf("esperava progresso zerado, obteve %d", job.Progresso)
		}
		if job.IniciadoEm != nil {
			t.Fatal("esperava IniciadoEm zerado")
		}
		if job.FinalizadoEm != nil {
			t.Fatal("esperava FinalizadoEm zerado")
		}
		if job.Tentativas != tentativas {
			t.Fatalf("esperava tentativas preservadas (%d), obteve %d", tentativas, job.Tentativas)
		}
		if job.Terminal() {
			t.Fatal("job reenfileirado não é terminal")
		}
	})

	t.Run("reenfileirar soma tentativas ao reiniciar", func(t *testing.T) {
		t.Parallel()

		job := jobExecutando(t, TipoAnalisar)
		if err := job.Falhar("falha temporária"); err != nil {
			t.Fatalf("não esperava erro, obteve %v", err)
		}
		if err := job.Reenfileirar(); err != nil {
			t.Fatalf("não esperava erro, obteve %v", err)
		}
		if err := job.Iniciar(); err != nil {
			t.Fatalf("não esperava erro, obteve %v", err)
		}
		if job.Tentativas != 2 {
			t.Fatalf("esperava 2 tentativas, obteve %d", job.Tentativas)
		}
	})

	t.Run("reenfileirar job concluído gera conflito", func(t *testing.T) {
		t.Parallel()

		job := jobExecutando(t, TipoAnalisar)
		if err := job.Concluir(nil); err != nil {
			t.Fatalf("não esperava erro, obteve %v", err)
		}
		exigirConflito(t, job.Reenfileirar())
		if job.Status != StatusConcluido {
			t.Fatalf("esperava status concluido preservado, obteve %q", job.Status)
		}
	})

	t.Run("reenfileirar job pendente gera conflito", func(t *testing.T) {
		t.Parallel()

		job := jobPendente(t, TipoAnalisar)
		exigirConflito(t, job.Reenfileirar())
	})

	t.Run("reenfileirar job cancelado gera conflito", func(t *testing.T) {
		t.Parallel()

		job := jobPendente(t, TipoAnalisar)
		if err := job.Cancelar(); err != nil {
			t.Fatalf("não esperava erro, obteve %v", err)
		}
		exigirConflito(t, job.Reenfileirar())
	})
}

func TestJobDuracao(t *testing.T) {
	t.Parallel()

	t.Run("job pendente não tem duração", func(t *testing.T) {
		t.Parallel()

		job := jobPendente(t, TipoAnalisar)
		if _, ok := job.Duracao(); ok {
			t.Fatal("esperava ok==false para job pendente")
		}
	})

	t.Run("job executando não tem duração", func(t *testing.T) {
		t.Parallel()

		job := jobExecutando(t, TipoAnalisar)
		if _, ok := job.Duracao(); ok {
			t.Fatal("esperava ok==false para job em execução")
		}
	})

	t.Run("job concluído tem duração não negativa", func(t *testing.T) {
		t.Parallel()

		job := jobExecutando(t, TipoAnalisar)
		if err := job.Concluir(nil); err != nil {
			t.Fatalf("não esperava erro, obteve %v", err)
		}

		duracao, ok := job.Duracao()
		if !ok {
			t.Fatal("esperava ok==true para job concluído")
		}
		if duracao < 0 {
			t.Fatalf("esperava duração não negativa, obteve %v", duracao)
		}
	})

	t.Run("job cancelado sem início não tem duração", func(t *testing.T) {
		t.Parallel()

		job := jobPendente(t, TipoAnalisar)
		if err := job.Cancelar(); err != nil {
			t.Fatalf("não esperava erro, obteve %v", err)
		}
		if _, ok := job.Duracao(); ok {
			t.Fatal("esperava ok==false para job cancelado que nunca iniciou")
		}
	})
}

func jobPendente(t *testing.T, tipo TipoJob) *Job {
	t.Helper()

	var rulesetID *uuid.UUID
	if tipo.ExigeRuleset() {
		id := uuid.New()
		rulesetID = &id
	}

	job, err := NovoJob(uuid.New(), tipo, rulesetID)
	if err != nil {
		t.Fatalf("pré-condição: não esperava erro em NovoJob, obteve %v", err)
	}
	return &job
}

func jobExecutando(t *testing.T, tipo TipoJob) *Job {
	t.Helper()

	job := jobPendente(t, tipo)
	if err := job.Iniciar(); err != nil {
		t.Fatalf("pré-condição: não esperava erro em Iniciar, obteve %v", err)
	}
	return job
}

// Captura valores independentes para detectar também mutações nos ponteiros e bytes.
func exigirJobInalterado(t *testing.T, job *Job) func() {
	t.Helper()
	anterior := *job
	if job.RulesetID != nil {
		id := *job.RulesetID
		anterior.RulesetID = &id
	}
	if job.IniciadoEm != nil {
		inicio := *job.IniciadoEm
		anterior.IniciadoEm = &inicio
	}
	if job.FinalizadoEm != nil {
		fim := *job.FinalizadoEm
		anterior.FinalizadoEm = &fim
	}
	if job.Resultado != nil {
		anterior.Resultado = append(json.RawMessage{}, job.Resultado...)
	}
	return func() {
		t.Helper()
		if !reflect.DeepEqual(*job, anterior) {
			t.Error("esperava todos os campos do job inalterados após erro")
		}
	}
}

func exigirConflito(t *testing.T, err error) {
	t.Helper()

	if err == nil {
		t.Fatal("esperava *ErroConflito, obteve nil")
	}
	var conflito *errors.ErroConflito
	if !errors.Como(err, &conflito) {
		t.Fatalf("esperava *ErroConflito, obteve %T", err)
	}
}

func exigirCampoInvalido(t *testing.T, err error, campo string) {
	t.Helper()

	if err == nil {
		t.Fatalf("esperava *ErroValidacao no campo %q, obteve nil", campo)
	}
	var invalido *errors.ErroValidacao
	if !errors.Como(err, &invalido) {
		t.Fatalf("esperava *ErroValidacao, obteve %T", err)
	}
	if !contemCampoDeValidacao(invalido, campo) {
		t.Fatalf("esperava o campo %q entre %v", campo, invalido.Campos)
	}
}

func contemCampoDeValidacao(erro *errors.ErroValidacao, campo string) bool {
	for _, invalido := range erro.Campos {
		if invalido.Campo == campo {
			return true
		}
	}
	return false
}
