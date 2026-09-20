import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'

import { chaveDocumentosDaSessao, enviarDocumento, type DocumentoEnviado } from '../api/documentos'
import { ErroDaApi } from '../api/cliente'

interface EnvioDeDocumentoProps {
  onEnviado: (documento: DocumentoEnviado) => void
}

/**
 * Zona de envio de um artigo em .docx: aceita clique, teclado e arrastar.
 *
 * O <input type="file"> continua no DOM e só é escondido visualmente
 * (sr-only): esconder com `hidden` ou `display:none` o tiraria da ordem de
 * tabulação e do leitor de tela, e é o <label> associado a ele que faz a zona
 * inteira funcionar no clique sem precisar de onClick nenhum.
 */
export function EnvioDeDocumento({ onEnviado }: EnvioDeDocumentoProps) {
  const [erroDeExtensao, setErroDeExtensao] = useState<string | null>(null)
  const [arrastando, setArrastando] = useState(false)
  const clienteDeConsultas = useQueryClient()

  const mutacao = useMutation({
    mutationFn: enviarDocumento,
    onSuccess: (documento) => {
      onEnviado(documento)
      // Invalida a listagem para ela refletir o documento recém-enviado sem
      // depender de um reload manual.
      void clienteDeConsultas.invalidateQueries({ queryKey: chaveDocumentosDaSessao })
    },
  })

  function processarArquivo(arquivo: File | undefined) {
    if (!arquivo) {
      return
    }

    if (!arquivo.name.toLowerCase().endsWith('.docx')) {
      setErroDeExtensao('Selecione um arquivo com extensão .docx')
      return
    }

    setErroDeExtensao(null)
    mutacao.mutate(arquivo)
  }

  function aoSelecionarArquivo(evento: React.ChangeEvent<HTMLInputElement>) {
    processarArquivo(evento.target.files?.[0])
  }

  function aoSoltar(evento: React.DragEvent<HTMLElement>) {
    evento.preventDefault()
    setArrastando(false)
    if (mutacao.isPending) {
      return
    }
    processarArquivo(evento.dataTransfer?.files?.[0])
  }

  // preventDefault no dragOver é obrigatório: sem ele o navegador trata o
  // arquivo como navegação e abre o .docx numa aba, em vez de soltá-lo aqui.
  function aoArrastarSobre(evento: React.DragEvent<HTMLElement>) {
    evento.preventDefault()
    setArrastando(true)
  }

  const moldura = arrastando
    ? 'border-sky-500 bg-sky-50'
    : 'border-slate-300 bg-white hover:border-sky-400 hover:bg-slate-50'

  return (
    <div className="space-y-3">
      <label
        htmlFor="arquivo-do-documento"
        data-testid="zona-de-arraste"
        onDrop={aoSoltar}
        onDragOver={aoArrastarSobre}
        onDragLeave={() => setArrastando(false)}
        className={`flex cursor-pointer flex-col items-center justify-center gap-3 rounded-xl border-2 border-dashed px-6 py-14 text-center transition-colors focus-within:ring-2 focus-within:ring-sky-500 focus-within:ring-offset-2 ${moldura} ${
          mutacao.isPending ? 'pointer-events-none opacity-60' : ''
        }`}
      >
        <svg
          aria-hidden="true"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.5"
          className="h-12 w-12 text-slate-400"
        >
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            d="M12 16.5V6m0 0L8.25 9.75M12 6l3.75 3.75M3 16.5v1.875A2.625 2.625 0 005.625 21h12.75A2.625 2.625 0 0021 18.375V16.5"
          />
        </svg>

        <span className="text-lg font-medium text-slate-800">
          Arraste seu artigo aqui ou clique para selecionar arquivo
        </span>
        <span className="text-sm text-slate-500">Documento do Word (.docx) de até 25 MB</span>

        <input
          id="arquivo-do-documento"
          type="file"
          accept=".docx"
          disabled={mutacao.isPending}
          onChange={aoSelecionarArquivo}
          className="sr-only"
        />
      </label>

      {erroDeExtensao && (
        <p role="alert" className="text-sm font-medium text-red-600">
          {erroDeExtensao}
        </p>
      )}

      {mutacao.isPending && (
        <p className="text-sm text-slate-600">Enviando e gerando a pré-visualização…</p>
      )}

      {mutacao.isError && (
        <p role="alert" className="text-sm font-medium text-red-600">
          {mutacao.error instanceof ErroDaApi && mutacao.error.razoes.length > 0
            ? mutacao.error.razoes.join('; ')
            : mutacao.error.message}
        </p>
      )}

      {mutacao.isSuccess && (
        <p className="text-sm text-emerald-700">{mutacao.data.nome_original}</p>
      )}
    </div>
  )
}
