/**
 * Cliente HTTP da API. Toda chamada passa por aqui para que o tratamento de
 * erro e o envio de credenciais fiquem em um lugar só.
 */

const BASE_URL = '/v1'

/** Erro devolvido pela API, no formato padrão do backend. */
export class ErroDaApi extends Error {
  readonly status: number
  readonly codigo: string
  readonly razoes: string[]

  constructor(status: number, codigo: string, descricao: string, razoes: string[] = []) {
    super(descricao)
    this.name = 'ErroDaApi'
    this.status = status
    this.codigo = codigo
    this.razoes = razoes
  }
}

interface CorpoErro {
  codigo: string
  descricao: string
  razoes?: string[]
}

export async function buscarNaApi<T>(caminho: string, opcoes: RequestInit = {}): Promise<T> {
  const resposta = await fetch(`${BASE_URL}${caminho}`, {
    // A sessão vive em cookie httpOnly; nada de token em localStorage.
    credentials: 'include',
    headers: { Accept: 'application/json', ...opcoes.headers },
    ...opcoes,
  })

  if (!resposta.ok) {
    const corpo = (await resposta.json().catch(() => null)) as CorpoErro | null
    throw new ErroDaApi(
      resposta.status,
      corpo?.codigo ?? 'erro_desconhecido',
      corpo?.descricao ?? 'não foi possível completar a requisição',
      corpo?.razoes ?? [],
    )
  }

  return (await resposta.json()) as T
}
