import { afterEach, describe, expect, it, vi } from 'vitest'

import { ErroDaApi } from './cliente'
import { enviarDocumento, listarDocumentos, obterDocumento, obterPreviewDeDocumento } from './documentos'

afterEach(() => {
  vi.restoreAllMocks()
})

function criarArquivoDocx(nome = 'artigo.docx') {
  return new File(['conteudo'], nome, {
    type: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
  })
}

describe('enviarDocumento', () => {
  it('envia o arquivo como multipart no campo "arquivo" via POST', async () => {
    const chamada = vi.fn().mockResolvedValue({
      ok: true,
      status: 201,
      json: async () => ({
        id: 'doc-1',
        nome_original: 'artigo.docx',
        formato: 'docx',
        tamanho_bytes: 1234,
        status: 'recebido',
        tem_preview: true,
      }),
    })
    vi.stubGlobal('fetch', chamada)

    const arquivo = criarArquivoDocx()
    const documento = await enviarDocumento(arquivo)

    expect(documento).toMatchObject({ id: 'doc-1', nome_original: 'artigo.docx', tem_preview: true })

    const [url, opcoes] = chamada.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/v1/documentos')
    expect(opcoes.method).toBe('POST')
    expect(opcoes.body).toBeInstanceOf(FormData)
    expect((opcoes.body as FormData).get('arquivo')).toBe(arquivo)
  })

  it('propaga ErroDaApi com as razões quando o backend recusa o arquivo', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 400,
        json: async () => ({
          codigo: 'requisicao_invalida',
          descricao: 'requisição inválida',
          razoes: ['arquivo: o pacote excede os limites de descompressão permitidos'],
        }),
      }),
    )

    const erro = await enviarDocumento(criarArquivoDocx('bomba.docx')).catch((e: unknown) => e)

    expect(erro).toBeInstanceOf(ErroDaApi)
    expect((erro as ErroDaApi).razoes).toContain(
      'arquivo: o pacote excede os limites de descompressão permitidos',
    )
  })
})

describe('obterDocumento', () => {
  it('busca o documento pelo id com as credenciais da sessão', async () => {
    const chamada = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        id: 'doc-1',
        nome_original: 'artigo.docx',
        formato: 'docx',
        tamanho_bytes: 1234,
        status: 'processado',
        tem_preview: true,
      }),
    })
    vi.stubGlobal('fetch', chamada)

    const documento = await obterDocumento('doc-1')

    expect(documento.id).toBe('doc-1')
    expect(chamada).toHaveBeenCalledWith(
      '/v1/documentos/doc-1',
      expect.objectContaining({ credentials: 'include' }),
    )
  })

  it('lança ErroDaApi quando o documento não existe', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 404,
        json: async () => ({ codigo: 'recurso_nao_encontrado', descricao: 'documento não encontrado' }),
      }),
    )

    const erro = await obterDocumento('inexistente').catch((e: unknown) => e)

    expect(erro).toBeInstanceOf(ErroDaApi)
    expect((erro as ErroDaApi).status).toBe(404)
  })
})

describe('listarDocumentos', () => {
  it('busca a lista de documentos da sessão sem parâmetros', async () => {
    const chamada = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => [] })
    vi.stubGlobal('fetch', chamada)

    const documentos = await listarDocumentos()

    expect(documentos).toEqual([])
    const [url, opcoes] = chamada.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/v1/documentos')
    expect(opcoes).toMatchObject({ credentials: 'include' })
  })

  it('envia limite e deslocamento como query string quando informados', async () => {
    const chamada = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => [] })
    vi.stubGlobal('fetch', chamada)

    await listarDocumentos(5, 10)

    const [url] = chamada.mock.calls[0] as [string, RequestInit]
    const consulta = new URL(url, 'http://localhost').searchParams
    expect(consulta.get('limite')).toBe('5')
    expect(consulta.get('deslocamento')).toBe('10')
  })

  it('devolve a lista de documentos convertida a partir do JSON da API', async () => {
    const chamada = vi.fn().mockResolvedValue({
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
      ],
    })
    vi.stubGlobal('fetch', chamada)

    const documentos = await listarDocumentos()

    expect(documentos).toHaveLength(1)
    expect(documentos[0]).toMatchObject({ id: 'doc-1', nome_original: 'artigo.docx', status: 'recebido' })
  })

  it('sessão nova sem documentos devolve lista vazia, não erro', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => [] }))

    await expect(listarDocumentos()).resolves.toEqual([])
  })

  it('propaga ErroDaApi quando a API falha ao listar', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 500,
        json: async () => ({ codigo: 'erro_interno', descricao: 'falha ao listar documentos' }),
      }),
    )

    const erro = await listarDocumentos().catch((e: unknown) => e)

    expect(erro).toBeInstanceOf(ErroDaApi)
    expect((erro as ErroDaApi).status).toBe(500)
  })
})

describe('obterPreviewDeDocumento', () => {
  it('busca a url pré-assinada do preview', async () => {
    const chamada = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ url: 'https://minio.local/preview/doc-1.pdf?assinatura=abc' }),
    })
    vi.stubGlobal('fetch', chamada)

    const preview = await obterPreviewDeDocumento('doc-1')

    expect(preview.url).toBe('https://minio.local/preview/doc-1.pdf?assinatura=abc')
    expect(chamada).toHaveBeenCalledWith(
      '/v1/documentos/doc-1/preview',
      expect.objectContaining({ credentials: 'include' }),
    )
  })

  it('lança ErroDaApi quando o documento não tem preview (404)', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 404,
        json: async () => ({ codigo: 'recurso_nao_encontrado', descricao: 'documento não encontrado' }),
      }),
    )

    const erro = await obterPreviewDeDocumento('doc-1').catch((e: unknown) => e)

    expect(erro).toBeInstanceOf(ErroDaApi)
    expect((erro as ErroDaApi).status).toBe(404)
  })
})
