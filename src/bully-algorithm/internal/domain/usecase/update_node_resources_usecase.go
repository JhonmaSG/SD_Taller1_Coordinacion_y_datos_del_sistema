package usecase

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/domain/model"
	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/ports"
)

var ErrNotCoordinator = ports.ErrNotCoordinator

type UpdateNodeResourcesCommand struct{ Resources model.NodeResources }

type UpdateNodeResourcesUseCase struct {
	membership  ports.MembershipPort
	coordinator ports.CoordinatorView
	repository  ports.NodeDocumentRepository
	mu          sync.Mutex
}

func NewUpdateNodeResourcesUseCase(membership ports.MembershipPort, coordinator ports.CoordinatorView, repository ports.NodeDocumentRepository) (*UpdateNodeResourcesUseCase, error) {
	if membership == nil || coordinator == nil || repository == nil {
		return nil, errors.New("update resources dependencies must not be nil")
	}
	return &UpdateNodeResourcesUseCase{membership: membership, coordinator: coordinator, repository: repository}, nil
}

func (uc *UpdateNodeResourcesUseCase) Update(ctx context.Context, command UpdateNodeResourcesCommand) (model.NodeDocument, error) {
	if ctx == nil {
		return model.NodeDocument{}, errors.New("context must not be nil")
	}
	if err := command.Resources.Validate(); err != nil {
		return model.NodeDocument{}, err
	}
	if !uc.coordinator.IsLocalCoordinator() {
		return model.NodeDocument{}, ErrNotCoordinator
	}

	uc.mu.Lock()
	defer uc.mu.Unlock()
	local := uc.membership.LocalNode()
	for attempt := 0; attempt < 3; attempt++ {
		current, err := uc.repository.Get(ctx, local.ID)
		if errors.Is(err, ports.ErrNodeDocumentNotFound) {
			current = model.NewNodeDocument(local.ID)
		} else if err != nil {
			return model.NodeDocument{}, err
		}
		cluster, err := uc.repository.GetClusterMetadata(ctx)
		if errors.Is(err, ports.ErrClusterMetadataNotFound) {
			cluster = model.ClusterMetadata{VersionVector: model.NewVersionVector(uc.knownNodeIDs())}
		} else if err != nil {
			return model.NodeDocument{}, err
		}
		next := current
		next.ID = local.ID
		next.Role = model.RoleCoordinator
		next.Status = model.StatusOnline
		next.Resources = command.Resources
		nextCluster := cluster.Advance(local.ID, uc.knownNodeIDs())
		if err := uc.repository.UpdateAtomic(ctx, local.ID, cluster, nextCluster, next); err != nil {
			if errors.Is(err, ports.ErrVersionConflict) {
				continue
			}
			return model.NodeDocument{}, fmt.Errorf("update node document: %w", err)
		}
		return next, nil
	}
	return model.NodeDocument{}, ports.ErrVersionConflict
}

func (uc *UpdateNodeResourcesUseCase) knownNodeIDs() []model.NodeID {
	local := uc.membership.LocalNode()
	ids := []model.NodeID{local.ID}
	for _, peer := range uc.membership.Peers() {
		ids = append(ids, peer.ID)
	}
	return ids
}
