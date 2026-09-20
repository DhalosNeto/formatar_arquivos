# Plano do backend — F2 a F7

**Atualizado:** 2026-09-20 · Substitui as fases de `plano.md` no que diz
respeito ao backend.

Este plano cobre **só o backend**. O frontend é responsabilidade de outra
pessoa e consome `contrato-api.md`. F0 e F1 estão fechadas — ver
`estado-do-backend.md`.

Cada fase abaixo diz: **o que entra**, **o que a API passa a expor**, **critério
de pronto** e **onde costuma dar errado**.

---

## Regras que valem em toda fase

Estas não se negociam por pressa, e estão no `CLAUDE.md`:

1. **Teste primeiro.** O teste que falha vem antes da implementação.
2. **Não existe "verde" sem integração executada** contra o serviço real.
   Quatro bugs sérios passaram por teste unitário verde neste projeto — a lista
   está em `estado-do-backend.md`.
3. **Conteúdo de documento do usuário nunca** em log, mensagem de erro ou trace.
4. **Falha de infraestrutura é `ErroAplicacao`**, nunca `ErroValidacao`.
   `errors.Envolver` não reclassifica: um `ErroValidacao` na cadeia faz a API
   responder 400 e culpar o cliente por uma falha do servidor.
5. **Nomes em português**; acesso a dados só por `data/contracts`; handler nunca
   conhece o Echo.
6. Cobertura mínima de **80% em `internal/domain/`**. Hoje nenhum pacote está
   abaixo de 95% — não deixe a fase nova puxar a média para baixo.
7. Toda decisão que contraria este plano vira um **ADR** em `docs/adr/`.

---

## F2 — Parser e CDM

O risco técnico real do projeto mora aqui.

### O que entra

**`internal/infra/ooxml`** — abrir e salvar o pacote DOCX.

`Abrir(r)` desserializa as partes que interessam (`document.xml`, `styles.xml`,
`numbering.xml`, `settings.xml`, `_rels`) e **guarda as demais como bytes
crus**. `Salvar(w)` reescreve o ZIP preservando intacto o que não foi tocado.

**`internal/domain/cdm`** — o Modelo Canônico. Índice semântico **por cima** do
pacote, não uma cópia: `Bloco{Papel, TextoResumo, Confianca, Origem, RefXML}`,
onde `RefXML` aponta para o nó `w:p`/`w:tbl` correspondente. O CDM diz *o quê*
cada parágrafo é; o `ooxml.Documento` é *onde* mexer.

`Papel` ∈ Titulo, ListaAutores, Resumo, PalavrasChave, Secao(nível), Paragrafo,
Citacao, ItemLista, Tabela, Figura, Legenda, Equacao, Referencia, NotaRodape.
`Origem` ∈ `estilo-docx` | `heuristica` | `llm` | `usuario`.

**Heurística de classificação** — três camadas, nesta ordem de precedência:
estilos nomeados do DOCX → heurística estrutural → correção do usuário (que
sempre vence). A camada LLM entra só na F5.

**Ligar o upload à fila.** Hoje a conversão é síncrona e a fila está ociosa.
Com a análise entrando no fluxo, o trabalho assíncrono passa a valer a pena.

⚠️ **Bloqueio conhecido:** o worker não tem porta para gravar
`chave_storage_pdf` em `documentos`. `DocumentoInternoRepo` só tem
`AtualizarStatus` e `DefinirCDM`; `DefinirChavePreviewPDF` exige `vo.Dono`, e o
domínio deliberadamente **não** tem "dono de sistema". Criar essa capacidade é
decisão de arquitetura — merece ADR antes da implementação.

### O que a API passa a expor

```
POST  /v1/documentos/{id}/analisar     dispara a análise, devolve 202
GET   /v1/documentos/{id}/estrutura    o CDM classificado
PATCH /v1/documentos/{id}/estrutura    corrige o papel de um bloco
GET   /v1/jobs/{id}                    progresso do processamento
```

A correção manual grava `Origem: usuario` e **nunca** é sobrescrita por
reclassificação posterior.

### Critério de pronto

O sistema identifica título, resumo, palavras-chave, seções e referências num
artigo real — e **o round-trip não perde nada**.

### Onde costuma dar errado

