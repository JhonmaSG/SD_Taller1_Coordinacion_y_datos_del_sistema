package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/domain/model"
	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/ports"
)

type resourceCoordinatorFake struct{ local bool }

func (c resourceCoordinatorFake) IsLocalCoordinator() bool { return c.local }
func (c resourceCoordinatorFake) CurrentCoordinator() (model.Node, bool) {
	return model.Node{ID: 1}, c.local
}

type resourceMembershipFake struct{}

func (resourceMembershipFake) LocalNode() model.Node { return model.Node{ID: 1} }
func (resourceMembershipFake) Peers() []model.Node   { return []model.Node{{ID: 2}, {ID: 3}} }

type resourceRepositoryFake struct {
	document model.NodeDocument
	cluster  model.ClusterMetadata
	conflict bool
}

func (r *resourceRepositoryFake) Get(context.Context, model.NodeID) (model.NodeDocument, error) {
	return r.document, nil
}
func (r *resourceRepositoryFake) GetClusterMetadata(context.Context) (model.ClusterMetadata, error) {
	return r.cluster, nil
}
func (r *resourceRepositoryFake) UpdateClusterMetadataAtomic(_ context.Context, _, next model.ClusterMetadata) error {
	r.cluster = next
	return nil
}
func (r *resourceRepositoryFake) UpdateAtomic(_ context.Context, _ model.NodeID, _, nextCluster model.ClusterMetadata, next model.NodeDocument) error {
	if r.conflict {
		r.conflict = false
		return ports.ErrVersionConflict
	}
	r.cluster = nextCluster
	r.document = next
	return nil
}

func TestUpdateResourcesRejectsFollower(t *testing.T) {
	uc, err := NewUpdateNodeResourcesUseCase(resourceMembershipFake{}, resourceCoordinatorFake{local: false}, &resourceRepositoryFake{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = uc.Update(context.Background(), UpdateNodeResourcesCommand{Resources: model.NodeResources{CPUCores: 1}})
	if !errors.Is(err, ports.ErrNotCoordinator) {
		t.Fatalf("error = %v, want ErrNotCoordinator", err)
	}
}

func TestUpdateResourcesCoordinatorAdvancesGlobalMetadataAndRetriesCAS(t *testing.T) {
	repo := &resourceRepositoryFake{document: model.NewNodeDocument(1), cluster: model.ClusterMetadata{VersionVector: model.NewVersionVector([]model.NodeID{1, 2, 3}), ActiveCoordinator: 1, LastModifiedBy: 1}}
	uc, err := NewUpdateNodeResourcesUseCase(resourceMembershipFake{}, resourceCoordinatorFake{local: true}, repo)
	if err != nil {
		t.Fatal(err)
	}
	repo.conflict = true
	updated, err := uc.Update(context.Background(), UpdateNodeResourcesCommand{Resources: model.NodeResources{CPUCores: 4, MemoryGB: 8, BandwidthMbps: 1000}})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Resources.CPUCores != 4 {
		t.Fatalf("resources = %#v", updated.Resources)
	}
	if repo.cluster.VersionVector["node_1"] != 1 {
		t.Fatalf("cluster vector = %#v", repo.cluster.VersionVector)
	}
}
