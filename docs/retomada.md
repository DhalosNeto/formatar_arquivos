# Retomada — onde paramos e o que vem agora

**Atualizado:** 2026-09-27 · Arquivo único, sobrescrito a cada sessão. Substitui
a antiga cadeia de cinco arquivos de retomada.

## Em uma frase

F0, F1 e F2 funcionalmente fechadas. F3 tem unidades, schema, loader e seed;
**não tem motor, endpoints nem perfil normativo publicado**.

## Estado do repositório

⚠️ **A árvore está suja e nada foi commitado desde `e373525`.** São duas semanas
de trabalho F2/F3 fora do git, mais a refatoração de 27/09. Não resete, não
restaure testes antigos e não reverta arquivos alheios.

## Medição de 27/09 — refatoração

Executada após auditoria de todo o backend. Resultado real:

```
go build ./...                     PASS
go vet ./...                       limpo
go vet -tags=integration ./...     limpo
gofmt -l .                         limpo
go test ./... -race -count=1       PASS, 31 pacotes, 0 falhas
```

O que mudou, e por quê:

| Mudança | Motivo |
|---|---|
| `internal/rotas/sessao` passa a ser o único dono do cookie de sessão | havia **duas** implementações e duas constantes `"sessao_id"`; trocar uma e esquecer a outra derrubaria metade da API sem erro de compilação |
| `TestDominioNaoImportaInfra` em `internal/arquitetura` | a regra nº 1 da arquitetura não tinha teste. Estava respeitada por disciplina, e basta um autoimport da IDE para quebrá-la |
| `cdm.ErroIndiceCorrompido` e `cdm.ErroRefXMLDuplicado` | a reclassificação de CDM corrompido existia em três lugares, com duas variantes — uma descartando a causa |
| `documentos` volta a ter **um** controlador e **um** roteador | havia dois de cada, registrados separadamente na raiz, para o mesmo recurso |
| `telemetry.Encerrar` | o worker fechava o tracing fora de `defer` e **perdia os spans** quando o laço falhava. Bug real, não estilo |
| `cfg.OrigensCORS` | `cmd/api` lia `os.Getenv` direto; `config` é o único lugar que lê o ambiente |
| `montarDependencias` extraída de `executar` | `executar` tinha 114 linhas e o ciclo de vida do processo ficava ilegível no meio de oito construções de serviço |
| godoc nos símbolos exportados | de ~80 sem documentação para 0. Os arquivos novos não tinham nenhuma, os antigos explicavam cada decisão |

Nenhuma mudança de comportamento observável pela API.

## Medição de 27/09 — seed F3 (entrega anterior)

`RulesetRepo.Semear`, facade de dados, adaptador Postgres e `rulesetctl semear`.
Contrato: validar o lote inteiro antes de qualquer SQL; máximo 4096 definições;
ordenar por slug/versão; transação **explicitamente READ COMMITTED**; INSERT ON
CONFLICT DO NOTHING com SELECT separado, em novo snapshot. Divergência de nome
ou de JSONB reverte o lote inteiro. **Nunca UPDATE**: repetir preserva ID,
checksum e `ativo`, inclusive perfil desativado. Checksum é SHA256 do JSON
tipado da primeira inserção, não dos bytes YAML.

Evidências daquela rodada, **não reexecutadas** depois:

- Seed em Postgres real: PASS em 12,369s. Lock real com deadline de 100ms: PASS
  em 8,183s, preservando `DeadlineExceeded`.
- Revisão independente aprovou o delta.
- **Não houve E2E do processo CLI contra banco**: a CLI foi verificada com fake e
  o adaptador com banco real, separadamente. Nenhum banco de usuário foi alterado.

Contrato detalhado em `plano-backend.md`, seção F3, e em `mapa-modulos.md`,
`MOD: rulesets-seed`.

## Próxima tarefa

**Primeiro recorte do motor:** aplicar tamanho de página e margens a `w:sectPr`
a partir de uma definição sintética validada, sem alterar texto nem partes
não-alvo.

Ele tem **duas partes**, e a primeira não é óbvia:

