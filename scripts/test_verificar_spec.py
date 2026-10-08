import importlib.util
import json
import subprocess
import sys
import tempfile
import unittest
from copy import deepcopy
from pathlib import Path


RAIZ = Path(__file__).resolve().parents[1]
MODULO = importlib.util.spec_from_file_location(
    "verificar_spec", RAIZ / ".agents/skills/spec-verificavel/scripts/verificar_spec.py"
)
verificador = importlib.util.module_from_spec(MODULO)
MODULO.loader.exec_module(verificador)


class TestVerificarSpec(unittest.TestCase):
    def setUp(self):
        self.temporario = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporario.cleanup)
        self.raiz = Path(self.temporario.name) / "projeto"
        self.raiz.mkdir()
        (self.raiz / "fonte.txt").write_text("fonte", encoding="utf-8")
        self.spec = dict(
            versao=1, id="spec", objetivo="Validar contrato", estado="rascunho",
            fora_escopo=["backend"], fontes=["fonte.txt"], pendencias=[],
            arquivos=[dict(caminho="novo.txt", acao="criar", responsavel="codador")],
            contratos=[dict(id="c1", descricao="Preservar contrato")],
            criterios=[dict(id="a1", contrato="c1", cenario="entrada válida",
                            esperado="sem erros", verificacao="v1")],
            verificacoes=[dict(id="v1", comando="true", diretorio=".",
                              responsavel="testador", estado="nao_executada", evidencia="")],
        )

    def conferir(self, spec, valido=False):
        erros = verificador.verificar_spec(spec, self.raiz)
        self.assertIsInstance(erros, list)
        self.assertTrue(all(isinstance(erro, str) for erro in erros))
        self.assertEqual(bool(erros), not valido, erros)

    def test_estados_validos_e_criacao_ausente(self):
        for estado in ("rascunho", "pronta", "em_execucao", "bloqueada", "concluida"):
            with self.subTest(estado=estado):
                spec = deepcopy(self.spec)
                spec["estado"] = estado
                spec["pendencias"] = ["aguarda decisão"] if estado == "bloqueada" else []
                if estado == "concluida":
                    (self.raiz / "novo.txt").touch()
                    spec["verificacoes"][0].update(estado="passou", evidencia="saída real")
                self.conferir(spec, valido=True)

    def test_campos_ausentes_e_tipos_errados(self):
        for entrada in (None, [], "spec", 1, True):
            with self.subTest(entrada=entrada):
                self.conferir(entrada)
        for colecao in (None, "arquivos", "contratos", "criterios", "verificacoes"):
            original = self.spec if colecao is None else self.spec[colecao][0]
            for campo in original:
                for invalido in ("REMOVER", None, {}, 7, True, ""):
                    if campo == "evidencia" and invalido == "":
                        continue  # Vazio é válido enquanto não executada.
                    with self.subTest(colecao=colecao, campo=campo, invalido=invalido):
                        spec = deepcopy(self.spec)
                        alvo = spec if colecao is None else spec[colecao][0]
                        alvo.pop(campo) if invalido == "REMOVER" else alvo.update({campo: invalido})
                        self.conferir(spec)
        self.invalidos([((c,), [item]) for c in self.spec if isinstance(self.spec[c], list)
                        for item in (None, 42, [], {})])

    def invalidos(self, casos):
        for alteracoes in casos:
            if isinstance(alteracoes, tuple):
                alteracoes = [alteracoes]
            with self.subTest(alteracoes=alteracoes):
                spec = deepcopy(self.spec)
                for caminho, valor in alteracoes:
                    alvo = spec
                    for chave in caminho[:-1]:
                        alvo = alvo[chave]
                    alvo[caminho[-1]] = valor
                self.conferir(spec)

    def test_regras_referencias_duplicatas_e_conclusao(self):
        casos = [(("versao",), 2), (("estado",), "desconhecido")]
        casos += [((c,), []) for c in ("fora_escopo", "arquivos", "contratos", "criterios")]
        casos += [(("arquivos", 0, c), "invalido") for c in ("acao", "responsavel")]
        casos += [(("verificacoes", 0, c), "invalido") for c in ("estado", "responsavel")]
        casos += [(("criterios", 0, c), "ausente") for c in ("contrato", "verificacao")]
        casos += [((c,), self.spec[c] * 2) for c in ("contratos", "criterios", "verificacoes")]
        casos += [(("contratos",), self.spec["contratos"] + [dict(id="c2", descricao="Sem critério")])]
        for estado, pendencias in (("pronta", ["pendente"]), ("em_execucao", ["pendente"]),
                                   ("concluida", ["pendente"]), ("bloqueada", [])):
            casos.append([(("estado",), estado), (("pendencias",), pendencias),
                          (("arquivos", 0, "caminho"), "fonte.txt"),
                          (("verificacoes", 0, "estado"), "passou"),
                          (("verificacoes", 0, "evidencia"), "saída")])
        casos += [(("verificacoes", 0, "estado"), e) for e in ("passou", "falhou", "bloqueada")]
        for caminho, estado, evidencia in (("novo.txt", "passou", "saída"),
                ("fonte.txt", "passou", ""), ("fonte.txt", "nao_executada", ""),
                ("fonte.txt", "falhou", "saída"), ("fonte.txt", "bloqueada", "saída")):
            casos.append([(("estado",), "concluida"), (("arquivos", 0, "caminho"), caminho),
                          (("verificacoes", 0, "estado"), estado),
                          (("verificacoes", 0, "evidencia"), evidencia)])
        self.invalidos(casos)

    def test_caminhos_e_links_externos(self):
        externo = Path(self.temporario.name) / "externo"
        externo.mkdir()
        (externo / "fonte.txt").touch()
        (self.raiz / "link").symlink_to(externo, target_is_directory=True)
        for alvo in ("fonte", "criar", "alterar", "ler", "diretorio"):
            invalidos = ["../externo", str(externo), "pasta\\arquivo", "./fonte.txt",
                         "pasta/../fonte.txt", "link" if alvo == "diretorio" else "link/fonte.txt"]
            invalidos += ["link/novo.txt"] if alvo == "criar" else ["ausente"]
            for caminho in invalidos:
                with self.subTest(alvo=alvo, caminho=caminho):
                    spec = deepcopy(self.spec)
                    if alvo == "fonte":
                        spec["fontes"] = [caminho]
                    elif alvo == "diretorio":
                        spec["verificacoes"][0]["diretorio"] = caminho
                    else:
                        spec["arquivos"][0].update(caminho=caminho, acao=alvo)
                    self.conferir(spec)

    def test_symlink_interno_rejeita_responsaveis_distintos_para_mesmo_arquivo(self):
        (self.raiz / "alias.txt").symlink_to("fonte.txt")
        spec = deepcopy(self.spec)
        original = dict(caminho="fonte.txt", acao="alterar", responsavel="codador")
        alias = dict(caminho="alias.txt", acao="alterar", responsavel="testador")
        for arquivo in (original, alias):
            with self.subTest(caminho_individual=arquivo["caminho"]):
                spec["arquivos"] = [arquivo]
                self.conferir(spec, valido=True)
        spec["arquivos"] = [original, alias]
        self.conferir(spec)

    def test_comando_nunca_executado(self):
        marcador = self.raiz / "MARCADOR"
        self.spec["verificacoes"][0]["comando"] = f"touch '{marcador}'"
        self.conferir(self.spec, valido=True)
        self.assertFalse(marcador.exists())

    def test_cli_json_e_comando_nao_executado(self):
        marcador = self.raiz / "MARCADOR_CLI"
        self.spec["verificacoes"][0]["comando"] = f"touch '{marcador}'"
        conteudo = json.dumps(self.spec)
        casos = [("válido", conteudo, 0), ("JSON truncado", conteudo[:-1], 1),
                 ("chave duplicada", '{"versao": 1,' + conteudo[1:], 1),
                 ("chave aninhada duplicada", conteudo.replace(
                     '"comando":', '"comando": "true", "comando":'), 1)]
        for nome, entrada, codigo in casos:
            with self.subTest(caso=nome):
                arquivo = self.raiz / "spec.json"
                arquivo.write_text(entrada, encoding="utf-8")
                resultado = subprocess.run(
                    [sys.executable, "-B", MODULO.origin, str(arquivo), "--raiz", str(self.raiz)],
                    capture_output=True, text=True, timeout=10,
                )
                self.assertEqual(resultado.returncode, codigo, resultado.stdout + resultado.stderr)
                self.assertNotIn("Traceback", resultado.stdout + resultado.stderr)
                self.assertFalse(marcador.exists())


if __name__ == "__main__":
    unittest.main()
