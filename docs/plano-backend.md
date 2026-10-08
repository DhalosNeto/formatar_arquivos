# Plano do backend — F2 a F7

**Atualizado:** 2026-09-22 · Substitui as fases de `plano.md` no que diz
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
   tornam verde a suíte global. A auditoria aprovou arquivar integralmente a
   spec legada, recuperável pelo histórico do git. A movimentação e a migração de quatro
   asserções compatíveis para `job/execucao` foram concluídas; consultar a
   medição atual da suíte global no estado do backend.
7. Toda decisão que contraria este plano vira um **ADR** em `docs/adr/`.

---

## F2 — Parser e CDM

O risco técnico real do projeto mora aqui.

**Estado atual:** ✅ round-trip byte a byte, ✅ extração de blocos
(`ooxml.ExtrairBlocos`), ✅ CDM e serialização (`domain/cdm`), ✅ camadas 1 e 2
da classificação, ✅ análise via fila com persistência do CDM,
✅ `POST .../analisar`, `GET .../estrutura`, `PATCH .../estrutura` e
`GET /v1/jobs/{id}`. As medições históricas por recorte
e o fluxo real de 21/09 estão em `estado-do-backend.md`. Em 22/09, a suíte
global com `-race` e a integração Postgres passaram; auditoria do delta
PATCH/GET aprovada, com documentação corrigida.

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
por runa e não por byte.

⚠️ **O corte é um TETO, não uma garantia de compressão — medido, não
presumido.** No artigo real do fixture o CDM sai com **8411 bytes contra 6376
de texto (132%)**: os blocos são curtos, cabem inteiros no resumo, e as chaves
JSON somam por cima. Onde o teto trabalha de verdade é no bloco longo — um
parágrafo de 5000 palavras contribui as mesmas 200 runas que um de 10. O que a
frase "índice, não cópia" significa de fato é: o CDM **não cresce com o
tamanho do bloco** e **não guarda formatação** (runs, estilos, mídia, rels
ficam todos no pacote). Não significa que o JSON seja menor que o texto.

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

**Análise ligada à fila.** Após o upload, `POST .../analisar` cria ou obtém um
job idempotente, consumido pelo worker. A conversão do preview no upload
continua síncrona. A reivindicação seleciona somente `pendente`, sem retry
automático de `falhou`; reenfileiramento, backoff e teto de tentativas ainda
exigem uma política e implementação.

⚠️ **Pendência do preview assíncrono, não da análise:** o worker não tem porta para gravar
`chave_storage_pdf` em `documentos`. `DocumentoInternoRepo` só tem
`AtualizarStatus` e `DefinirCDM`; `DefinirChavePreviewPDF` exige `vo.Dono`, e o
domínio deliberadamente **não** tem "dono de sistema". Criar essa capacidade é
decisão de arquitetura — merece ADR antes da implementação.

O `cmd/worker` já liga e executa o laço; não falta esse bootstrap. Além do
registro do preview no documento, falta recuperação de jobs que permanecem
`executando` quando `Concluir`/`Falhar` não consegue persistir a finalização.

### O que a API passa a expor

```
POST  /v1/documentos/{id}/analisar     implementado: análise, devolve 202
GET   /v1/documentos/{id}/estrutura    implementado: CDM classificado
PATCH /v1/documentos/{id}/estrutura    implementado: corrige o papel de um bloco
GET   /v1/jobs/{id}                    implementado: progresso do processamento
```

O PATCH grava `Origem: usuario`; o domínio protege essa origem
contra reclassificação automática posterior.

### Critério de pronto

O sistema identifica título, resumo, palavras-chave, seções e referências num
artigo real — e **o round-trip não perde nada**.

✅ **Medido**, não presumido: `TestAplicarHeuristicaFixtureRealIdentificaEstruturaDoArtigo`
roda extrair → camada 1 → camada 2 sobre `artigo-real-libreoffice.docx` e
confere o papel dos 40 blocos um por um; `TestExtrairBlocosNaoAlteraOPacote`
confere o SHA256 do pacote depois de extrair.

