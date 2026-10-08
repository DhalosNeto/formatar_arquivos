# Documentação do Formatador Acadêmico

Índice de entrada. Leia as instruções e a retomada primeiro; use os demais
arquivos por assunto, sem carregar a documentação inteira em cada agente.

## Comece aqui

| # | Arquivo | Responde |
|---|---|---|
| 1 | [`../CLAUDE.md`](../CLAUDE.md) | as regras do projeto — arquitetura, nomes, erros, testes. **É normativo**, não sugestão |
| 2 | [`retomada.md`](retomada.md) e [`spec-ativa.json`](spec-ativa.json) | checkpoint e contrato do recorte atual |
| 3 | [`mapa-modulos.md`](mapa-modulos.md) | localizar o assunto com `rg` antes de abrir arquivos |
| 4 | [`estado-do-backend.md`](estado-do-backend.md) | estado medido por data; consultar o recorte relevante |
| 5 | [`plano-backend.md`](plano-backend.md) | fase, decisões duráveis e critério de pronto |

Vai mexer no **frontend**? Só [`contrato-api.md`](contrato-api.md) importa. Ele é
escrito para quem consome a API e não pressupõe conhecimento do backend.

## Referência por assunto

| Arquivo | Assunto |
|---|---|
| [`contrato-api.md`](contrato-api.md) | comportamento público de cada endpoint, códigos de erro, o que ainda não existe |
| [`adr/0001-docx-in-place.md`](adr/0001-docx-in-place.md) | por que o motor muta o DOCX em vez de reconstruí-lo. **Leia antes de tocar em OOXML** |
| [`adr/0002-fila-sem-river.md`](adr/0002-fila-sem-river.md) | por que a tabela `jobs` é a própria fila, sem broker externo |

Contratos duráveis vivem no plano e no código, localizados pelo mapa. A spec
operacional é um único JSON reutilizável; não crie fichas MD por recorte.

## Workflow compartilhado

As regras comuns de contexto, quota, compactação e delegação estão em
`CLAUDE.md`; os seis papéis em `.claude/agents/`. As cópias Codex são geradas.
As skills têm fonte em `.agents/skills/` e links para descoberta pelo Claude,
evitando manter o mesmo texto duas vezes. Nenhum MD novo por sessão.

Peça **“use spec-verificavel para definir o próximo recorte”**. A skill em
`../.agents/skills/spec-verificavel/SKILL.md` gera/revisa a spec ativa e executa:

```sh
python3 .agents/skills/spec-verificavel/scripts/verificar_spec.py docs/spec-ativa.json
```

Isso confere estrutura, caminhos e vínculos sem executar comandos da spec.
Autoverificação não substitui testes nem revisão independente. Uma execução
realista da skill também deve ser conferida antes de confiar no workflow.

## O que NÃO é fonte de verdade

[`plano.md`](plano.md) — visão original de produto e stack. **As fases ali estão
desatualizadas.** Serve para entender por que o projeto é como é, não para saber
o que fazer. O roteiro vigente é `plano-backend.md`.

## Como escrever aqui

Três regras que mantêm esta pasta utilizável:

**Toda medição carrega data.** "A suíte passa" envelhece em uma hora; "a suíte
passou em 27/09 com `-race`" continua verdadeiro para sempre. Sem data, ninguém
sabe se pode confiar.

**Separe o que foi medido do que foi inferido.** Revisão de código não é
execução. Teste de componente não é ponta a ponta. Quando a distinção não está
escrita, a próxima pessoa assume a leitura otimista.

**Artefato de sessão não é documentação.** A cadeia de "onde paramos" já chegou a
cinco arquivos se referenciando em círculo, nenhum deles útil para quem chega.
Existe **um** arquivo vigente, `retomada.md`, sobrescrito a cada sessão. O que
tem valor durável migra para os arquivos acima; o resto não é versionado.

**Não crie ficha por recorte.** Se a informação importa depois que o recorte
fecha, ela pertence a `plano-backend.md`, a `mapa-modulos.md` ou ao godoc do
código. Um arquivo novo por entrega é como esta pasta chegou a 17.

`AGENTS.md` e `.codex/agents/*.toml` são **cópias geradas** de `CLAUDE.md` e
`.claude/agents/`. Não edite as cópias: altere a fonte e rode
`python3 scripts/sincronizar_instrucoes.py --write`.
