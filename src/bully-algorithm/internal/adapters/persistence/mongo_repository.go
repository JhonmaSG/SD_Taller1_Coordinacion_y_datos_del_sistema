package persistence

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/domain/model"
	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/ports"
)

const clusterMetadataID = "cluster_metadata"

// MongoNodeDocumentRepository persiste los documentos de nodos y el documento
// global de metadata en la misma colección. Una escritura actualiza ambos
// documentos dentro de una transacción MongoDB.
type MongoNodeDocumentRepository struct {
	client      *mongo.Client
	collection  *mongo.Collection
	coordinator ports.CoordinatorView
}

type nodeDocumentBSON struct {
	ID        string        `bson:"_id"`
	NodeRole  string        `bson:"node_role"`
	Resources resourcesBSON `bson:"resources"`
	Status    string        `bson:"status"`
}

type resourcesBSON struct {
	CPUCores      int64 `bson:"cpu_cores"`
	MemoryGB      int64 `bson:"memory_gb"`
	BandwidthMbps int64 `bson:"bandwidth_mbps"`
}

type clusterMetadataBSON struct {
	ID                 string            `bson:"_id"`
	GlobalLamportClock uint64            `bson:"global_lamport_clock"`
	VersionVector      map[string]uint64 `bson:"version_vector"`
	ActiveCoordinator  string            `bson:"active_coordinator"`
	LastModifiedBy     string            `bson:"last_modified_by"`
}

// NewMongoNodeDocumentRepository crea el repositorio y verifica conectividad.
func NewMongoNodeDocumentRepository(
	ctx context.Context,
	uri string,
	databaseName string,
	collectionName string,
	coordinator ports.CoordinatorView,
) (*MongoNodeDocumentRepository, error) {
	if ctx == nil {
		return nil, errors.New("context must not be nil")
	}
	if uri == "" || databaseName == "" || collectionName == "" {
		return nil, errors.New("MongoDB URI, database and collection must not be empty")
	}
	if coordinator == nil {
		return nil, errors.New("coordinator view must not be nil")
	}

	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return nil, fmt.Errorf("connect to MongoDB: %w", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, fmt.Errorf("ping MongoDB: %w", err)
	}

	return &MongoNodeDocumentRepository{
		client:      client,
		collection:  client.Database(databaseName).Collection(collectionName),
		coordinator: coordinator,
	}, nil
}

func (r *MongoNodeDocumentRepository) Close(ctx context.Context) error {
	if r == nil || r.client == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return r.client.Disconnect(ctx)
}

// Get recupera el documento de nodo y le adjunta en memoria la metadata global.
// La metadata no se lee del documento del nodo porque se persiste aparte.
func (r *MongoNodeDocumentRepository) Get(ctx context.Context, nodeID model.NodeID) (model.NodeDocument, error) {
	if r == nil || r.collection == nil {
		return model.NodeDocument{}, errors.New("MongoDB repository is not initialized")
	}

	var node nodeDocumentBSON
	if err := r.collection.FindOne(ctx, bson.M{"_id": nodeID.Key()}).Decode(&node); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return model.NodeDocument{}, ports.ErrNodeDocumentNotFound
		}
		return model.NodeDocument{}, fmt.Errorf("get node document %s: %w", nodeID.Key(), err)
	}

	result, err := fromNodeBSON(node)
	if err != nil {
		return model.NodeDocument{}, err
	}

	var cluster clusterMetadataBSON
	if err := r.collection.FindOne(ctx, bson.M{"_id": clusterMetadataID}).Decode(&cluster); err == nil {
		result.Metadata = fromClusterBSON(cluster)
	} else if !errors.Is(err, mongo.ErrNoDocuments) {
		return model.NodeDocument{}, fmt.Errorf("get cluster metadata: %w", err)
	}

	return result, nil
}