✅ Serialização pronta: `cdm.Indice`, `Serializar`/`Desserializar` e o
envelope `{"versao":1,"blocos":[...]}`. Round-trip dos 40 blocos do artigo
real verificado, e a saída passa em `entity.ValidarCDM`.

✅ **Análise funciona ponta a ponta pela fila**, verificado contra a stack
real: `POST .../analisar` devolve 202 (idempotente — duas chamadas, o mesmo
job), o worker reivindica, roda `ooxml.AnalisarEstrutura`, persiste via
`ConcluirAnalise`, e `GET .../estrutura` devolve os 40 blocos do artigo.
Depois de iniciar a análise, falhas no processamento levam a uma tentativa de
`MarcarFalha`. Se essa gravação também falhar, o executor devolve ambas as
causas em `errors.ErroPersistirFalha`, com mensagem e log fixos.
A garantia é limitada: o banco pode recusar a gravação, deixando o documento
em `analisando`; a finalização do job também pode falhar, deixando-o em
`executando`. Recuperação e reconciliação dos dois estados continuam pendentes.

✅ `PATCH .../estrutura` (correção manual) e `GET /v1/jobs/{id}` implementados
e testados com Postgres. O PATCH usa `EstruturaRepo.SalvarEstrutura`, escopada
ao dono e com comparação de status/CDM anterior; `DefinirCDM` permanece interno.
Auditoria independente do delta aprovada. Não é auditoria retroativa de todo o
parser nem prova de prontidão de produção; ver riscos em `estado-do-backend.md`.

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

**Iniciada em 22/09:** primeiro recorte de conversões de unidade no domínio,
independente de valores normativos. Contrato pré-auditado e rastreabilidade em
este documento, seção F3. Schema, loader estrito e CLI validar implementados e
testados. Seed imutável e comando semear implementados em 27/09;
mutadores e endpoints ainda pendentes. Evidências finais na retomada do seed.

### Contrato do recorte de página/margens — 30/09/2026

Spec concluída `f3-pagina-margens`: `Documento.AplicarPagina(ruleset.Pagina,
MargensComplementares)`. As quatro margens e dimensões usam os conversores
existentes. Complementos explícitos em twips preenchem apenas header/footer/gutter
ausentes; atributos existentes são preservados. Nenhum valor desta API representa
uma regra ABNT ou Geousp.

Alteração lexical localizada em seções correntes diretas de body e p/pPr,
com criação da seção final se ausente, preservando histórico de alterações.
Preparar todas as edições antes de substituir document.xml; repetir deve ser
idempotente. Caminhos correntes não suportados são recusados, nunca ignorados.
Preservar prefixos, texto, comentários, declaração e partes ZIP não-alvo.
Sem validação XSD integral ou promessa de suportar qualquer DOCX.

Limites defensivos do mutador: XML de entrada e saída até 32 MiB, profundidade
256, 100000 elementos, 128 atributos por elemento e 1024 declarações de namespace
no documento. Escopos de namespace são compartilhados de forma imutável, com cópia
apenas quando há declaração local. Excesso é rejeitado sem gravar delta.
Filhos desconhecidos não recebem validação XSD completa; seções correntes fora
dos caminhos suportados são recusadas. Não há validação visual Word/LibreOffice
neste recorte de componente.

