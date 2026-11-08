package usecase

import (
	"context"
	"errors"
	"time"

	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/domain/model"
	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/ports"
)

// ClusterMetadataUseCase registra una única versión global cuando cambia el
// coordinador activo. Las escrituras de recursos reutilizan esta metadata.
type ClusterMetadataUseCase struct {
	membership  ports.MembershipPort
	coordinator ports.CoordinatorView
	repository  ports.NodeDocumentRepository
}

func NewClusterMetadataUseCase(membership ports.MembershipPort, coordinator ports.CoordinatorView, repository ports.NodeDocumentRepository) (*ClusterMetadataUseCase, error) {
	if membership == nil || coordinator == nil || repository == nil {
		return nil, errors.New("cluster metadata dependencies must not be nil")
	}
	return &ClusterMetadataUseCase{membership: membership, coordinator: coordinator, repository: repository}, nil
}

func (uc *ClusterMetadataUseCase) SyncOnce(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context must not be nil")
	}
	if !uc.coordinator.IsLocalCoordinator() {
		return nil
	}
	local := uc.membership.LocalNode()
	current, err := uc.repository.GetClusterMetadata(ctx)
	if errors.Is(err, ports.ErrClusterMetadataNotFound) {
		current = model.ClusterMetadata{VersionVector: model.NewVersionVector(uc.knownNodeIDs())}
	} else if err != nil {
		return err
	}
	key := local.ID.Key()
	if current.ActiveCoordinator == local.ID && current.LastModifiedBy == local.ID && current.GlobalLamportClock > 0 && current.VersionVector[key] > 0 {
		return nil
	}
	next := current.Advance(local.ID, uc.knownNodeIDs())
	return uc.repository.UpdateClusterMetadataAtomic(ctx, current, next)
}

func (uc *ClusterMetadataUseCase) Run(ctx context.Context, interval, attemptTimeout time.Duration) error {
	if ctx == nil || interval <= 0 || attemptTimeout <= 0 {
		return errors.New("invalid cluster metadata monitor configuration")
	}
	attempt := func() {
		attemptCtx, cancel := context.WithTimeout(ctx, attemptTimeout)
		defer cancel()
		_ = uc.SyncOnce(attemptCtx)
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
