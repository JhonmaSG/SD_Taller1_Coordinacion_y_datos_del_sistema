package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/adapters/config"
	grpcadapter "github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/adapters/grpc"
	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/adapters/mongodb"
	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/domain/usecase"
)

const (
	mongoStartupTimeout = 10 * time.Second
	mongoCloseTimeout   = 5 * time.Second
)

func main() {
	if err := run(); err != nil {
		log.Printf("node stopped with error: %v", err)
		os.Exit(1)
	}
}

func run() error {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.LUTC)

	// El archivo es opcional. Las variables de entorno ya definidas siempre
	// tienen prioridad sobre los valores del archivo.
	if err := config.LoadDotEnvFile(".env"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("load .env: %w", err)
	}

	cfg, err := config.LoadFromEnv()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	membership, err := config.NewStaticMembership(cfg.LocalNode, cfg.Peers)
	if err != nil {
		return fmt.Errorf("create membership: %w", err)
	}

	transport := grpcadapter.NewElectionTransport()
	election, err := usecase.NewBullyElectionUseCase(membership, transport)
	if err != nil {
		return fmt.Errorf("create election use case: %w", err)
	}

	// La conexión se inicializa aunque todavía no exista un caso de uso de
	// inventario. De esta forma el proceso falla temprano si el Replica Set no
	// está disponible y deja el repositorio listo para los siguientes casos de
	// uso.
	mongoContext, cancelMongo := context.WithTimeout(context.Background(), mongoStartupTimeout)
	repository, err := mongodb.NewResourceRepository(
		mongoContext,
		cfg.DatabaseURI,
		cfg.DatabaseName,
		cfg.ResourceTable,
	)
	cancelMongo()
	if err != nil {
		return fmt.Errorf("initialize MongoDB repository: %w", err)
	}
	defer func() {
		closeContext, cancelClose := context.WithTimeout(context.Background(), mongoCloseTimeout)
		defer cancelClose()
		if err := repository.Close(closeContext); err != nil {
			log.Printf("close MongoDB repository: %v", err)
		}
	}()

	electionServer, err := grpcadapter.NewElectionServer(election)
	if err != nil {
		return fmt.Errorf("create gRPC server adapter: %w", err)
	}

	listener, err := net.Listen("tcp", cfg.ListenAddress)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.ListenAddress, err)
	}
	defer listener.Close()

	grpcServer := electionServer.NewGRPCServer()
	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("gRPC server listening on %s", cfg.ListenAddress)
		serverErrors <- grpcServer.Serve(listener)
	}()

	if err := startElection(election, cfg.ElectionTimeout); err != nil {
		return err
	}

	log.Printf(
		"node %d started with %d peer(s), MongoDB database=%s collection=%s",
		cfg.LocalNode.ID,
		len(cfg.Peers),
		cfg.DatabaseName,
		cfg.ResourceTable,
	)

	return waitForShutdown(grpcServer, serverErrors)
}

func startElection(election *usecase.BullyElectionUseCase, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	result, err := election.Start(ctx)
	if err != nil {
		return fmt.Errorf("start Bully election: %w", err)
	}

	if result.Coordinator == nil {
		log.Printf(
			"election state=%s higher_priority=%d acknowledgements=%d",
			result.State,
			result.HigherPriorityNodes,
			result.AcknowledgedCandidates,
		)
		return nil
	}

	log.Printf(
		"election state=%s coordinator=%d address=%s announcement_failures=%v",
		result.State,
		result.Coordinator.ID,
		result.Coordinator.Address,
		result.AnnouncementFailures,
	)
	return nil
}

func waitForShutdown(grpcServer interface{ GracefulStop() }, serverErrors <-chan error) error {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)

	select {
	case err := <-serverErrors:
		if err != nil {
			return fmt.Errorf("gRPC server: %w", err)
		}
		return nil
	case received := <-signals:
		log.Printf("received %s, stopping gracefully", received)
		grpcServer.GracefulStop()
		return nil
	}
}
