package persistence

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/domain/model"
	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/ports"
)

const clusterMetadataID = "cluster_metadata"

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

func NewMongoNodeDocumentRepository(ctx context.Context, uri, databaseName, collectionName string, coordinator ports.CoordinatorView) (*MongoNodeDocumentRepository, error) {
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
	return &MongoNodeDocumentRepository{client: client, collection: client.Database(databaseName).Collection(collectionName), coordinator: coordinator}, nil
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

func (r *MongoNodeDocumentRepository) Get(ctx context.Context, nodeID model.NodeID) (model.NodeDocument, error) {
	if r == nil || r.collection == nil {
		return model.NodeDocument{}, errors.New("MongoDB repository is not initialized")
	}
	var raw nodeDocumentBSON
	if err := r.collection.FindOne(ctx, bson.M{"_id": nodeID.Key()}).Decode(&raw); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return model.NodeDocument{}, ports.ErrNodeDocumentNotFound
		}
		return model.NodeDocument{}, fmt.Errorf("get node document %s: %w", nodeID.Key(), err)
	}
	return fromNodeBSON(raw)
}

func (r *MongoNodeDocumentRepository) GetClusterMetadata(ctx context.Context) (model.ClusterMetadata, error) {
	if r == nil || r.collection == nil {
		return model.ClusterMetadata{}, errors.New("MongoDB repository is not initialized")
	}
	var raw clusterMetadataBSON
	if err := r.collection.FindOne(ctx, bson.M{"_id": clusterMetadataID}).Decode(&raw); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return model.ClusterMetadata{}, ports.ErrClusterMetadataNotFound
		}
		return model.ClusterMetadata{}, err
	}
	active, err := parseNodeKey(raw.ActiveCoordinator)
	if err != nil {
		return model.ClusterMetadata{}, err
	}
	modified, err := parseNodeKey(raw.LastModifiedBy)
	if err != nil {
		return model.ClusterMetadata{}, err
	}
	return model.ClusterMetadata{GlobalLamportClock: model.LamportTime(raw.GlobalLamportClock), VersionVector: raw.VersionVector, ActiveCoordinator: active, LastModifiedBy: modified}, nil
}

func (r *MongoNodeDocumentRepository) UpdateClusterMetadataAtomic(ctx context.Context, expected, next model.ClusterMetadata) error {
	if r == nil || r.collection == nil {
		return errors.New("MongoDB repository is not initialized")
	}
	if r.coordinator == nil || !r.coordinator.IsLocalCoordinator() {
		return ports.ErrNotCoordinator
	}
	log.Printf("MongoDB write planned operation=cluster_metadata_update lamport=%d vector=%s active_coordinator=%s last_modified_by=%s", next.GlobalLamportClock, formatVersionVector(next.VersionVector), next.ActiveCoordinator.Key(), next.LastModifiedBy.Key())
	result, err := r.collection.ReplaceOne(ctx, clusterFilter(expected), toClusterBSON(next), options.Replace().SetUpsert(true))
	if err != nil {
		return mapMongoWriteError(err)
	}
	if result.MatchedCount == 0 && result.UpsertedCount == 0 {
		log.Printf("MongoDB write conflict operation=cluster_metadata_update lamport=%d vector=%s", next.GlobalLamportClock, formatVersionVector(next.VersionVector))
		return ports.ErrVersionConflict
	}
	log.Printf("MongoDB write committed operation=cluster_metadata_update lamport=%d vector=%s active_coordinator=%s", next.GlobalLamportClock, formatVersionVector(next.VersionVector), next.ActiveCoordinator.Key())
	return nil
}

