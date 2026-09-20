// Package storage adapta o armazenamento de documentos a um bucket compatível
// com S3 (Amazon S3 em produção, MinIO em desenvolvimento).
package storage

import (
	"context"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/daniel-halos/formatador/internal/domain/vo"
	"github.com/daniel-halos/formatador/internal/infra/config"
	"github.com/daniel-halos/formatador/internal/infra/errors"
)

// ValidadePadraoURL é a validade sugerida para uma URL pré-assinada quando o
// chamador não tem um requisito específico.
const ValidadePadraoURL = 15 * time.Minute

// ValidadeMaximaURL é o teto de validade aceito para uma URL pré-assinada.
const ValidadeMaximaURL = time.Hour

// regiaoPadrao é usada quando a configuração não informa uma região; MinIO
// ignora o valor, mas o SDK exige que ele esteja preenchido.
const regiaoPadrao = "us-east-1"

// ClienteS3 adapta o SDK da AWS a um bucket compatível com S3.
type ClienteS3 struct {
	s3        *s3.Client
	assinador *s3.PresignClient
	// enviador usa feature/s3/manager, que o SDK marca como deprecado em favor
	// de feature/s3/transfermanager. Ficamos no manager de propósito: o
	// substituto ainda está em v0.4.7 (pré-1.0), e trocar uma dependência
	// estável por uma pré-1.0 é risco sem retorno em cima de um bug que acabamos
	// de corrigir. ponytail: dependência deprecada mas estável, teto = pré-1.0
	// do substituto, upgrade quando feature/s3/transfermanager publicar v1.0.0.
	enviador *manager.Uploader //nolint:staticcheck // SA1019: ver comentário acima
	bucket   string
}

// NovoClienteS3 valida a configuração e monta o cliente S3/MinIO.
//
// O context.Context existe só por simetria com o resto do sistema: este
// construtor não faz nenhuma chamada de rede. s3.New apenas monta valores em
// memória, e as credenciais fixas dispensam qualquer resolução (IMDS,
// arquivo de credenciais, etc.) que LoadDefaultConfig faria.
func NovoClienteS3(_ context.Context, cfg config.Storage) (*ClienteS3, error) {
	invalidos := &errors.ErroValidacao{Mensagem: "configuração de storage inválida"}

	if strings.TrimSpace(cfg.Bucket) == "" {
		invalidos.Acrescentar("bucket", "obrigatório")
	}
	if !enderecoValido(cfg.Endpoint) {
		invalidos.Acrescentar("endpoint", "precisa ser uma URL http ou https")
	}
	if invalidos.TemCampos() {
		return nil, invalidos
	}

	regiao := cfg.Regiao
	if regiao == "" {
		regiao = regiaoPadrao
	}

	cliente := s3.New(s3.Options{
		Region:       regiao,
		BaseEndpoint: aws.String(cfg.Endpoint),
		UsePathStyle: true, // MinIO: virtual-host style exigiria DNS curinga
		Credentials:  aws.CredentialsProviderFunc(credenciaisFixas(cfg)),
		// WhenRequired evita o trailer CRC32 com aws-chunked, que MinIO rejeita.
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
	})

	// O Uploader tem RequestChecksumCalculation próprio, com default WhenSupported:
	// sem repetir WhenRequired aqui, o upload multipart ligaria CRC32 e voltaria a
	// mandar trailer aws-chunked, que o MinIO rejeita.
	//nolint:staticcheck // SA1019: feature/s3/manager está deprecado, ver comentário no campo enviador
	enviador := manager.NewUploader(cliente, func(u *manager.Uploader) {
		u.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
	})

	return &ClienteS3{
		s3:        cliente,
		assinador: s3.NewPresignClient(cliente),
		enviador:  enviador,
		bucket:    cfg.Bucket,
	}, nil
}

// enderecoValido exige esquema http/https e host preenchido. url.Parse
// sozinho não basta: "minio:9000" não erra e devolve Scheme "minio" com Host
// vazio, e "://quebrado" erra na sintaxe.
func enderecoValido(endpoint string) bool {
	if strings.TrimSpace(endpoint) == "" {
		return false
	}
	endereco, err := url.Parse(endpoint)
	if err != nil {
		return false
	}
	if endereco.Scheme != "http" && endereco.Scheme != "https" {
		return false
	}
	return endereco.Host != ""
}

// credenciaisFixas devolve um provedor de credenciais que nunca faz I/O nem
// loga nada: os valores já vieram prontos da configuração.
func credenciaisFixas(cfg config.Storage) func(context.Context) (aws.Credentials, error) {
	return func(context.Context) (aws.Credentials, error) {
		return aws.Credentials{
			AccessKeyID:     cfg.AccessKey,
			SecretAccessKey: cfg.SecretKey,
			Source:          "config",
		}, nil
	}
}