// UpdateAtomic reemplaza el documento de nodo y el documento cluster_metadata
// en una transacción. La precondición CAS se aplica sobre la metadata global.
func (r *MongoNodeDocumentRepository) UpdateAtomic(
	ctx context.Context,
	nodeID model.NodeID,
	expected model.Metadata,
	next model.NodeDocument,
) error {
	if r == nil || r.collection == nil {
		return errors.New("MongoDB repository is not initialized")
	}
	if r.coordinator == nil || !r.coordinator.IsLocalCoordinator() {
		return ports.ErrNotCoordinator
	}
	if next.ID != nodeID {
		return errors.New("node document id does not match repository key")
	}
	if err := next.Validate(); err != nil {
		return err
	}

	activeCoordinator := nodeID
	if coordinator, ok := r.coordinator.CurrentCoordinator(); ok {
		activeCoordinator = coordinator.ID
	}
	cluster := toClusterBSON(next.Metadata, activeCoordinator)
	filter := clusterFilter(expected)

	session, err := r.client.StartSession()
	if err != nil {
		return fmt.Errorf("start MongoDB session: %w", err)
	}
	defer session.EndSession(ctx)

	_, err = session.WithTransaction(ctx, func(sessionContext context.Context) (interface{}, error) {
		clusterResult, err := r.collection.ReplaceOne(sessionContext, filter, cluster, options.Replace().SetUpsert(true))
		if err != nil {
			if strings.Contains(err.Error(), "E11000") {
				return nil, ports.ErrVersionConflict
			}
			return nil, fmt.Errorf("atomic update cluster metadata: %w", err)
		}
		if clusterResult.MatchedCount == 0 && clusterResult.UpsertedCount == 0 {
			return nil, ports.ErrVersionConflict
		}

		nodeResult, err := r.collection.ReplaceOne(sessionContext, bson.M{"_id": nodeID.Key()}, toNodeBSON(next), options.Replace().SetUpsert(true))
		if err != nil {
			return nil, fmt.Errorf("atomic update node document: %w", err)
		}
		if nodeResult.MatchedCount == 0 && nodeResult.UpsertedCount == 0 {
			return nil, ports.ErrVersionConflict
		}
		return nil, nil
	})
	if err != nil {
		return err
	}

	return nil
}

func clusterFilter(expected model.Metadata) bson.M {
	filter := bson.M{"_id": clusterMetadataID}
	if expected.UpdatedBy == 0 && expected.Lamport == 0 && len(expected.VersionVector) == 0 {
		return filter
	}
	filter["global_lamport_clock"] = uint64(expected.Lamport)
	filter["last_modified_by"] = expected.UpdatedBy.Key()
	for nodeKey, counter := range expected.VersionVector {
		filter["version_vector."+nodeKey] = counter
	}
	return filter
}

func toNodeBSON(document model.NodeDocument) nodeDocumentBSON {
	return nodeDocumentBSON{
		ID: document.ID.Key(), NodeRole: string(document.Role),
		Resources: resourcesBSON{CPUCores: document.Resources.CPUCores, MemoryGB: document.Resources.MemoryGB, BandwidthMbps: document.Resources.BandwidthMbps},
		Status:    string(document.Status),
	}
}

func fromNodeBSON(document nodeDocumentBSON) (model.NodeDocument, error) {
	nodeID, err := parseNodeKey(document.ID)
	if err != nil {
		return model.NodeDocument{}, err
	}
	result := model.NodeDocument{
		ID: nodeID, Role: model.NodeRole(document.NodeRole), Status: model.NodeStatus(document.Status),
		Resources: model.NodeResources{CPUCores: document.Resources.CPUCores, MemoryGB: document.Resources.MemoryGB, BandwidthMbps: document.Resources.BandwidthMbps},
	}
	if err := result.Validate(); err != nil {
		return model.NodeDocument{}, fmt.Errorf("invalid persisted node document: %w", err)
	}
	return result, nil
}

func toClusterBSON(metadata model.Metadata, activeCoordinator model.NodeID) clusterMetadataBSON {
	return clusterMetadataBSON{
		ID: clusterMetadataID, GlobalLamportClock: uint64(metadata.Lamport),
		VersionVector: metadata.VersionVector.Clone(), ActiveCoordinator: activeCoordinator.Key(), LastModifiedBy: metadata.UpdatedBy.Key(),
	}
}

