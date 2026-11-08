package usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/domain/model"
	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/ports"
)

// RegisterNodeUseCase coordina el alta de nodos. Un coordinador escribe los
// documentos y un agent solicita el registro por gRPC.
type RegisterNodeUseCase struct {
	membership  ports.MembershipPort
	coordinator ports.CoordinatorView
	repository  ports.NodeDocumentRepository
	capacity    ports.MachineCapacityReader
	transport   ports.NodeRegistrationTransport
	resourcesMu sync.Mutex
	resources   *model.NodeResources
}

func NewRegisterNodeUseCase(membership ports.MembershipPort, coordinator ports.CoordinatorView, repository ports.NodeDocumentRepository, capacity ports.MachineCapacityReader, transport ports.NodeRegistrationTransport) (*RegisterNodeUseCase, error) {
	if membership == nil || coordinator == nil || repository == nil || capacity == nil || transport == nil {
		return nil, errors.New("register node use case dependencies must not be nil")
	}
	return &RegisterNodeUseCase{membership: membership, coordinator: coordinator, repository: repository, capacity: capacity, transport: transport}, nil
}

func (uc *RegisterNodeUseCase) Register(ctx context.Context) (model.NodeDocument, error) {
	if ctx == nil {
		return model.NodeDocument{}, errors.New("context must not be nil")
	}
	local := uc.membership.LocalNode()
	if err := local.Validate(); err != nil {
		return model.NodeDocument{}, err
	}
	resources, err := uc.machineResources(ctx)
	if err != nil {
		return model.NodeDocument{}, fmt.Errorf("read machine capacity: %w", err)
	}
	if !uc.coordinator.IsLocalCoordinator() {
		coordinator, ok := uc.coordinator.CurrentCoordinator()
		if !ok || coordinator.SameIdentity(local) {
			return model.NodeDocument{}, nil
		}
		return uc.transport.RegisterNode(ctx, local, coordinator, resources)
	}
	return uc.registerLocal(ctx, local, resources)
}

func (uc *RegisterNodeUseCase) registerLocal(ctx context.Context, local model.Node, resources model.NodeResources) (model.NodeDocument, error) {
	for attempt := 0; attempt < 3; attempt++ {
		current, cluster, err := uc.readState(ctx, local.ID)
		if err != nil {
			return model.NodeDocument{}, err
		}
		if current.Role == model.RoleCoordinator && current.Status == model.StatusOnline && current.Resources == resources {
			return current, nil
		}
		next := current
		next.ID = local.ID
		next.Role = model.RoleCoordinator
		next.Status = model.StatusOnline
		next.Resources = resources
		if err := next.Validate(); err != nil {
			return model.NodeDocument{}, err
		}
		if err := uc.repository.UpdateAtomic(ctx, local.ID, cluster, cluster, next); err != nil {
			if errors.Is(err, ports.ErrVersionConflict) {
				continue
			}
			return model.NodeDocument{}, fmt.Errorf("register local node document: %w", err)
		}
		log.Printf("coordinator document persisted node=%d role=%s", next.ID, next.Role)
		return next, nil
	}
	return model.NodeDocument{}, ports.ErrVersionConflict
}

// RegisterRemote es invocado únicamente por el coordinador. La metadata global
// ya no viaja en el request: el vector pertenece al cluster, no al agent.
func (uc *RegisterNodeUseCase) RegisterRemote(ctx context.Context, sender model.Node, resources model.NodeResources) (model.NodeDocument, error) {
	if ctx == nil {
		return model.NodeDocument{}, errors.New("context must not be nil")
	}
	if !uc.coordinator.IsLocalCoordinator() {
		return model.NodeDocument{}, ports.ErrNotCoordinator
	}
	if err := sender.Validate(); err != nil {
		return model.NodeDocument{}, err
	}
	if !isKnownNode(sender, uc.membership.LocalNode(), uc.membership.Peers()) {
		return model.NodeDocument{}, fmt.Errorf("registration sender %d is not part of membership", sender.ID)
	}
	for attempt := 0; attempt < 3; attempt++ {
		current, cluster, err := uc.readState(ctx, sender.ID)
		if err != nil {
			return model.NodeDocument{}, err
		}
		if current.Status == model.StatusOnline && current.Role == model.RoleAgent && current.Resources == resources {
			return current, nil
		}
		next := current
		next.ID = sender.ID
		next.Role = model.RoleAgent
		next.Status = model.StatusOnline
		next.Resources = resources
		nextCluster := cluster.Advance(uc.membership.LocalNode().ID, uc.knownNodeIDs())
		if err := next.Validate(); err != nil {
			return model.NodeDocument{}, err
		}
		if err := uc.repository.UpdateAtomic(ctx, sender.ID, cluster, nextCluster, next); err != nil {
			if errors.Is(err, ports.ErrVersionConflict) {
				continue
			}
			return model.NodeDocument{}, fmt.Errorf("register remote node document: %w", err)
		}
		return next, nil
	}
	return model.NodeDocument{}, ports.ErrVersionConflict
}

