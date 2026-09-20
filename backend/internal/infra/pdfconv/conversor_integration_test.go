//go:build integration

// Sobe o sidecar de conversão de verdade a partir de deploy/Dockerfile.libreoffice
// (raiz do repo) e prova o cliente contra um LibreOffice/unoserver real. Este
// arquivo é esperado a falhar (RED) até o codador construir o Dockerfile e o
// handler HTTP do sidecar: prova de que o contrato do pacote já está travado
// em teste antes de existir implementação.
package pdfconv

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"net/netip"
	"os"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/daniel-halos/formatador/internal/infra/config"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// docxMinimo monta, em memória, o menor pacote OOXML que o LibreOffice aceita:
// três entradas na ordem exigida pelo formato — [Content_Types].xml,
// _rels/.rels e word/document.xml. Nada é commitado em disco: o fixture nasce
// a cada execução do teste.
func docxMinimo(t *testing.T, texto string) []byte {
	t.Helper()

	var escapado bytes.Buffer
	if err := xml.EscapeText(&escapado, []byte(texto)); err != nil {
		t.Fatalf("escapar texto do fixture: %v", err)
	}

	contentTypes := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`

	rels := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`

	documento := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>%s</w:t></w:r></w:p><w:sectPr/></w:body></w:document>`, escapado.String())

	buffer := &bytes.Buffer{}
	escritor := zip.NewWriter(buffer)
	// Ordem importa para o LibreOffice aceitar o pacote: [Content_Types].xml,
	// depois _rels/.rels, depois word/document.xml.
	entradas := []struct{ nome, conteudo string }{
		{"[Content_Types].xml", contentTypes},
		{"_rels/.rels", rels},
		{"word/document.xml", documento},
	}
	for _, entrada := range entradas {
		parte, err := escritor.Create(entrada.nome)
		if err != nil {
			t.Fatalf("criar entrada %q do zip: %v", entrada.nome, err)
		}
		if _, err := parte.Write([]byte(entrada.conteudo)); err != nil {
			t.Fatalf("escrever entrada %q do zip: %v", entrada.nome, err)
		}
	}
	if err := escritor.Close(); err != nil {
		t.Fatalf("fechar zip do fixture: %v", err)
	}
	return buffer.Bytes()
}

// subirSidecar builda e sobe o sidecar de conversão a partir do Dockerfile do
// repo. O contexto de build é ".." relativo a este pacote porque go test roda
// com cwd em backend/internal/infra/pdfconv/, e o Dockerfile precisa enxergar
// a raiz do repositório (onde vive deploy/).
func subirSidecar(ctx context.Context, t *testing.T) (testcontainers.Container, string) {
	t.Helper()

	instancia, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			FromDockerfile: testcontainers.FromDockerfile{
				// go test roda com cwd em backend/internal/infra/pdfconv/: quatro
				// níveis (pdfconv → infra → internal → backend) sobem até a raiz
				// do repo, onde deploy/Dockerfile.libreoffice vive.
				Context:       "../../../..",
				Dockerfile:    "deploy/Dockerfile.libreoffice",
				PrintBuildLog: true,
			},
			ExposedPorts: []string{"2004/tcp"},
			HostConfigModifier: func(cfg *container.HostConfig) {
				cfg.NetworkMode = container.NetworkMode(os.Getenv("MIGRACOES_REDE_CONTAINER"))
				cfg.PortBindings = network.PortMap{network.MustParsePort("2004/tcp"): {{HostIP: netip.MustParseAddr("127.0.0.1"), HostPort: "0"}}}
			},
			WaitingFor: wait.ForHTTP(CaminhoSaude).WithPort("2004/tcp").WithStartupTimeout(3 * time.Minute),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("subir sidecar de conversão: %v", err)
	}
	t.Cleanup(func() {
		// Contexto novo e próprio: o contexto do teste pode já ter estourado
		// quando o teardown roda. Mesmo padrão de
		// internal/data/postgres/postgres_integration_test.go.
		ctxCleanup, cancelarCleanup := context.WithTimeout(context.Background(), 30*time.Second) //nolint:contextcheck // ver comentário acima.
		defer cancelarCleanup()
		if err := instancia.Terminate(ctxCleanup); err != nil {
			t.Logf("remover sidecar de conversão: %v", err)
		}
	})

	porta, err := instancia.MappedPort(ctx, "2004/tcp")
	if err != nil {
		t.Fatalf("obter porta mapeada do sidecar: %v", err)
	}
	return instancia, "http://127.0.0.1:" + porta.Port()
}

func TestConverterParaPDFContraSidecarReal(t *testing.T) {
	ctx, cancelar := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancelar()

	_, url := subirSidecar(ctx, t)

	cliente, err := NovoCliente(config.Conversor{URL: url, TempoLimite: 2 * time.Minute})
	if err != nil {
		t.Fatalf("construir cliente não deveria falhar, obteve %v", err)
	}

	pdf, err := cliente.ConverterParaPDF(ctx, docxMinimo(t, "Olá mundo"))
	if err != nil {
		t.Fatalf("conversão contra sidecar real não deveria falhar, obteve %v", err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Fatalf("esperava PDF começando com %%PDF-, obteve os primeiros bytes: %q", pdf[:min(len(pdf), 32)])
	}
	if len(pdf) <= 400 {
		t.Fatalf("esperava PDF com mais de 400 bytes, obteve %d", len(pdf))
	}
}

func TestVerificadorContraSidecarReal(t *testing.T) {
	ctx, cancelar := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancelar()

	_, url := subirSidecar(ctx, t)

	cliente, err := NovoCliente(config.Conversor{URL: url, TempoLimite: 2 * time.Minute})
	if err != nil {
		t.Fatalf("construir cliente não deveria falhar, obteve %v", err)
	}

	if err := NovoVerificador(cliente).Verificar(ctx); err != nil {
		t.Fatalf("esperava sidecar saudável, obteve %v", err)
	}
}

func TestConverterParaPDFTempoLimiteCurtoAbortaContraSidecarReal(t *testing.T) {
	ctx, cancelar := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancelar()

	_, url := subirSidecar(ctx, t)

	cliente, err := NovoCliente(config.Conversor{URL: url, TempoLimite: time.Millisecond})
	if err != nil {
		t.Fatalf("construir cliente não deveria falhar, obteve %v", err)
	}

	inicio := time.Now()
	pdf, err := cliente.ConverterParaPDF(ctx, docxMinimo(t, "Olá mundo"))
	decorrido := time.Since(inicio)

	if pdf != nil {
		t.Fatal("não pode devolver PDF quando o tempo limite estourou")
	}
	var aplicacao *errors.ErroAplicacao
	if !errors.Como(err, &aplicacao) {
		t.Fatalf("esperava *errors.ErroAplicacao, obteve %T (%v)", err, err)
	}
	if decorrido >= 5*time.Second {
		t.Fatalf("esperava abortar rápido em vez de esperar o LibreOffice terminar, levou %v", decorrido)
	}
}

func TestConverterParaPDFConversorIndisponivelDevolveErroAplicacao(t *testing.T) {
	ctx, cancelar := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelar()

	cliente, err := NovoCliente(config.Conversor{URL: "http://127.0.0.1:1", TempoLimite: 5 * time.Second})
	if err != nil {
		t.Fatalf("construir cliente não deveria falhar, obteve %v", err)
	}

	pdf, err := cliente.ConverterParaPDF(ctx, docxMinimo(t, "Olá mundo"))
	if pdf != nil {
		t.Fatal("não pode devolver PDF quando o conversor está indisponível")
	}
	var aplicacao *errors.ErroAplicacao
	if !errors.Como(err, &aplicacao) {
		t.Fatalf("esperava *errors.ErroAplicacao, obteve %T (%v)", err, err)
	}
	var validacao *errors.ErroValidacao
	if errors.Como(err, &validacao) {
		t.Fatalf("conversor indisponível não pode virar *errors.ErroValidacao: %v", err)
	}
}
