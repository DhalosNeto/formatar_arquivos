# Contrato da API — para quem constrói o frontend

**Atualizado:** 2026-09-22 · Base: `http://localhost:8080` em desenvolvimento

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

### `POST /v1/documentos/{id}/analisar`

Dispara a análise estrutural. **Assíncrona**: responde **202** com o job, não
com o resultado.

```json
{ "id": "…", "documento_id": "…", "tipo": "analisar",
  "status": "pendente", "progresso": 0, "criado_em": "2026-09-21T00:00:00Z" }
```

**Chamar duas vezes devolve o MESMO job**, com o mesmo `id` — a chave de
idempotência é derivada do documento no servidor. Duplo clique no botão não
cria duas análises concorrentes, então não é preciso desabilitá-lo por medo
disso (desabilite por clareza, se quiser).

Para acompanhar, consulte `GET /v1/jobs/{id}` usando o ID retornado.
O documento também expõe o `status`: `recebido` → `analisando` →
`analisado`, ou `falhou`.

**404** se o documento não existe ou é de outro dono.

### `GET /v1/documentos/{id}/estrutura`

O CDM do documento — o que o sistema entendeu da estrutura dele.

```json
{ "versao": 1,
  "blocos": [
    { "papel": "titulo", "texto_resumo": "A PERCEPÇÃO DE ESTUDANTES…",
      "confianca": 0.95, "origem": "estilo-docx", "ref_xml": 0 },
    { "papel": "secao", "nivel": 1, "texto_resumo": "1 INTRODUÇÃO",
      "confianca": 0.95, "origem": "estilo-docx", "ref_xml": 12 }
  ] }
```

`nivel` **só aparece em `papel: "secao"`**. Não trate sua ausência como erro.

`texto_resumo` é um trecho de **até 200 caracteres**, não o texto do bloco.
Serve para o usuário reconhecer o bloco na tela; não reconstrua o documento a
partir dele.

`papel` ∈ `titulo`, `lista_autores`, `resumo`, `palavras_chave`, `secao`,
`paragrafo`, `citacao`, `item_lista`, `tabela`, `figura`, `legenda`,
`equacao`, `referencia`, `nota_rodape`.

`origem` ∈ `estilo-docx`, `heuristica`, `llm`, `usuario` — quem classificou.
`confianca` vai de 0 a 1. **Bloco de confiança baixa é onde o usuário mais
provavelmente precisa corrigir**; vale destacar na interface.

⚠️ **`lista_autores` não é identificado automaticamente hoje.** Nenhuma evidência textual
confiável separa um nome de autor de um parágrafo comum, então autores e
afiliações vêm como `paragrafo` com confiança baixa. É trabalho da correção
manual ou do fallback Jev experimental, se habilitado no worker.

Com `JEV_HABILITADO=true`, a resposta pode conter `revisoes` (omitido sem
pendências). Cada item identifica um bloco existente:

```json
{"ref_xml": 7, "papel_sugerido": {"nome": "secao", "nivel": 2},
 "confianca": 0.82, "acao": "confirmar"}
```

`acao` é `confirmar` ou `revisar`. `papel_sugerido` é opcional e usa
objeto com `nome` e `nivel` opcional, enquanto o bloco mantém o papel efetivo.
Falha no classificador inclui `motivo: "classificador_indisponivel"`.
Use o PATCH existente para confirmar ou escolher outro papel: ele remove
a revisão desse bloco. Confiança baixa/sem correspondência e candidatos além
do orçamento também geram revisão, sem alterar a classificação determinística.
O backend oferece esses dados; a tela de confirmação ainda não foi implementada.

**409** se o documento ainda não foi analisado — confira o `status` antes.
**404** se não existe ou é de outro dono.

---

### `PATCH /v1/documentos/{id}/estrutura`

Corrige o papel de um bloco de documento `analisado`. Envie
`Content-Type: application/json`, cookie de sessão e até 4096 bytes:

```json
{"ref_xml": 0, "papel": "secao", "nivel": 2}
```

`ref_xml` e `papel` são obrigatórios. `nivel` deve ser de 1 a 6 para `secao`;
nos outros papéis, omita ou envie zero. Campos desconhecidos, duplicados,
valores `null` e JSON adicional são recusados. Não envie texto nem origem.

**200** devolve a estrutura completa, no formato do GET. O bloco corrigido
fica com `origem: "usuario"` e `confianca: 1`, preservados pela classificação
automática. **400** indica corpo, papel ou referência inválidos; **404**
indica ausência de sessão, documento ausente ou de terceiro; **409** indica
status incompatível ou alteração concorrente; **500** indica CDM corrompido.

A gravação compara o CDM lido pelo servidor com o persistido. Em **409**,
busque a estrutura novamente. Isso não detecta formulários antigos no navegador:
o contrato ainda não oferece revisão/ETag enviada pelo cliente.

### `GET /v1/jobs/{id}`

Consulta o processamento de um documento da sessão. **200** retorna o mesmo
formato de job do POST de análise, com `status` e `progresso` atuais.
Os status são `pendente`, `executando`, `concluido`, `falhou` e `cancelado`
(não há endpoint público de cancelamento nesta fase).
A resposta não expõe `resultado`, chave de storage ou conteúdo do documento.
**400** para ID inválido; **404** para ausência de sessão, job inexistente
ou documento de terceiro. Use polling; SSE ainda não está disponível.

## Valores de `status`

| Valor | Significado |
|---|---|
| `recebido` | arquivo aceito e armazenado, sem análise |
| `analisando` | extração de estrutura em andamento (F2) |
| `analisado` | CDM disponível, pronto para formatar (F2) |
| `formatando` | motor aplicando o ruleset (F3) |
| `formatado` | artefatos prontos para download (F3) |
| `falhou` | processamento interrompido por erro |

O upload bem-sucedido devolve `recebido` — a conversão do preview é síncrona.
A **análise** é assíncrona: `POST .../analisar` devolve 202 e o status caminha
`recebido` → `analisando` → `analisado`, ou `falhou`. A formatação (F3) ainda
não é exposta.

---

## O que **ainda não existe**

Não construa tela para estes; eles chegam nas fases seguintes:

- catálogo de revistas e escolha de ruleset (F3)
- disparar formatação e baixar DOCX/PDF/LaTeX (F3, F7)
- acompanhamento de job por SSE (polling via GET já disponível)
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
