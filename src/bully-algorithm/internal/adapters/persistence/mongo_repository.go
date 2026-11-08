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

// MongoNodeDocumentRepository persiste un documento lógico por nodo. MongoDB
// replica ese documento entre los miembros del Replica Set; la aplicación no
// escribe copias manualmente en cada servidor.
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
	Metadata  metadataBSON  `bson:"metadata"`
}

type resourcesBSON struct {
	CPUCores      int64 `bson:"cpu_cores"`
	MemoryGB      int64 `bson:"memory_gb"`
	BandwidthMbps int64 `bson:"bandwidth_mbps"`
}

type metadataBSON struct {
	LamportClock   uint64            `bson:"lamport_clock"`
	VersionVector  map[string]uint64 `bson:"version_vector"`
	LastModifiedBy string            `bson:"last_modified_by"`
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

// Close cierra el pool del driver.
func (r *MongoNodeDocumentRepository) Close(ctx context.Context) error {
	if r == nil || r.client == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	return r.client.Disconnect(ctx)
}

// Get recupera el documento del nodo identificado por su NodeID.
func (r *MongoNodeDocumentRepository) Get(ctx context.Context, nodeID model.NodeID) (model.NodeDocument, error) {
	if r == nil || r.collection == nil {
		return model.NodeDocument{}, errors.New("MongoDB repository is not initialized")
	}

	var document nodeDocumentBSON
	if err := r.collection.FindOne(ctx, bson.M{"_id": nodeID.Key()}).Decode(&document); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return model.NodeDocument{}, ports.ErrNodeDocumentNotFound
		}
		return model.NodeDocument{}, fmt.Errorf("get node document %s: %w", nodeID.Key(), err)
	}

	return fromBSON(document)
}

// UpdateAtomic reemplaza el documento solo si su metadata todavía coincide
// con expected. El filtro compara Lamport, last_modified_by y cada contador del
// vector, para evitar lost updates sin depender del orden de las claves.
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

	filter := bson.M{"_id": nodeID.Key()}
	if expected.UpdatedBy != 0 || expected.Lamport != 0 || len(expected.VersionVector) > 0 {
		filter["metadata.lamport_clock"] = uint64(expected.Lamport)
		filter["metadata.last_modified_by"] = expected.UpdatedBy.Key()
		// Cada contador se compara por separado. Comparar el mapa completo como
		// documento embebido depende del orden de sus claves, y el orden de un
		// mapa de Go no es estable: el CAS fallaba aunque nadie hubiera escrito.
		for nodeKey, counter := range expected.VersionVector {
			filter["metadata.version_vector."+nodeKey] = counter
		}
	}

	result, err := r.collection.ReplaceOne(ctx, filter, toBSON(next), options.Replace().SetUpsert(true))
	if err != nil {
		if strings.Contains(err.Error(), "E11000") {
			return ports.ErrVersionConflict
		}
		return fmt.Errorf("atomic update node document %s: %w", nodeID.Key(), err)
	}
	if result.MatchedCount == 0 && result.UpsertedCount == 0 {
		return ports.ErrVersionConflict
	}

	return nil
}

func toBSON(document model.NodeDocument) nodeDocumentBSON {
	return nodeDocumentBSON{
		ID:        document.ID.Key(),
		NodeRole:  string(document.Role),
		Resources: resourcesBSON{CPUCores: document.Resources.CPUCores, MemoryGB: document.Resources.MemoryGB, BandwidthMbps: document.Resources.BandwidthMbps},
		Status:    string(document.Status),
		Metadata: metadataBSON{
			LamportClock:   uint64(document.Metadata.Lamport),
			VersionVector:  versionVectorToBSON(document.Metadata.VersionVector),
			LastModifiedBy: document.Metadata.UpdatedBy.Key(),
		},
	}
}

func fromBSON(document nodeDocumentBSON) (model.NodeDocument, error) {
	nodeID, err := parseNodeKey(document.ID)
	if err != nil {
		return model.NodeDocument{}, err
	}
	updatedBy, err := parseNodeKey(document.Metadata.LastModifiedBy)
	if err != nil {
		return model.NodeDocument{}, fmt.Errorf("invalid last_modified_by: %w", err)
	}

	result := model.NodeDocument{
		ID:        nodeID,
		Role:      model.NodeRole(document.NodeRole),
		Resources: model.NodeResources{CPUCores: document.Resources.CPUCores, MemoryGB: document.Resources.MemoryGB, BandwidthMbps: document.Resources.BandwidthMbps},
		Status:    model.NodeStatus(document.Status),
		Metadata:  model.Metadata{Lamport: model.LamportTime(document.Metadata.LamportClock), VersionVector: document.Metadata.VersionVector, UpdatedBy: updatedBy},
	}
	if err := result.Validate(); err != nil {
		return model.NodeDocument{}, fmt.Errorf("invalid persisted node document: %w", err)
	}

	return result, nil
}

func versionVectorToBSON(vector model.VersionVector) map[string]uint64 {
	result := make(map[string]uint64, len(vector))
	for nodeKey, counter := range vector {
		result[nodeKey] = counter
	}

	return result
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
