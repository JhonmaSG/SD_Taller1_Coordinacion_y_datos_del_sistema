package model

// VersionVector relaciona cada nodo con el contador de versiones que ha
// generado para un recurso.
type VersionVector map[NodeID]uint64

// VersionRelation describe la relación parcial entre dos vectores.
type VersionRelation uint8

const (
	VersionEqual VersionRelation = iota
	VersionBefore
	VersionAfter
	VersionConcurrent
)

// Clone devuelve una copia independiente del vector.
func (v VersionVector) Clone() VersionVector {
	clone := make(VersionVector, len(v))
	for nodeID, counter := range v {
		clone[nodeID] = counter
	}

	return clone
}

// Increment incrementa el contador del nodo que produjo una nueva versión.
func (v VersionVector) Increment(nodeID NodeID) {
	if v == nil {
		return
	}

	v[nodeID]++
}

// Merge combina dos vectores tomando el contador máximo de cada nodo.
func (v VersionVector) Merge(other VersionVector) VersionVector {
	merged := v.Clone()
	if merged == nil {
		merged = make(VersionVector)
	}

	for nodeID, counter := range other {
		if counter > merged[nodeID] {
			merged[nodeID] = counter
		}
	}

	return merged
}

// Compare determina si un vector precede, sucede o es concurrente con otro.
// Un nodo ausente equivale a un contador cero.
func (v VersionVector) Compare(other VersionVector) VersionRelation {
	less, greater := false, false

	for nodeID, counter := range v {
		otherCounter := other[nodeID]
		if counter < otherCounter {
			less = true
		}
		if counter > otherCounter {
			greater = true
		}
	}

	for nodeID, counter := range other {
		thisCounter := v[nodeID]
		if counter < thisCounter {
			less = true
		}
		if counter > thisCounter {
			greater = true
		}
	}

	switch {
	case less && greater:
		return VersionConcurrent
	case less:
		return VersionBefore
	case greater:
		return VersionAfter
	default:
		return VersionEqual
	}
}
