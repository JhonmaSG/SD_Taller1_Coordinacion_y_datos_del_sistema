package ports

import (
	"context"

	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/domain/model"
)

// ElectionTransportPort abstrae los mensajes que el caso de uso necesita.
// Un adaptador gRPC implementará este contrato sin filtrarlo al dominio.
type ElectionTransportPort interface {
	// RequestElection envía una solicitud ELECTION y devuelve true si el nodo
	// destino confirmó que está vivo y tiene mayor prioridad.
	RequestElection(ctx context.Context, from, to model.Node) (acknowledged bool, err error)

	// AnnounceCoordinator propaga un mensaje COORDINATOR a un nodo conocido.
	AnnounceCoordinator(ctx context.Context, from, to, coordinator model.Node) error
}
