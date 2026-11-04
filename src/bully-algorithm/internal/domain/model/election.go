package model

// ElectionState es el estado observable del caso de uso de elección.
type ElectionState string

const (
	ElectionIdle                  ElectionState = "idle"
	ElectionInProgress            ElectionState = "in_progress"
	ElectionWaitingForCoordinator ElectionState = "waiting_for_coordinator"
	ElectionFollowing             ElectionState = "following"
	ElectionCoordinator           ElectionState = "coordinator"
)

// ElectionResult resume el resultado de una ronda de Bully sin exponer detalles
// del transporte utilizado para ejecutarla.
type ElectionResult struct {
	State                  ElectionState
	Coordinator            *Node
	HigherPriorityNodes    int
	AcknowledgedCandidates int
	AnnouncementFailures   []NodeID
}

// Clone devuelve un resultado seguro para entregar fuera del caso de uso.
func (r ElectionResult) Clone() ElectionResult {
	if r.Coordinator != nil {
		coordinator := *r.Coordinator
		r.Coordinator = &coordinator
	}
	r.AnnouncementFailures = append([]NodeID(nil), r.AnnouncementFailures...)
	return r
}
