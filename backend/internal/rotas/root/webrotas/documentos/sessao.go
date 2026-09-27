package documentos

// recursoDocumento é o nome de recurso usado nas respostas de ausência.
//
// É "documento" e não "sessão" de propósito: o objetivo é que a resposta de
// cookie ausente seja indistinguível da de um documento inexistente. Dizer
// "sessão" entregaria ao atacante que a falha foi de autenticação e não de
// existência — além de concordar errado em português.
//
// A leitura e a gravação do cookie em si vivem em internal/rotas/sessao, que é
// o único lugar que conhece o nome dele.
const recursoDocumento = "documento"
