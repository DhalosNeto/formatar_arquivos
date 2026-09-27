# Regras de lint desligadas, e por quê

`.oxlintrc.json` não aceita comentários, então o motivo de cada exceção mora
aqui. **Desligar regra sem motivo escrito é como o lint deixa de ser lido.**

| Regra | Onde | Motivo |
|---|---|---|
| `react/react-in-jsx-scope` | tudo | Obsoleta. React 17+ com o runtime automático de JSX não precisa de `React` no escopo, e o projeto usa `@vitejs/plugin-react`. A regra acusava 24 falsos positivos |
| `eslint/max-lines-per-function` | tudo | Conta markup JSX como se fosse lógica. `EnvioDeDocumento` tem 119 linhas quase todas de marcação e `svg`; quebrá-lo por contagem de linha pioraria a leitura |
| `unicorn/prefer-query-selector` | tudo | `getElementById` é o idioma para achar a raiz do React, é mais direto e não se beneficia de seletor CSS |
| `eslint/require-unicode-regexp` | **só testes** | Ruído em matcher do Testing Library (`/carregando/i`). **Continua ativa em produção**, onde regex sobre entrada do usuário precisa da flag `u` |
| `unicorn/consistent-function-scoping` | **só testes** | Dublê declarado dentro do caso de teste é proposital: ele fecha sobre o estado daquele caso |
| `no-console` | **só testes** | Depuração pontual em teste. **Erro em produção** |

O que está **ligado** e vale conhecer: `correctness` e `suspicious` como erro,
`pedantic` e `perf` como aviso, mais `eqeqeq` e `no-debugger`. Foi esse conjunto
que achou o `<iframe>` sem `sandbox` em `PreviewDeDocumento` — o padrão do
oxlint não pegava.
