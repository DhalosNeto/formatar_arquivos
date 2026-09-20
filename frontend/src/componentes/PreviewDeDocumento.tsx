import { useQuery } from '@tanstack/react-query'

import { obterPreviewDeDocumento } from '../api/documentos'

interface PreviewDeDocumentoProps {
  documentoId: string
  temPreview: boolean
}

/** Pré-visualização em PDF do documento, renderizada pelo próprio navegador. */
export function PreviewDeDocumento({ documentoId, temPreview }: PreviewDeDocumentoProps) {
  const { data, isError, error } = useQuery({
    queryKey: ['preview-de-documento', documentoId],
    queryFn: () => obterPreviewDeDocumento(documentoId),
    enabled: temPreview,
  })

  if (!temPreview) {
    return <p className="text-sm text-slate-500">Este documento não tem pré-visualização disponível.</p>
  }

  if (isError) {
    return (
      <p role="alert" className="text-sm text-red-600">
        {error.message}
      </p>
    )
  }

  if (!data) {
    return <p className="text-sm text-slate-500">Carregando pré-visualização…</p>
  }

  return (
    <iframe
      title="Pré-visualização do documento"
      src={data.url}
      // Altura relativa à janela, não fixa em pixel: o leitor de PDF nativo
      // gasta espaço com a própria barra e a faixa de miniaturas, então um
      // quadro baixo demais deixa a página do artigo ilegível.
      className="h-[calc(100vh-13rem)] min-h-[32rem] w-full rounded-lg border border-slate-200 bg-slate-100"
    />
  )
}
