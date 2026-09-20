import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { EnvioDeDocumento } from './EnvioDeDocumento'

function renderizarComConsultas(no: ReactNode) {
  const cliente = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(<QueryClientProvider client={cliente}>{no}</QueryClientProvider>)
}

function criarArquivoDocx(nome = 'artigo.docx') {
  return new File(['conteudo'], nome, {
    type: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
  })
}

function respostaDeEnvioOk() {
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

afterEach(() => {
  vi.restoreAllMocks()
})

describe('EnvioDeDocumento', () => {
  it('aceita arquivo solto na zona de arrastar', async () => {
    const enviado = vi.fn()
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(respostaDeEnvioOk() as unknown as Response)
    renderizarComConsultas(<EnvioDeDocumento onEnviado={enviado} />)

    const zona = screen.getByTestId('zona-de-arraste')
    fireEvent.drop(zona, { dataTransfer: { files: [criarArquivoDocx()] } })

    await waitFor(() => expect(enviado).toHaveBeenCalledTimes(1))
  })

  it('recusa arquivo solto que não seja .docx sem chamar a API', async () => {
    const buscar = vi.spyOn(globalThis, 'fetch')
    renderizarComConsultas(<EnvioDeDocumento onEnviado={vi.fn()} />)

    const zona = screen.getByTestId('zona-de-arraste')
    fireEvent.drop(zona, { dataTransfer: { files: [criarArquivoDocx('planilha.xlsx')] } })

    expect(await screen.findByRole('alert')).toHaveTextContent(/\.docx/i)
    expect(buscar).not.toHaveBeenCalled()
  })

  it('tem um input de arquivo com rótulo associado, alcançável por teclado', () => {
    renderizarComConsultas(<EnvioDeDocumento onEnviado={vi.fn()} />)

    const input = screen.getByLabelText(/selecionar arquivo/i)
    expect(input).toHaveAttribute('type', 'file')

    input.focus()
    expect(input).toHaveFocus()
  })

  it('recusa arquivo que não é .docx antes de chamar a API', async () => {
    const chamada = vi.fn()
    vi.stubGlobal('fetch', chamada)

    renderizarComConsultas(<EnvioDeDocumento onEnviado={vi.fn()} />)

    const arquivoInvalido = new File(['conteudo'], 'artigo.pdf', { type: 'application/pdf' })
    fireEvent.change(screen.getByLabelText(/selecionar arquivo/i), { target: { files: [arquivoInvalido] } })

    const aviso = await screen.findByRole('alert')
    expect(aviso).toHaveTextContent(/\.docx/i)
    expect(chamada).not.toHaveBeenCalled()
  })

  it('arquivo selecionado dispara POST /v1/documentos com FormData no campo "arquivo"', async () => {
    const chamada = vi.fn().mockResolvedValue(respostaDeEnvioOk())
    vi.stubGlobal('fetch', chamada)

    renderizarComConsultas(<EnvioDeDocumento onEnviado={vi.fn()} />)

    const arquivo = criarArquivoDocx()
    fireEvent.change(screen.getByLabelText(/selecionar arquivo/i), { target: { files: [arquivo] } })

    await waitFor(() => expect(chamada).toHaveBeenCalledTimes(1))
    const [url, opcoes] = chamada.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/v1/documentos')
    expect(opcoes.body).toBeInstanceOf(FormData)
    expect((opcoes.body as FormData).get('arquivo')).toBeInstanceOf(File)
  })

  it('mostra estado de carregamento e desabilita novo envio enquanto sobe', async () => {
    let resolverEnvio: (valor: unknown) => void = () => {}
    const chamada = vi.fn().mockReturnValue(
      new Promise((resolve) => {
        resolverEnvio = resolve
      }),
    )
    vi.stubGlobal('fetch', chamada)

    renderizarComConsultas(<EnvioDeDocumento onEnviado={vi.fn()} />)

    const input = screen.getByLabelText(/selecionar arquivo/i) as HTMLInputElement
    fireEvent.change(input, { target: { files: [criarArquivoDocx()] } })

    await screen.findByText(/enviando/i)
    expect(input).toBeDisabled()

    resolverEnvio(respostaDeEnvioOk())
    await waitFor(() => expect(chamada).toHaveBeenCalledTimes(1))
  })

  it('sucesso mostra o nome do arquivo e chama onEnviado com o documento', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(respostaDeEnvioOk()))

    const aoEnviar = vi.fn()
    renderizarComConsultas(<EnvioDeDocumento onEnviado={aoEnviar} />)

    fireEvent.change(screen.getByLabelText(/selecionar arquivo/i), { target: { files: [criarArquivoDocx()] } })

    await screen.findByText('artigo.docx')
    expect(aoEnviar).toHaveBeenCalledWith(expect.objectContaining({ id: 'doc-1', nome_original: 'artigo.docx' }))
  })

  it('mostra a razão devolvida pelo backend quando o envio é recusado, não uma mensagem genérica', async () => {
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

    renderizarComConsultas(<EnvioDeDocumento onEnviado={vi.fn()} />)

    fireEvent.change(screen.getByLabelText(/selecionar arquivo/i), { target: { files: [criarArquivoDocx()] } })

    const aviso = await screen.findByRole('alert')
    expect(aviso).toHaveTextContent('arquivo: o pacote excede os limites de descompressão permitidos')
  })
})