func fromClusterBSON(document clusterMetadataBSON) model.Metadata {
	updatedBy, _ := parseNodeKey(document.LastModifiedBy)
	return model.Metadata{Lamport: model.LamportTime(document.GlobalLamportClock), VersionVector: document.VersionVector, UpdatedBy: updatedBy}
}

func parseNodeKey(value string) (model.NodeID, error) {
	if !strings.HasPrefix(value, "node_") {
		return 0, fmt.Errorf("invalid node key %q", value)
	}
	rawID := strings.TrimPrefix(value, "node_")
	id, err := strconv.ParseUint(rawID, 10, 64)
	if err != nil || id == 0 {
		return 0, fmt.Errorf("invalid node key %q", value)
	}
	return model.NodeID(id), nil
}

var _ ports.NodeDocumentRepository = (*MongoNodeDocumentRepository)(nil)

// GetClusterMetadata lee el documento global de control.
func (r *MongoNodeDocumentRepository) GetClusterMetadata(ctx context.Context) (model.ClusterMetadata, error) {
	if r == nil || r.collection == nil {
		return model.ClusterMetadata{}, errors.New("MongoDB repository is not initialized")
	}
	var document clusterMetadataBSON
	if err := r.collection.FindOne(ctx, bson.M{"_id": clusterMetadataID}).Decode(&document); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return model.ClusterMetadata{}, ports.ErrClusterMetadataNotFound
		}
		return model.ClusterMetadata{}, err
	}
	active, err := parseNodeKey(document.ActiveCoordinator)
	if err != nil {
		return model.ClusterMetadata{}, err
	}
	modified, err := parseNodeKey(document.LastModifiedBy)
	if err != nil {
		return model.ClusterMetadata{}, err
	}
	return model.ClusterMetadata{GlobalLamportClock: model.LamportTime(document.GlobalLamportClock), VersionVector: document.VersionVector, ActiveCoordinator: active, LastModifiedBy: modified}, nil
}

// UpdateClusterMetadataAtomic registra el evento de cambio de coordinador con
// CAS sobre la metadata global.
func (r *MongoNodeDocumentRepository) UpdateClusterMetadataAtomic(ctx context.Context, expected, next model.ClusterMetadata) error {
	if r == nil || r.collection == nil {
		return errors.New("MongoDB repository is not initialized")
	}
	if r.coordinator == nil || !r.coordinator.IsLocalCoordinator() {
		return ports.ErrNotCoordinator
	}
	filter := clusterFilterFromModel(expected)
	result, err := r.collection.ReplaceOne(ctx, filter, toClusterBSONFromModel(next), options.Replace().SetUpsert(true))
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 && result.UpsertedCount == 0 {
		return ports.ErrVersionConflict
	}
	return nil
}

func clusterFilterFromModel(expected model.ClusterMetadata) bson.M {
	filter := bson.M{"_id": clusterMetadataID}
	if expected.LastModifiedBy == 0 && expected.GlobalLamportClock == 0 && len(expected.VersionVector) == 0 {
		return filter
	}
	filter["global_lamport_clock"] = uint64(expected.GlobalLamportClock)
	filter["active_coordinator"] = expected.ActiveCoordinator.Key()
	filter["last_modified_by"] = expected.LastModifiedBy.Key()
	for nodeKey, counter := range expected.VersionVector {
		filter["version_vector."+nodeKey] = counter
	}
	return filter
}

func toClusterBSONFromModel(metadata model.ClusterMetadata) clusterMetadataBSON {
	return clusterMetadataBSON{ID: clusterMetadataID, GlobalLamportClock: uint64(metadata.GlobalLamportClock), VersionVector: metadata.VersionVector.Clone(), ActiveCoordinator: metadata.ActiveCoordinator.Key(), LastModifiedBy: metadata.LastModifiedBy.Key()}
}
