package grpcadapter

import (
	"context"
	"errors"
	"fmt"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	electionpb "github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/election/v1"
	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/domain/usecase"
)

// ElectionServer es el adaptador de entrada gRPC. Convierte mensajes de
// transporte a modelos de dominio y delega toda la decisión al caso de uso.
type ElectionServer struct {
	electionpb.UnimplementedElectionServiceServer
	useCase *usecase.BullyElectionUseCase
}

// NewElectionServer crea un servidor gRPC conectado al caso de uso de elección.
func NewElectionServer(useCase *usecase.BullyElectionUseCase) (*ElectionServer, error) {
	if useCase == nil {
		return nil, errors.New("bully election use case must not be nil")
	}

	return &ElectionServer{useCase: useCase}, nil
}

// Register registra el servicio en una instancia gRPC gestionada por el
// proceso ejecutable.
func (s *ElectionServer) Register(registrar grpc.ServiceRegistrar) {
	electionpb.RegisterElectionServiceServer(registrar, s)
}

// NewGRPCServer crea una instancia registrable y controlable por el proceso
// principal. Esto permite ejecutar GracefulStop durante el apagado.
func (s *ElectionServer) NewGRPCServer() *grpc.Server {
	grpcServer := grpc.NewServer()
	s.Register(grpcServer)
	return grpcServer
}

// Serve inicia el servidor sobre un listener ya configurado por el adaptador
// de infraestructura.
func (s *ElectionServer) Serve(listener net.Listener) error {
	if listener == nil {
		return errors.New("listener must not be nil")
	}

	return s.NewGRPCServer().Serve(listener)
}

// RequestElection procesa ELECTION y devuelve OK únicamente si el nodo local
// tiene mayor prioridad que el candidato.
func (s *ElectionServer) RequestElection(
	ctx context.Context,
	request *electionpb.RequestElectionRequest,
) (*electionpb.RequestElectionResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "election request must not be nil")
	}

	candidate, err := fromProtoNode(request.GetSender())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid election sender: %v", err)
	}

	acknowledged, err := s.useCase.HandleElection(ctx, candidate)
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "%v", err)
	}

	return &electionpb.RequestElectionResponse{Acknowledged: acknowledged}, nil
}

// AnnounceCoordinator procesa COORDINATOR y actualiza el estado del caso de
// uso local.
func (s *ElectionServer) AnnounceCoordinator(
	_ context.Context,
	announcement *electionpb.AnnounceCoordinatorRequest,
) (*electionpb.AnnounceCoordinatorResponse, error) {
	if announcement == nil {
		return nil, status.Error(codes.InvalidArgument, "coordinator announcement must not be nil")
	}
	if _, err := fromProtoNode(announcement.GetSender()); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid announcement sender: %v", err)
	}
	coordinator, err := fromProtoNode(announcement.GetCoordinator())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid coordinator: %v", err)
	}

	if _, err := s.useCase.HandleCoordinator(coordinator); err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "%v", err)
	}

	return &electionpb.AnnounceCoordinatorResponse{Accepted: true}, nil
}

// ListenAndServe es una utilidad para la entrada principal cuando solo se
// dispone de una dirección TCP.
func (s *ElectionServer) ListenAndServe(address string) error {
	if address == "" {
		return errors.New("server address must not be empty")
	}

	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", address, err)
	}

	return s.Serve(listener)
}
