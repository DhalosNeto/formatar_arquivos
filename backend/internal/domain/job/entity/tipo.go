package entity

type TipoJob string

const (
	TipoRenderizarPreview TipoJob = "renderizar_preview"
	TipoAnalisar          TipoJob = "analisar"
	TipoFormatar          TipoJob = "formatar"
)

func (tipo TipoJob) Valido() bool {
	switch tipo {
	case TipoRenderizarPreview, TipoAnalisar, TipoFormatar:
		return true
	default:
		return false
	}
}

func (tipo TipoJob) ExigeRuleset() bool { return tipo == TipoFormatar }

func (tipo TipoJob) String() string { return string(tipo) }
