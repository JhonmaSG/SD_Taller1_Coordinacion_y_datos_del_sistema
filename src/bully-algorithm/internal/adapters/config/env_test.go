package config

import (
	"testing"

	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/domain/model"
)

func TestStaticMembershipRejectsLocalPeerAndDuplicates(t *testing.T) {
	local := model.Node{ID: 1, Address: "127.0.0.1:5001"}
	if _, err := NewStaticMembership(local, []model.Node{{ID: 1, Address: "127.0.0.1:5001"}}); err == nil {
		t.Fatal("expected local peer rejection")
	}
	if _, err := NewStaticMembership(local, []model.Node{{ID: 2}, {ID: 2}}); err == nil {
		t.Fatal("expected duplicate peer rejection")
	}
}
