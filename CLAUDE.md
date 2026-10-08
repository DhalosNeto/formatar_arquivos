# Formatador Acadêmico

Sistema web que recebe um artigo, aplica as diretrizes de uma revista ou norma
(ABNT, APA, periódico específico) e devolve o trabalho formatado em DOCX, PDF ou
LaTeX. Monorepo: backend Go (toda a lógica) + frontend React (só telas).

## Como rodar

```bash
make up            # sobe tudo via compose
make migrar        # aplica as migrations
make seed          # carrega os rulesets das revistas
make test          # testes unitários com -race e cobertura
make lint          # gofmt, vet, golangci-lint, lint e typecheck do front
```

Sem containers, para desenvolvimento rápido do backend:

```bash
cd backend && POSTGRES_DSN=... go run ./cmd/api
cd frontend && npm run dev
```

## Arquitetura

### Fontes de verdade e instruções geradas

- `docs/README.md`: **índice de entrada da documentação.** Comece por ele.
- `docs/estado-do-backend.md`: estado medido e pendências.
- `docs/plano-backend.md`: fases e critérios vigentes do backend.
- `docs/retomada.md`: onde a última sessão parou e qual é a próxima tarefa.
- `docs/spec-ativa.json`: contrato executável do recorte atual; uma única spec,
  gerada pela skill `.agents/skills/spec-verificavel/SKILL.md`.
- `docs/contrato-api.md`: comportamento público para quem desenvolve o frontend.
- `docs/plano.md`: histórico de produto/stack, não roteiro de implementação.
- `CLAUDE.md` e `.claude/agents/*.md` são as fontes das instruções;
  `AGENTS.md` e `.codex/agents/*.toml` são cópias geradas. Não editar as cópias.
  Regenerar com `python3 scripts/sincronizar_instrucoes.py --write` e verificar
  com `python3 scripts/sincronizar_instrucoes.py --check`. Modelos/ferramentas
  específicos do frontmatter Claude não são impostos aos agentes Codex.

F1 está funcional no escopo síncrono; isso não é aprovação para produção.
Consultar riscos abertos e diferenciar teste de componente de fluxo ponta a
ponta. Specs legadas/vermelhas e testes em desenvolvimento não tornam a suíte
global verde: registrar separadamente o resultado global e o do recorte.

> **Procurando onde algo mora?** `docs/mapa-modulos.md` é um índice greppável por
> módulo: `grep -i "<assunto>" docs/mapa-modulos.md` devolve o arquivo e os
> símbolos. Use antes de varrer o repositório.


Hexagonal. **A dependência aponta sempre para dentro.**

Exceção técnica existente: o domínio usa `internal/infra/errors`, conforme a
regra 2. Isso não autoriza imports de outros pacotes de infra no domínio;
separar o pacote de erros exigiria um recorte próprio, não uma correção incidental.

```
rotas ──▶ application ──▶ domain ◀── data ◀── infra
```

| Camada | Onde | Responsabilidade |
|---|---|---|
| `internal/domain/` | entidades, VOs, interfaces de repositório, serviços | **toda regra de negócio**; não importa `infra`, `data` nem `rotas` |
| `internal/application/web/` | `webmodel` (DTOs) e `webservices` | orquestra casos de uso e converte entidade ⇄ DTO |
| `internal/data/` | `contracts` + `postgres` | única porta de acesso a dados |
| `internal/infra/` | errors, log, config, storage, fila, ooxml, pdfconv, llm, telemetry | detalhes técnicos e integrações |
| `internal/rotas/` | contrato HTTP, adaptador Echo, middlewares, controladores | entrada HTTP; **sem regra de negócio** |

### Transformação DOCX → DOCX in-place

O motor **não reconstrói** o documento: abre o `.docx`, muta apenas os nós XML
que a norma exige (`w:sectPr`, `w:pPr`, `w:rPr`, `styles.xml`, `numbering.xml`) e
salva o mesmo pacote. Tudo que não é tocado (imagens, equações, cabeçalhos,
`_rels`) sai byte a byte igual. O PDF é a conversão do DOCX já formatado, então
os dois downloads nunca divergem. Ver `docs/adr/0001-docx-in-place.md`.

