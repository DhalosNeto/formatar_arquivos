"""Contrato do gerador: fontes preservadas, TOML válido e check sem escrita."""
import tempfile
import tomllib
import unittest
from pathlib import Path

from sincronizar_instrucoes import sincronizar


class TestSincronizar(unittest.TestCase):
    def test_gera_confere_e_detecta_divergencia(self):
        with tempfile.TemporaryDirectory() as diretorio:
            raiz = Path(diretorio)
            fonte = raiz / ".claude/agents"
            fonte.mkdir(parents=True)
            (raiz / "CLAUDE.md").write_text("# Projeto\nTime: `.claude/agents/`\n", encoding="utf-8")
            corpo = 'Regra em português: "não perder contexto".\nConsulte CLAUDE.md.\n'
            agente = '---\nname: teste\ndescription: Revisa "código"\nmodel: opus\ntools: Read\n---\n\n' + corpo
            (fonte / "teste.md").write_text(agente, encoding="utf-8")
            self.assertFalse(sincronizar(raiz, escrever=False))
            self.assertFalse((raiz / "AGENTS.md").exists())
            self.assertFalse((raiz / ".codex").exists())
            self.assertTrue(sincronizar(raiz, escrever=True))
            self.assertTrue(sincronizar(raiz, escrever=False))
            destino = raiz / ".codex/agents/teste.toml"
            dados = tomllib.loads(destino.read_text(encoding="utf-8"))
            self.assertEqual(dados, {"name": "teste", "description": 'Revisa "código"', "developer_instructions": corpo})
            self.assertIn("`.codex/agents/`", (raiz / "AGENTS.md").read_text(encoding="utf-8"))
            self.assertEqual((fonte / "teste.md").read_text(encoding="utf-8"), agente)
            destino.write_text("# divergente\n", encoding="utf-8")
            self.assertFalse(sincronizar(raiz, escrever=False))
            self.assertEqual(destino.read_text(encoding="utf-8"), "# divergente\n")

    def test_recusa_metadados_incompletos_antes_de_escrever(self):
        with tempfile.TemporaryDirectory() as diretorio:
            raiz = Path(diretorio)
            fonte = raiz / ".claude/agents"
            fonte.mkdir(parents=True)
            (raiz / "CLAUDE.md").write_text("projeto", encoding="utf-8")
            (fonte / "teste.md").write_text("---\nname: teste\n---\ncorpo", encoding="utf-8")
            with self.assertRaises(ValueError):
                sincronizar(raiz, escrever=True)
            self.assertFalse((raiz / "AGENTS.md").exists())


if __name__ == "__main__":
    unittest.main()
