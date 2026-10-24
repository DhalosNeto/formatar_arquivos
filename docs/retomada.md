# Retomada

Atualizado em 17/10/2026. Recorte `f3-mutadores-por-papel` **CONCLUÍDO**.
A máquina das exceções de espaçamento simples da §5.2 existe; **nenhum perfil a
declara**, então a saída de produção não mudou. Próximo passo é o `v2.yaml`.

## O que ficou pronto

- **Piso de autorização** em `formatador.Planejar` (C3): só muta bloco de origem
  `OrigemEstiloDocx` (o documento declarou) ou `OrigemUsuario` (o humano
  confirmou). Região 0,6, recuo 0,5 e regra textual 0,8 **não** autorizam.
  Decisão do usuário. Confiança não entra na condição.
- **`Plano.EntrelinhaPorPapel []GrupoEntrelinha`**, fatia ordenada por
  `Papel.String()`, sem grupo vazio, disjunta de `ReferenciasCorpo` por teorema
  de `Planejar`. `AplicarPlano` apensa uma etapa por grupo **ao fim**.
- **`entrelinha_por_papel`** em `ruleset.Definicao` e no `_schema.json`, com
  **lista branca literal**: só `citacao`, `referencia`, `legenda`,
  `nota_rodape`. Validação em **duas passadas** (lista branca inteira antes das
  medidas, chaves ordenadas) para o campo do 400 ser determinístico.
- **C1**: o caminho do recuo passa a emitir `OrigemHeuristica`; estilo nomeado e
  `w:tbl` seguem `OrigemEstiloDocx`. Sem isso o piso autorizaria o palpite
  geométrico de 0,5 — o oposto da decisão.
- **Invariante `Tabela` ⟺ `w:tbl`**, vedada nas três fontes: `estrutura.go`
  recusa transição de/para `Tabela` (C5), e `fallback.go` recusa `Tabela` como
  julgamento **e** exclui bloco `Tabela` dos candidatos (C6, duas pontas).

## Evidência real — 17/10/2026, dir `backend`

| Comando | Resultado |
|---|---|
| `go build ./... && go vet ./... && go vet -tags=integration ./... && gofmt -l .` | exit 0, sem saída |
| `golangci-lint run ./...` | `0 issues.` |
| `go test ./... -race -count=1 -shuffle=on` | nenhuma linha `FAIL` |
| `go run ./cmd/rulesetctl validar --dir rulesets` | `1 ruleset(s) validado(s)`, exit 0 |
| focais V1/V2/V6 | verdes · formatador 93,8% · ruleset 98,8% · cdm 94,6% · documento/service 95,8% · infra/ruleset 82,8% |

Cobertura é de **statements**, não de condições. Integração não executada (o
recorte não toca `data/postgres`); para rodar, Podman, conforme
`backend/migrations/README.md:13-27`.

Revisões: validador **APROVA COM RESSALVA** (quatro rodadas de pré-auditoria,
uma REPROVA no meio, mais a revisão do delta); segurança **APROVA COM RESSALVA**,
sem achado crítico ou alto.

**`V4` é verde e não cobre o que parece cobrir:** nenhum perfil declara a chave
nova, porque `abnt-14724/v1.yaml` é imutável. Quem exercita o schema é A14, por
YAML inline, dentro de V6.

## O brick: bug vivo que a pré-auditoria encontrou

`estrutura.go` reclassificava **sem restrição de papel**. No código já empurrado,
marcar o ordinal de um `w:tbl` como `paragrafo` punha o bloco em
`ReferenciasCorpo`, `alinhamento.go:70` recusava alvo que não é `w:p`, o job
falhava — e como `ChaveIdempotencia = SHA1(documentoID, rulesetID)` e a fila só
reivindica `pendente`, o par (documento, perfil) travava **permanentemente**.

C5 fecha a porta para estados novos. **A guarda é forward-only:** linha de
`cdm_jsonb` já gravada com `paragrafo` num `w:tbl` continua falhando, e desfazer
exigiria marcar o ordinal de volta como `tabela` — que é o que C5 agora recusa.
Reparo depende da migração do `cdm_jsonb` ou do reenfileiramento com escopo de
dono (F7).

## Lições registradas, as duas sobre medir e não afirmar

1. **"Nasceu verde" ≠ "mata mutante".** Dois critérios desta spec nasceram
   **infalsificáveis**: um afirmava blindar um mutante num cenário sem nenhum
   bloco `OrigemLLM`; o outro (A18, que eu pedi) tinha um grupo só, sendo ele o
   que falha, então uma etapa que valide antes de escrever nunca vazaria. É o
   mesmo padrão do A19 do recorte anterior, agora reconhecido duas vezes — o
   hábito é escrever "este critério blinda X" sem o mutante na mão.
