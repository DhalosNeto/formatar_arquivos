# Retomada

Atualizado em 16/10/2026. Recorte `f3-cdm-classifica-citacao` **CONCLUÍDO**.
`cdm.Citacao` deixou de ser papel declarado e nunca atribuído. A engenharia da
F3 segue completa; **o que falta para conformidade real são os mutadores por
papel**, que agora têm o pré-requisito pronto.

## O que ficou pronto neste recorte

Camada 1 (`internal/infra/ooxml/blocos.go`) produz `cdm.Citacao` por dois
caminhos:

- **estilo nomeado**, confiança 0,95: `Citao`, `Blocodecitao`, `Quote`,
  `IntenseQuote`. Lista **medida** no `soffice`, não intuída — o LibreOffice
  descarta os caracteres não-ASCII do styleId, então `"Citação"` sai `Citao` e
  `"Título 1"` sai `Ttulo1user`. `"Citacao"`, `"Citação"` e `"BlockQuote"`
  nunca chegam de ferramenta conhecida.
- **recuo à esquerda** acima de `limiarRecuoCitacaoTwips = 567` (1 cm; 1440 ÷
  2,54 = 566,93 por cm), confiança **0,5** — abaixo de `LimiteConsulta` 0,7 de
  propósito, para o bloco seguir candidato ao desempate do Jev.

`BlocoBruto.RecuoEsquerdaTwips` lê `w:ind` com **três guardas**: só parágrafo,
primeiro `w:ind` vence, e `Skip()` da subárvore de `w:pPrChange` antes de contar
profundidade. `w:start` tem precedência sobre `w:left` **quando parseia** (o
LibreOffice emite `w:start`; medido). `w:leftChars` não é lido. Negativo,
medida universal e overflow saneiam para 0, sem erro e sem pânico.

`cdm.AplicarHeuristica` ganhou a guarda de **C5** (um termo: `Papel !=
Paragrafo`) e a delimitação de **C6** (dois termos:
`ehCabecalho(bloco.Papel) || ehCabecalho(papelPeloTexto)`).

## Evidência real — 16/10/2026, dir `backend`

| Comando | Resultado |
|---|---|
| `go build ./... && go vet ./... && go vet -tags=integration ./... && gofmt -l .` | exit 0, sem saída |
| `golangci-lint run ./...` | `0 issues.` |
| `go test ./... -race -count=1 -shuffle=on` | nenhuma linha `FAIL` |
| `go test ./internal/infra/ooxml ./internal/domain/cdm -race -count=1 -cover` | `ok` 96,292s 90,6% · `ok` 1,061s 94,6% — **statements, não condições** |

Integração **não** executada: `go vet -tags=integration` compila as tags mas não
sobe testcontainers, e o recorte não toca `data/postgres`. Para rodar, Podman,
como documentado em `backend/migrations/README.md:13-27`.

Revisões: validador **APROVA COM RESSALVA** em duas rodadas (a segunda com
sondas de mutação); segurança **APROVA COM RESSALVA**, sem achado crítico ou
alto.

## A lição deste recorte: "nasceu verde" ≠ "mata mutante"

A19 foi escrito para blindar o limite do termo 2 de C6 e **não blindava**. O
validador mutou `ehCabecalho(papelPeloTexto)` para `casou` e a suíte de `cdm`
inteira passou, A19 incluído: em A19 o bloco do meio é `Paragrafo` e por isso
**nunca alcança a guarda**. A20 (`Citacao "Tabela 1 — distribuição"` dentro da
região) é o único vermelho sob aquele mutante, reproduzido com `go test
-overlay`.

**Consequência para o registro de evidência deste projeto:** A20 tem morte de
mutante medida; A18 e A19 não. Cobertura de statements não distingue as duas, e
critério que nasce verde precisa dizer contra o quê ele é blindagem.

## Erros de processo registrados, para não repetir

1. **Paralelismo que destruiu evidência.** Rodei testador e codador ao mesmo
   tempo, então A19 foi medido contra a árvore **já consertada** — o que não
   distingue "blindagem" de "código inalcançável". Declarei o viés achando que
   custava uma leitura; custou um critério falso. Quando um teste existe para
   provar que um conserto está certo, meça-o **antes** do conserto.
2. **Afirmação sem prova dentro do próprio contrato.** C6 dizia "A19 existe
   para pegar isso". Não pegava. Contrato não é lugar de hipótese sobre
   cobertura.
3. **Asserção impossível em A1**, minha: "nenhum caso acima do limiar" é falso
   por construção, porque A11 depende de 2268 estar acima de 567.
4. **Contradição interna que três auditorias não viram:** C5 exigia
   `Confianca > confiancaRegiao` com a confiança do recuo em 0,5 e
   `confiancaRegiao` em 0,6 — e aquele termo não tinha nenhum caso que
   falsificasse sua remoção, violando a regra 12 dentro da spec.
