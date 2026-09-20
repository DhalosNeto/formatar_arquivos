import { QueryClient, QueryClientProvider } from '@tanstack/react-query'

import { Inicio } from './paginas/Inicio'

const clienteDeConsultas = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      refetchOnWindowFocus: false,
    },
  },
})

export function App() {
  return (
    <QueryClientProvider client={clienteDeConsultas}>
      <Inicio />
    </QueryClientProvider>
  )
}
