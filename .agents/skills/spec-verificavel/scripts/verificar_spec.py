"""Confere uma spec local sem executar seus comandos nem alterar arquivos."""
import argparse
import json
from pathlib import Path


def texto(valor):
    return isinstance(valor, str) and bool(valor.strip())


def verificar_spec(spec, raiz):
    """Devolve erros estruturais; não certifica a veracidade das evidências."""
    erros = []
    if not isinstance(spec, dict):
        return ["spec deve ser um objeto"]
    if type(spec.get("versao")) is not int or spec["versao"] != 1:
        erros.append("versao deve ser 1")
    for campo in ("id", "objetivo", "estado"):
        if not texto(spec.get(campo)):
            erros.append(f"{campo}: texto obrigatório")
    for campo in ("fora_escopo", "fontes", "pendencias"):
        valor = spec.get(campo)
        if not isinstance(valor, list) or not all(texto(item) for item in valor):
            erros.append(f"{campo}: lista de textos obrigatória")
        elif not valor and campo != "pendencias":
            erros.append(f"{campo}: lista vazia")

    campos = {
        "arquivos": ("caminho", "acao", "responsavel"),
        "contratos": ("id", "descricao"),
        "criterios": ("id", "contrato", "cenario", "esperado", "verificacao"),
        "verificacoes": ("id", "comando", "diretorio", "responsavel", "estado"),
    }
    for colecao, obrigatorios in campos.items():
        itens = spec.get(colecao)
        if not isinstance(itens, list) or not itens:
            erros.append(f"{colecao}: lista não vazia obrigatória")
            continue
        for indice, item in enumerate(itens):
            if not isinstance(item, dict) or any(not texto(item.get(c)) for c in obrigatorios):
                erros.append(f"{colecao}[{indice}]: campos obrigatórios inválidos")
            elif colecao == "verificacoes" and not isinstance(item.get("evidencia"), str):
                erros.append(f"verificacoes[{indice}]: evidencia deve ser texto")
    if erros:
        return erros

    estados = {"rascunho", "pronta", "em_execucao", "concluida", "bloqueada"}
    estado = spec["estado"]
    if estado not in estados:
        erros.append("estado da spec inválido")
    if estado in {"pronta", "em_execucao", "concluida"} and spec["pendencias"]:
        erros.append("spec pronta/em execução/concluída ainda tem pendências")
    if estado == "bloqueada" and not spec["pendencias"]:
        erros.append("spec bloqueada exige uma pendência")
    raiz = Path(raiz).resolve()

    def conferir_caminho(valor, campo, *, diretorio=False, novo=False):
        segmentos = valor.split("/")
        if (not texto(valor) or "\\" in valor or "\x00" in valor
                or ":" in segmentos[0]
                or any(s in {"", ".", ".."} for s in segmentos)
                and not (diretorio and valor == ".")):
            erros.append(f"{campo}: caminho relativo não canônico")
            return
        try:
            destino = (raiz / valor).resolve()
            if not destino.is_relative_to(raiz):
                erros.append(f"{campo}: caminho fora do repositório")
            elif diretorio and not destino.is_dir():
                erros.append(f"{campo}: diretório não existe")
            elif not diretorio and (destino.exists() or not novo) and not destino.is_file():
                erros.append(f"{campo}: arquivo não existe ou não é regular")
            else:
                return destino
        except (OSError, ValueError, RuntimeError):
            erros.append(f"{campo}: caminho inválido")

    for fonte in spec["fontes"]:
        conferir_caminho(fonte, "fontes")
    caminhos = set()
    for arquivo in spec["arquivos"]:
        caminho = arquivo["caminho"]
        if arquivo["acao"] not in {"criar", "alterar", "ler"}:
            erros.append("arquivos: acao inválida")
        if arquivo["responsavel"] not in {"principal", "codador", "testador"}:
            erros.append("arquivos: responsavel inválido")
        destino = conferir_caminho(caminho, "arquivos",
                                   novo=arquivo["acao"] == "criar" and estado != "concluida")
        if destino is not None:
            if destino in caminhos:
                erros.append("arquivos: destino repetido; definir um único responsável")
            caminhos.add(destino)

    indices = {}
    for colecao in ("contratos", "criterios", "verificacoes"):
        indices[colecao] = {item["id"] for item in spec[colecao]}
        if len(indices[colecao]) != len(spec[colecao]):
            erros.append(f"{colecao}: IDs duplicados")
    cobertos = set()
    for criterio in spec["criterios"]:
        cobertos.add(criterio["contrato"])
        if criterio["contrato"] not in indices["contratos"]:
            erros.append("criterios: contrato inexistente")
        if criterio["verificacao"] not in indices["verificacoes"]:
            erros.append("criterios: verificacao inexistente")
    if indices["contratos"] - cobertos:
        erros.append("contratos sem critério de aceite")

    for verificacao in spec["verificacoes"]:
        resultado = verificacao["estado"]
        if resultado not in {"nao_executada", "passou", "falhou", "bloqueada"}:
            erros.append("verificacoes: estado inválido")
        if verificacao["responsavel"] not in {"principal", "testador"}:
            erros.append("verificacoes: responsavel inválido")
        if resultado != "nao_executada" and not texto(verificacao["evidencia"]):
            erros.append("verificacoes: resultado exige evidência")
        if estado == "concluida" and resultado != "passou":
            erros.append("spec concluída exige todas as verificações aprovadas")
        conferir_caminho(verificacao["diretorio"], "verificacoes.diretorio", diretorio=True)
    return erros


def objeto_sem_duplicatas(pares):
    resultado = {}
    for chave, valor in pares:
        if chave in resultado:
            raise ValueError("chave JSON duplicada")
        resultado[chave] = valor
    return resultado


def main():
    argumentos = argparse.ArgumentParser(description=__doc__)
    argumentos.add_argument("spec", type=Path)
    argumentos.add_argument("--raiz", type=Path, default=Path(__file__).resolve().parents[4])
    opcoes = argumentos.parse_args()
    try:
        spec = json.loads(opcoes.spec.read_text(encoding="utf-8"),
                          object_pairs_hook=objeto_sem_duplicatas)
    except (OSError, ValueError):
        print("Não foi possível ler a spec: arquivo ou JSON inválido.")
        return 1
    erros = verificar_spec(spec, opcoes.raiz)
    for erro in erros:
        print(f"ERRO: {erro}")
    if erros:
        return 1
    print("Spec estruturalmente válida; comandos e evidências não foram executados nem auditados.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