2. **Cobertura de condição não aparece na de statement.** Os dois pinos finais
   deixaram a cobertura de V1 **idêntica**, porque não acrescentam statement,
   só condição. Regra 12 em forma mensurável.

Mais duas de processo, minhas: **`head` numa varredura de verificação não é
verificação** (afirmei que um arquivo estava limpo olhando 20 das 69
ocorrências; havia um quarto teste mentindo), e **documentação de API é onde
inventar comportamento sai mais caro** (escrevi um 400 que a API não emite, e
inventei "telas administrativas" para justificá-lo).

## Próximo recorte — `abnt-14724/v2.yaml`

Declarar as exceções da §5.2 no perfil normativo. **É o que torna este recorte
observável em produção.** Pré-condições bloqueantes, todas em
`docs/estado-do-backend.md`:

1. **Migrar o CDM persistido.** Linhas de `cdm_jsonb` carregam o palpite de
   recuo com `OrigemEstiloDocx` e podem carregar `Tabela` em `w:p` gravado antes
   da vedação; `VersaoFormatoCDM` segue 1. O piso não vale para documento
   analisado antes do deploy, e **qualquer bump precisa carregar a vedação**.
2. **Herdar por escrito a direção do erro do proxy:** `cdm.Citacao` por estilo
   nomeado não distingue citação longa de curta, e a §5.2 pede espaço simples só
   acima de três linhas — contagem de linhas renderizadas, que o motor não
   calcula. O proxy **super-aplica**.
3. Lembrar que **arquivo semeado é imutável**: errar a forma agora custa um
   `v3.yaml`.

## Débitos abertos (todos em `docs/estado-do-backend.md`, Pendências abertas)

Deste recorte: a guarda forward-only de `Tabela`; a invariante `Tabela` ⟺
`w:tbl` sustentada **por construção**, sem check em runtime, com os quatro
escritores nomeados no contrato (um quinto reabre C5 e C6); o limite de A18, que
só é falsificável porque nenhuma etapa anterior aos grupos substitui a parte
incondicionalmente (`pagina.go:54`, `alinhamento.go:104`, `tipografia.go:109`);
CPU 6+k passes por job; e o custo de qualidade de reprovar o lote inteiro quando
um julgamento traz `Tabela` (até 31 reclassificações legítimas viram
`acaoRevisar`).

Anteriores, que seguem valendo: `abrirDocumentoPrincipal` sem `io.LimitReader`
(MÉDIO preexistente, até 250 MiB por job); sem teto de job por dono e sem
deadline por job (F7); job em `executando` sem lease nem reclaim; checksum do
ruleset nunca verificado; SSRF em `Fonte`; `envolverPostgres` vazando
usuário/banco/endereço; a mutação sobrevivente em `definicao.go` que reporta
campo `"corpo"` para `EspacoAntesPT` e `EspacoDepoisPT`; `MargensComplementares{}`
materializando cabeçalho/rodapé zero quando `pgMar` está ausente; `Citacao`
dentro de região deixando a região aberta; e `regioes[emCaixaAlta]` consultado
antes de verificar papel.

## Normas: o que falta de fonte

**NBR 10520** (citações) e **NBR 6023** (referências) seguem sem fonte oficial na
skill `normas-abnt`, e a §5.2.1 diz "fonte menor" sem número. **Nenhum valor
inferido entrou em spec ou perfil.** Numeração de página (§5.3) exige mutar
cabeçalho, que o motor não toca. O `fonte` do `v1.yaml` aponta para a página
inicial do catálogo da ABNT, não para o registro individual.

## Estado do Git

**`main` está atualizada e empurrada** (`2144275`, merge de
`f3-motor-e-gate-de-ruleset`), e a branch segue em `f45f470`, também empurrada.
Nada pendente de push, árvore limpa, nenhum PR aberto.

Atenção para quem retomar em outra IDE: a `main` recebeu, por outro caminho, os
dois commits anteriores deste trabalho com **hashes diferentes** (`b1130ce` e
`20a0eb8`, mais o merge do PR #1 em `6555a70`) — conteúdo equivalente aos locais
`a2e027a` e `6ad0e79`. O merge saiu limpo porque a única diferença real era o
recorte dos mutadores; se aparecer divergência de histórico, é essa.

## Agentes e quota

Nenhum agente ativo. Investigador, testador, codador, validador e segurança
foram usados e encerrados. Quota: consultar antes de abrir o próximo recorte —
não abrir novo por volta de 80%, parar em ponto seguro perto de 90%.