**Escreva o teste de round-trip primeiro, antes de qualquer mutação.** Abrir e
salvar sem mutar tem que produzir um ZIP equivalente. Sem essa rede, todo bug
posterior de mutação vira caça ao fantasma.

O `encoding/xml` da stdlib **reescreve namespaces** e não preserva a ordem de
atributos. Round-trip ingênuo corrompe o documento de um jeito que o Word aceita
calado e o LibreOffice não. A skill `ooxml-referencia` tem as armadilhas
conhecidas — leia antes de escrever a primeira linha.

Ordem de filhos em `w:pPr`/`w:rPr`/`w:sectPr` é **mandada pelo schema**. Confira
contra o schema, nunca contra memória.

Timestamps de entrada de ZIP quebram golden file: normalize.

---

## F3 — Motor de formatação — **marco de produto**

É aqui que o sistema passa a valer para quem usa.

### Pré-requisito que não é código

**`backend/rulesets/` está vazio.** Antes de implementar, alguém precisa
preencher os valores da **ABNT NBR 14724** a partir de fonte oficial: margens,
corpo, entrelinha, recuo, ordem das seções. A skill `normas-abnt` tem a tabela
de conversão de unidades pronta e os valores normativos como **placeholder de
propósito** — não se inventa valor de norma sem fonte.

Isso não é trabalho de implementação: é decisão sobre qual documento é a fonte.

### O que entra

**Schema e loader de ruleset** — um YAML por revista em
`backend/rulesets/<slug>/vN.yaml`, validado contra `_schema.json`, semeado no
Postgres. **Arquivo semeado é imutável**: mudança de diretriz cria `v2.yaml`.
Documento formatado guarda `ruleset_id + versao`, então o resultado é sempre
reproduzível.

**`cmd/rulesetctl`** — valida no CI e roda o seed.

**`internal/domain/formatador`** — o motor. Aplica o ruleset escrevendo direto
nos nós: `w:sectPr` (margens, tamanho de página, numeração), `w:pPr` (entrelinha,
recuo, alinhamento, espaçamento, `w:keepNext`), `w:rPr` (fonte, tamanho,
negrito), `styles.xml` (redefine estilos nomeados), `numbering.xml` (numeração
de seções), além de reordenar blocos.

**Os dois testes de invariante**, que são a garantia central do produto:

1. **Integridade do texto** — a sequência de runes de todo `w:t` antes e depois
   é idêntica, fora das transformações declaradas.
2. **Preservação byte a byte** — a lista de partes do ZIP e o hash das partes
   não-alvo (mídia, embeds, `_rels`) saem idênticos.

### O que a API passa a expor

```
GET   /v1/rulesets                          catálogo de normas e revistas
GET   /v1/rulesets/{id}                     as regras de um ruleset
POST  /v1/documentos/{id}/formatar          {ruleset_id, formatos:[docx,pdf]}
GET   /v1/artefatos/{id}/download           URL pré-assinada do resultado
```

### Critério de pronto

Baixo o **DOCX** formatado em ABNT, ele abre no Word com margens e tipografia
corretas e **com as imagens intactas**, e o **PDF** correspondente não diverge —
porque é conversão do mesmo DOCX, não geração paralela.

### Onde costuma dar errado

Reconstruir o documento em vez de mutar. O ADR 0001 existe por isso: reconstrução
perde silenciosamente o que não soubermos reescrever.

Fonte ausente no container de conversão muda a métrica do texto e a paginação do
PDF deixa de bater com a do DOCX. O sidecar já traz `fonts-liberation2`
(metricamente compatível com Times New Roman e Arial) — não remova.

Todo bug de formatação vira **fixture antes da correção**.

---

## F4 — Citações e referências

### O que entra

Parser de entradas de referência e normalização para uma estrutura intermediária
(autores, título, veículo, ano, páginas, DOI).

Formatadores **ABNT 6023/10520** e **APA 7**, com reescrita das citações no
texto no estilo do ruleset (autor-data, numérico).

Detecção cruzada: referência citada mas ausente da lista, e listada mas nunca
citada — ambas entram no relatório de mudanças, não quebram o job.

### O que a API passa a expor

Nada de novo: o resultado entra no artefato e no relatório da F5.

### Critério de pronto

Uma lista de referências fora de ordem e em formato misto sai ordenada
alfabeticamente e normalizada, **sem perder informação**.

### Onde costuma dar errado

