import { afterEach, describe, expect, it, vi } from 'vitest'

import { buscarNaApi, ErroDaApi } from './cliente'

afterEach(() => {
  vi.restoreAllMocks()
})

describe('buscarNaApi', () => {
  it('devolve o corpo quando a resposta é bem-sucedida', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({ id: 'abc' }) }))

    await expect(buscarNaApi<{ id: string }>('/documentos')).resolves.toEqual({ id: 'abc' })
  })

  it('lança ErroDaApi preservando código e razões', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 400,
        json: async () => ({
          codigo: 'requisicao_invalida',
          descricao: 'requisição inválida',
          razoes: ['email: obrigatório'],
        }),
      }),
    )

    const erro = await buscarNaApi('/usuarios').catch((e: unknown) => e)

    expect(erro).toBeInstanceOf(ErroDaApi)
    expect(erro).toMatchObject({
      status: 400,
      codigo: 'requisicao_invalida',
      razoes: ['email: obrigatório'],
    })
  })

  it('não quebra quando o corpo do erro não é JSON', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 502,
        json: async () => {
          throw new Error('resposta vazia')
        },
      }),
    )

    const erro = (await buscarNaApi('/saude').catch((e: unknown) => e)) as ErroDaApi

    expect(erro.status).toBe(502)
    expect(erro.codigo).toBe('erro_desconhecido')
  })
})

describe('opções do chamador não desligam o que é obrigatório', () => {
  it('mantém credentials include mesmo se o chamador passar outro valor', async () => {
    const espiao = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ ok: true }), { status: 200 }),
    )
    vi.stubGlobal('fetch', espiao)

    // 'omit' não enviaria o cookie de sessão: a requisição viraria anônima e a
    // API responderia 404 como se o documento não existisse.
    await buscarNaApi('/documentos', { credentials: 'omit' })

    expect(espiao.mock.calls[0][1].credentials).toBe('include')
  })

  it('preserva o Accept quando o chamador passa outros cabeçalhos', async () => {
    const espiao = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ ok: true }), { status: 200 }),
    )
    vi.stubGlobal('fetch', espiao)

    await buscarNaApi('/documentos', { headers: { 'X-Teste': '1' } })

    const cabecalhos = espiao.mock.calls[0][1].headers
    expect(cabecalhos.Accept).toBe('application/json')
    expect(cabecalhos['X-Teste']).toBe('1')
  })
})
