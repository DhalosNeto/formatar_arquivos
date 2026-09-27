"""Consulta de desenvolvimento Astra–Jev; não faz parte do backend do produto."""

import argparse
import json
import os
import sys
from pathlib import Path
from urllib import error, request

ENDPOINT = "https://api.typesafe.ai/v1/systemone"
LIMITE_BYTES = 64 * 1024


class SemRedirecionamento(request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def montar_requisicao(ficha):
    if not isinstance(ficha, dict):
        raise ValueError("A ficha deve ser um objeto JSON.")
    if not ficha.get("contexto") or not isinstance(ficha.get("pergunta"), str):
        raise ValueError("Informe contexto e pergunta na ficha.")
    opcoes = ficha.get("opcoes")
    if not isinstance(opcoes, dict) or not 2 <= len(opcoes) <= 254:
        raise ValueError("Informe entre 2 e 254 opções.")
    if any(not isinstance(valor, str) or not valor.strip() for valor in opcoes.values()):
        raise ValueError("Cada opção precisa de uma descrição.")
    return {
        "model": "jev-latest",
        "state": ficha["contexto"],
        "questions": {
            "decisao": {
                "type": "choice",
                "instructions": ficha["pergunta"],
                "criteria": {
                    **opcoes,
                    "revisar": "Evidência insuficiente ou nenhuma alternativa atende às restrições.",
                },
            }
        },
    }


def consultar(corpo, chave):
    dados = json.dumps(corpo, ensure_ascii=False).encode("utf-8")
    if len(dados) > LIMITE_BYTES:
        raise ValueError("Contexto excede 64 KiB; selecione um recorte menor.")
    requisicao = request.Request(
        ENDPOINT, data=dados, method="POST",
        headers={"Authorization": "Bearer " + chave, "Content-Type": "application/json"},
    )
    # Não encaminhar a credencial para outro destino em resposta a redirects.
    cliente = request.build_opener(SemRedirecionamento())
    with cliente.open(requisicao, timeout=45) as resposta:
        recebido = resposta.read(LIMITE_BYTES + 1)
    if len(recebido) > LIMITE_BYTES:
        raise ValueError("Resposta excede o limite permitido.")
    resultado = json.loads(recebido)
    decisao = resultado["answers"]["decisao"]
    if decisao["type"] != "choice" or decisao["choice"] not in corpo["questions"]["decisao"]["criteria"]:
        raise ValueError("Resposta incompatível com as alternativas enviadas.")
    return resultado


def main():
    argumentos = argparse.ArgumentParser(description=__doc__)
    argumentos.add_argument("ficha", type=Path, help="JSON explícito: contexto, pergunta e opcoes")
    argumentos.add_argument("--validar", action="store_true", help="Valida localmente, sem API ou custo")
    opcoes = argumentos.parse_args()
    try:
        with opcoes.ficha.open("rb") as arquivo:
            conteudo = arquivo.read(LIMITE_BYTES + 1)
        if len(conteudo) > LIMITE_BYTES:
            raise ValueError("Ficha excede 64 KiB.")
        corpo = montar_requisicao(json.loads(conteudo))
        if opcoes.validar:
            print("Ficha válida; nenhuma consulta enviada.")
            return 0
        chave = os.environ.get("TYPESAFE_API_KEY", "").strip()
        if not chave:
            print("Configure TYPESAFE_API_KEY no ambiente antes de consultar.", file=sys.stderr)
            return 1
        print(json.dumps(consultar(corpo, chave), ensure_ascii=False, indent=2))
        return 0
    except error.HTTPError as falha:
        print(f"TypeSafe respondeu HTTP {falha.code}; resposta privada omitida. Sem retry automático.", file=sys.stderr)
    except (OSError, ValueError, KeyError, TypeError):
        print("Não foi possível validar a ficha ou consultar o Jev; detalhes privados omitidos.", file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
