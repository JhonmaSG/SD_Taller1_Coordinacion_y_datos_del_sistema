package ports

import (
	"context"

	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/domain/model"
)

// MachineCapacityReader obtiene la capacidad física del host sin introducir
// APIs de sistema operativo en el dominio.
type MachineCapacityReader interface {
	Read(ctx context.Context) (model.NodeResources, error)
}
