import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { PreviewDeDocumento } from './PreviewDeDocumento'

function renderizarComConsultas(no: ReactNode) {
  const cliente = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={cliente}>{no}</QueryClientProvider>)
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('PreviewDeDocumento', () => {
  it('busca e renderiza a url pré-assinada quando o documento tem preview', async () => {
    const chamada = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ url: 'https://minio.local/preview/doc-1.pdf?assinatura=abc' }),
    })
    vi.stubGlobal('fetch', chamada)

    renderizarComConsultas(<PreviewDeDocumento documentoId="doc-1" temPreview />)

    const quadro = await screen.findByTitle(/pré-visualiza/i)
    expect(quadro).toHaveAttribute('src', 'https://minio.local/preview/doc-1.pdf?assinatura=abc')
    expect(chamada).toHaveBeenCalledWith(
      '/v1/documentos/doc-1/preview',
      expect.objectContaining({ credentials: 'include' }),
    )
  })

  it('documento sem preview mostra aviso, não um quadro quebrado, e não chama a API', () => {
    const chamada = vi.fn()
    vi.stubGlobal('fetch', chamada)

    renderizarComConsultas(<PreviewDeDocumento documentoId="doc-1" temPreview={false} />)

    expect(screen.getByText(/não tem.*pré-visualiza/i)).toBeInTheDocument()
    expect(screen.queryByTitle(/pré-visualiza/i)).not.toBeInTheDocument()
    expect(chamada).not.toHaveBeenCalled()
  })

  it('mostra erro legível quando a busca do preview devolve 404', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 404,
        json: async () => ({ codigo: 'recurso_nao_encontrado', descricao: 'documento não encontrado' }),
      }),
    )

    renderizarComConsultas(<PreviewDeDocumento documentoId="doc-1" temPreview />)

    const aviso = await screen.findByRole('alert')
    expect(aviso).toHaveTextContent('documento não encontrado')
  })
})
