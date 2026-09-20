import { useState } from 'react'

import type { DocumentoEnviado } from '../api/documentos'
import { EnvioDeDocumento } from '../componentes/EnvioDeDocumento'
import { ListaDeDocumentos } from '../componentes/ListaDeDocumentos'
import { PreviewDeDocumento } from '../componentes/PreviewDeDocumento'

/** Página inicial: envio do artigo seguido da pré-visualização. */
export function Inicio() {
  const [documento, setDocumento] = useState<DocumentoEnviado | null>(null)

  return (
    <main className="mx-auto w-full max-w-6xl px-6 py-10">
      <header className="mb-8 space-y-2">
        <h1 className="text-3xl font-semibold text-slate-900">Formatador Acadêmico</h1>
        <p className="text-slate-600">
          Envie seu artigo, escolha a revista e baixe o trabalho já formatado nas diretrizes dela.
        </p>
      </header>

      {/* Enquanto não há documento, o envio ocupa a largura toda. Depois ele
          encolhe para uma coluna lateral e a pré-visualização fica com o
          espaço, que é o que o usuário passa a querer olhar. */}
      <div
        className={
          documento ? 'grid items-start gap-6 lg:grid-cols-[22rem_minmax(0,1fr)]' : 'mx-auto max-w-3xl'
        }
      >
        <section className="space-y-6">
          <EnvioDeDocumento onEnviado={setDocumento} />
          <ListaDeDocumentos />
        </section>

        {documento && (
          <section className="space-y-3">
            {/* Sem repetir o nome do arquivo aqui: a confirmação verde do
                envio, ao lado, já o mostra. */}
            <h2 className="text-lg font-medium text-slate-800">Pré-visualização</h2>
            <PreviewDeDocumento documentoId={documento.id} temPreview={documento.tem_preview} />
          </section>
        )}
      </div>
    </main>
  )
}
