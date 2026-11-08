package persistence

import (
	"testing"

	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/domain/model"
)

func TestClusterFilterUsesIndividualVectorCounters(t *testing.T) {
	metadata := model.ClusterMetadata{GlobalLamportClock: 4, ActiveCoordinator: 2, LastModifiedBy: 2, VersionVector: model.VersionVector{"node_1": 1, "node_2": 2}}
	filter := clusterFilter(metadata)
	if filter["version_vector.node_1"] != uint64(1) || filter["version_vector.node_2"] != uint64(2) {
		t.Fatalf("filter = %#v", filter)
	}
}
