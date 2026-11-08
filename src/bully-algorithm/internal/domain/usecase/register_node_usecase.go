package usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/domain/model"
	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/ports"
)

// RegisterNodeUseCase coordina el alta de nodos. Un coordinador puede escribir
// el documento local y los documentos de followers; un follower solo envía su
// capacidad al coordinador mediante NodeRegistrationTransport.
type RegisterNodeUseCase struct {
	membership  ports.MembershipPort
	coordinator ports.CoordinatorView
	repository  ports.NodeDocumentRepository
	capacity    ports.MachineCapacityReader
	transport   ports.NodeRegistrationTransport
	clock       model.LamportClock
}

func NewRegisterNodeUseCase(
	membership ports.MembershipPort,
	coordinator ports.CoordinatorView,
	repository ports.NodeDocumentRepository,
	capacity ports.MachineCapacityReader,
	transport ports.NodeRegistrationTransport,
) (*RegisterNodeUseCase, error) {
	if membership == nil || coordinator == nil || repository == nil || capacity == nil || transport == nil {
		return nil, errors.New("register node use case dependencies must not be nil")
	}

	return &RegisterNodeUseCase{
		membership:  membership,
		coordinator: coordinator,
		repository:  repository,
		capacity:    capacity,
		transport:   transport,
		clock:       model.NewLamportClock(0),
	}, nil
}

// Register sincroniza el alta del nodo local. Si este proceso es follower, no
// toca MongoDB: solicita al coordinador que persista su documento.
func (uc *RegisterNodeUseCase) Register(ctx context.Context) (model.NodeDocument, error) {
	if ctx == nil {
		return model.NodeDocument{}, errors.New("context must not be nil")
	}

	local := uc.membership.LocalNode()
	if err := local.Validate(); err != nil {
		return model.NodeDocument{}, err
	}
	resources, err := uc.capacity.Read(ctx)
	if err != nil {
		return model.NodeDocument{}, fmt.Errorf("read machine capacity: %w", err)
	}

	if !uc.coordinator.IsLocalCoordinator() {
		coordinator, ok := uc.coordinator.CurrentCoordinator()
		if !ok || coordinator.SameIdentity(local) {
			return model.NodeDocument{}, nil
		}
		metadata := uc.registrationMetadata(local)
		return uc.transport.RegisterNode(ctx, local, coordinator, resources, metadata)
	}

	return uc.registerLocal(ctx, local, resources)
}

func (uc *RegisterNodeUseCase) registerLocal(
	ctx context.Context,
	local model.Node,
	resources model.NodeResources,
) (model.NodeDocument, error) {
	knownIDs := uc.knownNodeIDs()
	for attempt := 0; attempt < 3; attempt++ {
		current, err := uc.repository.Get(ctx, local.ID)
		expected := model.Metadata{}
		if err != nil {
			if !errors.Is(err, ports.ErrNodeDocumentNotFound) {
				return model.NodeDocument{}, fmt.Errorf("get local node document: %w", err)
			}
			current = model.NewNodeDocument(local.ID)
			current.Metadata.VersionVector = model.NewVersionVector(knownIDs)
		} else {
			expected = current.Metadata.Clone()
			if current.Role == model.RoleCoordinator && current.Status == model.StatusOnline && current.Resources == resources && hasKnownKeys(current.Metadata.VersionVector, knownIDs) {
				return current, nil
			}
		}

		next := current.Clone()
		next.ID = local.ID
		next.Role = model.RoleCoordinator
		next.Status = model.StatusOnline
		next.Resources = resources
		uc.observeAndTick(current.Metadata.Lamport)
		next.Metadata.Lamport = uc.clock.Value()
		next.Metadata.VersionVector = ensureKnownKeys(current.Metadata.VersionVector, knownIDs)
		next.Metadata.VersionVector.Increment(local.ID)
		next.Metadata.UpdatedBy = local.ID

		if err := next.Validate(); err != nil {
			return model.NodeDocument{}, err
		}
		if err := uc.repository.UpdateAtomic(ctx, local.ID, expected, next); err != nil {
			if errors.Is(err, ports.ErrVersionConflict) {
				continue
			}
			return model.NodeDocument{}, fmt.Errorf("register local node document: %w", err)
		}
		return next, nil
	}

	return model.NodeDocument{}, ports.ErrVersionConflict
}

