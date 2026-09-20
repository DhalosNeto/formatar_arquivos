import { useQuery } from '@tanstack/react-query'

import { chaveDocumentosDaSessao, listarDocumentos } from '../api/documentos'
import { ErroDaApi } from '../api/cliente'

/** Lista os documentos já enviados na sessão atual. */
export function ListaDeDocumentos() {
  const consulta = useQuery({
    queryKey: chaveDocumentosDaSessao,
    queryFn: () => listarDocumentos(),
  })

  if (consulta.isError) {
    const erro = consulta.error
    return (
      <p role="alert" className="text-sm font-medium text-red-600">
        {erro instanceof ErroDaApi ? erro.message : 'não foi possível carregar os documentos'}
      </p>
    )
  }

  const documentos = consulta.data ?? []

  if (documentos.length === 0) {
    return <p className="text-sm text-slate-500">Nenhum documento enviado ainda.</p>
  }

  return (
    <ul role="list" className="divide-y divide-slate-200 rounded-lg border border-slate-200 bg-white">
      {documentos.map((documento) => (
        <li key={documento.id} className="flex items-center justify-between gap-4 px-4 py-3">
          <span className="truncate text-sm font-medium text-slate-800">{documento.nome_original}</span>
          <span className="text-xs text-slate-500">{documento.status}</span>
        </li>
      ))}
    </ul>
  )
}
