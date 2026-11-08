package mongodb

import (
	"context"
	"errors"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/domain/model"
	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/ports"
)

// ResourceRepository implementa la persistencia de recursos sobre MongoDB.
// La conexión puede apuntar a un Replica Set mediante la URI, por ejemplo
// mongodb://host/?replicaSet=rs-grupo.
type ResourceRepository struct {
	client      *mongo.Client
	collection  *mongo.Collection
	coordinator ports.CoordinatorView
}

type resourceDocument struct {
	ID       string           `bson:"_id"`
	Name     string           `bson:"name"`
	Quantity int64            `bson:"quantity"`
	Metadata metadataDocument `bson:"metadata"`
}

type metadataDocument struct {
	Lamport       uint64            `bson:"lamport"`
	VersionVector map[string]uint64 `bson:"version_vector"`
	UpdatedBy     uint64            `bson:"updated_by"`
}

// NewResourceRepository crea el cliente, verifica conectividad con MongoDB y
// devuelve el repositorio listo para ser inyectado en futuros casos de uso.
func NewResourceRepository(
	ctx context.Context,
	uri string,
	databaseName string,
	collectionName string,
	coordinator ports.CoordinatorView,
) (*ResourceRepository, error) {
	if ctx == nil {
		return nil, errors.New("context must not be nil")
	}
	if uri == "" {
		return nil, errors.New("MongoDB URI must not be empty")
	}
	if databaseName == "" || collectionName == "" {
		return nil, errors.New("MongoDB database and collection must not be empty")
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

	return &ResourceRepository{
		client:      client,
		collection:  client.Database(databaseName).Collection(collectionName),
		coordinator: coordinator,
	}, nil
}

// Close libera el pool de conexiones de MongoDB.
func (r *ResourceRepository) Close(ctx context.Context) error {
	if r == nil || r.client == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	return r.client.Disconnect(ctx)
}

// FindByID obtiene una versión persistida del recurso.
func (r *ResourceRepository) FindByID(ctx context.Context, resourceID string) (model.Resource, error) {
	if r == nil || r.collection == nil {
		return model.Resource{}, errors.New("MongoDB repository is not initialized")
	}
	if resourceID == "" {
		return model.Resource{}, errors.New("resource id must not be empty")
	}

	var document resourceDocument
	if err := r.collection.FindOne(ctx, bson.M{"_id": resourceID}).Decode(&document); err != nil {
		return model.Resource{}, err
	}

	return fromDocument(document)
}

// Save inserta o reemplaza la versión completa del recurso usando su ID como
// clave estable.
func (r *ResourceRepository) Save(ctx context.Context, resource model.Resource) error {
	if r == nil || r.collection == nil {
		return errors.New("MongoDB repository is not initialized")
	}
	if r.coordinator == nil || !r.coordinator.IsLocalCoordinator() {
		return ports.ErrNotCoordinator
	}
	if err := resource.Validate(); err != nil {
		return err
	}

	_, err := r.collection.ReplaceOne(
		ctx,
		bson.M{"_id": resource.ID},
		toDocument(resource),
		options.Replace().SetUpsert(true),
	)
	if err != nil {
		return fmt.Errorf("save resource %s: %w", resource.ID, err)
	}

	return nil
}

func toDocument(resource model.Resource) resourceDocument {
	versions := make(map[string]uint64, len(resource.Metadata.VersionVector))
	for nodeKey, counter := range resource.Metadata.VersionVector {
		versions[nodeKey] = counter
	}

	return resourceDocument{
		ID:       resource.ID,
		Name:     resource.Name,
		Quantity: resource.Quantity,
		Metadata: metadataDocument{
			Lamport:       uint64(resource.Metadata.Lamport),
			VersionVector: versions,
			UpdatedBy:     uint64(resource.Metadata.UpdatedBy),
		},
	}
}

func fromDocument(document resourceDocument) (model.Resource, error) {
	versions := make(model.VersionVector, len(document.Metadata.VersionVector))
	for nodeKey, counter := range document.Metadata.VersionVector {
		versions[nodeKey] = counter
	}

	resource := model.Resource{
		ID:       document.ID,
		Name:     document.Name,
		Quantity: document.Quantity,
		Metadata: model.Metadata{
			Lamport:       model.LamportTime(document.Metadata.Lamport),
			VersionVector: versions,
			UpdatedBy:     model.NodeID(document.Metadata.UpdatedBy),
		},
	}
	if err := resource.Validate(); err != nil {
		return model.Resource{}, fmt.Errorf("invalid persisted resource: %w", err)
	}

	return resource, nil
}
