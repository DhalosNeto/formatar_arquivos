# Plano do backend — F2 a F7

**Atualizado:** 2026-09-20 · Substitui as fases de `plano.md` no que diz
respeito ao backend.

Este plano cobre **só o backend**. O frontend é responsabilidade de outra
pessoa e consome `contrato-api.md`. F0 e F1 estão funcionalmente fechadas;
F1 entrega upload e preview síncronos, **não prontidão de produção**. Ver
evidências e pendências atuais em `estado-do-backend.md`.

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
6. Cobertura mínima de **80% em `internal/domain/`**. Na revisão de 20/09,
   `documento/processamento` tem 93,8%; coberturas dos recortes aprovados não
   tornam verde a suíte global, ainda bloqueada por legado e testes WIP.
7. Toda decisão que contraria este plano vira um **ADR** em `docs/adr/`.

---

## F2 — Parser e CDM

O risco técnico real do projeto mora aqui.

**Estado atual:** ✅ round-trip byte a byte, ✅ extração de blocos
(`ooxml.ExtrairBlocos`), ✅ entidade do CDM (`domain/cdm`) e ✅ camada 1 da
classificação (`ooxml.ClassificarPorEstiloDocx`) — pacotes verdes com `-race`,
92,3% e 92,9%. ⬜ Heurística estrutural, persistência do CDM, os endpoints
abaixo e a ligação do upload à fila continuam pendentes.

### O que entra

**`internal/infra/ooxml`** — abrir e salvar o pacote DOCX.

`Abrir(io.ReaderAt, int64)` valida a estrutura mínima e `Salvar(io.Writer)`
recopia entradas sem descomprimir, via `zip.Writer.Copy` — **a escrita
continua sem desserializar XML**.

`(*Documento).ExtrairBlocos()` devolve os `BlocoBruto` de nível superior do
corpo (`w:p` e `w:tbl`, na ordem, com `Indice` ordinal). Ela parseia
`word/document.xml` num **caminho paralelo e somente leitura**: reabre a
entrada do ZIP e nunca toca o que `Salvar` recopia. A decisão está travada por
teste — `TestExtrairBlocosNaoAlteraOPacote` compara o SHA256 do pacote depois
de extrair. Parsear para ler não pode virar parsear para escrever.

**`internal/domain/cdm`** — o Modelo Canônico. Índice semântico **por cima** do
pacote, não uma cópia: `Bloco{Papel, TextoResumo, Confianca, Origem, RefXML}`,
onde `RefXML` aponta para o nó `w:p`/`w:tbl` correspondente. O CDM diz *o quê*
cada parágrafo é; o `ooxml.Documento` é *onde* mexer.

`Papel` ∈ Titulo, ListaAutores, Resumo, PalavrasChave, Secao(nível), Paragrafo,
Citacao, ItemLista, Tabela, Figura, Legenda, Equacao, Referencia, NotaRodape.
`Origem` ∈ `estilo-docx` | `heuristica` | `llm` | `usuario`.

Implementado: `Papel` é VO comparável de campos privados, `Secao(n)` válido em
1..6, `NovoBloco` acumula todos os campos reprovados num só `ErroValidacao`, e
`Reclassificar` trata tentativa automática sobre bloco `OrigemUsuario` como
**no-op sem erro** — rodar a heurística de novo é fluxo normal, não falha.

`TextoResumo` é truncado em **200 runas** (`ooxml.TamanhoMaximoTextoResumo`),
por runa e não por byte. É o que fecha "índice, não cópia": guardar o texto
integral de cada bloco duplicaria o documento dentro de `cdm_jsonb`, e nada se
perde — `RefXML` aponta para o nó de origem.

**Heurística de classificação** — três camadas, nesta ordem de precedência:
estilos nomeados do DOCX → heurística estrutural → correção do usuário (que
sempre vence). A camada LLM entra só na F5.

✅ Camada 1 pronta: `ClassificarPorEstiloDocx` mapeia `Title` → Titulo,
`HeadingN` → `Secao(N)` (só 1..6; fora da faixa cai no fallback, senão
produziria um bloco que o domínio recusa), `Normal` e `w:tbl` → Paragrafo e
Tabela. Estilo desconhecido ou ausente vira Paragrafo com **confiança baixa**,
que é o sinal para a camada 2 — não um erro.

✅ Camada 2 pronta: `cdm.AplicarHeuristica` — rótulo de região
(`RESUMO`/`ABSTRACT`, `REFERÊNCIAS`/`REFERENCES`), palavras-chave por prefixo,
legenda (`Tabela N`/`Fonte:`) e seção numerada (`2.1 ` → `Secao(2)`). Mora no
domínio: opera sobre `[]cdm.Bloco`, sem tocar infra.

Ela só reclassifica quando o papel **difere** do atual — concordar com a camada
1 não pode trocar `OrigemEstiloDocx`/0,95 por `OrigemHeuristica`/0,8.

⚠️ `ListaAutores` continua sem camada determinística, de propósito: nenhuma
evidência textual separa um nome de autor de um parágrafo comum. É caso da F5
ou da correção manual.

**Ligar o upload à fila.** Hoje a conversão é síncrona e a fila está ociosa.
Com a análise entrando no fluxo, o trabalho assíncrono passa a valer a pena.

⚠️ **Bloqueio conhecido:** o worker não tem porta para gravar
`chave_storage_pdf` em `documentos`. `DocumentoInternoRepo` só tem
`AtualizarStatus` e `DefinirCDM`; `DefinirChavePreviewPDF` exige `vo.Dono`, e o
domínio deliberadamente **não** tem "dono de sistema". Criar essa capacidade é
decisão de arquitetura — merece ADR antes da implementação.

O `cmd/worker` já liga e executa o laço; não falta esse bootstrap. Além do
registro do preview no documento, falta recuperação de jobs que permanecem
`executando` quando `Concluir`/`Falhar` não consegue persistir a finalização.

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

✅ **Medido**, não presumido: `TestAplicarHeuristicaFixtureRealIdentificaEstruturaDoArtigo`
roda extrair → camada 1 → camada 2 sobre `artigo-real-libreoffice.docx` e
confere o papel dos 40 blocos um por um; `TestExtrairBlocosNaoAlteraOPacote`
confere o SHA256 do pacote depois de extrair.

⬜ **Falta para fechar a fase:** serializar o CDM para `cdm_jsonb` e expor as
rotas. `Papel` tem campos privados e não tem `MarshalJSON`, e
`entity.ValidarCDM` exige um objeto JSON — o CDM precisa de um envelope
(`{"versao":N,"blocos":[...]}`), não de um array cru.

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

Esta fase prevê envio de conteúdo do usuário ao provedor LLM. Trate o recorte
enviado como decisão de privacidade, não de custo. Isso não elimina o risco
atual de saída de rede do conversor, registrado em `estado-do-backend.md`.

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
