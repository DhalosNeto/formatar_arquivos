import io
import json
import unittest
from unittest.mock import patch

import consultar_jev


class ConsultaJevTest(unittest.TestCase):
    def test_consulta_envia_contexto_explicito_e_preserva_resposta(self):
        corpo = consultar_jev.montar_requisicao({
            "contexto": {"restricao": "usar portas internas"},
            "pergunta": "Qual alternativa respeita a restrição?",
            "opcoes": {"porta": "porta específica", "global": "acesso global"},
        })
        esperado = {"answers": {"decisao": {"type": "choice", "choice": "porta"}}}
        with patch.object(consultar_jev.request, "build_opener") as criar:
            criar.return_value.open.return_value = io.BytesIO(json.dumps(esperado).encode())
            self.assertEqual(esperado, consultar_jev.consultar(corpo, "chave-ficticia"))
            chamada = criar.return_value.open.call_args
            requisicao = chamada.args[0]
            self.assertEqual(consultar_jev.ENDPOINT, requisicao.full_url)
            self.assertEqual("Bearer chave-ficticia", requisicao.get_header("Authorization"))
            self.assertEqual(45, chamada.kwargs["timeout"])
            self.assertNotIn("chave-ficticia", requisicao.data.decode())
            self.assertIn("revisar", json.loads(requisicao.data)["questions"]["decisao"]["criteria"])

    def test_entrada_invalida_nao_chega_a_rede(self):
        for ficha in ([], {}, {"contexto": "x", "pergunta": "x", "opcoes": {"a": "única"}}):
            with self.subTest(ficha=ficha), self.assertRaises(ValueError):
                consultar_jev.montar_requisicao(ficha)

    def test_redirect_nao_encaminha_credencial(self):
        self.assertIsNone(consultar_jev.SemRedirecionamento().redirect_request(
            None, None, 302, "", {}, "https://outro.example/",
        ))
