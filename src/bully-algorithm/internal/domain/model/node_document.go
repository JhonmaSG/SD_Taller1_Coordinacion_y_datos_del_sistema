package model

import "errors"

// NodeRole representa el rol de aplicación persistido para un nodo.
type NodeRole string

const (
	RoleCoordinator NodeRole = "COORDINATOR"
	RoleAgent       NodeRole = "AGENT"
)

// NodeStatus representa la disponibilidad lógica del nodo.
type NodeStatus string

const (
	StatusOnline  NodeStatus = "ONLINE"
	StatusOffline NodeStatus = "OFFLINE"
)

// NodeResources agrupa la capacidad física detectada en el host.
type NodeResources struct {
	CPUCores      int64
	MemoryGB      int64
	BandwidthMbps int64
}

// NodeDocument contiene únicamente rol, estado y capacidad. La metadata de
// concurrencia se persiste en el documento global cluster_metadata.
type NodeDocument struct {
	ID        NodeID
	Role      NodeRole
	Resources NodeResources
	Status    NodeStatus
}

func NewNodeDocument(nodeID NodeID) NodeDocument {
	return NodeDocument{ID: nodeID, Role: RoleAgent, Status: StatusOnline}
}

func (d NodeDocument) Validate() error {
	if d.ID == 0 {
		return errors.New("node document id must be greater than zero")
	}
	if d.Role != RoleCoordinator && d.Role != RoleAgent {
		return errors.New("node document role is invalid")
	}
	if d.Status != StatusOnline && d.Status != StatusOffline {
		return errors.New("node document status is invalid")
	}
	return d.Resources.Validate()
}

func (r NodeResources) Validate() error {
	if r.CPUCores < 0 || r.MemoryGB < 0 || r.BandwidthMbps < 0 {
		return errors.New("node resources cannot be negative")
	}
	return nil
}

func (d NodeDocument) Clone() NodeDocument { return d }
