package ports

import "github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/domain/model"

// CoordinatorView expone solo la información que los casos de uso de datos
// necesitan para autorizar escrituras, sin acoplarse al caso de uso Bully.
type CoordinatorView interface {
	CurrentCoordinator() (model.Node, bool)
	IsLocalCoordinator() bool
}