// motivoChaveInvalida aplica a regra de segurança do espaço de chaves. O
// motivo exato devolvido pelo VO é descartado de propósito: repeti-lo daria a
// quem sondasse a validação um oráculo sobre a regra reprovada.
func motivoChaveInvalida(chave vo.ChaveStorage) string {
	if vo.MotivoChaveInsegura(chave.String()) == "" {
		return ""
	}
	return "chave de storage inválida"
}

// Assina exclusivamente GET. Nunca acrescente PresignPutObject aqui: URL de
// escrita assinada é upload anônimo no bucket.
//
// Este adaptador NÃO identifica o solicitante e não verifica dono nem sessão.
// Autenticação e autorização são responsabilidade da camada que chama; quem
// assinar sem checar antes está distribuindo leitura pública do objeto.
func (c *ClienteS3) URLPreAssinada(ctx context.Context, chave vo.ChaveStorage, validade time.Duration) (string, error) {
	invalidos := &errors.ErroValidacao{Mensagem: "requisição de storage inválida"}

	if motivo := motivoChaveInvalida(chave); motivo != "" {
		invalidos.Acrescentar("chave", motivo)
	}
	if validade <= 0 || validade > ValidadeMaximaURL {
		invalidos.Acrescentar("validade", "precisa ser positiva e no máximo 1h")
	}
	if invalidos.TemCampos() {
		return "", invalidos
	}

	requisicao, err := c.assinador.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(chave.String()),
	}, s3.WithPresignExpires(validade))
	if err != nil {
		return "", errors.Envolver(err, "assinar URL de leitura")
	}
	return requisicao.URL, nil
}

// Salvar grava o conteúdo no bucket sob a chave informada.
func (c *ClienteS3) Salvar(ctx context.Context, chave vo.ChaveStorage, conteudo io.Reader, tamanho int64, contentType string) error {
	invalidos := &errors.ErroValidacao{Mensagem: "requisição de storage inválida"}

	if motivo := motivoChaveInvalida(chave); motivo != "" {
		invalidos.Acrescentar("chave", motivo)
	}
	if conteudo == nil {
		invalidos.Acrescentar("conteudo", "obrigatório")
	}
	if tamanho <= 0 {
		invalidos.Acrescentar("tamanho", "precisa ser positivo")
	}
	if strings.TrimSpace(contentType) == "" {
		invalidos.Acrescentar("contentType", "obrigatório")
	}
	if invalidos.TemCampos() {
		return invalidos
	}

	// tamanho é o número de bytes DECLARADO pelo chamador e serve a uma coisa só:
	// teto do io.LimitReader, que impede um leitor mentiroso de escrever mais do
	// que anunciou. Ele NÃO vai como Content-Length: quem deriva o Content-Length
	// é o SDK, a partir dos bytes realmente lidos — um tamanho declarado maior que
	// o corpo real mentiria no protocolo. tamanho não é evidência do tamanho real
	// e não pode alimentar decisão de cota, cobrança ou persistência: a MEDIÇÃO
	// cabe à camada acima, que deve ler o conteúdo por um io.LimitReader próprio e
	// gravar o total efetivamente lido (achado A2).
	//
	// Upload (e não PutObject direto) porque o corpo de produção é um io.Reader de
	// rede, não-seekable, e a assinatura SigV4 sobre endpoint http rebobina o
	// corpo; o Uploader bufferiza cada parte num bytes.Reader seekable.
	//nolint:staticcheck // SA1019: feature/s3/manager está deprecado, ver comentário no campo enviador
	_, err := c.enviador.Upload(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(chave.String()),
		Body:        io.LimitReader(conteudo, tamanho),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return errors.Envolver(err, "salvar objeto")
	}
	return nil
}

// Obter devolve o corpo do objeto sob a chave informada. O chamador é
// responsável por fechar o corpo devolvido.
func (c *ClienteS3) Obter(ctx context.Context, chave vo.ChaveStorage) (io.ReadCloser, error) {
	if motivo := motivoChaveInvalida(chave); motivo != "" {
		return nil, errors.NovoErroValidacao("chave", motivo)
	}

	saida, err := c.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(chave.String()),
	})
	if err != nil {
		var ausente *types.NoSuchKey
		if errors.Como(err, &ausente) {
			return nil, errors.NovoErroNaoEncontrado("documento")
		}
		return nil, errors.Envolver(err, "obter objeto")
	}
	return saida.Body, nil
}

// Verificador checa se o bucket configurado está acessível.
type Verificador struct{ cliente *ClienteS3 }

// NovoVerificador cria o verificador de saúde do storage.
func NovoVerificador(cliente *ClienteS3) *Verificador {
	return &Verificador{cliente: cliente}
}

// Nome identifica esta dependência no readiness.
func (v *Verificador) Nome() string { return "storage" }

// Verificar satisfaz, por duck typing, a interface Verificador de
// internal/rotas/root/webrotas/saude (não importada aqui: infra não importa rotas).
func (v *Verificador) Verificar(ctx context.Context) error {
	_, err := v.cliente.s3.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(v.cliente.bucket)})
	return errors.Envolver(err, "verificar bucket")
}
