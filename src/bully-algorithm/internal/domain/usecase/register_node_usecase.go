package usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/domain/model"
	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/ports"
)

// RegisterNodeUseCase crea o marca online el documento del nodo local al
// arrancar el proceso.
type RegisterNodeUseCase struct {
	membership  ports.MembershipPort
	coordinator ports.CoordinatorView
	repository  ports.NodeDocumentRepository
	capacity    ports.MachineCapacityReader
}

func NewRegisterNodeUseCase(
	membership ports.MembershipPort,
	coordinator ports.CoordinatorView,
	repository ports.NodeDocumentRepository,
	capacity ports.MachineCapacityReader,
) (*RegisterNodeUseCase, error) {
	if membership == nil || coordinator == nil || repository == nil || capacity == nil {
		return nil, errors.New("register node use case dependencies must not be nil")
	}

	return &RegisterNodeUseCase{
		membership:  membership,
		coordinator: coordinator,
		repository:  repository,
		capacity:    capacity,
	}, nil
}

// Register garantiza que exista un documento por nodo y que el nodo local
// figure online con el rol observado al terminar la elección inicial.
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

	for attempt := 0; attempt < 3; attempt++ {
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

		next := current.Clone()
		next.ID = local.ID
		next.Resources = resources
		next.Status = model.StatusOnline
		if uc.coordinator.IsLocalCoordinator() {
			next.Role = model.RoleCoordinator
		} else {
			next.Role = model.RoleFollower
		}
		if next.Metadata.UpdatedBy == 0 {
			next.Metadata = model.NewMetadata(local.ID, 1)
		} else {
			next.Metadata.VersionVector.Increment(local.ID)
			next.Metadata.UpdatedBy = local.ID
			next.Metadata.Lamport++
		}

		if err := next.Validate(); err != nil {
			return model.NodeDocument{}, err
		}
		if err := uc.repository.UpdateAtomic(ctx, local.ID, expected, next); err != nil {
			if errors.Is(err, ports.ErrVersionConflict) {
				continue
			}
			return model.NodeDocument{}, fmt.Errorf("register node document: %w", err)
		}

		return next, nil
	}

	return model.NodeDocument{}, ports.ErrVersionConflict
}

// SyncRole actualiza solo el rol/status cuando cambia el coordinador observado.
// No escribe en cada heartbeat, evitando una escritura por tick.
func (uc *RegisterNodeUseCase) SyncRole(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context must not be nil")
	}

	local := uc.membership.LocalNode()
	current, err := uc.repository.Get(ctx, local.ID)
	if err != nil {
		if errors.Is(err, ports.ErrNodeDocumentNotFound) {
			_, registerErr := uc.Register(ctx)
			return registerErr
		}
		return err
	}

	desiredRole := model.RoleFollower
	if uc.coordinator.IsLocalCoordinator() {
		desiredRole = model.RoleCoordinator
	}
	if current.Role == desiredRole && current.Status == model.StatusOnline {
		return nil
	}

	next := current.Clone()
	next.Role = desiredRole
	next.Status = model.StatusOnline
	clock := model.NewLamportClock(current.Metadata.Lamport)
	next.Metadata.Lamport = clock.Tick()
	next.Metadata.VersionVector.Increment(local.ID)
	next.Metadata.UpdatedBy = local.ID
	if err := uc.repository.UpdateAtomic(ctx, local.ID, current.Metadata, next); err != nil {
		return err
	}

	return nil
}

// Run sincroniza el rol persistido sin generar escrituras mientras el estado no
// cambie. Se detiene con el contexto de apagado.
func (uc *RegisterNodeUseCase) Run(ctx context.Context, interval time.Duration) error {
	if ctx == nil {
		return errors.New("context must not be nil")
	}
	if interval <= 0 {
		return errors.New("sync interval must be greater than zero")
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := uc.SyncRole(ctx); err != nil && !errors.Is(err, context.Canceled) {
				return err
			}
		}
	}
}

// MarkOffline actualiza el estado del documento durante un apagado ordenado.
func (uc *RegisterNodeUseCase) MarkOffline(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context must not be nil")
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
	clock := model.NewLamportClock(current.Metadata.Lamport)
	next.Metadata.Lamport = clock.Tick()
	next.Metadata.VersionVector.Increment(local.ID)
	next.Metadata.UpdatedBy = local.ID

	return uc.repository.UpdateAtomic(ctx, local.ID, current.Metadata, next)
}
