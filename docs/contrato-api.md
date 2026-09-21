# Contrato da API — para quem constrói o frontend

**Atualizado:** 2026-09-20 · Base: `http://localhost:8080` em desenvolvimento

Tudo sob o prefixo **`/v1`**. `GET /prontidao` responde **404**; o caminho é
`/v1/prontidao`. Healthcheck configurado sem o prefixo falha em silêncio.

---

## Sessão

Não há login ainda (é a F7). A identidade é uma **sessão anônima** criada pelo
próprio backend no primeiro `POST /v1/documentos`, entregue no cookie:

```
sessao_id   httpOnly · Secure · SameSite=Strict
```

O frontend **nunca lê nem escreve esse cookie** — é `httpOnly` por decisão de
segurança, e token de sessão em `localStorage` é proibido no projeto. Basta
enviar as credenciais em toda chamada:

```ts
fetch(url, { credentials: 'include', ... })
```

Sem cookie, o usuário simplesmente não tem documentos. Isso **não** é erro: a
listagem devolve `[]` com 200.

⚠️ `Secure` significa que o cookie só trafega em HTTPS — ou em `localhost`, que
os navegadores tratam como origem segura. Em rede local por IP (`192.168.x.x`)
o cookie é descartado silenciosamente e nada funciona.

---

## Formato de erro

Toda falha devolve o mesmo envelope:

```json
{
  "codigo": "requisicao_invalida",
  "descricao": "não foi possível aceitar o arquivo enviado",
  "razoes": ["arquivo: o pacote excede os limites de descompressão permitidos"]
}
```

`razoes` é opcional e só aparece em erro de validação.

**Mostre as `razoes` ao usuário, não a `descricao` genérica.** Elas explicam por
que o artigo foi recusado — "não é um pacote DOCX válido", "excede os limites de
descompressão". Trocá-las por "algo deu errado" transforma toda a validação do
backend numa caixa preta para quem está tentando submeter um trabalho.

### Códigos

| `codigo` | HTTP | Quando |
|---|---|---|
| `requisicao_invalida` | 400 | entrada rejeitada pela validação |
| `acesso_nao_autorizado` | 401 | (reservado para a F7) |
| `acesso_restrito` | 403 | (reservado para a F7) |
| `recurso_nao_encontrado` | 404 | não existe **ou** é de outro dono |
| `conflito` | 409 | transição de estado inválida ou documento ainda sem preview |
| `limite_de_requisicoes_excedido` | 429 | (reservado para a F7) |
| `erro_interno` | 500 | falha do servidor; a causa fica só no log |

**404 não distingue "não existe" de "é de outra pessoa"** — propositalmente. As
duas situações usam o mesmo erro público. Isso não constitui prova de tempo
constante nem garante igualdade de todos os cabeçalhos da resposta.

---

## Endpoints

### `GET /v1/saude`

Liveness. Responde 200 se o processo está de pé.

### `GET /v1/prontidao`

Readiness — checa Postgres, storage e conversor.

```json
{ "pronto": true,
  "dependencias": [ { "nome": "postgres",  "estado": "disponivel" },
                    { "nome": "storage",   "estado": "disponivel" },
                    { "nome": "conversor", "estado": "disponivel" } ] }
```

Com alguma dependência fora, responde **503** com o envelope de erro. A causa
real fica só no log do servidor: a mensagem pública é fixa, porque esta rota é
pública e sem autenticação.

### `POST /v1/documentos`

Envia um artigo. **`multipart/form-data`**, campo **`arquivo`**.

Não defina `Content-Type` à mão ao usar `FormData` — o navegador precisa gerar
o boundary sozinho.

**201**
```json
{ "id": "a670e6a2-bfc4-49e8-b5d7-aaaf7220e055",
  "nome_original": "artigo.docx",
  "formato": "docx",
  "tamanho_bytes": 5427,
  "status": "recebido",
  "tem_preview": true }
```

A conversão para PDF é **síncrona** nesta versão: quando o 201 chega, o preview
já existe (`tem_preview: true`). A requisição pode levar alguns segundos com
documento grande — mostre estado de carregamento.

**Rejeições, todas com `razoes` específicas:**

