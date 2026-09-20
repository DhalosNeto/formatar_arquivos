import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { StatusDaApi } from './StatusDaApi'

function renderizarComConsultas(no: ReactNode) {
  const cliente = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={cliente}>{no}</QueryClientProvider>)
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('StatusDaApi', () => {
  it('mostra a versão quando a API responde', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({ estado: 'ok', versao: '1.2.3' }),
      }),
    )

    renderizarComConsultas(<StatusDaApi />)

    expect(await screen.findByText(/API disponível/)).toBeInTheDocument()
    expect(await screen.findByText('1.2.3')).toBeInTheDocument()
  })

  it('avisa quando a API está fora do ar', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 503,
        json: async () => ({ codigo: 'dependencia_indisponivel', descricao: 'banco fora do ar' }),
      }),
    )

    renderizarComConsultas(<StatusDaApi />)

    const aviso = await screen.findByRole('alert')
    expect(aviso).toHaveTextContent('banco fora do ar')
  })

  it('envia as credenciais da sessão na requisição', async () => {
    const chamada = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ estado: 'ok', versao: 'dev' }),
    })
    vi.stubGlobal('fetch', chamada)

    renderizarComConsultas(<StatusDaApi />)
    await screen.findByText(/API disponível/)

    expect(chamada).toHaveBeenCalledWith('/v1/saude', expect.objectContaining({ credentials: 'include' }))
  })
})
