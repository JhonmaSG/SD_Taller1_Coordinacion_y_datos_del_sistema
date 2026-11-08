package usecase

import (
	"context"
	"testing"

	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/domain/model"
	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/ports"
)

type metadataMembershipFake struct {
	local model.Node
	peers []model.Node
}

func (m metadataMembershipFake) LocalNode() model.Node { return m.local }
func (m metadataMembershipFake) Peers() []model.Node   { return m.peers }

type metadataCoordinatorFake struct {
	local       bool
	coordinator model.Node
}

func (c metadataCoordinatorFake) IsLocalCoordinator() bool { return c.local }
func (c metadataCoordinatorFake) CurrentCoordinator() (model.Node, bool) {
	return c.coordinator, c.coordinator.ID != 0
}

type metadataRepositoryFake struct {
	metadata model.ClusterMetadata
	exists   bool
	updates  int
}

func (r *metadataRepositoryFake) Get(context.Context, model.NodeID) (model.NodeDocument, error) {
	return model.NodeDocument{}, ports.ErrNodeDocumentNotFound
}
func (r *metadataRepositoryFake) GetClusterMetadata(context.Context) (model.ClusterMetadata, error) {
	if !r.exists {
		return model.ClusterMetadata{}, ports.ErrClusterMetadataNotFound
	}
	return r.metadata, nil
}
func (r *metadataRepositoryFake) UpdateClusterMetadataAtomic(_ context.Context, _, next model.ClusterMetadata) error {
	r.metadata = next
	r.exists = true
	r.updates++
	return nil
}
func (r *metadataRepositoryFake) UpdateAtomic(context.Context, model.NodeID, model.ClusterMetadata, model.ClusterMetadata, model.NodeDocument) error {
	return nil
}

func TestClusterMetadataUseCaseAdvancesOnlyWhenLeadershipChanges(t *testing.T) {
	repo := &metadataRepositoryFake{}
	membership := metadataMembershipFake{local: model.Node{ID: 1}, peers: []model.Node{{ID: 2}, {ID: 3}}}
	view := metadataCoordinatorFake{local: true, coordinator: model.Node{ID: 1}}
	uc, err := NewClusterMetadataUseCase(membership, view, repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := uc.SyncOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := uc.SyncOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if repo.updates != 1 {
		t.Fatalf("updates = %d, want 1", repo.updates)
	}
	want := model.VersionVector{"node_1": 1, "node_2": 0, "node_3": 0}
	if repo.metadata.VersionVector.Compare(want) != model.VersionEqual {
		t.Fatalf("vector = %#v, want %#v", repo.metadata.VersionVector, want)
	}
}
