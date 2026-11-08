package grpcadapter

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	electionpb "github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/election/v1"
	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/domain/model"
	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/domain/usecase"
)

// ElectionServer es el adaptador de entrada gRPC. Convierte mensajes de
// transporte a modelos de dominio y delega toda la decisión al caso de uso.
type ElectionServer struct {
	electionpb.UnimplementedElectionServiceServer
	electionUseCase     *usecase.BullyElectionUseCase
	heartbeatUseCase    *usecase.ProcessHeartbeatUseCase
	registrationUseCase *usecase.RegisterNodeUseCase
	livenessUseCase     *usecase.FollowerLivenessUseCase
}

// SetFollowerLiveness conecta el registro de latidos de followers. Solo tiene
// efecto mientras el nodo local es coordinador.
func (s *ElectionServer) SetFollowerLiveness(liveness *usecase.FollowerLivenessUseCase) {
	s.livenessUseCase = liveness
}

// NewElectionServer crea un servidor gRPC conectado a los casos de uso de
// elección y heartbeat.
func NewElectionServer(
	electionUseCase *usecase.BullyElectionUseCase,
	heartbeatUseCase *usecase.ProcessHeartbeatUseCase,
) (*ElectionServer, error) {
	if electionUseCase == nil {
		return nil, errors.New("bully election use case must not be nil")
	}
	if heartbeatUseCase == nil {
		return nil, errors.New("heartbeat use case must not be nil")
	}

	return &ElectionServer{
		electionUseCase:  electionUseCase,
		heartbeatUseCase: heartbeatUseCase,
	}, nil
}

// SetRegistrationUseCase conecta el caso de uso cuando MongoDB está disponible.
func (s *ElectionServer) SetRegistrationUseCase(registrationUseCase *usecase.RegisterNodeUseCase) {
	s.registrationUseCase = registrationUseCase
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

	acknowledged, err := s.electionUseCase.HandleElection(ctx, candidate)
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "%v", err)
	}

	return &electionpb.RequestElectionResponse{Acknowledged: acknowledged}, nil
}

// AnnounceCoordinator procesa COORDINATOR y actualiza el estado del caso de
// uso local.
func (s *ElectionServer) AnnounceCoordinator(
	ctx context.Context,
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

	result, err := s.electionUseCase.HandleCoordinator(coordinator)
	if err != nil {
		log.Printf(
			"COORDINATOR rejected sender=%d coordinator=%d error=%v",
			announcement.GetSender().GetId(),
			coordinator.ID,
			err,
		)
		return nil, status.Errorf(codes.FailedPrecondition, "%v", err)
	}

	log.Printf(
		"COORDINATOR received sender=%d coordinator=%d state=%s",
		announcement.GetSender().GetId(),
		coordinator.ID,
		result.State,
	)

	if s.registrationUseCase != nil {
		registered, registrationErr := s.registrationUseCase.Register(ctx)
		if registrationErr != nil {
			log.Printf("follower registration after coordinator change failed: %v", registrationErr)
		} else if registered.ID != 0 {
			log.Printf("node document synchronized after coordinator change node=%d role=%s", registered.ID, registered.Role)
		}
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

// ProcessHeartbeat confirma si el nodo local sigue siendo el coordinador que
// el emisor está supervisando.
func (s *ElectionServer) ProcessHeartbeat(
	_ context.Context,
	request *electionpb.ProcessHeartbeatRequest,
) (*electionpb.ProcessHeartbeatResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "heartbeat request must not be nil")
	}

	sender, err := fromProtoNode(request.GetSender())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid heartbeat sender: %v", err)
	}
	coordinator, err := fromProtoNode(request.GetCoordinator())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid heartbeat coordinator: %v", err)
	}

	alive, err := s.heartbeatUseCase.HandleHeartbeat(sender, coordinator)
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "%v", err)
	}
	if alive && s.livenessUseCase != nil {
		s.livenessUseCase.RecordHeartbeat(sender.ID)
	}

	return &electionpb.ProcessHeartbeatResponse{Alive: alive}, nil
}

// RegisterNode recibe la capacidad de un follower y permite que únicamente el
// coordinador persista su documento.
func (s *ElectionServer) RegisterNode(
	_ context.Context,
	request *electionpb.RegisterNodeRequest,
) (*electionpb.RegisterNodeResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "register node request must not be nil")
	}
	sender, err := fromProtoNode(request.GetSender())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid registration sender: %v", err)
	}
	resources := model.NodeResources{CPUCores: request.GetResources().GetCpuCores(), MemoryGB: request.GetResources().GetMemoryGb(), BandwidthMbps: request.GetResources().GetBandwidthMbps()}
	metadata, err := fromProtoMetadata(request.GetMetadata())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid registration metadata: %v", err)
	}

	if s.registrationUseCase == nil {
		return nil, status.Error(codes.Unavailable, "node registration persistence is unavailable")
	}

	document, err := s.registrationUseCase.RegisterRemote(context.Background(), sender, resources, metadata)
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "%v", err)
	}

	return &electionpb.RegisterNodeResponse{Accepted: true, Document: toProtoDocument(document)}, nil
}

func fromProtoMetadata(metadata *electionpb.NodeMetadata) (model.Metadata, error) {
	if metadata == nil {
		return model.Metadata{}, errors.New("metadata message must not be nil")
	}
	updatedBy, err := parseNodeKey(metadata.GetLastModifiedBy())
	if err != nil {
		return model.Metadata{}, err
	}
	return model.Metadata{Lamport: model.LamportTime(metadata.GetLamportClock()), VersionVector: metadata.GetVersionVector(), UpdatedBy: updatedBy}, nil
}

func toProtoDocument(document model.NodeDocument) *electionpb.NodeDocument {
	return &electionpb.NodeDocument{
		Node:      &electionpb.Node{Id: uint64(document.ID)},
		NodeRole:  string(document.Role),
		Resources: &electionpb.NodeResources{CpuCores: document.Resources.CPUCores, MemoryGb: document.Resources.MemoryGB, BandwidthMbps: document.Resources.BandwidthMbps},
		Status:    string(document.Status),
		Metadata:  &electionpb.NodeMetadata{LamportClock: uint64(document.Metadata.Lamport), VersionVector: document.Metadata.VersionVector, LastModifiedBy: document.Metadata.UpdatedBy.Key()},
	}
}
