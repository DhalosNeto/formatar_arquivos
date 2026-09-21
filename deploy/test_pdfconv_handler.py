"""Testes do handler do sidecar de conversao.

Rodam sem container e sem LibreOffice: o modulo so importa stdlib, e a
conversao em si e substituida por um dublê. O que esta sendo testado nao e a
conversao -- e o que protege o container de cair: o teto de conversoes
simultaneas e o prazo maximo de uma conversao.
"""
import http.client
import importlib.util
import os
import pathlib
import subprocess
import threading
import unittest


def carregar_handler(**ambiente):
    """Importa pdfconv-handler.py com o ambiente pedido.

    O nome do arquivo tem hifen (nao e um identificador Python valido), por
    isso o import vai por importlib. Recarregar a cada teste e proposital: os
    limites viram constantes de modulo na importacao.
    """
    anterior = {chave: os.environ.get(chave) for chave in ambiente}
    os.environ.update({chave: str(valor) for chave, valor in ambiente.items()})
    try:
        caminho = pathlib.Path(__file__).parent / "pdfconv-handler.py"
        spec = importlib.util.spec_from_file_location("pdfconv_handler", caminho)
        modulo = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(modulo)
        return modulo
    finally:
        for chave, valor in anterior.items():
            if valor is None:
                os.environ.pop(chave, None)
            else:
                os.environ[chave] = valor


class ServidorDeTeste:
    """Sobe o ThreadingHTTPServer do modulo numa porta livre."""

    def __init__(self, modulo):
        self._modulo = modulo
        self._servidor = modulo.http.server.ThreadingHTTPServer(
            ("127.0.0.1", 0), modulo.ManipuladorConversor
        )
        self.porta = self._servidor.server_address[1]
        self._thread = threading.Thread(target=self._servidor.serve_forever, daemon=True)

    def __enter__(self):
        self._thread.start()
        return self

    def __exit__(self, *_):
        self._servidor.shutdown()
        self._servidor.server_close()

    def converter(self, corpo=b"PK\x03\x04conteudo"):
        conexao = http.client.HTTPConnection("127.0.0.1", self.porta, timeout=30)
        try:
            conexao.request("POST", "/converter", body=corpo,
                            headers={"Content-Length": str(len(corpo))})
            resposta = conexao.getresponse()
            resposta.read()
            return resposta.status
        finally:
            conexao.close()


class TestTetoDeConversoesSimultaneas(unittest.TestCase):
    def test_requisicao_alem_do_teto_recebe_503_em_vez_de_derrubar_o_container(self):
        modulo = carregar_handler(
            PDFCONV_MAXIMO_SIMULTANEAS=1,
            PDFCONV_ESPERA_VAGA_SEGUNDOS=1,
        )

        # Dublê: ocupa a vaga por tempo suficiente para as outras requisicoes
        # esgotarem a espera. Nenhum LibreOffice envolvido.
        barreira = threading.Event()

        def conversao_lenta(comando, **kwargs):
            barreira.wait(timeout=10)
            pathlib.Path(comando[-1]).write_bytes(b"%PDF-falso")
            return subprocess.CompletedProcess(comando, 0, b"", b"")

        modulo.subprocess.run = conversao_lenta

        respostas = []
        trava = threading.Lock()

        with ServidorDeTeste(modulo) as servidor:
            def disparar():
                status = servidor.converter()
                with trava:
                    respostas.append(status)

            threads = [threading.Thread(target=disparar) for _ in range(4)]
            for t in threads:
                t.start()
            # Solta as conversoes so depois que a espera por vaga estourou.
            barreira.wait(timeout=2.5)
            barreira.set()
            for t in threads:
                t.join(timeout=20)

        self.assertIn(503, respostas,
                      f"com 1 vaga e 4 requisicoes simultaneas, alguma tem que receber 503; obtive {respostas}")
        self.assertTrue(all(status in (200, 503) for status in respostas),
                        f"nenhuma requisicao pode falhar de outro jeito; obtive {respostas}")

    def test_vaga_e_devolvida_quando_a_conversao_falha(self):
        """Semaforo vazando em caminho de erro trava o sidecar para sempre."""
        modulo = carregar_handler(PDFCONV_MAXIMO_SIMULTANEAS=1, PDFCONV_ESPERA_VAGA_SEGUNDOS=1)

        def conversao_que_falha(comando, **kwargs):
            return subprocess.CompletedProcess(comando, 1, b"", b"erro")

        modulo.subprocess.run = conversao_que_falha

        with ServidorDeTeste(modulo) as servidor:
            for tentativa in range(3):
                status = servidor.converter()
                self.assertEqual(500, status, f"tentativa {tentativa}")

        # Se a vaga vazasse, a segunda tentativa teria voltado 503.


class TestPrazoDaConversao(unittest.TestCase):
    def test_conversao_alem_do_prazo_vira_504(self):
        modulo = carregar_handler(PDFCONV_PRAZO_SEGUNDOS=1)

        def estoura_o_prazo(comando, **kwargs):
            raise subprocess.TimeoutExpired(cmd=comando, timeout=kwargs.get("timeout", 1))

        modulo.subprocess.run = estoura_o_prazo

        with ServidorDeTeste(modulo) as servidor:
            self.assertEqual(504, servidor.converter())

    def test_prazo_e_realmente_repassado_ao_subprocesso(self):
        """Sem o timeout=, subprocess.run espera para sempre e o prazo e ficcao."""
        modulo = carregar_handler(PDFCONV_PRAZO_SEGUNDOS=37)
        recebidos = {}

        def registrar(comando, **kwargs):
            recebidos.update(kwargs)
            pathlib.Path(comando[-1]).write_bytes(b"%PDF-falso")
            return subprocess.CompletedProcess(comando, 0, b"", b"")

        modulo.subprocess.run = registrar

        with ServidorDeTeste(modulo) as servidor:
            self.assertEqual(200, servidor.converter())

        self.assertEqual(37, recebidos.get("timeout"))


class TestLimitesDoAmbiente(unittest.TestCase):
    def test_valor_invalido_ou_nao_positivo_cai_no_padrao(self):
        """Config quebrada nao pode virar 'sem limite' silenciosamente."""
        modulo = carregar_handler(
            PDFCONV_MAXIMO_SIMULTANEAS="abc",
            PDFCONV_PRAZO_SEGUNDOS="0",
        )
        self.assertEqual(2, modulo.MAXIMO_CONVERSOES_SIMULTANEAS)
        self.assertEqual(120, modulo.PRAZO_CONVERSAO_SEGUNDOS)


class TestContratoDeRota(unittest.TestCase):
    def test_corpo_vazio_e_rota_desconhecida(self):
        modulo = carregar_handler()
        with ServidorDeTeste(modulo) as servidor:
            self.assertEqual(400, servidor.converter(corpo=b""))

            conexao = http.client.HTTPConnection("127.0.0.1", servidor.porta, timeout=10)
            conexao.request("POST", "/rota-que-nao-existe", body=b"x",
                            headers={"Content-Length": "1"})
            resposta = conexao.getresponse()
            resposta.read()
            conexao.close()
            self.assertEqual(404, resposta.status)


if __name__ == "__main__":
    unittest.main()