// RegisterRemote es invocado únicamente por el servidor del coordinador.
// Persiste el documento del follower con la metadata recibida y la escritura
// propia del coordinador fusionadas en el vector del documento.
func (uc *RegisterNodeUseCase) RegisterRemote(
	ctx context.Context,
	sender model.Node,
	resources model.NodeResources,
	metadata model.Metadata,
) (model.NodeDocument, error) {
	if ctx == nil {
		return model.NodeDocument{}, errors.New("context must not be nil")
	}
	if !uc.coordinator.IsLocalCoordinator() {
		return model.NodeDocument{}, ErrNotCoordinator
	}
	if err := sender.Validate(); err != nil {
		return model.NodeDocument{}, err
	}
	if !isKnownNode(sender, uc.membership.LocalNode(), uc.membership.Peers()) {
		return model.NodeDocument{}, fmt.Errorf("registration sender %d is not part of membership", sender.ID)
	}

	local := uc.membership.LocalNode()
	knownIDs := uc.knownNodeIDs()
	for attempt := 0; attempt < 3; attempt++ {
		current, err := uc.repository.Get(ctx, sender.ID)
		expected := model.Metadata{}
		if err != nil {
			if !errors.Is(err, ports.ErrNodeDocumentNotFound) {
				return model.NodeDocument{}, fmt.Errorf("get remote node document: %w", err)
			}
			current = model.NewNodeDocument(sender.ID)
			current.Metadata.VersionVector = model.NewVersionVector(knownIDs)
		} else {
			expected = current.Metadata.Clone()
			if current.Status == model.StatusOnline && current.Role == model.RoleFollower && current.Resources == resources && current.Metadata.UpdatedBy == local.ID && hasKnownKeys(current.Metadata.VersionVector, knownIDs) {
				return current, nil
			}
		}

		next := current.Clone()
		next.ID = sender.ID
		next.Role = model.RoleFollower
		next.Status = model.StatusOnline
		next.Resources = resources
		uc.observeAndTick(metadata.Lamport)
		next.Metadata.Lamport = uc.clock.Value()
		next.Metadata.VersionVector = ensureKnownKeys(current.Metadata.VersionVector, knownIDs).Merge(metadata.VersionVector)
		next.Metadata.VersionVector.Increment(local.ID)
		next.Metadata.UpdatedBy = local.ID

		if err := next.Validate(); err != nil {
			return model.NodeDocument{}, err
		}
		if err := uc.repository.UpdateAtomic(ctx, sender.ID, expected, next); err != nil {
			if errors.Is(err, ports.ErrVersionConflict) {
				continue
			}
			return model.NodeDocument{}, fmt.Errorf("register remote node document: %w", err)
		}
		return next, nil
	}

	return model.NodeDocument{}, ports.ErrVersionConflict
}

// Run reintenta el registro del nodo cuando el coordinador aparece después
// del follower. Un registro sin cambios no produce una escritura adicional.
func (uc *RegisterNodeUseCase) Run(ctx context.Context, interval time.Duration) error {
	if ctx == nil {
		return errors.New("context must not be nil")
	}
	if interval <= 0 {
		return errors.New("registration interval must be greater than zero")
	}

	if _, err := uc.Register(ctx); err != nil && !errors.Is(err, context.Canceled) {
		// La elección o el servidor del coordinador pueden estar arrancando.
		// El siguiente tick reintentará sin detener el monitor.
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := uc.Register(ctx); err != nil && !errors.Is(err, context.Canceled) {
				continue
			}
		}
	}
}

func (uc *RegisterNodeUseCase) registrationMetadata(local model.Node) model.Metadata {
	metadata := model.NewMetadata(local.ID, 1)
	metadata.VersionVector = ensureKnownKeys(model.VersionVector{}, uc.knownNodeIDs())
	metadata.VersionVector.Increment(local.ID)
	return metadata
}

func (uc *RegisterNodeUseCase) observeAndTick(remote model.LamportTime) {
	uc.clock.Observe(remote)
}

func (uc *RegisterNodeUseCase) knownNodeIDs() []model.NodeID {
	local := uc.membership.LocalNode()
	ids := make([]model.NodeID, 0, len(uc.membership.Peers())+1)
	ids = append(ids, local.ID)
	for _, peer := range uc.membership.Peers() {
		ids = append(ids, peer.ID)
	}
	return ids
}

func ensureKnownKeys(vector model.VersionVector, nodeIDs []model.NodeID) model.VersionVector {
	result := vector.Clone()
	if result == nil {
		result = model.NewVersionVector(nodeIDs)
		return result
	}
	for _, nodeID := range nodeIDs {
		key := nodeID.Key()
		if _, exists := result[key]; !exists {
			result[key] = 0
		}
	}
	return result
}

// MarkOffline se conserva para apagados coordinados del coordinador. Un
// follower no escribe directamente su documento; su estado debe ser resuelto
// por el coordinador mediante health checks.
func (uc *RegisterNodeUseCase) MarkOffline(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context must not be nil")
	}
	if !uc.coordinator.IsLocalCoordinator() {
		return nil
	}

	local := uc.membership.LocalNode()
	current, err := uc.repository.Get(ctx, local.ID)
	if err != nil {
		if errors.Is(err, ports.ErrNodeDocumentNotFound) {
			return nil
		}
		return err
	}

	next := current.Clone()
	next.Status = model.StatusOffline
	uc.observeAndTick(current.Metadata.Lamport)
	next.Metadata.Lamport = uc.clock.Value()
	next.Metadata.VersionVector.Increment(local.ID)
	next.Metadata.UpdatedBy = local.ID
	return uc.repository.UpdateAtomic(ctx, local.ID, current.Metadata, next)
}

func hasKnownKeys(vector model.VersionVector, nodeIDs []model.NodeID) bool {
	for _, nodeID := range nodeIDs {
		if _, exists := vector[nodeID.Key()]; !exists {
			return false
		}
	}
	return true
}