**1. Dar a `ooxml.Documento` a capacidade de substituir os bytes de uma parte.**
Hoje ela guarda só `[]*zip.File` e `Salvar` recopia entradas cruas com
`zip.Writer.Copy` — **não existe nenhum caminho para gravar uma parte
modificada**. Sem isso o mutador não tem onde escrever. Consequência: os testes
de invariante comparam SHA-256 do ZIP **inteiro**, e isso deixa de poder valer;
passam a comparar por parte.

**2. O mutador de `w:sectPr`** — largura, altura e as quatro margens, com a
regra no domínio e o detalhe XML na infra. O domínio não importa o adaptador
OOXML (agora há teste para isso).

Recomendo recortes separados, com RED próprio. Juntos, o teste de invariante e o
mutador mudam ao mesmo tempo e não se sabe qual quebrou o round-trip.

### Cuidados que esse recorte exige

- **`encoding/xml` reescreve declarações de namespace.** Não dá para
  desserializar `word/document.xml` e reserializar: o Word abre com erro ou, pior,
  ignora o nó calado. `ExtrairBlocos` só lê por causa disso. Leia
  `.agents/skills/ooxml-referencia/SKILL.md` antes da primeira linha.
- Ordem dos filhos em `w:sectPr` é *sequence* do ECMA-376. Confira contra o
  schema, nunca contra memória. Nó ausente ≠ nó vazio.
- Invariantes: sequência de runes de todo `w:t` idêntica; partes não-alvo com os
  mesmos bytes descompactados; aplicar duas vezes não duplica propriedade.
- Fixture **sintética identificada**. Nunca apresentada como ABNT ou Geousp.
- Conversões devolvem `(int, error)`: o arredondamento precisa de caso próprio.

### Ordem sugerida depois

1. Página e margens com invariantes.
2. Tipografia de corpo com o schema atual. Regras por papel só após extensão
   explícita do schema — **elas não existem hoje, não presuma que existem**.
3. Consulta de rulesets, orquestração da formatação, persistência de artefatos e
   os endpoints F3, preservando autorização e concorrência.
4. DOCX final → PDF → download, com fluxo real e inspeção visual.
5. Perfil normativo publicável, **depois** de fonte verificada.

## Bloqueio que não é código

`backend/rulesets/` tem o schema técnico e **nenhum perfil normativo**. Publicar
um exige os valores da ABNT NBR 14724 de fonte oficial — margens, corpo,
entrelinha, recuo, ordem das seções. A skill `normas-abnt` traz a tabela de
conversão pronta e os valores normativos como **placeholder de propósito**.

Não inventar valor de norma. Isso não bloqueia os recortes técnicos, que usam
fixtures sintéticas.

## Comandos de verificação

A partir de `backend/`:

```sh
GOCACHE=/tmp/formatador-go-cache go build ./...
GOCACHE=/tmp/formatador-go-cache go vet ./...
gofmt -l .
GOCACHE=/tmp/formatador-go-cache go test ./... -race -count=1
```

Integração com Podman, em banco efêmero — **nunca com DSN de produção**:

```sh
DOCKER_HOST=unix:///run/user/1000/podman/podman.sock \
MIGRACOES_REDE_CONTAINER=slirp4netns \
TESTCONTAINERS_RYUK_DISABLED=true \
GOCACHE=/tmp/formatador-go-cache \
go test -tags=integration ./internal/data/postgres -race -count=1
```

Com Ryuk desativado, confira a limpeza dos containers de teste. Na raiz, depois
de mexer em instruções:

```sh
git diff --check
python3 scripts/sincronizar_instrucoes.py --check
```

⚠️ **`podman-compose up -d <serviço>` reaproveita o container existente e não
troca a imagem.** Sem `podman rm -f` antes, você depura código que não está
rodando. Custou duas rodadas para descobrir.

## Decisões que não devem ser desfeitas

- Backend Go contém as regras; o frontend React só consome a API. Hexagonal, com
  a dependência apontando sempre para dentro.
- DOCX é mutado **in-place**. O PDF vem do DOCX final. O CDM é índice semântico,
  não cópia estilizada.
- Autorização por `vo.Dono`. Não reintroduzir dono universal nem nil
  compartilhado. O worker tem fronteira própria. Preservar CAS e isolamento.
- Perfil semeado é imutável por versão. Fixture sintética não é ABNT nem Geousp.
- Entrelinha publicada em cm pela Geousp é ambígua: não converter silenciosamente
  para multiplicador.
