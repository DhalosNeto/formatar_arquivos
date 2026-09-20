import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { ListaDeDocumentos } from './ListaDeDocumentos'

function renderizarComConsultas() {
  const cliente = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={cliente}>
      <ListaDeDocumentos />
    </QueryClientProvider>,
  )
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('ListaDeDocumentos', () => {
  it('mostra nome e status de cada documento da sessão', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => [
          {
            id: 'doc-1',
            nome_original: 'artigo.docx',
            formato: 'docx',
            tamanho_bytes: 10,
            status: 'recebido',
            tem_preview: true,
          },
          {
            id: 'doc-2',
            nome_original: 'outro-artigo.docx',
            formato: 'docx',
            tamanho_bytes: 20,
            status: 'processado',
            tem_preview: false,
          },
        ],
      }),
    )

    renderizarComConsultas()

    expect(await screen.findByText('artigo.docx')).toBeInTheDocument()
    expect(screen.getByText('outro-artigo.docx')).toBeInTheDocument()
    expect(screen.getByText(/recebido/i)).toBeInTheDocument()
    expect(screen.getByText(/processado/i)).toBeInTheDocument()
  })

  it('mostra uma mensagem quando não há documentos, não uma lista em branco', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => [] }))

    renderizarComConsultas()

    expect(await screen.findByText(/nenhum documento/i)).toBeInTheDocument()
    expect(screen.queryByRole('list')).not.toBeInTheDocument()
  })

  it('mostra o erro da API com role="alert"', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 500,
        json: async () => ({ codigo: 'erro_interno', descricao: 'falha ao listar documentos' }),
      }),
    )

    renderizarComConsultas()

    expect(await screen.findByRole('alert')).toHaveTextContent(/falha ao listar documentos/i)
  })
})
