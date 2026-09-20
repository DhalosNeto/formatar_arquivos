package rotas

// RoteadorPadrao é a implementação de Roteador usada no projeto.
// A ordem de registro é preservada, para que o conjunto de rotas seja determinístico.
type RoteadorPadrao struct {
	rotas  []Rota
	filhos []filho
}

type filho struct {
	prefixo     string
	roteador    Roteador
	middlewares []Middleware
}

// NovoRoteador cria um roteador vazio.
func NovoRoteador() *RoteadorPadrao { return &RoteadorPadrao{} }

// Adicionar registra uma rota neste roteador.
func (r *RoteadorPadrao) Adicionar(metodo Metodo, caminho string, manipulador Manipulador, middlewares ...Middleware) {
	r.rotas = append(r.rotas, Rota{
		Metodo:      metodo,
		Caminho:     caminho,
		Manipulador: manipulador,
		Middlewares: middlewares,
	})
}

// Registrar monta um roteador filho sob um prefixo, com middlewares próprios.
func (r *RoteadorPadrao) Registrar(roteadorFilho Roteador, prefixo string, middlewares ...Middleware) {
	r.filhos = append(r.filhos, filho{prefixo: prefixo, roteador: roteadorFilho, middlewares: middlewares})
}

// Rotas resolve recursivamente todas as rotas, prefixando caminhos e
// acumulando middlewares do mais externo para o mais interno.
func (r *RoteadorPadrao) Rotas(prefixo string, middlewares ...Middleware) []Rota {
	resolvidas := make([]Rota, 0, len(r.rotas))

	for _, rota := range r.rotas {
		resolvidas = append(resolvidas, Rota{
			Metodo:      rota.Metodo,
			Caminho:     prefixo + rota.Caminho,
			Manipulador: rota.Manipulador,
			Middlewares: concatenar(middlewares, rota.Middlewares),
		})
	}

	for _, f := range r.filhos {
		resolvidas = append(resolvidas, f.roteador.Rotas(prefixo+f.prefixo, concatenar(middlewares, f.middlewares)...)...)
	}

	return resolvidas
}

// concatenar copia as duas listas para evitar que sub-roteadores compartilhem
// o mesmo array de apoio e sobrescrevam os middlewares uns dos outros.
func concatenar(externos, internos []Middleware) []Middleware {
	if len(externos) == 0 && len(internos) == 0 {
		return nil
	}
	juntos := make([]Middleware, 0, len(externos)+len(internos))
	juntos = append(juntos, externos...)
	juntos = append(juntos, internos...)
	return juntos
}
