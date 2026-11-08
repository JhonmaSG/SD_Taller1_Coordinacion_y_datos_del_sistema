package ports

import (
	"context"
	"errors"

	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/domain/model"
)

var (
	ErrNodeDocumentNotFound = errors.New("node document not found")
	ErrVersionConflict      = errors.New("node document version conflict")
	ErrNotCoordinator       = errors.New("only the active coordinator can write")
)

// NodeDocumentRepository abstrae lectura y actualización CAS de un documento
// por nodo. La implementación concreta vive fuera del dominio.
type NodeDocumentRepository interface {
	Get(ctx context.Context, nodeID model.NodeID) (model.NodeDocument, error)
	UpdateAtomic(
		ctx context.Context,
		nodeID model.NodeID,
		expected model.Metadata,
		next model.NodeDocument,
	) error
}
