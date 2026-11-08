package model

import "errors"

// NodeRole representa el rol de aplicación del nodo en el documento persistido.
type NodeRole string

const (
	RoleCoordinator NodeRole = "COORDINATOR"
	RoleFollower    NodeRole = "FOLLOWER"
)

// NodeStatus representa la disponibilidad lógica del nodo.
type NodeStatus string

const (
	StatusOnline  NodeStatus = "ONLINE"
	StatusOffline NodeStatus = "OFFLINE"
)

// NodeResources agrupa el inventario administrado por un nodo.
type NodeResources struct {
	CPUCores      int64
	MemoryGB      int64
	BandwidthMbps int64
}

// NodeDocument es el agregado de recursos y rol del nodo. La metadata que
// acompaña al agregado en memoria representa el estado global del cluster; en
// MongoDB se persiste por separado en _id=cluster_metadata.
type NodeDocument struct {
	ID        NodeID
	Role      NodeRole
	Resources NodeResources
	Status    NodeStatus
	Metadata  Metadata
}

// NewNodeDocument crea el estado inicial de un nodo online.
func NewNodeDocument(nodeID NodeID) NodeDocument {
	return NodeDocument{
		ID:        nodeID,
		Role:      RoleFollower,
		Status:    StatusOnline,
		Metadata:  NewMetadata(nodeID, 0),
		Resources: NodeResources{},
	}
}

// Validate comprueba invariantes de persistencia y evita cantidades negativas.
func (d NodeDocument) Validate() error {
	if d.ID == 0 {
		return errors.New("node document id must be greater than zero")
	}
	if d.Role != RoleCoordinator && d.Role != RoleFollower {
		return errors.New("node document role is invalid")
	}
	if d.Status != StatusOnline && d.Status != StatusOffline {
		return errors.New("node document status is invalid")
	}
	if d.Resources.CPUCores < 0 || d.Resources.MemoryGB < 0 || d.Resources.BandwidthMbps < 0 {
		return errors.New("node resources cannot be negative")
	}

	return nil
}

// Clone copia el agregado y sus mapas mutables.
func (d NodeDocument) Clone() NodeDocument {
	d.Metadata = d.Metadata.Clone()
	return d
}

// ConflictsWith detecta versiones concurrentes del mismo nodo.
func (d NodeDocument) ConflictsWith(other NodeDocument) bool {
	return d.ID == other.ID && d.Metadata.CompareVersion(other.Metadata) == VersionConcurrent
}

// ResolveNodeDocumentConflict aplica causalidad, Lamport y desempate por ID
// menor. El vector resultante conserva la historia de ambas versiones.
func ResolveNodeDocumentConflict(left, right NodeDocument) (NodeDocument, error) {
	if err := left.Validate(); err != nil {
		return NodeDocument{}, err
	}
	if err := right.Validate(); err != nil {
		return NodeDocument{}, err
	}
	if left.ID != right.ID {
		return NodeDocument{}, errors.New("cannot resolve node documents with different ids")
	}

	winner := left
	switch left.Metadata.CompareVersion(right.Metadata) {
	case VersionBefore:
		winner = right
	case VersionAfter:
		winner = left
	case VersionConcurrent, VersionEqual:
		if right.Metadata.Lamport < left.Metadata.Lamport ||
			(right.Metadata.Lamport == left.Metadata.Lamport && right.Metadata.UpdatedBy < left.Metadata.UpdatedBy) {
			winner = right
		}
	}

	winner = winner.Clone()
	winner.Metadata.Lamport = maxLamport(left.Metadata.Lamport, right.Metadata.Lamport)
	winner.Metadata.VersionVector = left.Metadata.VersionVector.Merge(right.Metadata.VersionVector)
	return winner, nil
}
