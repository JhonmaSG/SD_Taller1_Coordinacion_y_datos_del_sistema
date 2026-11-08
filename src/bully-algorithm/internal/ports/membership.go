package ports

import "github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/domain/model"

// MembershipPort proporciona la membresía al dominio sin acoplarla a env vars,
// archivos de configuración o descubrimiento de red.
type MembershipPort interface {
	LocalNode() model.Node
	Peers() []model.Node
}
