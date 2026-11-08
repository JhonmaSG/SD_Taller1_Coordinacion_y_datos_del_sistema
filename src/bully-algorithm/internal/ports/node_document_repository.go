package ports

import (
	"context"
	"errors"

	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/domain/model"
)

var (
	ErrNodeDocumentNotFound    = errors.New("node document not found")
	ErrClusterMetadataNotFound = errors.New("cluster metadata not found")
	ErrVersionConflict         = errors.New("node document version conflict")
	ErrNotCoordinator          = errors.New("only the active coordinator can write")
)

// NodeDocumentRepository contiene la persistencia del agregado de nodos y del
// documento global de concurrencia.
type NodeDocumentRepository interface {
	Get(ctx context.Context, nodeID model.NodeID) (model.NodeDocument, error)
	GetClusterMetadata(ctx context.Context) (model.ClusterMetadata, error)
	UpdateClusterMetadataAtomic(ctx context.Context, expected, next model.ClusterMetadata) error
	UpdateAtomic(ctx context.Context, nodeID model.NodeID, expected model.ClusterMetadata, nextCluster model.ClusterMetadata, nextNode model.NodeDocument) error
}
