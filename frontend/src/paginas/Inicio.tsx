import { StatusDaApi } from '../componentes/StatusDaApi'

/** Página inicial provisória: a tela de upload entra na F1. */
export function Inicio() {
  return (
    <main className="mx-auto flex min-h-full max-w-2xl flex-col justify-center gap-6 px-6 py-16">
      <header className="space-y-2">
        <h1 className="text-3xl font-semibold text-slate-900">Formatador Acadêmico</h1>
        <p className="text-slate-600">
          Envie seu artigo, escolha a revista e baixe o trabalho já formatado nas diretrizes dela.
        </p>
      </header>

      <section className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
        <h2 className="mb-2 text-sm font-medium text-slate-700">Ambiente</h2>
        <StatusDaApi />
      </section>

      <p className="text-xs text-slate-400">
        Fase F0 concluída: fundação do projeto. O envio de documentos entra na F1.
      </p>
    </main>
  )
}