5. **Sexto comentário mentiroso** (bloqueante B1): um comentário prometia um
   "bloco de controle" que não existia na entrada do teste.

## Próximo recorte — mutadores por papel

Habilita as **exceções de espaçamento simples da §5.2** (citação longa, nota,
referência, legenda), que é o maior buraco de conformidade real. Pré-requisito
pronto: `Citacao` agora é produzido.

**Primeira pergunta a resolver, antes de qualquer contrato:** qual é o **piso de
confiança que autoriza mutar**. Sem ele, um parâmetro de detecção (0,5 para
recuo, 0,6 para região, 0,8 para regra textual) vira fato normativo no documento
entregue ao usuário. Citação por recuo tem confiança 0,5 — metade de palpite — e
aplicar espaçamento simples com base nisso é decidir pelo autor.

Ordem sugerida: definir o piso → contrato dos mutadores por papel → RED →
código. Em paralelo, o perfil `abnt-14724` **não pode** receber as exceções da
§5.2 antes disso: arquivo semeado é imutável, e perfil que declare o que o motor
não aplica é pior que perfil incompleto.

## Débitos abertos (todos em `docs/estado-do-backend.md`, Pendências abertas)

Deste recorte:

- `w:ind` herdado de caixa de texto (`w:txbxContent`): a guarda é só
  `tipo == Paragrafo` e "o primeiro vence" não distingue aninhamento. Mesma
  limitação que `w:pStyle` já tinha. BAIXO, achado da segurança.
- `confiancaRecuoCitacao` (0,5, em `infra/ooxml`) precisa ficar abaixo de
  `LimiteConsulta` (em `infra/config`), **sem nada que trave o acoplamento**.
- `regioes[emCaixaAlta]` é consultado antes de qualquer verificação de papel:
  uma `Citacao "REFERÊNCIAS"` **abre** região, e nada de A7 a A20 cobre.
  Espelho do defeito que C6 consertou.
- `ehCabecalho(papelPeloTexto)` acima de `NivelSecaoMaximo` só é exercitado por
  bloco `Paragrafo`; sem caso, mutar `nivelPelaNumeracao` sobrevive na guarda.
- `Citacao` dentro de região deixa a região **aberta** e os vizinhos seguem
  virando `Resumo`: decisão de desenho não declarada por contrato nenhum.

Anteriores, que seguem valendo:

- **MÉDIO, preexistente:** `abrirDocumentoPrincipal` abre `word/document.xml`
  sem `io.LimitReader` — até 250 MiB por job concorrente. É o `io.LimitReader`
  que `vo/formatoarquivo.go:167-171` pede desde a F2.
- Sem teto de job por dono e sem deadline por job (F7). Job em `executando` não
  tem lease nem reclaim, então documento pode ficar preso em `formatando`.
- Qualquer falha do worker trava **permanentemente** o par (documento, perfil);
  recuperação exige caso de uso de reenfileiramento com escopo de dono, porque
  `Reenfileirar` vive no serviço interno sem dono e `fronteira_test.go:49`
  proíbe o HTTP alcançá-lo.
- Checksum do ruleset nunca é verificado: reprodutibilidade sem lastro.
- SSRF em `Fonte`. `envolverPostgres` vaza usuário/banco/endereço para log.
- Mutação sobrevivente: `definicao.go:224-231` reporta campo `"corpo"` para
  `EspacoAntesPT` e `EspacoDepoisPT`, então trocá-los é indetectável.
- `MargensComplementares{}` materializa cabeçalho/rodapé zero quando `pgMar`
  está ausente.

## Normas: o que falta de fonte

- **NBR 10520** (citações) e **NBR 6023** (referências): tabelas vazias na skill
  `normas-abnt`, faltam as fontes oficiais. O usuário precisa fornecer.
- Numeração de página §5.3 exige mutar cabeçalho, que o motor não toca.
- `abnt-14724/v1.yaml` tem `fonte` apontando para a **página inicial** do
  catálogo da ABNT, não para o registro individual — URL permanente não
  confirmada, e o schema (`additionalProperties: false`) não tem chave para
  registrar isso. O rastro está em comentário.

## Estado do Git

Branch `f3-motor-e-gate-de-ruleset`. Último push em `bddbd9d`; **`6ad0e79` e o
commit deste recorte são locais**. Não há PR aberto.

## Agentes e quota

Nenhum agente ativo. Testador, validador, codador e segurança foram usados e
encerrados neste recorte. Quota: consultar antes de abrir o próximo recorte —
não abrir novo por volta de 80%, parar em ponto seguro perto de 90%.
