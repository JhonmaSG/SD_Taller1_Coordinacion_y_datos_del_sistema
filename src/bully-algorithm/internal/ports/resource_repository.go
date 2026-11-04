package ports

import (
	"context"

	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/domain/model"
)

// ResourceRepositoryPort define la persistencia que necesitarán los casos de
// uso de inventario. La implementación MongoDB se agregará en adapters y no
// se filtra hacia el dominio.
type ResourceRepositoryPort interface {
	FindByID(ctx context.Context, resourceID string) (model.Resource, error)
	Save(ctx context.Context, resource model.Resource) error
}