A ordem pgSz antes de pgMar foi conferida no
[schema do Open XML SDK](https://github.com/dotnet/Open-XML-SDK/blob/main/data/schemas/schemas_openxmlformats_org_wordprocessingml_2006_main.json).
O teto de dimensão 31680 twips é compatibilidade do
[Word para pgSz](https://learn.microsoft.com/en-us/openspecs/office_standards/ms-oi29500/e7017520-06b4-438f-97d2-3e49f247ca9f).
As margens auxiliares obrigatórias são descritas nas
[notas de implementação pgMar](https://learn.microsoft.com/en-us/openspecs/office_standards/ms-oe376/5a1cdacc-ee75-4453-8118-de48e5e370d6).

Consulta real ao Jev em 30/09: recomendou edição local entre alternativas
fechadas, modelo jev-1.13.0, confiança 1,0, 552 tokens de entrada e 57 de saída.
Somente resumo técnico foi enviado. A recomendação não substitui schema/testes.

Em 02/10, nova consulta autorizada com resumo técnico sem código/documentos:
Jev-1.13.0 recomendou rejeição atômica de estruturas ambíguas ou não suportadas,
em vez de ignorá-las ou normalizar o XML inteiro (confiança 1,0;
444 tokens de entrada e 62 de saída). A decisão é aplicada por validações
determinísticas e testes; confiança do modelo não é evidência de correção.

### Recorte de alinhamento direto — 03/10/2026

Componente implementado: `Documento.AplicarAlinhamento(referencias []int, alinhamento string)`.
As referências seguem os ordinais de `ExtrairBlocos`, incluindo tabelas na contagem,
mas cada alvo precisa ser parágrafo direto do corpo. O chamador escolhe os papéis
do CDM; este adaptador não decide quem recebe formatação nem autoriza usuários.
Esquerda/direita/centralizado/justificado são validados no domínio e convertidos
para left/right/center/both na infraestrutura. Sem mexer no contrato YAML/JSON.

Somente `w:jc` direto é alterado/criado; `pPr` ausente é criado primeiro.
Filhos de propriedades desconhecidos, duplicados ou fora de ordem nos alvos são
recusados. Texto, estilos nomeados, histórico, parágrafos fora da seleção e partes
não-alvo ficam intactos. Não implementa entrelinha/recuo/fonte nem avaliação global
de herança; o alinhamento direto tem sua semântica própria.
Preservar limites do parser e verificar expansão antes de montar o buffer final.
Spec `f3-alinhamento-paragrafos` concluída com pré-auditoria, TDD e revisões finais
independentes. Global race PASS (OOXML71.692s); último delta somente de teste
validado focalmente (namespace entre irmãos1.028s). Build/vet/gofmt/lint limpos.
Sem inspeção visual ou integração de formatação ponta a ponta; entrelinha/recuos
e fonte/tamanho são próximos recortes, com análise de overrides/herança.

Fontes técnicas consultadas:
[w:jc](https://learn.microsoft.com/en-us/dotnet/api/documentformat.openxml.wordprocessing.justification),
[valores](https://learn.microsoft.com/en-us/dotnet/api/documentformat.openxml.wordprocessing.justificationvalues) e
[CT_PPr, ordem dos filhos](https://github.com/dotnet/Open-XML-SDK/blob/main/data/schemas/schemas_openxmlformats_org_wordprocessingml_2006_main.json).

### Recorte de entrelinha direta — 04/10/2026

Spec `f3-entrelinha-direta`: `Documento.AplicarEntrelinha(referencias []int, multiplo float64)`.
Somente `spacing/@line` e `@lineRule=auto` nos parágrafos diretos selecionados.
Conversão existente de múltiplo em unidades de 240, com resultado positivo.
Preservar before/after, suas variantes, histórico, estilos e partes não-alvo.
Compartilhar o caminho lexical de alinhamento, com ordem CT_PPr, limites e
atomicidade; não criar outro parser. Recuos/espaço antes-depois/fontes ficam
para recortes posteriores. Não promete efeito visual integral de snapToGrid/docGrid.
Concluído em04/10: focal race e global race aprovados (OOXML global88.636s),
build/vet/gofmt/lint limpos. Pré-auditoria e revisões finais aprovadas.
Proteção adicional limita textos de edição acumulados a32MiB durante preparo,
evitando amplificação por alias longo; não representa limite de RSS do processo.
Sem verificação visual ou fluxo ponta a ponta. Evidências na spec/estado/retomada.

Fonte técnica: [SpacingBetweenLines.Line](https://learn.microsoft.com/en-us/dotnet/api/documentformat.openxml.wordprocessing.spacingbetweenlines.line?view=openxml-3.0.1).
Não é valor normativo de revista; a ambiguidade Geousp abaixo continua pendente.

### Próximo recorte: recuo simples com verificação de herança — 05/10/2026

Planejamento, ainda sem mutador entregue. `Corpo.RecuoCM` será recuo adicional
de primeira linha; não muda recuos laterais. Não basta gravar `firstLine`:
`hanging` e variantes em caracteres podem prevalecer por herança.
O recorte inicial recusará atomicamente conflitos nos níveis ativos, inclusive
valores zero, sem zerar/remover atributos ou alterar estilos compartilhados.
Essa recusa é conservadora, não uma declaração de que o arquivo é inválido.

Verificar propriedades diretas, docDefaults e cadeia basedOn do estilo selecionado;
estilo padrão de parágrafo só quando não há pStyle explícito. Não rejeitar por
um estilo não utilizado conter recuos conflitantes. Numeração ativa, cadeia
ausente/cíclica ou além do limite, ambiguidade estrutural e relacionamentos de
estilos não suportados devem falhar antes de qualquer substituição. Parte externa
nunca deve ser buscada; não inferir ausência de estilo sem conferir relacionamentos.
Spec ativa `f3-recuo-primeira-linha-preflight` pronta para TDD após pré-auditoria
e reauditorias de arquitetura/segurança. C1-C5/A1-A17 separam decisões por rota;
nenhum mutador/teste de recuo foi implementado nesta rodada.

Fontes técnicas: [Indentation e herança por atributo](https://learn.microsoft.com/en-us/dotnet/api/documentformat.openxml.wordprocessing.indentation?view=openxml-3.0.1),
[MS-OI29500, variantes por caractere](https://learn.microsoft.com/en-us/openspecs/office_standards/ms-oi29500/138732dc-507a-4164-af66-808c2fd9f2f9).
Há LibreOffice local disponível, mas ainda não foi executada caracterização visual
de recuos. Não converter leitura de especificação em evidência de renderização.

### Atualização de F3 — 07/10/2026

O recuo da primeira linha planejado em 05/10 foi concluído como componente,
com preflight de conflitos diretos e herdados; evidências em
`docs/retomada.md`. O recorte seguinte, `f3-espacamento-direto-paragrafo`,
entregou `Documento.AplicarEspacamentoAntes` e `AplicarEspacamentoDepois`.
Cada operação grava apenas `w:before` ou `w:after` em twips nos parágrafos
selecionados. Reutiliza o editor lexical e o conversor de pontos existentes;
preserva estilos, propriedades paralelas e partes não alvo. Testes focais,
suíte global com `-race`, build, vet, gofmt e lint passaram em 07/10.

Este componente garante o atributo direto armazenado. `beforeAutospacing`
pode fazer o editor ignorar `before` e `contextualSpacing` pode reduzir o
espaçamento entre parágrafos do mesmo estilo; as propriedades podem vir da
hierarquia de estilos. A verificação da distância visual efetiva pertence à
integração do motor e à validação de documento renderizado.
Fontes técnicas: [BeforeAutoSpacing](https://learn.microsoft.com/en-us/dotnet/api/documentformat.openxml.wordprocessing.spacingbetweenlines.beforeautospacing?view=openxml-3.0.1),
[ContextualSpacing](https://learn.microsoft.com/en-us/dotnet/api/documentformat.openxml.wordprocessing.contextualspacing?view=openxml-3.0.1).

### Tipografia direta dos runs — 07/10/2026

Spec `f3-tipografia-direta-runs` concluída. `Documento.AplicarTipografia`
aplica fonte e tamanho em `w:rPr` dos runs de texto filhos diretos dos
parágrafos selecionados do corpo principal. A fonte é gravada nos quatro
canais de `w:rFonts`; o tamanho em meios-pontos em `w:sz` e `w:szCs`.
Valores de fonte passam por validação XML 1.0 e escape de atributos.
O mutador recusa temas concorrentes, homônimos sem namespace, estrutura
`w:rPr` ambígua e texto em wrappers inline como hyperlink. Não altera
estilos, tabelas, notas, histórico nem partes não alvo. Essas recusas
conservadoras ficam explícitas para a futura integração do motor.

Testes de componente, build/vet/gofmt, lint e suíte global com `-race`
passaram em 07/10; revisões estáticas independentes aprovaram após corrigir
quatro casos de whitespace NBSP que os testes encontraram. Evidências em
`docs/spec-ativa.json` e `docs/estado-do-backend.md`. Não há fluxo de
formatação ponta a ponta nem equivalência visual verificada. A família de
fonte pode não estar instalada no editor; os valores normativos continuam
pendentes de fonte oficial antes de publicar um perfil ABNT/Geousp.
Fontes técnicas: [RunFonts](https://learn.microsoft.com/en-us/dotnet/api/documentformat.openxml.wordprocessing.runfonts?view=openxml-3.0.1),
[szCs](https://learn.microsoft.com/en-us/dotnet/api/documentformat.openxml.wordprocessing.fontsizecomplexscript?view=openxml-3.0.1),
[caracteres válidos em XML 1.0](https://www.w3.org/TR/xml/#charsets).

### Pré-requisito normativo

**`backend/rulesets/` contém o schema técnico, mas nenhum perfil normativo publicado.**
Antes de publicar um perfil, alguém precisa preencher os valores da
**ABNT NBR 14724** a partir de fonte oficial: margens,
corpo, entrelinha, recuo, ordem das seções. A skill `normas-abnt` tem a tabela
de conversão de unidades pronta e os valores normativos como **placeholder de
propósito** — não se inventa valor de norma sem fonte.

Isso exige fonte verificável antes de publicar o perfil normativo. Não bloqueia
os recortes técnicos com fixtures sintéticas (conversões, seed e motor).

#### Geousp — o que a pesquisa achou, e o que ficou ambíguo

Periódico escolhido no produto. Busca consultada em **22/09/2026** nas
[diretrizes oficiais](https://revistas.usp.br/geousp/pt_BR/about/submissions):

| Item | Valor encontrado |
|---|---|
| Formato | Word, A4 |
| Margens | 2,5 cm |
| Corpo | Times New Roman 12 |
| Numeração de página | ausente |
| Recuo e espaçamento | não especificados |

⚠️ **A entrelinha é ambígua e NÃO foi reinterpretada.** O texto oficial diz
"entrelinhas de 1,5 **cm**". Entrelinha se expressa em multiplicador de linha ou
em pontos, não em centímetros — então "1,5 cm" pode significar o multiplicador
1,5 (leitura provável) ou uma medida absoluta de 1,5 cm (≈ 42,5 pt, que daria um
espaçamento bem maior). **Converter silenciosamente para multiplicador seria
inventar a norma.**

⚠️ **Procedência do dado:** o acesso direto à página caiu em loop de
redirecionamento; os trechos vieram do resultado indexado da página oficial.
Revalidar contra a diretriz ou o template oficial antes de publicar o perfil.

E o principal: **diretriz de periódico não substitui a norma ABNT.** As duas
fontes são independentes e um perfil Geousp não implica conformidade com a
NBR 14724.

#### Conversões, que já existem

`internal/domain/vo/unidades.go` converte para as unidades inteiras do OOXML:
centímetros → twips (fator exato `1440/2.54`), pontos → twips (20), pontos →
meios-pontos (2, para tamanho de fonte) e entrelinha → unidades (240, para
`lineRule="auto"`). Arredondamento `math.Round`, com NaN, infinitos e negativos
recusados antes de arredondar, e teto técnico int32 conferido inclusive durante
a multiplicação. **Esse teto é técnico e não equivale ao limite de cada atributo
OOXML** — os limites contextuais ficam na validação do ruleset.

### Contrato do suporte a partes alteradas — 29/09/2026

Pré-aprovado por validador e segurança; implementação e evidências são
registradas em `estado-do-backend.md`. Este suporte precede os mutadores XML.

`(*ooxml.Documento).SubstituirParte(nome string, conteudo []byte) error` aceita
bytes opacos para uma parte existente, única e não diretório. Nome é comparado
exatamente, sem normalização; caminhos absolutos, unidade Windows, barra
invertida, NUL e segmentos vazios/`.`/`..` são recusados. Receptor nil devolve
erro de argumento nulo. Demais rejeições usam erro de validação com mensagem fixa.

Nil e slice vazio substituem por zero bytes, sem excluir a entrada. Os bytes
são copiados defensivamente após validação. Cada conteúdo e a soma dos deltas
retidos têm teto de 250 MiB, reutilizando o limite de domínio; uma troca desconta
o delta anterior. Isso não limita o pico de memória nem valida o pacote inteiro.
Rejeição mantém o estado anterior; a mesma instância não admite uso concorrente.
O ReaderAt original deve permanecer disponível e imutável até terminar o uso.

Salvar mantém ordem e `zip.Writer.Copy` nas partes intactas. Alteradas usam
`CreateHeader`, apenas Store/Deflate, recalculando CRC e tamanhos. Cabeçalho e
Extra são copiados: timestamps DOS e extras existentes permanecem; Modified é
zerado na cópia para não acrescentar timestamps repetidos. Apenas extras ZIP64
obsoletos são removidos. Extra truncado é recusado antes de aceitar substituição.
Falhas de escrita/finalização usam erro de aplicação fixo; o destino pode ficar
parcial e deve ser descartado, mas o delta permanece para nova tentativa.

`ExtrairBlocos` lê o último delta aceito, inclusive vazio. Não há validação nem
reescrita XML na substituição; o parser continua responsável pela extração.
Testes de no-op preservam igualdade integral; novos casos com delta exigem
preservação das partes não-alvo, metadados, texto quando não alterado pelo
chamador e salvamento repetido determinístico. O mutador de página permanece
um recorte separado.

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

Recorte Jev antecipado por pedido do usuário em 29/09/2026: porta
`domain/cdm.ClassificadorEstrutura` e adaptador HTTP TypeSafe com Choice.
O worker o injeta quando `JEV_HABILITADO=true`; o padrão é desligado.
As variáveis `LLM_*`/Anthropic são legadas e não ativam este adaptador.

Entra nos blocos abaixo de `JEV_LIMITE_CONSULTA` (0,7), exceto origens usuário
e LLM. Até 32 candidatos por documento, uma chamada, trechos do CDM
(normalmente até 200 caracteres; limite defensivo do adaptador: 500 runes),
papel atual e nível. Sem arquivo, credenciais de sessão ou identificador do
documento no payload. Os demais candidatos vão para revisão.

Confiança >= `JEV_LIMITE_AUTOMATICO` (0,95) aplica o papel com origem LLM;
>= `JEV_LIMITE_CONFIRMACAO` (0,7) preserva a classe atual e sugere confirmação;
abaixo disso ou sem correspondência solicita revisão. Limites são experimentais,
sem calibração em artigos reais. Confiança descreve a distribuição de escolhas,
não comprova correção. Falha externa mantém o CDM determinístico e registra
revisão; cancelamento interrompe o trabalho.

Destino fixo HTTPS TypeSafe, prazo de 15s, resposta até 256 KiB, sem retries
automáticos ou redirects. Habilitar autoriza envio dos trechos ao provedor;
trecho limitado ainda pode conter dados pessoais. Não habilitar para documentos
reais sem política de privacidade apropriada. Testes usam transporte/fakes locais.

As sugestões são aditivas no JSON v1, em `revisoes`, e aparecem no GET/PATCH
de estrutura. A correção manual existente confirma ou substitui o papel e
remove a revisão do alvo, com o mesmo dono/status/CAS.

Pendentes para fechar F5: calibração, cache por hash de bloco, teto monetário
por job e métricas de tokens. Limite de candidatos não é teto de custo em moeda.

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
enviado como decisão de privacidade, não de custo. O conversor já está na rede
`sem-saida` (`internal: true`), sem porta 2004 publicada no host; os controles
e seus limites estão registrados em `estado-do-backend.md`.

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
