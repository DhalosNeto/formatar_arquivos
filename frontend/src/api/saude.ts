import { buscarNaApi } from './cliente'

export interface EstadoDaApi {
  estado: string
  versao: string
}

export function obterSaudeDaApi(): Promise<EstadoDaApi> {
  return buscarNaApi<EstadoDaApi>('/saude')
}
