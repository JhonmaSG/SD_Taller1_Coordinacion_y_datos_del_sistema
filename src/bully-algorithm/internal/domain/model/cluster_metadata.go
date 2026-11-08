package model

// ClusterMetadata representa la metadata global del cluster. Se persiste en el
// documento con _id=cluster_metadata, separado de los documentos de recursos
// individuales de cada nodo.
type ClusterMetadata struct {
	GlobalLamportClock LamportTime
	VersionVector      VersionVector
	ActiveCoordinator  NodeID
	LastModifiedBy     NodeID
}

func (m ClusterMetadata) Clone() ClusterMetadata {
	m.VersionVector = m.VersionVector.Clone()
	return m
}

// AsMetadata adapta la metadata global al valor que usan los agregados en
// memoria antes de persistir una operación conjunta.
func (m ClusterMetadata) AsMetadata() Metadata {
	return Metadata{Lamport: m.GlobalLamportClock, VersionVector: m.VersionVector.Clone(), UpdatedBy: m.LastModifiedBy}
}