// UpdateAtomic actualiza cluster_metadata y el documento de nodo dentro de una
// única transacción. El CAS se aplica sobre la metadata global esperada.
func (r *MongoNodeDocumentRepository) UpdateAtomic(ctx context.Context, nodeID model.NodeID, expected model.ClusterMetadata, nextCluster model.ClusterMetadata, nextNode model.NodeDocument) error {
	if r == nil || r.collection == nil {
		return errors.New("MongoDB repository is not initialized")
	}
	if r.coordinator == nil || !r.coordinator.IsLocalCoordinator() {
		return ports.ErrNotCoordinator
	}
	if nextNode.ID != nodeID {
		return errors.New("node document id does not match repository key")
	}
	if err := nextNode.Validate(); err != nil {
		return err
	}
	log.Printf("MongoDB write planned operation=node_and_cluster_update node=%s role=%s status=%s lamport=%d vector=%s active_coordinator=%s", nodeID.Key(), nextNode.Role, nextNode.Status, nextCluster.GlobalLamportClock, formatVersionVector(nextCluster.VersionVector), nextCluster.ActiveCoordinator.Key())

	session, err := r.client.StartSession()
	if err != nil {
		return err
	}
	defer session.EndSession(ctx)
	_, err = session.WithTransaction(ctx, func(sc context.Context) (interface{}, error) {
		clusterResult, err := r.collection.ReplaceOne(sc, clusterFilter(expected), toClusterBSON(nextCluster), options.Replace().SetUpsert(true))
		if err != nil {
			return nil, mapMongoWriteError(err)
		}
		if clusterResult.MatchedCount == 0 && clusterResult.UpsertedCount == 0 {
			return nil, ports.ErrVersionConflict
		}
		nodeResult, err := r.collection.ReplaceOne(sc, bson.M{"_id": nodeID.Key()}, toNodeBSON(nextNode), options.Replace().SetUpsert(true))
		if err != nil {
			return nil, err
		}
		if nodeResult.MatchedCount == 0 && nodeResult.UpsertedCount == 0 {
			return nil, ports.ErrVersionConflict
		}
		return nil, nil
	})
	if err != nil {
		log.Printf("MongoDB write failed operation=node_and_cluster_update node=%s lamport=%d vector=%s error=%v", nodeID.Key(), nextCluster.GlobalLamportClock, formatVersionVector(nextCluster.VersionVector), err)
		return err
	}
	log.Printf("MongoDB write committed operation=node_and_cluster_update node=%s role=%s status=%s lamport=%d vector=%s active_coordinator=%s", nodeID.Key(), nextNode.Role, nextNode.Status, nextCluster.GlobalLamportClock, formatVersionVector(nextCluster.VersionVector), nextCluster.ActiveCoordinator.Key())
	return nil
}

func clusterFilter(expected model.ClusterMetadata) bson.M {
	filter := bson.M{"_id": clusterMetadataID}
	if expected.LastModifiedBy == 0 && expected.GlobalLamportClock == 0 && len(expected.VersionVector) == 0 {
		return filter
	}
	filter["global_lamport_clock"] = uint64(expected.GlobalLamportClock)
	filter["active_coordinator"] = expected.ActiveCoordinator.Key()
	filter["last_modified_by"] = expected.LastModifiedBy.Key()
	for key, counter := range expected.VersionVector {
		filter["version_vector."+key] = counter
	}
	return filter
}
func toNodeBSON(node model.NodeDocument) nodeDocumentBSON {
	return nodeDocumentBSON{ID: node.ID.Key(), NodeRole: string(node.Role), Resources: resourcesBSON{node.Resources.CPUCores, node.Resources.MemoryGB, node.Resources.BandwidthMbps}, Status: string(node.Status)}
}

func fromNodeBSON(raw nodeDocumentBSON) (model.NodeDocument, error) {
	id, err := parseNodeKey(raw.ID)
	if err != nil {
		return model.NodeDocument{}, err
	}
	node := model.NodeDocument{ID: id, Role: model.NodeRole(raw.NodeRole), Resources: model.NodeResources{CPUCores: raw.Resources.CPUCores, MemoryGB: raw.Resources.MemoryGB, BandwidthMbps: raw.Resources.BandwidthMbps}, Status: model.NodeStatus(raw.Status)}
	if err := node.Validate(); err != nil {
		return model.NodeDocument{}, err
	}
	return node, nil
}

func toClusterBSON(metadata model.ClusterMetadata) clusterMetadataBSON {
	return clusterMetadataBSON{ID: clusterMetadataID, GlobalLamportClock: uint64(metadata.GlobalLamportClock), VersionVector: metadata.VersionVector.Clone(), ActiveCoordinator: metadata.ActiveCoordinator.Key(), LastModifiedBy: metadata.LastModifiedBy.Key()}
}

func parseNodeKey(value string) (model.NodeID, error) {
	if !strings.HasPrefix(value, "node_") {
		return 0, fmt.Errorf("invalid node key %q", value)
	}
	id, err := strconv.ParseUint(strings.TrimPrefix(value, "node_"), 10, 64)
	if err != nil || id == 0 {
		return 0, fmt.Errorf("invalid node key %q", value)
	}
	return model.NodeID(id), nil
}

func mapMongoWriteError(err error) error {
	if strings.Contains(err.Error(), "E11000") {
		return ports.ErrVersionConflict
	}
	return err
}

var _ ports.NodeDocumentRepository = (*MongoNodeDocumentRepository)(nil)

func formatVersionVector(vector model.VersionVector) string {
	keys := make([]string, 0, len(vector))
	for key := range vector {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s:%d", key, vector[key]))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}