O **CDM** (`internal/domain/cdm`) é um índice semântico por cima do pacote: diz
qual é o papel de cada bloco e aponta para o nó XML correspondente. Ele não é
uma cópia do documento e não contém estilo.

## Regras de código (valem para todo mundo, humano ou agente)

1. **Nomes em português** — variáveis, funções, tipos, campos, pacotes de
   domínio: `ObterDocumento`, `aplicarMargens`, `blocosNaoClassificados`.
   Termos técnicos consagrados ficam como são (`Context`, `Handler`, `ID`, `JSON`).
2. **Erros só via `internal/infra/errors`** — `errors.Envolver(err, msg)` e os
   construtores `NovoErroValidacao`, `NovoErroNaoEncontrado`, `NovoErroConflito`,
   `NovoErroNaoAutorizado`, `NovoErroProibido`. Controlador termina com
   `rotasutil.TratarErro(ctx, resposta, err)`. Nunca engula erro.
3. **`context.Context` é o primeiro parâmetro** de tudo que faz I/O, e é propagado.
4. **Construtores explícitos com dependências** (`Novo*(deps...)`, como no código).
   Proibido `sync.Once` global,
   singleton de pacote e variável global mutável — quebram teste paralelo.
5. **Handlers não conhecem o Echo** — assinatura sempre
   `(context.Context, rotas.Requisicao, rotas.Resposta) error`.
6. **Acesso a dados só por `data/contracts`.**
7. **Conteúdo de documento do usuário nunca vai para log**, nem para mensagem de
   erro, nem para trace.
8. **Segredo só por env** (`internal/infra/config`). Nada hardcoded, nada no front.
9. **Token de sessão nunca em `localStorage`** — cookie `httpOnly` + `Secure` +
   `SameSite=Strict`.
10. Antes de terminar qualquer entrega: `go build ./... && go vet ./... && gofmt -l .`
11. **VO que carregue identificador sensível precisa de `String()` E `GoString()`.**
    `%#v` não passa pelo `Stringer`: sem `GoStringer` o `fmt` imprime os campos
    privados. Campo privado **não** protege log. Quando o identificador é um
    `sessao_id` de cookie, o que vaza é credencial viva, não só privacidade.
12. **Em decisão composta, exija caso por termo, não por linha.** Cobertura de
    *statements* não é de *condições*: um OR cujo segundo termo nunca é
    exercitado marca 100% e ninguém quebra teste ao remover metade da condição.
13. **Especificação herdada é auditada antes de ser implementada.** Se os testes
    vierem prontos de outra sessão, `validador` e `seguranca` os auditam **antes**
    de o `codador` escrever qualquer linha contra eles.

## Testes (TDD)

Teste primeiro. Quatro camadas:

- **Unitário table-driven** — lógica pura, nomes dos casos em português.
- **Golden files** — `testdata/*.docx` → `*.golden.json` / `golden.document.xml`.
  Todo bug de formatação vira fixture antes da correção.
- **Invariantes de OOXML** — integridade do texto (sequência de runes de `w:t`
  idêntica) e preservação byte a byte das partes não-alvo do ZIP.
- **Integração** (`//go:build integration`, testcontainers) e **E2E** (Playwright).

Cobertura mínima: **80% em `internal/domain/`**. Nenhum teste chama uma API
externa de LLM de verdade — use um fake de `ClassificadorEstrutura`.

## Rulesets das revistas

Um YAML por revista em `backend/rulesets/<slug>/vN.yaml`, versionado no git e
semeado no Postgres. **Arquivo semeado é imutável**: mudança de diretriz cria
`v2.yaml`. Documento formatado guarda `ruleset_id + versao`, então o resultado é
sempre reproduzível.

## Time de agentes

`.claude/agents/` tem seis agentes: `orquestrador`, `investigador`, `codador`,
`testador`, `validador`, `seguranca`. O ciclo continua: spec → RED → código →
testes/revisões → retrabalho ou fechamento. O investigador usa a skill
spec-verificavel; não há um sétimo agente duplicando essa tarefa.