func (uc *RegisterNodeUseCase) Run(ctx context.Context, interval, attemptTimeout time.Duration) error {
	if ctx == nil || interval <= 0 || attemptTimeout <= 0 {
		return errors.New("invalid registration monitor configuration")
	}
	uc.registerOnce(ctx, attemptTimeout)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			uc.registerOnce(ctx, attemptTimeout)
		}
	}
}

func (uc *RegisterNodeUseCase) registerOnce(ctx context.Context, timeout time.Duration) {
	attempt, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if _, err := uc.Register(attempt); err != nil && ctx.Err() == nil {
		log.Printf("node document sync failed, retrying: %v", err)
	}
}

func (uc *RegisterNodeUseCase) machineResources(ctx context.Context) (model.NodeResources, error) {
	uc.resourcesMu.Lock()
	defer uc.resourcesMu.Unlock()
	if uc.resources != nil {
		return *uc.resources, nil
	}
	resources, err := uc.capacity.Read(ctx)
	if err != nil {
		return model.NodeResources{}, err
	}
	uc.resources = &resources
	return resources, nil
}

func (uc *RegisterNodeUseCase) readState(ctx context.Context, nodeID model.NodeID) (model.NodeDocument, model.ClusterMetadata, error) {
	node, err := uc.repository.Get(ctx, nodeID)
	if err != nil && !errors.Is(err, ports.ErrNodeDocumentNotFound) {
		return model.NodeDocument{}, model.ClusterMetadata{}, err
	}
	cluster, clusterErr := uc.repository.GetClusterMetadata(ctx)
	if errors.Is(clusterErr, ports.ErrClusterMetadataNotFound) {
		cluster = model.ClusterMetadata{VersionVector: model.NewVersionVector(uc.knownNodeIDs())}
	} else if clusterErr != nil {
		return model.NodeDocument{}, model.ClusterMetadata{}, clusterErr
	}
	if errors.Is(err, ports.ErrNodeDocumentNotFound) {
		node = model.NewNodeDocument(nodeID)
	}
	return node, cluster, nil
}

func (uc *RegisterNodeUseCase) knownNodeIDs() []model.NodeID {
	local := uc.membership.LocalNode()
	ids := []model.NodeID{local.ID}
	for _, peer := range uc.membership.Peers() {
		ids = append(ids, peer.ID)
	}
	return ids
}

func (uc *RegisterNodeUseCase) MarkOffline(ctx context.Context) error {
	if ctx == nil || !uc.coordinator.IsLocalCoordinator() {
		return nil
	}
	local := uc.membership.LocalNode()
	current, cluster, err := uc.readState(ctx, local.ID)
	if err != nil {
		return err
	}
	next := current
	next.Status = model.StatusOffline
	next.Role = model.RoleAgent
	nextCluster := cluster.Advance(local.ID, uc.knownNodeIDs())
	return uc.repository.UpdateAtomic(ctx, local.ID, cluster, nextCluster, next)
}

func (uc *RegisterNodeUseCase) MarkFollowerOffline(ctx context.Context, nodeID model.NodeID) error {
	if ctx == nil {
		return errors.New("context must not be nil")
	}
	if !uc.coordinator.IsLocalCoordinator() {
		return ports.ErrNotCoordinator
	}
	local := uc.membership.LocalNode()
	if nodeID == local.ID {
		return errors.New("the coordinator does not mark itself offline")
	}
	current, cluster, err := uc.readState(ctx, nodeID)
	if err != nil {
		return err
	}
	if current.Status == model.StatusOffline && current.Role == model.RoleAgent {
		return nil
	}
	next := current
	next.Status = model.StatusOffline
	next.Role = model.RoleAgent
	nextCluster := cluster.Advance(local.ID, uc.knownNodeIDs())
	return uc.repository.UpdateAtomic(ctx, nodeID, cluster, nextCluster, next)
}
