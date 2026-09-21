"""Gera instruções Codex das fontes Claude; --check não escreve arquivos."""
import argparse
import json
from pathlib import Path


def gerar_agente(texto: str, nome: str) -> str:
    partes = texto.split("---\n", 2)
    if len(partes) != 3 or partes[0]:
        raise ValueError(f"frontmatter inválido: {nome}")
    metadados = dict(linha.split(": ", 1) for linha in partes[1].splitlines() if ": " in linha)
    # O frontmatter atual usa name/description simples, numa única linha.
    # Não interpretar YAML arbitrário nem copiar modelos/ferramentas de outra IDE.
    if metadados.get("name") != nome or not metadados.get("description"):
        raise ValueError(f"name/description inválidos: {nome}")
    corpo = partes[2].lstrip("\n")
    valores = {"name": nome, "description": metadados["description"]}
    cabecalho = f"# Gerado de .claude/agents/{nome}.md; não editar à mão.\n"
    # Strings JSON usam escapes compatíveis com strings básicas TOML.
    instrucoes = "'''\n" + corpo + "'''" if "'''" not in corpo else json.dumps(corpo, ensure_ascii=False)
    return cabecalho + "\n".join(f"{chave} = {json.dumps(valor, ensure_ascii=False)}" for chave, valor in valores.items()) + "\ndeveloper_instructions = " + instrucoes + "\n"


def sincronizar(raiz: Path, escrever: bool) -> bool:
    fontes = sorted((raiz / ".claude/agents").glob("*.md"))
    if not fontes:
        raise ValueError("nenhum agente-fonte encontrado")
    # Preparar tudo antes de escrever: metadado inválido não gera cópia parcial.
    gerados = {
        raiz / ".codex/agents" / f"{fonte.stem}.toml": gerar_agente(fonte.read_text(encoding="utf-8"), fonte.stem)
        for fonte in fontes
    }
    cabecalho = "<!-- GERADO A PARTIR DE CLAUDE.md — não edite à mão.\n     Regenerar: python3 scripts/sincronizar_instrucoes.py --write\n     Somente a referência ao diretório do time é adaptada para Codex. -->\n\n"
    principal = (raiz / "CLAUDE.md").read_text(encoding="utf-8")
    principal = principal.replace("`.claude/agents/`", "`.codex/agents/`")
    gerados[raiz / "AGENTS.md"] = cabecalho + principal
    alinhados = True
    for destino, conteudo in gerados.items():
        if destino.exists() and destino.read_text(encoding="utf-8") == conteudo:
            continue
        if escrever:
            destino.parent.mkdir(parents=True, exist_ok=True)
            destino.write_text(conteudo, encoding="utf-8")
            print(f"Gerado: {destino.relative_to(raiz)}")
        else:
            alinhados = False
            print(f"Divergente: {destino.relative_to(raiz)}")
    return alinhados


if __name__ == "__main__":
    argumentos = argparse.ArgumentParser(description=__doc__)
    modo = argumentos.add_mutually_exclusive_group(required=True)
    modo.add_argument("--check", action="store_true")
    modo.add_argument("--write", action="store_true")
    opcoes = argumentos.parse_args()
    raise SystemExit(0 if sincronizar(Path(__file__).resolve().parent.parent, opcoes.write) else 1)