## Contexto, compactação e delegação

- Comece por `docs/README.md`, retomada e `git status`; carregue só a seção do
  plano e os módulos necessários. Use `rg` no mapa antes de varrer arquivos.
  Não releia fontes inalteradas nem envie histórico completo a outro agente.
- Spec e autoverificação têm uma fonte: `docs/spec-ativa.json`. Critérios recebem
  IDs ligados a contratos, testes e comandos. Autoverificação estrutural não
  comprova comportamento nem substitui validador/segurança. Spec herdada continua
  exigindo pré-auditoria (regra 13). Não peça de novo decisão já autorizada.
- Delegue trabalho delimitado que possa avançar em paralelo. Como padrão, até
  dois agentes ativos; até três no fechamento (testador, validador, segurança).
  Não use seis agentes simultâneos nem reabra investigação sem fato novo.
  Mudança trivial/documental pode ser feita pelo principal com verificação
  proporcional; não crie RED artificial. Mantenha independência nas revisões.
- Cada chamada informa objetivo, IDs dos critérios, arquivos permitidos,
  leitura essencial, dependências e entrega esperada. Um escritor por arquivo;
  investigador e auditores não editam. Não reverta mudanças de outros agentes.
  Reuse agente com contexto útil; encerre os concluídos. Disponibilidade e
  nomes das ferramentas são os da IDE atual, não de sessões antigas.
- Retornos curtos: decisão, arquivos/linhas, evidência real, lacunas e próximo
  passo (alvo de 300 palavras, ampliado se necessário para achados). Não copie
  a spec no retorno. O principal integra; não refaz o trabalho delegado.
- Atribua um responsável para a suíte global. Demais agentes rodam o focal ou
  revisão estática; só repita após mudança, falha ou evidência perdida. Cada
  medição informa data, comando, escopo e resultado. Sem resultado recuperável,
  não declare PASS. Build/vet/gofmt (regra 10) e lint continuam no fechamento.
- Consulte a quota antes de delegar e após cada entrega. Em torno de 80%, não
  abra novo recorte; perto de 90%, encerre trabalho em ponto seguro, registre
  pendências e pause. Não consumir reset sem autorização. Quota de uso e janela
  de contexto são medidas diferentes; se indisponíveis, não invente percentuais.
- Antes de compactar ou pausar, atualize **só** `docs/retomada.md`: objetivo,
  spec/IDs, decisões, arquivos e mudanças alheias, evidências, achados pendentes,
  agentes/comandos ainda ativos e próximo comando. Compactação manual depende
  da IDE; salvar checkpoint não significa que ela foi executada.
- Retomada após compactação confere checkpoint + git + delta, sem reiniciar
  tudo. Informação durável vai para estado/plano/mapa/contrato API. Não criar
  `sessao-*.md`, fichas, prompts ou relatórios novos por entrega. Substitua a
  spec ativa só após preservar pendências; histórico continua no Git.

Skills locais têm fonte em `.agents/skills/`; o Claude pode usar links em
`.claude/skills/`. Regras comuns vivem aqui, papéis nos seis agentes; não criar
mais uma cópia para outra IDE. Regenerar as instruções Codex pelo script.

## Fases

F0 fundação ✅ · F1 ingestão e preview síncrono ✅ · F2 parser e CDM funcional ✅ · F3 motor de formatação em andamento ·
F4 citações e referências · F5 fallback de LLM · F6 tabelas/figuras e revistas
reais · F7 auth e formatos extras.

Plano vigente: `docs/plano-backend.md`. Estado: `docs/estado-do-backend.md`.
Frontend: consumir `docs/contrato-api.md`; não ampliar o escopo de telas sem pedido.

# Compact instructions

Ao compactar, preserve: decisões de arquitetura tomadas, contratos de
função definidos pelo investigador, saída real de comando (go test,
go build), e achados de validador/segurança ainda não corrigidos.
Pode resumir: exploração de código já concluída e discussão descartada.