| Situação | HTTP |
|---|---|
| campo `arquivo` ausente | 400 |
| conteúdo não é DOCX (verificado por **magic bytes**, não pela extensão) | 400 |
| ZIP válido sem as partes obrigatórias do DOCX | 400 |
| zip bomb — razão de descompressão, nº de entradas ou tamanho total | 400 |
| acima de 25 MiB | 400 |
| corpo acima do teto de transporte | 413 |

Renomear um `.txt` para `.docx` **não** passa: a validação é por conteúdo.

### `GET /v1/documentos`

Lista os documentos da sessão, mais recentes primeiro.

Query string opcional: `limite` (padrão 20, máximo 100) e `deslocamento`
(padrão 0). O controlador converte ausente ou não numérico em zero. O domínio
normaliza `limite <= 0` para 20, `limite > 100` para 100 e `deslocamento < 0`
para zero, **sem erro**.

**200** — array de documentos no mesmo formato do 201. Sem cookie, devolve `[]`.
Nunca devolve `null`.

### `GET /v1/documentos/{id}`

Metadados de um documento. **200** com o mesmo formato. **404** se não existe ou
é de outro dono.

### `GET /v1/documentos/{id}/preview`

**200**
```json
{ "url": "http://127.0.0.1:9000/documentos/...?X-Amz-Signature=...",
  "expira_em": "2026-09-20T13:05:00Z" }
```

URL pré-assinada, **somente GET**, válida por 15 minutos. Aponta direto para o
storage (MinIO/S3), **não** para a API.

Duas consequências práticas:

1. O host da URL precisa ser acessível ao navegador, e os cabeçalhos precisam
   permitir a exibição. No compose, `STORAGE_ENDPOINT=http://minio:9000` também
   é usado na assinatura: esse nome interno pode não resolver no browser.
   É risco inferido do código, não falha reproduzida por E2E nesta revisão.
2. Ela expira. Não guarde em cache de longa duração nem em estado persistente;
   busque de novo quando precisar.

**404** se o documento não existe ou é de outro dono. **409** se o documento
autorizado ainda não tem preview. Confira `tem_preview` antes de pedir.

---

## Valores de `status`

| Valor | Significado |
|---|---|
| `recebido` | arquivo aceito e armazenado, sem análise |
| `analisando` | extração de estrutura em andamento (F2) |
| `analisado` | CDM disponível, pronto para formatar (F2) |
| `formatando` | motor aplicando o ruleset (F3) |
| `formatado` | artefatos prontos para download (F3) |
| `falhou` | processamento interrompido por erro |

No fluxo HTTP síncrono atual, o upload bem-sucedido devolve `recebido`.
As transições de processamento já existem no domínio, mas o upload não
enfileira jobs; análise e formatação ainda não são expostas pela API.

---

## O que **ainda não existe**

Não construa tela para estes; eles chegam nas fases seguintes:

- estrutura detectada do documento e correção manual de papel de bloco (F2)
- catálogo de revistas e escolha de ruleset (F3)
- disparar formatação e baixar DOCX/PDF/LaTeX (F3, F7)
- acompanhamento de job por polling ou SSE (F2/F3)
- cadastro, login e histórico por usuário (F7)

O `plano-backend.md` tem a ordem e o que cada fase passa a expor.

---

## Rodando o backend localmente

```bash
make up          # postgres, minio, sidecar de conversão, api e worker
make migrar      # aplica as migrations
```

Sem containers, só a API (exige Postgres, MinIO e o sidecar de pé):

```bash
cd backend && POSTGRES_DSN=... STORAGE_ENDPOINT=... CONVERSOR_URL=... go run ./cmd/api
```

Confira antes de investigar qualquer bug de frontend:

```bash
curl -s http://localhost:8080/v1/prontidao
```

Se alguma dependência estiver `indisponivel`, o upload falha por motivo de
infraestrutura, não de código.

### Fixture para testar

`backend/testdata/artigo-desformatado.docx` é um artigo acadêmico completo —
resumo, seções, citações, tabela, figura e referências — deliberadamente fora da
norma. Serve para exercitar o fluxo inteiro.
