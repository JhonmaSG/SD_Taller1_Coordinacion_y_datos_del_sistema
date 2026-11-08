package ports

import (
	"context"

	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/domain/model"
)

// NodeRegistrationTransport permite que un agent solicite al coordinador
// registrar su capacidad sin escribir MongoDB directamente.
type NodeRegistrationTransport interface {
	RegisterNode(ctx context.Context, sender model.Node, coordinator model.Node, resources model.NodeResources) (model.NodeDocument, error)
}
