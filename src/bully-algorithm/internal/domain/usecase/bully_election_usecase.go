package usecase

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/domain/model"
	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/ports"
)

// BullyElectionUseCase implementa una ronda del algoritmo del Matón.
//
// El caso de uso solo conoce puertos. Un adaptador gRPC puede transportar
// ELECTION, OK y COORDINATOR sin introducir dependencias de gRPC en el dominio.
type BullyElectionUseCase struct {
	membership ports.MembershipPort
	transport  ports.ElectionTransportPort

	mu          sync.RWMutex
	inProgress  bool
	lastResult  model.ElectionResult
	coordinator *model.Node
}

// NewBullyElectionUseCase construye el caso de uso con sus dependencias de
// entrada/salida.
func NewBullyElectionUseCase(
	membership ports.MembershipPort,
	transport ports.ElectionTransportPort,
) (*BullyElectionUseCase, error) {
	if membership == nil {
		return nil, errors.New("membership port must not be nil")
	}
	if transport == nil {
		return nil, errors.New("election transport port must not be nil")
	}

	return &BullyElectionUseCase{
		membership: membership,
		transport:  transport,
		lastResult: model.ElectionResult{State: model.ElectionIdle},
	}, nil
}

// Start inicia una elección local.
//
// Solo se consulta a nodos con un ID mayor. Si alguno responde, el nodo local
// queda esperando un mensaje COORDINATOR. Si ninguno responde, el nodo local se
// proclama coordinador y anuncia el resultado a toda la membresía conocida.
func (uc *BullyElectionUseCase) Start(ctx context.Context) (model.ElectionResult, error) {
	if ctx == nil {
		return model.ElectionResult{}, errors.New("context must not be nil")
	}

	local, peers, err := uc.membershipSnapshot()
	if err != nil {
		return model.ElectionResult{}, err
	}

	uc.mu.Lock()
	if uc.inProgress {
		result := uc.lastResult.Clone()
		uc.mu.Unlock()
		return result, nil
	}
	uc.inProgress = true
	uc.coordinator = nil
	uc.lastResult = model.ElectionResult{
		State:               model.ElectionInProgress,
		HigherPriorityNodes: countHigherPriority(local, peers),
	}
	uc.mu.Unlock()

	higherPriority := filterHigherPriority(local, peers)
	acknowledged := uc.requestElection(ctx, local, higherPriority)

	if err := ctx.Err(); err != nil {
		uc.finishWithState(model.ElectionIdle, nil)
		return model.ElectionResult{}, err
	}

	if acknowledged > 0 {
		result := model.ElectionResult{
			State:                  model.ElectionWaitingForCoordinator,
			HigherPriorityNodes:    len(higherPriority),
			AcknowledgedCandidates: acknowledged,
		}
		uc.mu.Lock()
		uc.inProgress = false
		uc.lastResult = result
		uc.mu.Unlock()
		return result.Clone(), nil
	}

	// No hubo respuesta de un nodo superior. El nodo local gana esta ronda.
	uc.mu.Lock()
	uc.inProgress = false
	uc.coordinator = &local
	uc.lastResult = model.ElectionResult{
		State:               model.ElectionCoordinator,
		Coordinator:         &local,
		HigherPriorityNodes: len(higherPriority),
	}
	uc.mu.Unlock()

	failures := uc.announceCoordinator(ctx, local, peers)
	uc.mu.Lock()
	uc.lastResult.AnnouncementFailures = failures
	result := uc.lastResult.Clone()
	uc.mu.Unlock()

	return result, nil
}

// HandleCoordinator procesa un mensaje COORDINATOR recibido por el adaptador de
// entrada. La validación de pertenencia evita aceptar un coordinador ajeno a la
// membresía conocida.
func (uc *BullyElectionUseCase) HandleCoordinator(coordinator model.Node) (model.ElectionResult, error) {
	if err := coordinator.Validate(); err != nil {
		return model.ElectionResult{}, err
	}

	local, peers, err := uc.membershipSnapshot()
	if err != nil {
		return model.ElectionResult{}, err
	}
	if !isKnownNode(coordinator, local, peers) {
		return model.ElectionResult{}, fmt.Errorf("coordinator %d is not part of the membership", coordinator.ID)
	}

	state := model.ElectionFollowing
	if coordinator.SameIdentity(local) {
		state = model.ElectionCoordinator
	}

	uc.mu.Lock()
	uc.inProgress = false
	uc.coordinator = &coordinator
	uc.lastResult = model.ElectionResult{
		State:       state,
		Coordinator: &coordinator,
	}
	result := uc.lastResult.Clone()
	uc.mu.Unlock()

	return result, nil
}