**Nunca invente campo ausente.** Entrada não-parseável é preservada **literal** e
marcada como "revisar". Adivinhar o ano ou o veículo de uma referência corrompe
dado do autor de um jeito que ele pode nem notar antes de submeter.

Nomes com partícula (`de`, `van`, `dos`), autor institucional e nome composto
quebram parser ingênuo. Cada caso real vira fixture.

---

## F5 — LLM como fallback

### O que entra

Cliente Anthropic com **structured outputs** (JSON Schema) devolvendo
`[]{block_id, role, confidence}`, atrás da interface
`domain/formatador.ClassificadorEstrutura`.

Entra **só** nos blocos onde a heurística ficou abaixo do limiar. Envia apenas os
primeiros ~200 caracteres de cada bloco candidato mais features estruturais —
**nunca o documento inteiro**.

Cache por hash de bloco, teto de custo por job, métrica de tokens.

`relatorios_mudanca`: o que foi alterado, por quê, e com que origem.

### Critério de pronto

Um documento sem estilos nomeados, que a heurística classifica mal, é
corretamente estruturado com a ajuda do LLM — e o custo por job fica dentro do
teto.

### Onde costuma dar errado

**Nenhum teste chama a API de verdade.** Use um fake de
`ClassificadorEstrutura`; a suíte precisa rodar offline e determinística.

Teto de custo tem que ser verificado **antes** da chamada, não depois.

Conteúdo do usuário sai da máquina aqui — é o único ponto do sistema em que isso
acontece. Trate o recorte enviado como decisão de privacidade, não de custo.

---

## F6 — Tabelas, figuras e revistas reais

### O que entra

Renumeração sequencial de tabelas e figuras, com legendas posicionadas conforme
o ruleset (tabela acima, figura abaixo, no caso da ABNT) e referências cruzadas
no texto atualizadas.

Rulesets reais: **Caminhos da Geografia** e **Geousp**, a partir das instruções
aos autores publicadas por cada periódico.

**Teste de conformidade por ruleset**: todo YAML em `rulesets/` precisa formatar
o artigo de fixture sem erro e produzir um PDF com as margens declaradas.

### Critério de pronto

Um artigo com cinco tabelas e três figuras fora de ordem sai renumerado e com
legendas corretas, em dois periódicos diferentes.

---

## F7 — Auth, rate limit e formatos extras

### O que entra

Cadastro e login com **argon2id**; sessão em cookie `httpOnly` + `Secure` +
`SameSite=Strict` com refresh rotativo — **nunca** token em `localStorage`.

Migração das sessões anônimas: um documento enviado antes do cadastro precisa
poder ser reivindicado pelo usuário que o enviou.

**Rate limit** por IP e por usuário nos endpoints de upload, login e formatação.

**Writer LaTeX** — única saída gerada a partir do CDM, explicitamente
best-effort.

**Entrada PDF** em modo degradado (`pdftotext -layout` + heurística), com aviso
na interface: nesse caminho não há DOCX in-place, o documento é montado a partir
do CDM.

### O que a API passa a expor

```
POST /v1/auth/cadastro
POST /v1/auth/sessao          login
DELETE /v1/auth/sessao        logout
GET  /v1/usuarios/eu
```

### Critério de pronto

Dois usuários distintos não enxergam documento um do outro, e a suíte de
segurança passa.

### Onde costuma dar errado

`vo.Dono` já suporta espécie `usuario` desde a F1 e o schema já tem
`usuario_id` — a fundação está pronta. O risco está na **migração** da sessão
anônima para a conta: feita errado, ou perde documento, ou entrega documento de
um usuário para outro.

---

## Ordem recomendada e o que pode andar em paralelo

```
F2 ──▶ F3 ──▶ F4 ──▶ F6
        │
        └──▶ F5   (independente da F4)

F7 ── independente: pode começar a qualquer momento
```

**F2 → F3 é caminho crítico.** Sem CDM não há o que formatar.

**F4 e F5 são independentes entre si**; as duas precisam da F3 fechada.

**F7 não depende de nenhuma delas** — se houver duas pessoas no backend, é a
melhor frente para paralelizar, e `vo.Dono` já foi desenhado para isso.

O preenchimento de `backend/rulesets/` com a NBR 14724 pode (e deve) começar
**agora**, em paralelo com a F2: é trabalho de leitura de norma, não de código,
e é pré-requisito duro da F3.
