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

	// La capacidad física se lee una vez por proceso. Run reintenta el registro
	// periódicamente; sin esta caché se ejecutaría PowerShell en cada ciclo.
	resourcesMu sync.Mutex
	resources   *model.NodeResources
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
	resources, err := uc.machineResources(ctx)
	if err != nil {
		return model.NodeDocument{}, fmt.Errorf("read machine capacity: %w", err)
	}

	if !uc.coordinator.IsLocalCoordinator() {
		coordinator, ok := uc.coordinator.CurrentCoordinator()
		if !ok || coordinator.SameIdentity(local) {
			log.Printf("registration skipped: no remote coordinator (known=%t self=%t)", ok, ok && coordinator.SameIdentity(local))
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
			if cluster, clusterErr := uc.repository.GetClusterMetadata(ctx); clusterErr == nil {
				current.Metadata = cluster.AsMetadata()
			} else if !errors.Is(clusterErr, ports.ErrClusterMetadataNotFound) {
				return model.NodeDocument{}, fmt.Errorf("get cluster metadata: %w", clusterErr)
			}
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
		next.Metadata = current.Metadata.Clone()

		if err := next.Validate(); err != nil {
			return model.NodeDocument{}, err
		}
		if err := uc.repository.UpdateAtomic(ctx, local.ID, expected, next); err != nil {
			if errors.Is(err, ports.ErrVersionConflict) {
				continue
			}
			return model.NodeDocument{}, fmt.Errorf("register local node document: %w", err)
		}
		log.Printf("coordinator document persisted node=%d role=%s lamport=%d", next.ID, next.Role, next.Metadata.Lamport)
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
	_ = metadata
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
			if cluster, clusterErr := uc.repository.GetClusterMetadata(ctx); clusterErr == nil {
				current.Metadata = cluster.AsMetadata()
			} else if !errors.Is(clusterErr, ports.ErrClusterMetadataNotFound) {
				return model.NodeDocument{}, fmt.Errorf("get cluster metadata: %w", clusterErr)
			}
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
		next.Metadata = current.Metadata.Clone()

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
func (uc *RegisterNodeUseCase) Run(ctx context.Context, interval, attemptTimeout time.Duration) error {
	if ctx == nil {
		return errors.New("context must not be nil")
	}
	if interval <= 0 {
		return errors.New("registration interval must be greater than zero")
	}

	// El primer intento puede fallar porque el coordinador todavía está
	// arrancando. Los ciclos siguientes reintentan sin detener el monitor.
	if attemptTimeout <= 0 {
		return errors.New("registration attempt timeout must be greater than zero")
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

// registerOnce ejecuta un intento de registro y deja constancia de los errores.
// El monitor no se detiene: el siguiente ciclo vuelve a intentarlo.
func (uc *RegisterNodeUseCase) registerOnce(ctx context.Context, timeout time.Duration) {
	// Cada intento tiene límite de tiempo: una llamada bloqueada no debe
	// detener el bucle de sincronización.
	attemptContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if _, err := uc.Register(attemptContext); err != nil && ctx.Err() == nil {
		log.Printf("node document sync failed, retrying: %v", err)
	}
}

// machineResources devuelve la capacidad leída al primer registro del proceso.
// Si la lectura falla, no se guarda nada y el siguiente ciclo la reintenta.
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
	next.Role = model.RoleFollower
	uc.observeAndTick(current.Metadata.Lamport)
	next.Metadata = current.Metadata.Clone()
	next.Metadata.Lamport = uc.clock.Value()
	next.Metadata.VersionVector = ensureKnownKeys(current.Metadata.VersionVector, uc.knownNodeIDs())
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

// MarkFollowerOffline marca como OFFLINE y FOLLOWER el documento de un follower
// que dejó de enviar heartbeats. Solo el coordinador activo puede ejecutarlo y
// usa el mismo reloj de Lamport que el resto de escrituras del proceso.
func (uc *RegisterNodeUseCase) MarkFollowerOffline(ctx context.Context, nodeID model.NodeID) error {
	if ctx == nil {
		return errors.New("context must not be nil")
	}
	if !uc.coordinator.IsLocalCoordinator() {
		return ErrNotCoordinator
	}

	local := uc.membership.LocalNode()
	if nodeID == local.ID {
		return errors.New("the coordinator does not mark itself offline")
	}

	current, err := uc.repository.Get(ctx, nodeID)
	if err != nil {
		if errors.Is(err, ports.ErrNodeDocumentNotFound) {
			return nil
		}
		return fmt.Errorf("get follower document: %w", err)
	}
	if current.Status == model.StatusOffline && current.Role == model.RoleFollower {
		return nil
	}

	next := current.Clone()
	next.ID = nodeID
	next.Status = model.StatusOffline
	next.Role = model.RoleFollower
	uc.observeAndTick(current.Metadata.Lamport)
	next.Metadata = current.Metadata.Clone()
	next.Metadata.Lamport = uc.clock.Value()
	next.Metadata.VersionVector = ensureKnownKeys(current.Metadata.VersionVector, uc.knownNodeIDs())
	next.Metadata.VersionVector.Increment(local.ID)
	next.Metadata.UpdatedBy = local.ID

	if err := next.Validate(); err != nil {
		return err
	}
	return uc.repository.UpdateAtomic(ctx, nodeID, current.Metadata, next)
}
