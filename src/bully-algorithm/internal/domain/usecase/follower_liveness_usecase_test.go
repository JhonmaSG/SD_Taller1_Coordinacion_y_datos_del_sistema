package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/domain/model"
	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/ports"
)

type livenessMembershipFake struct{}

func (livenessMembershipFake) LocalNode() model.Node { return model.Node{ID: 3} }
func (livenessMembershipFake) Peers() []model.Node   { return []model.Node{{ID: 1}} }

type livenessCoordinatorFake struct{}

func (livenessCoordinatorFake) IsLocalCoordinator() bool { return true }
func (livenessCoordinatorFake) CurrentCoordinator() (model.Node, bool) {
	return model.Node{ID: 3}, true
}

type livenessRegistrationFake struct{ calls int }

func (r *livenessRegistrationFake) MarkFollowerOffline(context.Context, model.NodeID) error {
	r.calls++
	return nil
}

func TestFollowerLivenessMarksStaleFollowerOnce(t *testing.T) {
	registration := &livenessRegistrationFake{}
	uc, err := NewFollowerLivenessUseCase(livenessMembershipFake{}, livenessCoordinatorFake{}, registration, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	uc.now = func() time.Time { return now }
	if err := uc.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	if err := uc.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := uc.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if registration.calls != 1 {
		t.Fatalf("calls = %d, want 1", registration.calls)
	}
}

var _ ports.CoordinatorView = livenessCoordinatorFake{}
