import { useQuery } from '@tanstack/react-query'

import { obterSaudeDaApi } from '../api/saude'

/** Mostra se a API está respondendo — a checagem viva do ambiente local. */
export function StatusDaApi() {
  const { data, isPending, isError, error } = useQuery({
    queryKey: ['saude-da-api'],
    queryFn: obterSaudeDaApi,
    retry: false,
  })

  if (isPending) {
    return <p className="text-sm text-slate-500">Consultando a API…</p>
  }

  if (isError) {
    return (
      <p role="alert" className="text-sm text-red-600">
        API indisponível: {error.message}
      </p>
    )
  }

  return (
    <p className="text-sm text-emerald-700">
      API disponível — versão <strong>{data.versao}</strong>
    </p>
  )
}
