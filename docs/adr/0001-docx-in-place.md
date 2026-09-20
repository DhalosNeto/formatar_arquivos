# ADR 0001 — Formatar o DOCX in-place, em vez de reconstruí-lo

- **Status:** aceito
- **Data:** 2026-09-12

## Contexto

O produto precisa entregar, a partir do arquivo do autor, um DOCX formatado nas
diretrizes da revista e um PDF correspondente. Havia dois caminhos:

1. **Reconstruir**: interpretar o documento para um modelo próprio e gerar um
   `.docx` novo a partir dele.
2. **Mutar in-place**: abrir o `.docx` do autor e alterar apenas os nós XML que a
   norma exige.

## Decisão

Mutação in-place.

Um `.docx` real carrega muito mais do que parágrafos: imagens e seus
relacionamentos em `word/_rels/`, equações OMML, cabeçalhos e rodapés, notas de
rodapé, campos, comentários, objetos incorporados, numeração e estilos herdados.
Reconstruir significa perder silenciosamente tudo que o nosso escritor não
souber reproduzir — e a perda só aparece quando o autor abre o arquivo.

Mutando in-place, o que não é tocado permanece byte a byte igual. O risco de
perda de conteúdo cai a quase zero, que é a garantia que importa para quem vai
submeter o trabalho a um periódico.

O **CDM** deixa de ser uma cópia do documento e passa a ser um índice semântico:
diz qual é o papel de cada bloco (título, resumo, seção, referência) e aponta
para o nó XML correspondente. Classificação e mutação ficam separadas e
testáveis isoladamente.

O **PDF é gerado a partir do DOCX já formatado**, pelo LibreOffice headless.
Assim é impossível o PDF divergir do que o autor vê no Word.

## Consequências

**Positivas**
- Nenhuma perda silenciosa de conteúdo do usuário.
- DOCX e PDF sempre consistentes entre si.
- Escrevemos bem menos OOXML do que uma reconstrução exigiria.
- Dois invariantes automatizáveis: integridade do texto e preservação das partes
  não-alvo do ZIP.

**Negativas**
- Precisamos entender o OOXML que o autor produziu, incluindo arquivos gerados
  pelo Google Docs, LibreOffice e Word em versões diferentes.
- Estilos herdados exigem cuidado: mexer só no `w:pPr` direto do parágrafo não
  basta quando o valor vem do estilo nomeado — por isso alteramos também
  `styles.xml`.
- A entrada PDF (modo degradado) não tem pacote para mutar, então segue o
  caminho de reconstrução a partir do CDM, com fidelidade explicitamente menor.

## Verificação

Dois testes obrigatórios em toda tarefa que toque OOXML:

1. A sequência de runes de todo `w:t` antes e depois da formatação é idêntica,
   salvo as transformações declaradas (reordenação de blocos, reformatação de
   referências).
2. A lista de partes do ZIP e o hash das partes não-alvo (mídia, embeds, rels)
   saem iguais.
