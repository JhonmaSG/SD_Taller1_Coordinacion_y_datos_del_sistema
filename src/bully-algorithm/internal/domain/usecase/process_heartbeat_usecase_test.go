package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/domain/model"
)

func TestProcessHeartbeatHandleHeartbeatAcceptsCurrentCoordinator(t *testing.T) {
	transport := &electionTransportFake{election: map[model.NodeID]bool{}}
	membership := electionMembershipFake{local: model.Node{ID: 1}, peers: []model.Node{{ID: 2}}}
	election, err := NewBullyElectionUseCaseWithTimeout(membership, transport, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := election.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	uc, err := NewProcessHeartbeatUseCase(membership, transport, election, time.Millisecond, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	alive, err := uc.HandleHeartbeat(model.Node{ID: 2}, model.Node{ID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !alive {
		t.Fatal("expected heartbeat to be accepted")
	}
}

func TestProcessHeartbeatRejectsNonCoordinatorAndUnknownSender(t *testing.T) {
	transport := &electionTransportFake{election: map[model.NodeID]bool{}}
	membership := electionMembershipFake{local: model.Node{ID: 1}, peers: []model.Node{{ID: 2}}}
	election, err := NewBullyElectionUseCaseWithTimeout(membership, transport, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := election.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	uc, err := NewProcessHeartbeatUseCase(membership, transport, election, time.Millisecond, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	alive, err := uc.HandleHeartbeat(model.Node{ID: 2}, model.Node{ID: 2})
	if err != nil {
		t.Fatal(err)
	}
	if alive {
		t.Fatal("expected non-coordinator heartbeat to be rejected")
	}
	if _, err := uc.HandleHeartbeat(model.Node{ID: 9}, model.Node{ID: 1}); err == nil {
		t.Fatal("expected unknown sender error")
	}
}

func TestProcessHeartbeatCheckOnceWithoutCoordinatorDoesNothing(t *testing.T) {
	transport := &electionTransportFake{election: map[model.NodeID]bool{}}
	membership := electionMembershipFake{local: model.Node{ID: 1}, peers: []model.Node{{ID: 2}}}
	election, err := NewBullyElectionUseCaseWithTimeout(membership, transport, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	uc, err := NewProcessHeartbeatUseCase(membership, transport, election, time.Millisecond, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := uc.CheckOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
}