// HandleElection procesa un mensaje ELECTION recibido por el adaptador de
// entrada. Un nodo de mayor prioridad confirma el mensaje y comienza su propia
// elección de forma independiente.
func (uc *BullyElectionUseCase) HandleElection(ctx context.Context, candidate model.Node) (bool, error) {
	if ctx == nil {
		return false, errors.New("context must not be nil")
	}
	if err := candidate.Validate(); err != nil {
		return false, err
	}

	local, peers, err := uc.membershipSnapshot()
	if err != nil {
		return false, err
	}
	if !isKnownNode(candidate, local, peers) {
		return false, fmt.Errorf("election candidate %d is not part of the membership", candidate.ID)
	}
	if !local.HasHigherPriorityThan(candidate) {
		return false, nil
	}

	go func() {
		_, _ = uc.Start(context.Background())
	}()

	return true, nil
}

// CurrentResult permite consultar el último estado sin modificarlo.
func (uc *BullyElectionUseCase) CurrentResult() model.ElectionResult {
	uc.mu.RLock()
	defer uc.mu.RUnlock()

	return uc.lastResult.Clone()
}

type electionResponse struct {
	acknowledged bool
}

func (uc *BullyElectionUseCase) requestElection(
	ctx context.Context,
	local model.Node,
	candidates []model.Node,
) int {
	if len(candidates) == 0 {
		return 0
	}

	responses := make(chan electionResponse, len(candidates))
	for _, candidate := range candidates {
		go func(candidate model.Node) {
			acknowledged, _ := uc.transport.RequestElection(ctx, local, candidate)
			responses <- electionResponse{acknowledged: acknowledged}
		}(candidate)
	}

	acknowledged := 0
	for range candidates {
		select {
		case response := <-responses:
			if response.acknowledged {
				acknowledged++
			}
		case <-ctx.Done():
			return acknowledged
		}
	}

	return acknowledged
}

func (uc *BullyElectionUseCase) announceCoordinator(
	ctx context.Context,
	local model.Node,
	peers []model.Node,
) []model.NodeID {
	if len(peers) == 0 {
		return nil
	}

	type announcementResult struct {
		id      model.NodeID
		failure bool
	}
	results := make(chan announcementResult, len(peers))
	for _, peer := range peers {
		go func(peer model.Node) {
			err := uc.transport.AnnounceCoordinator(ctx, local, peer, local)
			results <- announcementResult{id: peer.ID, failure: err != nil}
		}(peer)
	}

	failed := make(map[model.NodeID]bool)
	for range peers {
		select {
		case result := <-results:
			if result.failure {
				failed[result.id] = true
			}
		case <-ctx.Done():
			for _, peer := range peers {
				failed[peer.ID] = true
			}
			return orderedFailures(peers, failed)
		}
	}

	return orderedFailures(peers, failed)
}

func (uc *BullyElectionUseCase) membershipSnapshot() (model.Node, []model.Node, error) {
	local := uc.membership.LocalNode()
	if err := local.Validate(); err != nil {
		return model.Node{}, nil, fmt.Errorf("invalid local node: %w", err)
	}

	peers := uc.membership.Peers()
	seen := make(map[model.NodeID]struct{}, len(peers))
	validPeers := make([]model.Node, 0, len(peers))
	for _, peer := range peers {
		if err := peer.Validate(); err != nil {
			return model.Node{}, nil, fmt.Errorf("invalid peer node: %w", err)
		}
		if peer.SameIdentity(local) {
			continue
		}
		if _, exists := seen[peer.ID]; exists {
			return model.Node{}, nil, fmt.Errorf("duplicate peer node id: %d", peer.ID)
		}
		seen[peer.ID] = struct{}{}
		validPeers = append(validPeers, peer)
	}

	return local, validPeers, nil
}

func (uc *BullyElectionUseCase) finishWithState(state model.ElectionState, coordinator *model.Node) {
	uc.mu.Lock()
	defer uc.mu.Unlock()

	uc.inProgress = false
	uc.coordinator = coordinator
	uc.lastResult = model.ElectionResult{State: state, Coordinator: coordinator}
}

func filterHigherPriority(local model.Node, peers []model.Node) []model.Node {
	higher := make([]model.Node, 0, len(peers))
	for _, peer := range peers {
		if peer.HasHigherPriorityThan(local) {
			higher = append(higher, peer)
		}
	}

	return higher
}

func countHigherPriority(local model.Node, peers []model.Node) int {
	return len(filterHigherPriority(local, peers))
}

func isKnownNode(candidate, local model.Node, peers []model.Node) bool {
	if candidate.SameIdentity(local) {
		return true
	}
	for _, peer := range peers {
		if candidate.SameIdentity(peer) {
			return true
		}
	}

	return false
}

func orderedFailures(peers []model.Node, failed map[model.NodeID]bool) []model.NodeID {
	failures := make([]model.NodeID, 0, len(failed))
	for _, peer := range peers {
		if failed[peer.ID] {
			failures = append(failures, peer.ID)
		}
	}

	return failures
}
