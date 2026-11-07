package model

// Metadata contiene la información necesaria para ordenar y reconciliar una
// versión replicada de un recurso.
type Metadata struct {
	Lamport       LamportTime
	VersionVector VersionVector
	UpdatedBy     NodeID
}

// NewMetadata crea metadatos con la primera versión producida por un nodo.
func NewMetadata(nodeID NodeID, lamport LamportTime) Metadata {
	return Metadata{
		Lamport:       lamport,
		VersionVector: VersionVector{nodeID.Key(): 1},
		UpdatedBy:     nodeID,
	}
}

// Clone devuelve una copia independiente de los metadatos.
func (m Metadata) Clone() Metadata {
	m.VersionVector = m.VersionVector.Clone()
	return m
}

// CompareVersion compara únicamente los vectores de versión.
func (m Metadata) CompareVersion(other Metadata) VersionRelation {
	return m.VersionVector.Compare(other.VersionVector)
}
