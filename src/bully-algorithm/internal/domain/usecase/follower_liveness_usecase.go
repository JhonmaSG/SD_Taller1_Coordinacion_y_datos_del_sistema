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

// FollowerLivenessUseCase lo ejecuta únicamente el coordinador activo. Anota la
// última vez que cada follower envió un heartbeat y marca OFFLINE a los que
// superan el umbral. Los followers no escriben su propio estado: el coordinador
// es el único que lo hace, mediante RegisterNodeUseCase.MarkFollowerOffline.
type FollowerLivenessUseCase struct {
	membership   ports.MembershipPort
	coordinator  ports.CoordinatorView
	registration ports.FollowerStatusWriter
	staleAfter   time.Duration
	now          func() time.Time

	mu       sync.Mutex
	lastSeen map[model.NodeID]time.Time
	offline  map[model.NodeID]bool
}

// NewFollowerLivenessUseCase crea el monitor de followers. staleAfter es el
// tiempo sin heartbeats a partir del cual un follower se marca OFFLINE.
func NewFollowerLivenessUseCase(
	membership ports.MembershipPort,
	coordinator ports.CoordinatorView,
	registration ports.FollowerStatusWriter,
	staleAfter time.Duration,
) (*FollowerLivenessUseCase, error) {
	if membership == nil || coordinator == nil || registration == nil {
		return nil, errors.New("follower liveness dependencies must not be nil")
	}
	if staleAfter <= 0 {
		return nil, errors.New("follower stale threshold must be greater than zero")
	}

	return &FollowerLivenessUseCase{
		membership:   membership,
		coordinator:  coordinator,
		registration: registration,
		staleAfter:   staleAfter,
		now:          time.Now,
		lastSeen:     make(map[model.NodeID]time.Time),
		offline:      make(map[model.NodeID]bool),
	}, nil
}

// RecordHeartbeat anota un heartbeat recibido de un follower. Solo tiene efecto
// si el nodo local es el coordinador activo; en otro caso se ignora.
func (uc *FollowerLivenessUseCase) RecordHeartbeat(sender model.NodeID) {
	if !uc.coordinator.IsLocalCoordinator() {
		return
	}

	uc.mu.Lock()
	defer uc.mu.Unlock()

	uc.lastSeen[sender] = uc.now()
	delete(uc.offline, sender)
}

// Sweep revisa a todos los peers. Un peer sin heartbeat registrado recibe un
// periodo de gracia que empieza en el primer barrido posterior a la elección.
// Un peer que supera staleAfter se marca OFFLINE una sola vez hasta que vuelva
// a enviar heartbeats.
func (uc *FollowerLivenessUseCase) Sweep(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context must not be nil")
	}
	if !uc.coordinator.IsLocalCoordinator() {
		uc.reset()
		return nil
	}

	now := uc.now()
	var errs []error
	for _, peer := range uc.membership.Peers() {
		uc.mu.Lock()
		seen, known := uc.lastSeen[peer.ID]
		if !known {
			uc.lastSeen[peer.ID] = now
			uc.mu.Unlock()
			continue
		}
		alreadyOffline := uc.offline[peer.ID]
		uc.mu.Unlock()

		if alreadyOffline || now.Sub(seen) <= uc.staleAfter {
			continue
		}

		if err := uc.registration.MarkFollowerOffline(ctx, peer.ID); err != nil {
			errs = append(errs, fmt.Errorf("mark node %d offline: %w", peer.ID, err))
			continue
		}

		// Si llegó un heartbeat durante la escritura, el peer sigue vivo y no
		// debe quedar marcado para siempre como OFFLINE.
		uc.mu.Lock()
		if current, ok := uc.lastSeen[peer.ID]; ok && current.Equal(seen) {
			uc.offline[peer.ID] = true
		}
		uc.mu.Unlock()
	}

	return errors.Join(errs...)
}

// Run ejecuta Sweep cada interval hasta que el contexto se cancela.
func (uc *FollowerLivenessUseCase) Run(ctx context.Context, interval, attemptTimeout time.Duration) error {
	if ctx == nil {
		return errors.New("context must not be nil")
	}
	if interval <= 0 {
		return errors.New("liveness interval must be greater than zero")
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			sweepContext, cancel := context.WithTimeout(ctx, attemptTimeout)
			err := uc.Sweep(sweepContext)
			cancel()
			if err != nil {
				log.Printf("follower liveness sweep: %v", err)
			}
		}
	}
}

// reset olvida el estado de un liderazgo anterior. Al volver a ser coordinador,
// el nodo empieza otra vez con periodo de gracia para cada follower.
func (uc *FollowerLivenessUseCase) reset() {
	uc.mu.Lock()
	defer uc.mu.Unlock()

	uc.lastSeen = make(map[model.NodeID]time.Time)
	uc.offline = make(map[model.NodeID]bool)
}
