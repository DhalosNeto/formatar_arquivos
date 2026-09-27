import { useQuery } from '@tanstack/react-query'

import { obterPreviewDeDocumento } from '../api/documentos'

/** Um minuto de folga antes do prazo, para a nova URL chegar antes da antiga morrer. */
const FOLGA_MS = 60_000

/**
 * Quanto falta para renovar a URL, em milissegundos.
 *
 * Devolve 0 quando não há prazo legível — 0 desliga a renovação automática no
 * TanStack Query, que é o comportamento certo: melhor não renovar do que
 * renovar em laço por causa de uma data que não soubemos ler.
 */
function margemAntesDeExpirar(expiraEm: string | undefined): number {
  if (!expiraEm) {
    return 0
  }
  const prazo = Date.parse(expiraEm)
  if (Number.isNaN(prazo)) {
    return 0
  }
  return Math.max(prazo - Date.now() - FOLGA_MS, 0)
}

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
    // A URL pré-assinada expira (15 min hoje). Buscar de novo ANTES do prazo é
    // o que evita o iframe em branco numa aba deixada aberta: o storage
    // recusaria a URL velha e a consulta não teria como saber, porque para ela
    // a chamada à API foi um sucesso.
    //
    // A margem de segurança vem do próprio `expira_em`, não de uma constante
    // copiada daqui: se o backend mudar a validade, isto acompanha.
    staleTime: (consulta) => margemAntesDeExpirar(consulta.state.data?.expira_em),
    refetchInterval: (consulta) => margemAntesDeExpirar(consulta.state.data?.expira_em),
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
      // O conteúdo vem de um DOCX que o usuário enviou, convertido pelo
      // LibreOffice e servido pelo storage — origem diferente da aplicação.
      // Sem `sandbox`, o documento emoldurado pode navegar a janela de cima
      // (`top.location`), que é vetor de redirecionamento e phishing. Qualquer
      // valor de sandbox já bloqueia isso: allow-top-navigation não entra por
      // padrão.
      //
      // SEM allow-scripts de propósito. Combinar allow-scripts com
      // allow-same-origin deixaria o quadro remover o próprio sandbox, e o
      // visualizador de PDF do navegador é interno — não depende de script da
      // página. allow-same-origin fica porque o visualizador precisa buscar os
      // próprios recursos na origem do arquivo.
      //
      // ⚠️ NÃO verificado em navegador real: jsdom não renderiza PDF. Conferir
      // no Chrome e no Firefox antes de confiar — está registrado nas
      // pendências de docs/estado-do-backend.md.
      sandbox="allow-same-origin"
      // Altura relativa à janela, não fixa em pixel: o leitor de PDF nativo
      // gasta espaço com a própria barra e a faixa de miniaturas, então um
      // quadro baixo demais deixa a página do artigo ilegível.
      className="h-[calc(100vh-13rem)] min-h-[32rem] w-full rounded-lg border border-slate-200 bg-slate-100"
    />
  )
}
