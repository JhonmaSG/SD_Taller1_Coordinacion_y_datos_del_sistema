package usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/domain/model"
	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/ports"
)

// ProcessHeartbeatUseCase detecta la caída del coordinador y dispara una nueva
// elección. También valida los heartbeats recibidos por el servidor gRPC.
type ProcessHeartbeatUseCase struct {
	membership        ports.MembershipPort
	transport         ports.ElectionTransportPort
	election          *BullyElectionUseCase
	heartbeatInterval time.Duration
	electionTimeout   time.Duration
}

// NewProcessHeartbeatUseCase construye el caso de uso de detección de fallos.
func NewProcessHeartbeatUseCase(
	membership ports.MembershipPort,
	transport ports.ElectionTransportPort,
	election *BullyElectionUseCase,
	heartbeatInterval time.Duration,
	electionTimeout time.Duration,
) (*ProcessHeartbeatUseCase, error) {
	if membership == nil {
		return nil, errors.New("membership port must not be nil")
	}
	if transport == nil {
		return nil, errors.New("heartbeat transport port must not be nil")
	}
	if election == nil {
		return nil, errors.New("election use case must not be nil")
	}
	if heartbeatInterval <= 0 {
		return nil, errors.New("heartbeat interval must be greater than zero")
	}
	if electionTimeout <= 0 {
		return nil, errors.New("election timeout must be greater than zero")
	}

	return &ProcessHeartbeatUseCase{
		membership:        membership,
		transport:         transport,
		election:          election,
		heartbeatInterval: heartbeatInterval,
		electionTimeout:   electionTimeout,
	}, nil
}

// Run supervisa periódicamente al coordinador conocido. La cancelación del
// contexto detiene el monitor durante el apagado ordenado del proceso.
func (uc *ProcessHeartbeatUseCase) Run(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context must not be nil")
	}

	ticker := time.NewTicker(uc.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := uc.CheckOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
				return err
			}
		}
	}
}

// CheckOnce envía un heartbeat al coordinador actual. Si no responde, ejecuta
// Bully con un contexto nuevo para que la detección fallida no consuma el
// timeout de la elección posterior.
func (uc *ProcessHeartbeatUseCase) CheckOnce(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context must not be nil")
	}

	local := uc.membership.LocalNode()
	if err := local.Validate(); err != nil {
		return fmt.Errorf("invalid local node: %w", err)
	}

	result := uc.election.CurrentResult()
	if result.Coordinator == nil || result.Coordinator.SameIdentity(local) {
		return nil
	}

	heartbeatContext, cancelHeartbeat := context.WithTimeout(ctx, uc.electionTimeout)
	alive, err := uc.transport.SendHeartbeat(heartbeatContext, local, *result.Coordinator)
	cancelHeartbeat()
	if err == nil && alive {
		return nil
	}
	if err == nil {
		err = errors.New("coordinator did not acknowledge heartbeat")
	}

	log.Printf(
		"heartbeat failed coordinator=%d error=%v, starting re-election",
		result.Coordinator.ID,
		err,
	)

	if err := ctx.Err(); err != nil {
		return err
	}

	electionContext, cancelElection := context.WithTimeout(context.Background(), uc.electionTimeout)
	defer cancelElection()
	electionResult, electionErr := uc.election.Start(electionContext)
	if electionErr != nil {
		return fmt.Errorf("coordinator %d unavailable and re-election failed: %w", result.Coordinator.ID, electionErr)
	}

	if electionResult.Coordinator != nil {
		log.Printf(
			"heartbeat-triggered election state=%s coordinator=%d announcement_failures=%v",
			electionResult.State,
			electionResult.Coordinator.ID,
			electionResult.AnnouncementFailures,
		)
	}

	return nil
}

// HandleHeartbeat valida un heartbeat recibido. Solo el nodo que actualmente
// se considera coordinador confirma la solicitud.
func (uc *ProcessHeartbeatUseCase) HandleHeartbeat(
	sender model.Node,
	coordinator model.Node,
) (bool, error) {
	if err := sender.Validate(); err != nil {
		return false, fmt.Errorf("invalid heartbeat sender: %w", err)
	}
	if err := coordinator.Validate(); err != nil {
		return false, fmt.Errorf("invalid heartbeat coordinator: %w", err)
	}

	local := uc.membership.LocalNode()
	if err := local.Validate(); err != nil {
		return false, fmt.Errorf("invalid local node: %w", err)
	}
	if !isKnownNode(sender, local, uc.membership.Peers()) {
		return false, fmt.Errorf("heartbeat sender %d is not part of the membership", sender.ID)
	}
	if !local.SameIdentity(coordinator) {
		return false, nil
	}

	result := uc.election.CurrentResult()
	return result.Coordinator != nil && result.Coordinator.SameIdentity(local) &&
		result.Coordinator.SameIdentity(coordinator), nil
}
