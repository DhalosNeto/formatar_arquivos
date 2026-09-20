#!/usr/bin/env python3
"""Handler HTTP do sidecar de conversao DOCX -> PDF.

Contrato HTTP, unica fonte da verdade (replicado no godoc do pacote Go
internal/infra/pdfconv):
  POST /converter   corpo = bytes crus do .docx -> 200 + bytes crus do .pdf
                    corpo ausente/vazio -> 400
                    corpo maior que TAMANHO_MAXIMO_BYTES -> 413
                    conversao falhou -> 500, corpo = texto curto, sem detalhe interno
  GET  /saude        -> 200 quando uma chamada RPC de verdade ao unoserver
                        interno teve sucesso; 503 caso contrario.

Por que a checagem de /saude faz uma chamada RPC de verdade e nao so testa se
a porta 2003 esta aberta: o unoserver abre o socket XML-RPC ANTES de terminar
de conectar ao LibreOffice e registrar os metodos info()/convert() (ver
unoserver/server.py: o `with XMLRPCServer(...)` do metodo serve() abre o
socket na entrada do bloco; os metodos so sao registrados dezenas de linhas
depois, apos os retries de conexao ao LibreOffice). Um health check por porta
aberta mentiria "pronto" durante toda essa janela de startup.

A checagem de /saude NUNCA usa unoserver.client.UnoClient (ou o metodo
_connect que ele expõe): esse cliente foi desenhado para uso em linha de
comando e reexecuta a chamada ate 5 vezes com 10s de espera entre elas --
correto para um humano esperando o servidor subir, catastrófico dentro de uma
rota de health check que precisa responder rapido e uma unica vez.

O watchdog do unoserver roda numa thread dedicada. Se o processo do unoserver
morrer, a thread chama os._exit (nunca sys.exit): SystemExit levantado numa
thread que nao e a principal so encerra AQUELA thread -- o ThreadingHTTPServer
continuaria de pe aceitando requisicao para um conversor morto. os._exit mata
o processo inteiro de verdade, e a restart policy do compose reinicia o
container.
"""
import http.server
import logging
import os
import socket
import subprocess
import tempfile
import threading
import xmlrpc.client

PORTA_HTTP = 2004
PORTA_UNOSERVER = 2003
PORTA_UNO = 2002
TAMANHO_MAXIMO_BYTES = 64 * 1024 * 1024
TEMPO_LIMITE_SAUDE_SEGUNDOS = 3

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
log = logging.getLogger("pdfconv-handler")


def iniciar_unoserver() -> subprocess.Popen:
    return subprocess.Popen(
        [
            "unoserver",
            "--interface", "127.0.0.1",
            "--port", str(PORTA_UNOSERVER),
            "--uno-port", str(PORTA_UNO),
        ],
    )


def vigiar_unoserver(processo: subprocess.Popen) -> None:
    """Roda numa thread dedicada, separada do ThreadingHTTPServer."""
    codigo = processo.wait()
    log.critical("unoserver morreu (codigo %s); encerrando o processo", codigo)
    os._exit(1)  # nunca sys.exit aqui -- ver docstring do modulo


def unoserver_respondeu_rpc() -> bool:
    socket.setdefaulttimeout(TEMPO_LIMITE_SAUDE_SEGUNDOS)
    try:
        proxy = xmlrpc.client.ServerProxy(f"http://127.0.0.1:{PORTA_UNOSERVER}")
        info = proxy.info()
        return info.get("api") is not None
    except Exception:
        return False
    finally:
        socket.setdefaulttimeout(None)


class ManipuladorConversor(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, formato, *args):
        pass  # regra 7 do CLAUDE.md: nunca logar corpo/caminho de documento do usuario

    def do_GET(self):
        if self.path == "/saude" and unoserver_respondeu_rpc():
            self._responder(200, b"")
        else:
            self._responder(503, b"")

    def do_POST(self):
        if self.path != "/converter":
            self._responder(404, b"")
            return

        try:
            tamanho = int(self.headers.get("Content-Length", "0"))
        except ValueError:
            tamanho = 0
        if tamanho <= 0:
            self._responder(400, b"tamanho invalido")
            return
        if tamanho > TAMANHO_MAXIMO_BYTES:
            self._responder(413, b"documento excede o tamanho maximo")
            return

        corpo = self.rfile.read(tamanho)

        with tempfile.TemporaryDirectory() as diretorio:
            entrada = os.path.join(diretorio, "entrada.docx")
            saida = os.path.join(diretorio, "entrada.pdf")
            with open(entrada, "wb") as f:
                f.write(corpo)

            from unoserver.client import UnoClient

            try:
                UnoClient(port=str(PORTA_UNOSERVER)).convert(inpath=entrada, outpath=saida)
            except Exception as exc:
                log.error("falha na conversao: %s", exc.__class__.__name__)
                self._responder(500, b"falha na conversao")
                return

            if not os.path.exists(saida):
                self._responder(500, b"falha na conversao")
                return

            with open(saida, "rb") as f:
                pdf = f.read()

        self._responder(200, pdf, content_type="application/pdf")

    def _responder(self, status, corpo, content_type="text/plain"):
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(corpo)))
        self.end_headers()
        if corpo:
            self.wfile.write(corpo)


def main():
    processo = iniciar_unoserver()
    threading.Thread(target=vigiar_unoserver, args=(processo,), daemon=True).start()

    servidor = http.server.ThreadingHTTPServer(("0.0.0.0", PORTA_HTTP), ManipuladorConversor)
    log.info("sidecar pdfconv escutando em 0.0.0.0:%d", PORTA_HTTP)
    servidor.serve_forever()


if __name__ == "__main__":
    main()
