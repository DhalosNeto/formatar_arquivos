---
name: ooxml-referencia
description: Armadilhas conhecidas de manipulação OOXML neste projeto — namespace, ordem de nós, encoding, timezone. Use sempre que a tarefa mexer em XML do docx.
---

# OOXML — armadilhas conhecidas

Referência única do projeto. O motor **não reconstrói** o documento: abre o
`.docx`, muta só os nós que a norma exige e salva o mesmo pacote. Tudo que não é
tocado sai byte a byte igual. Toda armadilha abaixo é uma forma de quebrar essa
garantia.

---

## Estrutura do XML

- **Namespace.** `w:`, `r:`, `wp:`, `a:` precisam ser preservados exatamente como
  vieram. O `encoding/xml` do Go **reescreve declarações de namespace** ao
  serializar e é capaz de trocar o prefixo por um gerado. Se o prefixo mudar, o
  Word abre com erro ou ignora o nó silenciosamente — que é pior.
- **Ordem dos filhos é obrigatória.** `w:pPr`, `w:rPr` e `w:sectPr` são
  *sequences* no schema do ECMA-376: os filhos têm ordem fixa. Inserir `w:spacing`
  depois de `w:ind` pode gerar documento que o Word recusa. Ao adicionar um nó,
  insira na posição certa — nunca com append no fim. **Confira a ordem contra o
  schema, não contra a memória.**
- **Nó ausente ≠ nó vazio.** Não existe `w:pPr` num parágrafo sem formatação
  direta; criar um vazio muda o pacote sem necessidade e polui a comparação byte
  a byte. Só crie o nó quando for de fato gravar algo nele.
- **Herança de estilo.** Formatação direta em `w:pPr`/`w:rPr` vence `styles.xml`.
  Redefinir o estilo nomeado não corrige um parágrafo que tem override direto —
  os dois caminhos precisam ser tratados.
- **Elemento self-closing.** `<w:b/>` e `<w:b></w:b>` são equivalentes para o
  Word, mas diferentes em bytes. Round-trip que troca a forma quebra o teste de
  preservação.

## Encoding e texto

- **Declaração XML e BOM** têm que sair como entraram.
- **Fora do BMP** — emoji e CJK. Go trabalha em UTF-8 e o XML é UTF-16 no modelo
  do Word; contagem de caractere não é contagem de rune nem de byte.
- **Acentuação em NFD.** `á` pode vir como um code point ou como `a` + combining
  acute. Comparação de texto sem normalizar dá falso negativo — e o português
  garante que isso aparece.
- **`xml:space="preserve"`** — espaço no início/fim de `w:t` só sobrevive com ele.
  Perder o atributo come o espaço e altera o texto do usuário.

## Pacote ZIP

- **Partes não-alvo saem intactas.** Mídia, embeds, `_rels`, `[Content_Types].xml`:
  o que não é mutado é copiado cru, sem reserializar.
- **Timezone e timestamp.** O ZIP guarda data/hora por entrada, em horário local
  sem fuso. Regravar com o relógio atual muda bytes sem mudar conteúdo e **quebra
  golden file de forma não-determinística**. Fixe o timestamp.
- **Método e ordem de compressão** fazem parte dos bytes. Reordenar entradas ou
  trocar o nível de compressão produz ZIP diferente de um documento idêntico.
- **Zip bomb e zip slip.** Razão de descompressão, número de entradas e tamanho
  total descomprimido são limites **obrigatórios** na ingestão. Nome de entrada
  com `..` ou caminho absoluto é rejeitado.
- **XXE.** Entidades externas desabilitadas no parser, sempre.

## Invariantes que o teste trava

1. **Integridade do texto** — a sequência de runes de todo `w:t` é idêntica antes
   e depois, exceto nas transformações declaradas (reordenação de blocos,
   reformatação de referência).
2. **Preservação byte a byte** — a lista de partes do ZIP e o hash das partes
   não-alvo saem iguais.

## Casos de borda que todo teste de documento cobre

Documento vazio; sem resumo; sem referências; referência não-parseável; título em
múltiplos parágrafos; imagem e equação no meio do texto; tabela sem legenda;
caractere fora do BMP; acentuação em NFD; arquivo corrompido; DOCX que é na
verdade um ZIP qualquer; arquivo gigante; campo `nil`/vazio em toda entidade;
erro do storage; timeout do conversor; job repetido (idempotência).

---

**Unidades** (twips, meio-ponto, EMU) ficam na skill `normas-abnt` — não duplique
aqui.
