package entity

// TipoJob é a espécie de trabalho que um job representa.
type TipoJob string

const (
	TipoRenderizarPreview TipoJob = "renderizar_preview"
	TipoAnalisar          TipoJob = "analisar"
	TipoFormatar          TipoJob = "formatar"
)

// Valido informa se o tipo é um dos suportados.
func (tipo TipoJob) Valido() bool {
	switch tipo {
	case TipoRenderizarPreview, TipoAnalisar, TipoFormatar:
		return true
	default:
		return false
	}
}

// ExigeRuleset informa se o tipo precisa de um perfil de formatação.
// Só formatar exige: análise e preview não escolhem norma.
func (tipo TipoJob) ExigeRuleset() bool { return tipo == TipoFormatar }

func (tipo TipoJob) String() string { return string(tipo) }
