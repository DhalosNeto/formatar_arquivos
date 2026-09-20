//go:build integration

package storage

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"net/http"
	"net/netip"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/config"
)

const (
	minioUsuarioTeste = "usuario-teste"
	minioSenhaTeste   = "senha-teste-1234"
	bucketTeste       = "documentos-teste"
)

// TestRoundTripSalvarEObterContraMinIO prova que Salvar/Obter contra um MinIO
// real gravam e devolvem os mesmos bytes — a spec unitária só valida entrada
// antes da rede, nunca chega a tocar o objeto de verdade.
func TestRoundTripSalvarEObterContraMinIO(t *testing.T) {
	ctx, cancelar := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancelar()

	cliente, _ := novoClienteMinIO(t, ctx)

	conteudo := make([]byte, 4096)
	_, err := rand.Read(conteudo)
	if err != nil {
		t.Fatal(err)
	}

	chave := vo.ChaveStorage("documentos/roundtrip/arquivo.bin")

	if err := cliente.Salvar(ctx, chave, bytes.NewReader(conteudo), int64(len(conteudo)), "application/octet-stream"); err != nil {
		t.Fatalf("Salvar não deveria falhar: %v", err)
	}

	corpo, err := cliente.Obter(ctx, chave)
	if err != nil {
		t.Fatalf("Obter não deveria falhar: %v", err)
	}
	defer corpo.Close()

	lido, err := io.ReadAll(corpo)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(lido, conteudo) {
		t.Fatalf("conteúdo lido difere do gravado: %d bytes gravados, %d bytes lidos", len(conteudo), len(lido))
	}
}

