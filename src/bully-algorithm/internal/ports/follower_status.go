package ports

import (
	"context"

	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/domain/model"
)

// FollowerStatusWriter permite vigilar followers sin depender de un caso de
// uso concreto.
type FollowerStatusWriter interface {
	MarkFollowerOffline(ctx context.Context, nodeID model.NodeID) error
}
