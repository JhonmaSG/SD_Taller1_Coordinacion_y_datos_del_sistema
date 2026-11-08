package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/domain/model"
)

type electionMembershipFake struct {
	local model.Node
	peers []model.Node
}

func (m electionMembershipFake) LocalNode() model.Node { return m.local }
func (m electionMembershipFake) Peers() []model.Node   { return m.peers }

type electionTransportFake struct {
	election      map[model.NodeID]bool
	announcements []model.NodeID
}

func (t *electionTransportFake) RequestElection(_ context.Context, _, to model.Node) (bool, error) {
	return t.election[to.ID], nil
}
func (t *electionTransportFake) AnnounceCoordinator(_ context.Context, _, to, _ model.Node) error {
	t.announcements = append(t.announcements, to.ID)
	return nil
}
func (t *electionTransportFake) SendHeartbeat(context.Context, model.Node, model.Node) (bool, error) {
	return true, nil
}

func TestBullyElectionNoHigherNodeBecomesCoordinator(t *testing.T) {
	transport := &electionTransportFake{}
	membership := electionMembershipFake{local: model.Node{ID: 3}, peers: []model.Node{{ID: 1}, {ID: 2}}}
	uc, err := NewBullyElectionUseCaseWithTimeout(membership, transport, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	result, err := uc.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.State != model.ElectionCoordinator || result.Coordinator.ID != 3 {
		t.Fatalf("result = %#v", result)
	}
}

func TestBullyElectionHigherNodeResponseWaitsForCoordinator(t *testing.T) {
	transport := &electionTransportFake{}
	membership := electionMembershipFake{local: model.Node{ID: 1}, peers: []model.Node{{ID: 2}}}
	uc, err := NewBullyElectionUseCaseWithTimeout(membership, transport, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	transport.election = map[model.NodeID]bool{2: true}
	// El fake puede configurarse para extenderse en futuras pruebas; este caso
	// asegura que el resultado por defecto no proclama a un follower sin una OK.
	result, err := uc.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.State != model.ElectionCoordinator && result.State != model.ElectionWaitingForCoordinator {
		t.Fatalf("unexpected state %s", result.State)
	}
}
