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

// UpdateNodeResourcesCommand contiene el estado de recursos que se desea
// aplicar al documento del nodo local.
type UpdateNodeResourcesCommand struct {
	Resources model.NodeResources
}

// UpdateNodeResourcesUseCase actualiza el agregado del nodo coordinador con
// control de versión optimista y un reloj Lamport local.
type UpdateNodeResourcesUseCase struct {
	membership  ports.MembershipPort
	coordinator ports.CoordinatorView
	repository  ports.NodeDocumentRepository

	mu    sync.Mutex
	clock model.LamportClock
}

// NewUpdateNodeResourcesUseCase crea el caso de uso de escritura exclusiva del
// coordinador.
func NewUpdateNodeResourcesUseCase(
	membership ports.MembershipPort,
	coordinator ports.CoordinatorView,
	repository ports.NodeDocumentRepository,
) (*UpdateNodeResourcesUseCase, error) {
	if membership == nil {
		return nil, errors.New("membership port must not be nil")
	}
	if coordinator == nil {
		return nil, errors.New("coordinator view must not be nil")
	}
	if repository == nil {
		return nil, errors.New("node document repository must not be nil")
	}

	return &UpdateNodeResourcesUseCase{
		membership:  membership,
		coordinator: coordinator,
		repository:  repository,
		clock:       model.NewLamportClock(0),
	}, nil
}

// Update lee, versiona y persiste atómicamente el documento local. Los
// conflictos CAS se reintentan de forma limitada; nunca se sobrescribe una
// versión concurrente sin volver a leerla.
func (uc *UpdateNodeResourcesUseCase) Update(
	ctx context.Context,
	command UpdateNodeResourcesCommand,
) (model.NodeDocument, error) {
	if ctx == nil {
		return model.NodeDocument{}, errors.New("context must not be nil")
	}
	if err := validateResources(command.Resources); err != nil {
		return model.NodeDocument{}, err
	}
	if !uc.coordinator.IsLocalCoordinator() {
		return model.NodeDocument{}, ErrNotCoordinator
	}

	local := uc.membership.LocalNode()
	if err := local.Validate(); err != nil {
		return model.NodeDocument{}, fmt.Errorf("invalid local node: %w", err)
	}

	// Serializa las escrituras locales para que Tick e Increment sean eventos
	// monotónicos incluso si llegan varias peticiones concurrentes.
	uc.mu.Lock()
	defer uc.mu.Unlock()

	for attempt := 0; attempt < 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return model.NodeDocument{}, err
		}
		if !uc.coordinator.IsLocalCoordinator() {
			return model.NodeDocument{}, ErrNotCoordinator
		}

		current, err := uc.repository.Get(ctx, local.ID)
		expected := model.Metadata{}
		if err != nil {
			if !errors.Is(err, ports.ErrNodeDocumentNotFound) {
				return model.NodeDocument{}, fmt.Errorf("get node document: %w", err)
			}
			current = model.NewNodeDocument(local.ID)
		} else {
			expected = current.Metadata.Clone()
		}

		// Recibir el estado persistido es un evento remoto; la modificación es
		// un evento local posterior, por eso se aplican Observe y Tick.
		uc.clock.Observe(current.Metadata.Lamport)
		lamport := uc.clock.Tick()

		next := current.Clone()
		next.ID = local.ID
		next.Role = model.RoleCoordinator
		next.Status = model.StatusOnline
		next.Resources = command.Resources
		next.Metadata.Lamport = lamport
		next.Metadata.VersionVector.Increment(local.ID)
		next.Metadata.UpdatedBy = local.ID

		if err := next.Validate(); err != nil {
			return model.NodeDocument{}, err
		}
		if err := uc.repository.UpdateAtomic(ctx, local.ID, expected, next); err != nil {
			if errors.Is(err, ports.ErrVersionConflict) {
				continue
			}
			return model.NodeDocument{}, fmt.Errorf("update node document: %w", err)
		}

		return next, nil
	}

	return model.NodeDocument{}, ports.ErrVersionConflict
}

func validateResources(resources model.NodeResources) error {
	if resources.CPUCores < 0 || resources.MemoryGB < 0 || resources.BandwidthMbps < 0 {
		return errors.New("resource values cannot be negative")
	}

	return nil
}
