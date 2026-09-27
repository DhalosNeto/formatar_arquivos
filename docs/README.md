# Documentação do Formatador Acadêmico

Índice de entrada. Se você chegou agora, leia nesta ordem.

## Comece aqui

| # | Arquivo | Responde |
|---|---|---|
| 1 | [`../CLAUDE.md`](../CLAUDE.md) | as regras do projeto — arquitetura, nomes, erros, testes. **É normativo**, não sugestão |
| 2 | [`estado-do-backend.md`](estado-do-backend.md) | em que pé está cada fase e o que existe de verdade, com data de cada medição |
| 3 | [`plano-backend.md`](plano-backend.md) | o que falta, por fase, e o critério de pronto de cada uma |
| 4 | [`mapa-modulos.md`](mapa-modulos.md) | onde algo mora. `grep -i "<assunto>" docs/mapa-modulos.md` antes de varrer o repositório |
| 5 | [`retomada.md`](retomada.md) | onde a última sessão parou e qual é a próxima tarefa |

Vai mexer no **frontend**? Só [`contrato-api.md`](contrato-api.md) importa. Ele é
escrito para quem consome a API e não pressupõe conhecimento do backend.

## Referência por assunto

| Arquivo | Assunto |
|---|---|
| [`contrato-api.md`](contrato-api.md) | comportamento público de cada endpoint, códigos de erro, o que ainda não existe |
| [`adr/0001-docx-in-place.md`](adr/0001-docx-in-place.md) | por que o motor muta o DOCX em vez de reconstruí-lo. **Leia antes de tocar em OOXML** |
| [`adr/0002-fila-sem-river.md`](adr/0002-fila-sem-river.md) | por que a tabela `jobs` é a própria fila, sem broker externo |

Contratos por recorte — schema de ruleset, conversão de unidades, correção de
estrutura — vivem em `plano-backend.md` e `mapa-modulos.md`, não em fichas
separadas. Ficha por recorte multiplica arquivos e envelhece sem ninguém notar.

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
