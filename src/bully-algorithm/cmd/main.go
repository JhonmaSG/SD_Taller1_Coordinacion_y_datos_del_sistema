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
	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/adapters/persistence"
	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/adapters/system"
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
	election, err := usecase.NewBullyElectionUseCaseWithTimeout(
		membership,
		transport,
		cfg.ElectionTimeout,
	)
	if err != nil {
		return fmt.Errorf("create election use case: %w", err)
	}

	heartbeat, err := usecase.NewProcessHeartbeatUseCase(
		membership,
		transport,
		election,
		cfg.HeartbeatInterval,
		cfg.ElectionTimeout,
	)
	if err != nil {
		return fmt.Errorf("create heartbeat use case: %w", err)
	}

	electionServer, err := grpcadapter.NewElectionServer(election, heartbeat)
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

	// El monitor de heartbeats detecta la caída del coordinador y dispara una
	// nueva elección sin depender de que el proceso reciba tráfico entrante.
	runtimeContext, stopRuntime := context.WithCancel(context.Background())
	defer stopRuntime()
	go func() {
		if err := heartbeat.Run(runtimeContext); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("heartbeat monitor stopped: %v", err)
		}
	}()

	// MongoDB pertenece al plano de datos. Se intenta inicializar después de la
	// elección y su indisponibilidad deja al nodo en modo degradado, sin impedir
	// que participe en Bully o atienda mensajes de otros nodos.
	repository := initializeRepository(cfg)
	if repository != nil {
		defer closeRepository(repository)
		registerNode, registerErr := usecase.NewRegisterNodeUseCase(
			membership,
			election,
			repository,
			system.NewMachineCapacityReader(),
		)
		if registerErr != nil {
			log.Printf("node registration unavailable: %v", registerErr)
		} else {
			registerContext, cancelRegister := context.WithTimeout(context.Background(), mongoStartupTimeout)
			doc, registerErr := registerNode.Register(registerContext)
			cancelRegister()
			if registerErr != nil {
				log.Printf("node document registration failed: %v", registerErr)
			} else {
				log.Printf("machine capacity registered cpu_cores=%d memory_gb=%d bandwidth_mbps=%d", doc.Resources.CPUCores, doc.Resources.MemoryGB, doc.Resources.BandwidthMbps)
				go func() {
					if err := registerNode.Run(runtimeContext, cfg.HeartbeatInterval); err != nil && !errors.Is(err, context.Canceled) {
						log.Printf("node document role monitor stopped: %v", err)
					}
				}()
				defer func() {
					offlineContext, cancelOffline := context.WithTimeout(context.Background(), mongoCloseTimeout)
					defer cancelOffline()
					if err := registerNode.MarkOffline(offlineContext); err != nil {
						log.Printf("mark node offline: %v", err)
					}
				}()
			}
		}
	}

	log.Printf(
		"node %d started with %d peer(s), MongoDB database=%s collection=%s heartbeat=%s",
		cfg.LocalNode.ID,
		len(cfg.Peers),
		cfg.DatabaseName,
		cfg.NodeCollection,
		cfg.HeartbeatInterval,
	)

	return waitForShutdown(grpcServer, serverErrors, stopRuntime)
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

func initializeRepository(cfg config.Config) *persistence.MongoNodeDocumentRepository {
	mongoContext, cancelMongo := context.WithTimeout(context.Background(), mongoStartupTimeout)
	defer cancelMongo()

	repository, err := persistence.NewMongoNodeDocumentRepository(
		mongoContext,
		cfg.DatabaseURI,
		cfg.DatabaseName,
		cfg.NodeCollection,
	)
	if err != nil {
		log.Printf(
			"MongoDB unavailable, node continues in degraded mode; inventory operations are disabled: %v",
			err,
		)
		return nil
	}

	log.Printf("MongoDB connection ready for %s.%s", cfg.DatabaseName, cfg.NodeCollection)
	return repository
}

func closeRepository(repository *persistence.MongoNodeDocumentRepository) {
	closeContext, cancelClose := context.WithTimeout(context.Background(), mongoCloseTimeout)
	defer cancelClose()

	if err := repository.Close(closeContext); err != nil {
		log.Printf("close MongoDB repository: %v", err)
	}
}

func waitForShutdown(
	grpcServer interface{ GracefulStop() },
	serverErrors <-chan error,
	stopRuntime context.CancelFunc,
) error {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)

	select {
	case err := <-serverErrors:
		stopRuntime()
		if err != nil {
			return fmt.Errorf("gRPC server: %w", err)
		}
		return nil
	case received := <-signals:
		log.Printf("received %s, stopping gracefully", received)
		stopRuntime()
		grpcServer.GracefulStop()
		return nil
	}
}
