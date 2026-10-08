---
name: spec-verificavel
description: Gera ou revisa a spec de um recorte do Formatador Acadêmico e verifica contratos, critérios, caminhos e evidências antes da implementação. Use para planejar uma mudança não trivial ou retomar uma spec herdada.
---

# Spec verificável

Use as regras de contexto e delegação de CLAUDE.md. Produza um contrato pequeno
que o testador e o codador consigam executar sem outra rodada de exploração.
Gerar spec não autoriza implementar, publicar, enviar dados ou modificar produto.

## Gerar ou retomar

1. Confira pedido, retomada, git e a spec ativa. Reutilize decisões ainda válidas.
   Busque o módulo no mapa e leia só os arquivos/skills relevantes.
2. Defina objetivo observável, fora de escopo, arquivos com responsável,
   contratos (assinaturas, erros e invariantes) e critérios entrada → resultado.
   Cada contrato tem ao menos um critério e cada critério uma verificação.
3. Registre fatos faltantes em pendencias; não invente símbolo existente, fonte
   normativa, autorização ou resultado. Mudança de produto precisa de decisão.
4. Grave a única spec em `docs/spec-ativa.json`. Se agir como investigador
   somente leitura, devolva JSON e a checagem semântica ao principal para ele
   gravar e executar o verificador. Não alegue que executou essa etapa. Não crie outro MD,
   ficha ou prompt copiando essa informação. Preserve pendências da spec anterior.

Formato JSON (IDs e textos abaixo são um exemplo, não evidência):

```json
{
  "versao": 1,
  "id": "recorte-atual",
  "estado": "rascunho",
  "objetivo": "Descrever o comportamento observável solicitado",
  "fora_escopo": ["Mudanças não solicitadas"],
  "fontes": ["CLAUDE.md"],
  "arquivos": [{"caminho": "backend/exemplo.go", "acao": "criar", "responsavel": "codador"}],
  "contratos": [{"id": "C1", "descricao": "Assinatura proposta, entradas, resultado e erros"}],
  "criterios": [{"id": "A1", "contrato": "C1", "cenario": "Entrada concreta", "esperado": "Saída observável", "verificacao": "V1"}],
  "verificacoes": [{"id": "V1", "comando": "go test ./pacote-alvo -race -count=1", "diretorio": "backend", "responsavel": "testador", "estado": "nao_executada", "evidencia": ""}],
  "pendencias": ["Resolver o contrato e o pacote exatos antes de marcar pronta"]
}
```

Estados da spec: rascunho, pronta, em_execucao, concluida, bloqueada.
Arquivos: criar, alterar ou ler; responsáveis: principal, codador ou testador.
Verificações: nao_executada, passou, falhou, bloqueada; responsáveis: principal
ou testador. Caminhos exatos relativos ao repositório, sem glob/escape.

## Autoverificar antes de entregar

Execute na raiz:

```sh
python3 .agents/skills/spec-verificavel/scripts/verificar_spec.py docs/spec-ativa.json
```

O verificador é somente leitura: valida estrutura, vínculos, caminhos e estados;
**não executa os comandos da spec** e não atesta a verdade das evidências.
Corrija erros estruturais e rode novamente. Confira também semanticamente:

- Um implementador saberia o resultado de nil/vazio, falha e repetição relevantes?
- Cada critério testa a regra certa e falharia se ela fosse removida?
- Assinaturas propostas estão identificadas; arquivos existentes foram conferidos?
- Dono, concorrência, limites e preservação de dados estão cobertos quando aplicáveis?
- Comandos são adequados ao recorte, viáveis no ambiente e têm responsável único?
- Evidência informa execução real/data/resultado, ou está explicitamente ausente?

Faça até duas passagens de correção. Incerteza factual persiste como pendência
com estado rascunho/bloqueada; não converta dúvida em aprovação para parar o loop.
Relate a autoverificação e as lacunas ao principal. Specs herdadas continuam
passando por validador e segurança antes do código. Revisão final é independente.

## Durante e após execução

Atualize só os resultados efetivamente observados; mantenha RED e falhas relevantes
na evidência e registre o GREEN posterior. Uma verificação não executada nunca
vira passou por inferência. Concluida exige critérios satisfeitos, comandos
aprovados e pendências vazias; o principal registra pareceres independentes na
retomada antes de fechar. Decisões duráveis vão para a documentação vigente.
