import { buscarNaApi } from './cliente'

export interface DocumentoEnviado {
  id: string
  nome_original: string
  formato: string
  tamanho_bytes: number
  status: string
  tem_preview: boolean
}

export interface RespostaPreview {
  url: string
}

export function enviarDocumento(arquivo: File): Promise<DocumentoEnviado> {
  const formulario = new FormData()
  formulario.append('arquivo', arquivo)

  // Sem Content-Type manual: o browser gera o boundary do multipart sozinho.
  return buscarNaApi<DocumentoEnviado>('/documentos', {
    method: 'POST',
    body: formulario,
  })
}

export function obterDocumento(id: string): Promise<DocumentoEnviado> {
  return buscarNaApi<DocumentoEnviado>(`/documentos/${id}`)
}

/** Chave de query do TanStack Query para a listagem de documentos da sessão. */
export const chaveDocumentosDaSessao = ['documentos-da-sessao']

export function listarDocumentos(limite?: number, deslocamento?: number): Promise<DocumentoEnviado[]> {
  const parametros = new URLSearchParams()
  if (limite !== undefined) {
    parametros.set('limite', String(limite))
  }
  if (deslocamento !== undefined) {
    parametros.set('deslocamento', String(deslocamento))
  }
  const consulta = parametros.toString()
  return buscarNaApi<DocumentoEnviado[]>(`/documentos${consulta ? `?${consulta}` : ''}`)
}

export function obterPreviewDeDocumento(id: string): Promise<RespostaPreview> {
  return buscarNaApi<RespostaPreview>(`/documentos/${id}/preview`)
}
