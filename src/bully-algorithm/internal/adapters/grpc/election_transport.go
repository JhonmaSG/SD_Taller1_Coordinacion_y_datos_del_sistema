package grpcadapter

import (
	"context"
	"errors"
	"fmt"
	"log"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	electionpb "github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/election/v1"
	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/domain/model"
)

// ElectionTransport es el adaptador de salida gRPC para el algoritmo Bully.
// Abre una conexión por llamada para respetar el endpoint actual de cada peer y
// delega el timeout al context.Context entregado por el caso de uso.
type ElectionTransport struct {
	dialOptions []grpc.DialOption
}

// NewElectionTransport crea un cliente gRPC sin TLS para una red privada como
// Tailscale. En producción se pueden pasar opciones adicionales, por ejemplo
// credenciales TLS, mediante grpc.DialOption.
func NewElectionTransport(options ...grpc.DialOption) *ElectionTransport {
	dialOptions := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}
	dialOptions = append(dialOptions, options...)

	return &ElectionTransport{dialOptions: dialOptions}
}

// RequestElection implementa ports.ElectionTransportPort.
func (t *ElectionTransport) RequestElection(
	ctx context.Context,
	from, to model.Node,
) (bool, error) {
	if ctx == nil {
		return false, errors.New("context must not be nil")
	}
	if err := from.Validate(); err != nil {
		return false, fmt.Errorf("invalid sender: %w", err)
	}
	if err := validatePeer(to); err != nil {
		return false, err
	}

	conn, err := t.dial(ctx, to.Address)
	if err != nil {
		return false, err
	}
	defer conn.Close()

	response, err := electionpb.NewElectionServiceClient(conn).RequestElection(ctx, &electionpb.RequestElectionRequest{
		Sender: toProtoNode(from),
	}, grpc.WaitForReady(true))
	if err != nil {
		return false, err
	}

	return response.GetAcknowledged(), nil
}

// AnnounceCoordinator implementa ports.ElectionTransportPort.
func (t *ElectionTransport) AnnounceCoordinator(
	ctx context.Context,
	from, to, coordinator model.Node,
) error {
	if ctx == nil {
		return errors.New("context must not be nil")
	}
	if err := from.Validate(); err != nil {
		return fmt.Errorf("invalid sender: %w", err)
	}
	if err := coordinator.Validate(); err != nil {
		return fmt.Errorf("invalid coordinator: %w", err)
	}
	if err := validatePeer(to); err != nil {
		return err
	}

	conn, err := t.dial(ctx, to.Address)
	if err != nil {
		log.Printf("COORDINATOR failed sender=%d coordinator=%d target=%d address=%s phase=dial error=%v", from.ID, coordinator.ID, to.ID, to.Address, err)
		return err
	}
	defer conn.Close()

	response, err := electionpb.NewElectionServiceClient(conn).AnnounceCoordinator(ctx, &electionpb.AnnounceCoordinatorRequest{
		Sender:      toProtoNode(from),
		Coordinator: toProtoNode(coordinator),
	}, grpc.WaitForReady(true))
	if err != nil {
		log.Printf("COORDINATOR failed sender=%d coordinator=%d target=%d address=%s phase=rpc error=%v", from.ID, coordinator.ID, to.ID, to.Address, err)
		return err
	}
	if !response.GetAccepted() {
		err := errors.New("coordinator announcement was rejected")
		log.Printf("COORDINATOR failed sender=%d coordinator=%d target=%d address=%s phase=response error=%v", from.ID, coordinator.ID, to.ID, to.Address, err)
		return err
	}

	log.Printf("COORDINATOR sent sender=%d coordinator=%d target=%d address=%s", from.ID, coordinator.ID, to.ID, to.Address)
	return nil
}

func (t *ElectionTransport) dial(ctx context.Context, address string) (*grpc.ClientConn, error) {
	if len(t.dialOptions) == 0 {
		return nil, errors.New("gRPC transport has no dial options")
	}

	options := append([]grpc.DialOption{}, t.dialOptions...)
	options = append(options, grpc.WithBlock())
	return grpc.DialContext(ctx, address, options...)
}

func validatePeer(peer model.Node) error {
	if err := peer.Validate(); err != nil {
		return fmt.Errorf("invalid peer: %w", err)
	}
	if peer.Address == "" {
		return fmt.Errorf("peer %d has an empty address", peer.ID)
	}

	return nil
}

func toProtoNode(node model.Node) *electionpb.Node {
	return &electionpb.Node{Id: uint64(node.ID), Address: node.Address}
}

func fromProtoNode(node *electionpb.Node) (model.Node, error) {
	if node == nil {
		return model.Node{}, errors.New("node message must not be nil")
	}

	converted := model.Node{ID: model.NodeID(node.GetId()), Address: node.GetAddress()}
	if err := converted.Validate(); err != nil {
		return model.Node{}, err
	}

	return converted, nil
}

// SendHeartbeat implementa ports.ElectionTransportPort.
func (t *ElectionTransport) SendHeartbeat(
	ctx context.Context,
	from, coordinator model.Node,
) (bool, error) {
	if ctx == nil {
		return false, errors.New("context must not be nil")
	}
	if err := from.Validate(); err != nil {
		return false, fmt.Errorf("invalid heartbeat sender: %w", err)
	}
	if err := validatePeer(coordinator); err != nil {
		return false, err
	}

	conn, err := t.dial(ctx, coordinator.Address)
	if err != nil {
		return false, fmt.Errorf("heartbeat dial to node %d: %w", coordinator.ID, err)
	}
	defer conn.Close()

	response, err := electionpb.NewElectionServiceClient(conn).ProcessHeartbeat(ctx, &electionpb.ProcessHeartbeatRequest{
		Sender:      toProtoNode(from),
		Coordinator: toProtoNode(coordinator),
	}, grpc.WaitForReady(true))
	if err != nil {
		return false, fmt.Errorf("heartbeat RPC to node %d: %w", coordinator.ID, err)
	}

	return response.GetAlive(), nil
}