// TestURLPreAssinadaFuncionaViaHTTPDireto prova que a URL devolvida por
// URLPreAssinada é EFETIVA: chamada por http.Get puro, sem passar pelo SDK,
// tem que devolver 200 e o conteúdo certo. Isso comprova operação (GET),
// bucket, chave e validade reais — não só que a string foi montada.
func TestURLPreAssinadaFuncionaViaHTTPDireto(t *testing.T) {
	ctx, cancelar := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancelar()

	cliente, _ := novoClienteMinIO(t, ctx)

	conteudo := []byte("conteúdo assinado de teste — 文 😀")
	chave := vo.ChaveStorage("documentos/assinada/arquivo.txt")
	if err := cliente.Salvar(ctx, chave, bytes.NewReader(conteudo), int64(len(conteudo)), "text/plain"); err != nil {
		t.Fatalf("Salvar não deveria falhar: %v", err)
	}

	url, err := cliente.URLPreAssinada(ctx, chave, time.Minute)
	if err != nil {
		t.Fatalf("URLPreAssinada não deveria falhar: %v", err)
	}

	resposta, err := http.Get(url) //nolint:gosec,noctx // URL de teste gerada localmente, sem entrada do usuário.
	if err != nil {
		t.Fatalf("GET direto na URL assinada falhou: %v", err)
	}
	defer resposta.Body.Close()

	if resposta.StatusCode != http.StatusOK {
		t.Fatalf("esperava 200 na URL assinada, obteve %d", resposta.StatusCode)
	}
	lido, err := io.ReadAll(resposta.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(lido, conteudo) {
		t.Fatalf("conteúdo devolvido pela URL assinada difere do gravado: obteve %q", lido)
	}
}

// TestBucketNaoEhPublico prova que, sem uma URL assinada válida, o objeto não
// é acessível: nem por acesso direto à chave, nem por uma URL assinada já
// expirada. Sem isso a suíte não distingue "assinatura funciona" de "bucket é
// público e a assinatura nunca foi verificada".
func TestBucketNaoEhPublico(t *testing.T) {
	ctx, cancelar := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancelar()

	cliente, endpoint := novoClienteMinIO(t, ctx)

	conteudo := []byte("segredo")
	chave := vo.ChaveStorage("documentos/privado/arquivo.txt")
	if err := cliente.Salvar(ctx, chave, bytes.NewReader(conteudo), int64(len(conteudo)), "text/plain"); err != nil {
		t.Fatalf("Salvar não deveria falhar: %v", err)
	}

	t.Run("acesso_direto_sem_assinatura", func(t *testing.T) {
		urlDireta := endpoint + "/" + bucketTeste + "/" + chave.String()
		resposta, err := http.Get(urlDireta) //nolint:gosec,noctx // URL de teste gerada localmente.
		if err != nil {
			t.Fatalf("GET direto falhou por rede, não por autorização: %v", err)
		}
		defer resposta.Body.Close()
		if resposta.StatusCode == http.StatusOK {
			t.Fatal("bucket privado devolveu 200 sem assinatura: objeto está público")
		}
	})

	t.Run("url_assinada_expirada", func(t *testing.T) {
		// Validade mínima positiva permitida pelo contrato de URLPreAssinada é
		// usada aqui, e o teste aguarda o relógio ultrapassar essa validade
		// antes de bater na URL — sem isso o teste provaria só que o SDK
		// aceita "1s", não que o MinIO recusa depois de vencer.
		validadeCurta := time.Second
		url, err := cliente.URLPreAssinada(ctx, chave, validadeCurta)
		if err != nil {
			t.Fatalf("URLPreAssinada não deveria falhar: %v", err)
		}

		time.Sleep(validadeCurta + 2*time.Second)

		resposta, err := http.Get(url) //nolint:gosec,noctx // URL de teste gerada localmente.
		if err != nil {
			t.Fatalf("GET na URL expirada falhou por rede, não por autorização: %v", err)
		}
		defer resposta.Body.Close()
		if resposta.StatusCode == http.StatusOK {
			t.Fatal("URL assinada expirada ainda devolveu 200: validade não é efetiva")
		}
	})
}

// soReader expõe só Read, de propósito: garante em tempo de compilação que o
// corpo passado a Salvar NÃO satisfaz io.Seeker, io.ReaderAt nem io.WriterTo —
// o cenário real de produção é um corpo de upload HTTP multipart, que também
// não é seekable. Um bytes.Reader (usado nos outros testes) satisfaz todas
// essas interfaces e mascararia justamente o bug que motivou a troca de
// PutObject para manager.Uploader.
type soReader struct{ io.Reader }

// TestSalvarComCorpoNaoSeekableFazRoundTrip prova que Salvar aceita um
// io.Reader puro, sem Seek/ReadAt/WriteTo — o corpo real de um upload HTTP
// multipart — e grava exatamente os mesmos bytes. É o regression test direto
// do achado corrigido nesta rodada: PutObject com io.LimitReader por cima
// falhava contra S3/MinIO real com "failed to seek body to start, request
// stream is not seekable"; manager.Uploader bufferiza em bytes.Reader
// internamente e não exige que o corpo original seja seekable.
func TestSalvarComCorpoNaoSeekableFazRoundTrip(t *testing.T) {
	ctx, cancelar := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancelar()

	cliente, _ := novoClienteMinIO(t, ctx)

	conteudo := make([]byte, 4096)
	if _, err := rand.Read(conteudo); err != nil {
		t.Fatal(err)
	}

	// Garante em runtime, e não só na assinatura do tipo, que o corpo
	// entregue ao SDK não satisfaz Seeker: um type assertion aqui pegaria
	// qualquer refatoração futura que trocasse soReader por algo seekable.
	var corpo io.Reader = soReader{bytes.NewReader(conteudo)}
	if _, seekavel := corpo.(io.Seeker); seekavel {
		t.Fatal("soReader não deveria satisfazer io.Seeker")
	}

	chave := vo.ChaveStorage("documentos/nao-seekable/arquivo.bin")
	if err := cliente.Salvar(ctx, chave, corpo, int64(len(conteudo)), "application/octet-stream"); err != nil {
		t.Fatalf("Salvar não deveria falhar com corpo não-seekable: %v", err)
	}

	obtido, err := cliente.Obter(ctx, chave)
	if err != nil {
		t.Fatalf("Obter não deveria falhar: %v", err)
	}
	defer obtido.Close()

	lido, err := io.ReadAll(obtido)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(lido, conteudo) {
		t.Fatalf("conteúdo lido difere do gravado: %d bytes gravados, %d bytes lidos", len(conteudo), len(lido))
	}
}

// TestSalvarComLeitorMentirosoTruncaNoTamanhoDeclarado prova que o teto de
// segurança do achado A2 continua valendo depois da troca de PutObject para
// manager.Uploader: um leitor com mais dados reais do que o tamanho
// declarado só pode gravar até o tamanho declarado, nunca mais.
func TestSalvarComLeitorMentirosoTruncaNoTamanhoDeclarado(t *testing.T) {
	ctx, cancelar := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancelar()

	cliente, _ := novoClienteMinIO(t, ctx)

	const tamanhoDeclarado = 1024
	conteudoReal := make([]byte, 10*1024) // 10 KiB reais, dez vezes o declarado
	if _, err := rand.Read(conteudoReal); err != nil {
		t.Fatal(err)
	}

	chave := vo.ChaveStorage("documentos/mentiroso/arquivo.bin")
	if err := cliente.Salvar(ctx, chave, bytes.NewReader(conteudoReal), tamanhoDeclarado, "application/octet-stream"); err != nil {
		t.Fatalf("Salvar não deveria falhar: %v", err)
	}

	obtido, err := cliente.Obter(ctx, chave)
	if err != nil {
		t.Fatalf("Obter não deveria falhar: %v", err)
	}
	defer obtido.Close()

	lido, err := io.ReadAll(obtido)
	if err != nil {
		t.Fatal(err)
	}
	if len(lido) != tamanhoDeclarado {
		t.Fatalf("esperava exatamente %d bytes gravados (teto do io.LimitReader), obteve %d", tamanhoDeclarado, len(lido))
	}
	if !bytes.Equal(lido, conteudoReal[:tamanhoDeclarado]) {
		t.Fatal("bytes gravados não são o prefixo do conteúdo real: corte não aconteceu no lugar certo")
	}
}

// TestSalvarComCorpoMenorQueTamanhoDeclaradoNaoFalha prova que um corpo com
// menos bytes reais do que o tamanho declarado não é erro: o io.LimitReader
// simplesmente devolve EOF mais cedo, e o objeto gravado tem exatamente os
// bytes reais, sem padding e sem Content-Length mentiroso (o Uploader deriva
// o Content-Length do que foi de fato lido, não do parâmetro tamanho).
func TestSalvarComCorpoMenorQueTamanhoDeclaradoNaoFalha(t *testing.T) {
	ctx, cancelar := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancelar()

	cliente, _ := novoClienteMinIO(t, ctx)

	conteudoReal := []byte("0123456789") // 10 bytes reais
	const tamanhoDeclarado = 4096

	chave := vo.ChaveStorage("documentos/menor-que-declarado/arquivo.bin")
	if err := cliente.Salvar(ctx, chave, bytes.NewReader(conteudoReal), tamanhoDeclarado, "application/octet-stream"); err != nil {
		t.Fatalf("Salvar não deveria falhar quando o corpo real é menor que tamanho: %v", err)
	}

	obtido, err := cliente.Obter(ctx, chave)
	if err != nil {
		t.Fatalf("Obter não deveria falhar: %v", err)
	}
	defer obtido.Close()

	lido, err := io.ReadAll(obtido)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(lido, conteudoReal) {
		t.Fatalf("esperava exatamente os %d bytes reais sem padding, obteve %d bytes: %q", len(conteudoReal), len(lido), lido)
	}
}

// novoClienteMinIO sobe um container MinIO descartável, cria o bucket de
// teste via SDK e devolve um ClienteS3 apontado para ele, junto do endpoint
// HTTP usado (para os testes que precisam montar uma URL à mão). Reaproveita
// o padrão de backend/migrations/documento_dono_test.go (HostConfigModifier +
// WaitingFor + Cleanup com contexto próprio).
func novoClienteMinIO(t *testing.T, ctx context.Context) (*ClienteS3, string) {
	t.Helper()

	instancia, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			// Registro oficial da MinIO: docker.io/minio/minio nega pull anônimo.
			// Fixado por digest para a suíte não quebrar sozinha quando a tag mover.
			Image: "quay.io/minio/minio@sha256:14cea493d9a34af32f524e538b8346cf79f3321eff8e708c1e2960462bd8936e",
			Env: map[string]string{
				"MINIO_ROOT_USER":     minioUsuarioTeste,
				"MINIO_ROOT_PASSWORD": minioSenhaTeste,
			},
			Cmd:          []string{"server", "/data"},
			ExposedPorts: []string{"9000/tcp"},
			HostConfigModifier: func(cfg *container.HostConfig) {
				cfg.NetworkMode = container.NetworkMode(os.Getenv("MIGRACOES_REDE_CONTAINER"))
				cfg.PortBindings = network.PortMap{network.MustParsePort("9000/tcp"): {{HostIP: netip.MustParseAddr("127.0.0.1"), HostPort: "0"}}}
			},
			WaitingFor: wait.ForHTTP("/minio/health/live").WithPort("9000/tcp").WithStartupTimeout(time.Minute),
		},
		Started: true,
	})
	if instancia != nil {
		t.Cleanup(func() {
			// Contexto novo e próprio, de propósito: o contexto do teste pode já
			// ter sido cancelado quando o cleanup roda, e Terminate precisa de um
			// contexto vivo para desmontar o container. Mesmo padrão do harness de
			// Postgres em backend/migrations/documento_dono_test.go.
			ctxCleanup, cancelarCleanup := context.WithTimeout(context.Background(), 30*time.Second) //nolint:contextcheck // ver comentário acima.
			defer cancelarCleanup()
			if err := instancia.Terminate(ctxCleanup); err != nil {
				t.Errorf("remover MinIO descartável: %v", err)
			}
		})
	}
	if err != nil {
		t.Fatal(err)
	}

	porta, err := instancia.MappedPort(ctx, "9000/tcp")
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "http://127.0.0.1:" + porta.Port()

	cfg := config.Storage{
		Endpoint:  endpoint,
		Bucket:    bucketTeste,
		AccessKey: minioUsuarioTeste,
		SecretKey: minioSenhaTeste,
		Regiao:    "us-east-1",
	}

	cliente, err := NovoClienteS3(ctx, cfg)
	if err != nil {
		t.Fatalf("construir cliente contra MinIO real: %v", err)
	}

	if _, err := cliente.s3.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucketTeste)}); err != nil {
		t.Fatalf("criar bucket de teste no MinIO: %v", err)
	}

	return cliente, endpoint
}
