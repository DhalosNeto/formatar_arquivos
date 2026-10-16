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

> **PREENCHIDA em 12/10/2026** a partir de `ABNT NBR 14724:2024`, PDF fornecido
> pelo usuário. Seção citada em cada linha. Valores em twips conferidos pela
> tabela de conversão acima.

| Item | Valor | Fonte |
|---|---|---|
| Formato do papel | **A4, 21 cm × 29,7 cm** = 11906 × 16838 twips | §5.1, "deve ser utilizado papel branco ou reciclado, no formato A4 (21 cm × 29,7 cm)" |
| Margem superior | **3 cm** = 1701 twips | §5.1, "para o anverso: esquerda e superior de 3 cm" |
| Margem inferior | **2 cm** = 1134 twips | §5.1, "direita e inferior de 2 cm" |
| Margem esquerda/interna | **3 cm** = 1701 twips | §5.1 (anverso) |
| Margem direita/externa | **2 cm** = 1134 twips | §5.1 (anverso) |
| Margens no VERSO (anverso e verso) | direita e superior 3 cm; esquerda e inferior 2 cm | §5.1 alínea b) |
| Corpo do texto | **12** (`w:sz` = 24 meio-pontos) | §5.1, "Recomenda-se... a fonte tamanho 12 para todo o texto" — é RECOMENDAÇÃO, não obrigação |
| Entrelinha do corpo | **1,5** (`w:line`=360, `w:lineRule="auto"`) | §5.2, "espaçamento 1,5 entre as linhas" |
| Em espaço SIMPLES (exceções) | citações diretas com mais de três linhas, notas de rodapé, referências, títulos de ilustrações e tabelas, fontes e legendas, natureza do trabalho | §5.2 |
| Referências entre si | separadas por um espaço simples em branco | §5.2 |
| Nota de rodapé | dentro das margens; separada do texto por espaço simples e **filete de 5 cm** a partir da margem esquerda; **fonte menor**; alinhada a partir da 2ª linha abaixo da 1ª letra | §5.2.1 |
| Numeração de página | **canto superior direito**, a **2 cm da borda superior**, último algarismo a **2 cm da borda direita**; em algarismos arábicos, a partir da 1ª folha textual; pré-textuais contadas e não numeradas | §5.3 |
| Numeração progressiva | remete à **ABNT NBR 6024** | §5.4 |
| Citações | remetem à **ABNT NBR 10520** | §5.5 |

### O que esta norma NÃO especifica — e por isso NÃO pode ser inventado

Verificado por busca no documento inteiro, não por ausência de memória:

| Item | Situação |
|---|---|
| **Família da fonte** | A norma diz apenas "fonte tamanho 12". `grep -ci "times new roman"` no PDF = **0**; nenhuma menção a Arial ou a qualquer família. "Times New Roman 12" é CONVENÇÃO de mercado e de regimentos institucionais, **não** requisito da 14724:2024. |
| **Recuo de primeira linha de parágrafo** | `grep -ci "recuo"` no documento inteiro = **0**. O valor de 1,25 cm que circula amplamente **não vem desta norma**. |
| **Recuo de citação longa (4 cm)** | Não está aqui; §5.5 remete à **NBR 10520**. |
| **Alinhamento do corpo (justificado)** | Não especificado. "Alinhado à esquerda" aparece só para indicativo de seção (§5.2.2). Justificar é convenção. |
| **Tamanho exato da fonte menor** | §5.1 e §5.2.1 dizem "tamanho menor e uniforme" e "fonte menor", sem número. |
| **Espaço antes/depois de parágrafo** | Não especificado. |
| **Ordem das seções** | Está na norma como estrutura (pré-textuais, textuais, pós-textuais), mas não como tabela de formatação; ver o corpo da norma se o recorte precisar. |

**Consequência prática para o ruleset:** `ruleset.Definicao` exige `Corpo.Fonte`,
`Corpo.RecuoCM` e `Corpo.Alinhamento`, que a norma NÃO define. Esses três são
escolha **institucional** de quem submete — regimento do programa ou diretriz do
periódico —, e um perfil que os preencha NÃO pode alegar que o valor é
normativo. Perguntar ao usuário, nunca derivar.

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
