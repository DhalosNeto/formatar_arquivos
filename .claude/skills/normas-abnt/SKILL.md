---
name: normas-abnt
description: Valores exatos de norma ABNT/APA para formatação de documento acadêmico — margens, corpo, entrelinha, ordem de seções, conversão de unidade. Use sempre que a tarefa tocar formatação ou revisão de norma.
---

# Normas de formatação acadêmica

## Regra de ouro

**Nunca invente um valor de norma.** Se a fonte oficial não foi consultada, o
valor é uma **pendência para o usuário**, não um palpite. Um número errado aqui
não quebra teste — ele sai num artigo submetido e volta rejeitado.

---

## Conversão de unidade (verificável por aritmética)

O OOXML não guarda centímetro. Toda medida da norma tem que ser convertida antes
de virar atributo XML.

| De | Para | Fator | Origem |
|---|---|---|---|
| 1 polegada | twips | **1440** | definição do twip (1/1440 pol) |
| 1 cm | twips | **566,93** | 1440 ÷ 2,54 |
| 1 pt | twips | **20** | 1440 ÷ 72 |
| 1 polegada | EMU | **914400** | definição do EMU |
| 1 cm | EMU | **360000** | 914400 ÷ 2,54 |

**Onde cada unidade aparece:**

- `w:pgMar` (margens) e `w:pgSz` (tamanho de página) — **twips**.
- `w:ind` (recuo) e `w:spacing` (`w:before`, `w:after`, `w:line`) — **twips**.
- `w:sz` e `w:szCs` (corpo da fonte) — **meio-ponto**. Fonte 12pt é `w:val="24"`.
- Dimensão de imagem (`wp:extent`) — **EMU**.

**Arredondamento é parte do contrato, não detalhe.** 3cm = 1700,79 twips; o valor
gravado tem que ser inteiro. Fixe a regra de arredondamento no VO de unidade e
cubra com teste, senão dois caminhos de código produzem margens diferentes para a
mesma norma.

---

## Entrelinha (`w:spacing`)

`w:line` depende de `w:lineRule`:

- `w:lineRule="auto"` — `w:line` é múltiplo de 240. Simples = 240, 1,5 = 360,
  duplo = 480.
- `w:lineRule="exact"` / `"atLeast"` — `w:line` é em twips absolutos.

Confundir os dois é o erro clássico: entrelinha 1,5 gravada como `360` com
`lineRule="exact"` vira 18pt fixos, não 1,5 linha.

---

## ABNT NBR 14724 — apresentação de trabalhos acadêmicos

> **PENDENTE — a preencher com a fonte oficial.** Não implemente contra esta
> tabela enquanto os valores estiverem vazios; trate como bloqueio e escale ao
> usuário.

| Item | Valor | Fonte |
|---|---|---|
| Margem superior | _(a preencher)_ | |
| Margem inferior | _(a preencher)_ | |
| Margem esquerda/interna | _(a preencher)_ | |
| Margem direita/externa | _(a preencher)_ | |
| Fonte do corpo | _(a preencher)_ | |
| Corpo do texto | _(a preencher)_ | |
| Entrelinha do corpo | _(a preencher)_ | |
| Citação longa (corpo/recuo/entrelinha) | _(a preencher)_ | |
| Nota de rodapé | _(a preencher)_ | |
| Recuo de primeira linha | _(a preencher)_ | |
| Numeração de página (posição) | _(a preencher)_ | |
| Ordem das seções | _(a preencher)_ | |

Normas irmãs: **NBR 6023** (referências) e **NBR 10520** (citações) — tabelas
próprias, a preencher também.

---

## APA 7

> **PENDENTE — a preencher com a fonte oficial.** Mesma regra acima.

| Item | Valor | Fonte |
|---|---|---|
| Margens (todas) | _(a preencher)_ | |
| Fonte e corpo | _(a preencher)_ | |
| Entrelinha | _(a preencher)_ | |
| Recuo de primeira linha | _(a preencher)_ | |
| Referências (recuo hanging) | _(a preencher)_ | |
| Níveis de título | _(a preencher)_ | |

---

## Revistas

Os valores de cada revista vivem em `backend/rulesets/<slug>/vN.yaml`, versionados
e imutáveis depois de semeados. Esta skill é referência de **norma**; a diretriz
de periódico é dado, não código. Revistas da F6: **Caminhos da Geografia** e
**Geousp**.
