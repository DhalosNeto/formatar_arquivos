package documentos

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"

	"github.com/daniel-halos/formatador/internal/infra/errors"
	"github.com/daniel-halos/formatador/internal/rotas"
	"github.com/daniel-halos/formatador/internal/rotas/rotasutil"
	"github.com/daniel-halos/formatador/internal/rotas/sessao"
)

const tamanhoMaximoCorrecao = 4096

// TratarCorrecao aplica a correção manual do papel de um bloco.
//
// Vive no mesmo Controlador do resto de /documentos: a estrutura é o mesmo
// recurso, e um controlador paralelo obrigava a raiz a registrar o pacote
// documentos duas vezes.
func (controlador *Controlador) TratarCorrecao(ctx context.Context, requisicao rotas.Requisicao, resposta rotas.Resposta) error {
	id, err := rotasutil.IDDaRota(requisicao, campoID)
	if err != nil {
		return rotasutil.TratarErro(ctx, resposta, err)
	}
	dono, err := sessao.Existente(requisicao, recursoDocumento)
	if err != nil {
		return rotasutil.TratarErro(ctx, resposta, err)
	}
	tipo, _, err := mime.ParseMediaType(requisicao.Cabecalho("Content-Type"))
	if err != nil || tipo != "application/json" {
		return rotasutil.TratarErro(ctx, resposta, erroCorrecaoInvalida())
	}
	dados, err := lerCorrecao(requisicao.Corpo())
	if err != nil {
		return rotasutil.TratarErro(ctx, resposta, err)
	}
	estrutura, err := controlador.estrutura.Corrigir(ctx, dono, id, dados.refXML, dados.papel, dados.nivel)
	if err != nil {
		return rotasutil.TratarErro(ctx, resposta, err)
	}
	return resposta.Ok(estrutura)
}

type correcaoJSON struct {
	refXML int
	papel  string
	nivel  int
}

func erroCorrecaoInvalida() error {
	return errors.NovoErroValidacao("estrutura", "informe ref_xml, papel e nível opcional em um único objeto JSON válido de até 4096 bytes")
}

// A leitura por tokens rejeita duplicatas que json.Unmarshal aceitaria.
func lerCorrecao(corpo io.Reader) (correcaoJSON, error) {
	var correcao correcaoJSON
	dados, err := io.ReadAll(io.LimitReader(corpo, tamanhoMaximoCorrecao+1))
	if err != nil || len(dados) > tamanhoMaximoCorrecao {
		return correcao, erroCorrecaoInvalida()
	}
	leitor := json.NewDecoder(bytes.NewReader(dados))
	token, err := leitor.Token()
	if err != nil || token != json.Delim('{') {
		return correcao, erroCorrecaoInvalida()
	}
	vistos := make(map[string]bool)
	for leitor.More() {
		token, err = leitor.Token()
		if err != nil {
			return correcao, erroCorrecaoInvalida()
		}
		nome, ok := token.(string)
		if !ok || vistos[nome] {
			return correcao, erroCorrecaoInvalida()
		}
		vistos[nome] = true
		var bruto json.RawMessage
		if err = leitor.Decode(&bruto); err != nil || bytes.Equal(bytes.TrimSpace(bruto), []byte("null")) {
			return correcao, erroCorrecaoInvalida()
		}
		switch nome {
		case "ref_xml":
			err = json.Unmarshal(bruto, &correcao.refXML)
		case "papel":
			err = json.Unmarshal(bruto, &correcao.papel)
		case "nivel":
			err = json.Unmarshal(bruto, &correcao.nivel)
		default:
			return correcao, erroCorrecaoInvalida()
		}
		if err != nil {
			return correcao, erroCorrecaoInvalida()
		}
	}
	if _, err = leitor.Token(); err != nil {
		return correcao, erroCorrecaoInvalida()
	}
	if err = leitor.Decode(new(any)); !errors.E(err, io.EOF) || !vistos["ref_xml"] || !vistos["papel"] {
		return correcao, erroCorrecaoInvalida()
	}
	return correcao, nil
}
