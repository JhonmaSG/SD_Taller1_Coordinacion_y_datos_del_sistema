package usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/domain/model"
	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/ports"
)

// ClusterMetadataUseCase registra una única nueva versión global cuando cambia
// el coordinador activo. Las escrituras posteriores del documento de recursos
// reutilizan esa metadata y no incrementan el vector por cada documento.
type ClusterMetadataUseCase struct {
	membership  ports.MembershipPort
	coordinator ports.CoordinatorView
	repository  ports.NodeDocumentRepository
	clock       model.LamportClock
}

func NewClusterMetadataUseCase(
	membership ports.MembershipPort,
	coordinator ports.CoordinatorView,
	repository ports.NodeDocumentRepository,
) (*ClusterMetadataUseCase, error) {
	if membership == nil || coordinator == nil || repository == nil {
		return nil, errors.New("cluster metadata dependencies must not be nil")
	}
	return &ClusterMetadataUseCase{membership: membership, coordinator: coordinator, repository: repository}, nil
}

// SyncOnce persiste el evento de liderazgo únicamente si el coordinador global
// almacenado es distinto del coordinador local.
func (uc *ClusterMetadataUseCase) SyncOnce(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context must not be nil")
	}
	if !uc.coordinator.IsLocalCoordinator() {
		return nil
	}

	local := uc.membership.LocalNode()
	current, err := uc.repository.GetClusterMetadata(ctx)
	if err != nil && !errors.Is(err, ports.ErrClusterMetadataNotFound) {
		return err
	}
	if errors.Is(err, ports.ErrClusterMetadataNotFound) {
		current = model.ClusterMetadata{VersionVector: model.NewVersionVector(uc.knownNodeIDs())}
	}
	localKey := local.ID.Key()
	if current.ActiveCoordinator == local.ID &&
		current.LastModifiedBy == local.ID &&
		current.GlobalLamportClock > 0 &&
		current.VersionVector[localKey] > 0 {
		return nil
	}

	uc.clock.Observe(current.GlobalLamportClock)
	next := current.Clone()
	next.GlobalLamportClock = uc.clock.Value()
	next.VersionVector = ensureKnownKeys(current.VersionVector, uc.knownNodeIDs())
	next.VersionVector.Increment(local.ID)
	next.ActiveCoordinator = local.ID
	next.LastModifiedBy = local.ID

	if err := uc.repository.UpdateClusterMetadataAtomic(ctx, current, next); err != nil {
		if errors.Is(err, ports.ErrVersionConflict) {
			return fmt.Errorf("cluster leadership changed concurrently: %w", err)
		}
		return err
	}
	return nil
}

func (uc *ClusterMetadataUseCase) Run(ctx context.Context, interval, attemptTimeout time.Duration) error {
	if ctx == nil || interval <= 0 || attemptTimeout <= 0 {
		return errors.New("invalid cluster metadata monitor configuration")
	}
	attempt := func() {
		attemptContext, cancel := context.WithTimeout(ctx, attemptTimeout)
		defer cancel()
		_ = uc.SyncOnce(attemptContext)
	}
	attempt()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			attempt()
		}
	}
}

func (uc *ClusterMetadataUseCase) knownNodeIDs() []model.NodeID {
	local := uc.membership.LocalNode()
	ids := []model.NodeID{local.ID}
	for _, peer := range uc.membership.Peers() {
		ids = append(ids, peer.ID)
	}
	return ids
}
