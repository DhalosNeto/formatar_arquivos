import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { Inicio } from './Inicio'

function renderizarComConsultas() {
  const cliente = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={cliente}>
      <Inicio />
    </QueryClientProvider>,
  )
}

/**
 * Mock de fetch que roteia por método + trecho da URL, na ordem declarada.
 *
 * O método faz parte da chave de propósito: POST /v1/documentos (envio) e
 * GET /v1/documentos (listagem) compartilham o caminho e devolvem formatos
 * diferentes — objeto e array. Casar só pela URL faria a listagem receber a
 * resposta do envio, um erro que não existe contra a API real.
 */
function mockarFetchRoteado(rotas: Array<[string, string, () => unknown]>) {
  return vi.fn(async (entrada: RequestInfo | URL, opcoes?: RequestInit) => {
    const caminho = typeof entrada === 'string' ? entrada : entrada.toString()
    const metodo = opcoes?.method ?? 'GET'
    const rota = rotas.find(([padrao, metodoEsperado]) =>
      caminho.includes(padrao) && metodoEsperado === metodo,
    )
    if (!rota) {
      throw new Error(`rota não mockada: ${metodo} ${caminho}`)
    }
    return { ok: true, status: 200, json: async () => rota[2]() }
  })
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('Inicio — fluxo de envio até o preview', () => {
  it('arquivo enviado com sucesso leva à exibição do preview', async () => {
    const chamada = mockarFetchRoteado([
      ['/v1/documentos/doc-1/preview', 'GET', () => ({ url: 'https://minio.local/preview/doc-1.pdf' })],
      [
        '/v1/documentos',
        'POST',
        () => ({
          id: 'doc-1',
          nome_original: 'artigo.docx',
          formato: 'docx',
          tamanho_bytes: 10,
          status: 'recebido',
          tem_preview: true,
        }),
      ],
      ['/v1/documentos', 'GET', () => []],
      ['/v1/saude', 'GET', () => ({ estado: 'ok', versao: 'dev' })],
    ])
    vi.stubGlobal('fetch', chamada)

    renderizarComConsultas()

    const arquivo = new File(['conteudo'], 'artigo.docx', {
      type: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
    })
    fireEvent.change(screen.getByLabelText(/selecionar arquivo/i), { target: { files: [arquivo] } })

    await screen.findByText('artigo.docx')

    const quadro = await screen.findByTitle(/pré-visualiza/i)
    expect(quadro).toHaveAttribute('src', 'https://minio.local/preview/doc-1.pdf')
  })

  it('lista de documentos se atualiza sozinha depois que um novo documento é enviado', async () => {
    let chamadasDeListagem = 0
    const chamada = vi.fn(async (entrada: RequestInfo | URL, opcoes?: RequestInit) => {
      const caminho = typeof entrada === 'string' ? entrada : entrada.toString()
      const metodo = opcoes?.method ?? 'GET'

      if (caminho.includes('/v1/saude')) {
        return { ok: true, status: 200, json: async () => ({ estado: 'ok', versao: 'dev' }) }
      }
      if (caminho.includes('/v1/documentos') && metodo === 'POST') {
        return {
          ok: true,
          status: 201,
          json: async () => ({
            id: 'doc-1',
            nome_original: 'artigo.docx',
            formato: 'docx',
            tamanho_bytes: 10,
            status: 'recebido',
            tem_preview: true,
          }),
        }
      }
      if (caminho.includes('/v1/documentos/doc-1/preview')) {
        return { ok: true, status: 200, json: async () => ({ url: 'https://minio.local/preview/doc-1.pdf' }) }
      }
      // GET /v1/documentos (listagem): vazia antes do envio, com o
      // documento novo depois — é isso que prova a invalidação da query.
      if (caminho.includes('/v1/documentos')) {
        chamadasDeListagem += 1
        const corpo =
          chamadasDeListagem === 1
            ? []
            : [
                {
                  id: 'doc-1',
                  nome_original: 'artigo.docx',
                  formato: 'docx',
                  tamanho_bytes: 10,
                  status: 'recebido',
                  tem_preview: true,
                },
              ]
        return { ok: true, status: 200, json: async () => corpo }
      }
      throw new Error(`rota não mockada: ${caminho} (${metodo})`)
    })
    vi.stubGlobal('fetch', chamada)

    renderizarComConsultas()

    // Antes do envio, a listagem está vazia.
    await screen.findByText(/nenhum documento/i)

    const arquivo = new File(['conteudo'], 'artigo.docx', {
      type: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
    })
    fireEvent.change(screen.getByLabelText(/selecionar arquivo/i), { target: { files: [arquivo] } })

    // findAllByText, não findByText: depois do envio o nome aparece na
    // confirmação E na listagem, e a versão singular estoura com mais de um
    // match. Afirmar que existe exatamente uma ocorrência antes da outra seria
    // medir o instante de commit do React, não comportamento visível.
    await screen.findAllByText('artigo.docx')

    // A lista precisa refletir o novo documento sem qualquer reload manual:
    // o nome do arquivo aparece de novo, agora vindo da listagem.
    const ocorrencias = await screen.findAllByText('artigo.docx')
    expect(ocorrencias.length).toBeGreaterThan(1)
    expect(screen.queryByText(/nenhum documento/i)).not.toBeInTheDocument()
  })
})
