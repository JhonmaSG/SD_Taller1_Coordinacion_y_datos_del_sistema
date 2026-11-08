package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/domain/model"
	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/ports"
)

type registerMembershipFake struct {
	local model.Node
	peers []model.Node
}

func (m registerMembershipFake) LocalNode() model.Node { return m.local }
func (m registerMembershipFake) Peers() []model.Node   { return m.peers }

type registerCoordinatorFake struct {
	local       bool
	coordinator model.Node
}

func (c registerCoordinatorFake) IsLocalCoordinator() bool { return c.local }
func (c registerCoordinatorFake) CurrentCoordinator() (model.Node, bool) {
	return c.coordinator, c.coordinator.ID != 0
}

type registerCapacityFake struct {
	calls     int
	resources model.NodeResources
}

func (c *registerCapacityFake) Read(context.Context) (model.NodeResources, error) {
	c.calls++
	return c.resources, nil
}

type registerTransportFake struct {
	calls    int
	document model.NodeDocument
}

func (t *registerTransportFake) RegisterNode(context.Context, model.Node, model.Node, model.NodeResources) (model.NodeDocument, error) {
	t.calls++
	return t.document, nil
}

type registerRepositoryFake struct {
	document       model.NodeDocument
	cluster        model.ClusterMetadata
	documentExists bool
	clusterExists  bool
	updates        int
}

func (r *registerRepositoryFake) Get(context.Context, model.NodeID) (model.NodeDocument, error) {
	if !r.documentExists {
		return model.NodeDocument{}, ports.ErrNodeDocumentNotFound
	}
	return r.document, nil
}
func (r *registerRepositoryFake) GetClusterMetadata(context.Context) (model.ClusterMetadata, error) {
	if !r.clusterExists {
		return model.ClusterMetadata{}, ports.ErrClusterMetadataNotFound
	}
	return r.cluster, nil
}
func (r *registerRepositoryFake) UpdateClusterMetadataAtomic(_ context.Context, _, next model.ClusterMetadata) error {
	r.cluster = next
	r.clusterExists = true
	return nil
}
func (r *registerRepositoryFake) UpdateAtomic(_ context.Context, _ model.NodeID, _, nextCluster model.ClusterMetadata, next model.NodeDocument) error {
	r.cluster = nextCluster
	r.clusterExists = true
	r.document = next
	r.documentExists = true
	r.updates++
	return nil
}

func TestRegisterNodeFollowerUsesCoordinatorTransport(t *testing.T) {
	capacity := &registerCapacityFake{resources: model.NodeResources{CPUCores: 2}}
	transport := &registerTransportFake{document: model.NodeDocument{ID: 1, Role: model.RoleAgent, Status: model.StatusOnline}}
	repo := &registerRepositoryFake{}
	uc, err := NewRegisterNodeUseCase(registerMembershipFake{local: model.Node{ID: 1}, peers: []model.Node{{ID: 2}}}, registerCoordinatorFake{local: false, coordinator: model.Node{ID: 2}}, repo, capacity, transport)
	if err != nil {
		t.Fatal(err)
	}
	_, err = uc.Register(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if transport.calls != 1 || repo.updates != 0 {
		t.Fatalf("transport=%d repository=%d", transport.calls, repo.updates)
	}
}

func TestRegisterNodeCoordinatorPersistsCapacityAndCachesMachineRead(t *testing.T) {
	capacity := &registerCapacityFake{resources: model.NodeResources{CPUCores: 4, MemoryGB: 8}}
	repo := &registerRepositoryFake{cluster: model.ClusterMetadata{GlobalLamportClock: 1, VersionVector: model.NewVersionVector([]model.NodeID{1}), ActiveCoordinator: 1, LastModifiedBy: 1}, clusterExists: true}
	uc, err := NewRegisterNodeUseCase(registerMembershipFake{local: model.Node{ID: 1}}, registerCoordinatorFake{local: true, coordinator: model.Node{ID: 1}}, repo, capacity, &registerTransportFake{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uc.Register(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := uc.Register(context.Background()); err != nil {
		t.Fatal(err)
	}
	if capacity.calls != 1 {
		t.Fatalf("capacity reads=%d, want 1", capacity.calls)
	}
	if repo.document.Role != model.RoleCoordinator || repo.document.Resources.CPUCores != 4 {
		t.Fatalf("document=%#v", repo.document)
	}
}

func TestRegisterNodeMarksOfflineOnlyOnce(t *testing.T) {
	repo := &registerRepositoryFake{document: model.NodeDocument{ID: 2, Role: model.RoleAgent, Status: model.StatusOnline}, documentExists: true, cluster: model.ClusterMetadata{GlobalLamportClock: 1, VersionVector: model.NewVersionVector([]model.NodeID{1, 2}), ActiveCoordinator: 1, LastModifiedBy: 1}, clusterExists: true}
	uc, err := NewRegisterNodeUseCase(registerMembershipFake{local: model.Node{ID: 1}, peers: []model.Node{{ID: 2}}}, registerCoordinatorFake{local: true, coordinator: model.Node{ID: 1}}, repo, &registerCapacityFake{}, &registerTransportFake{})
	if err != nil {
		t.Fatal(err)
	}
	if err := uc.MarkFollowerOffline(context.Background(), 2); err != nil {
		t.Fatal(err)
	}
	if err := uc.MarkFollowerOffline(context.Background(), 2); err != nil {
		t.Fatal(err)
	}
	if repo.updates != 1 {
		t.Fatalf("updates=%d, want 1", repo.updates)
	}
	if repo.document.Status != model.StatusOffline {
		t.Fatalf("status=%s", repo.document.Status)
	}
}

var _ ports.NodeRegistrationTransport = (*registerTransportFake)(nil)
var _ = time.Second
