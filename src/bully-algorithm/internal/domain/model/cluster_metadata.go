package model

import "errors"

// ClusterMetadata es el único estado de concurrencia persistido. Se guarda en
// el documento con _id=cluster_metadata.
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

func (m ClusterMetadata) Validate() error {
	if m.LastModifiedBy == 0 {
		return errors.New("cluster metadata must identify the updater")
	}
	if m.ActiveCoordinator == 0 {
		return errors.New("cluster metadata must identify the coordinator")
	}
	return nil
}

// Advance crea una nueva versión producida por nodeID. El reloj y el vector
// avanzan juntos para representar una escritura coordinada.
func (m ClusterMetadata) Advance(nodeID NodeID, knownIDs []NodeID) ClusterMetadata {
	next := m.Clone()
	next.VersionVector = ensureVectorKeys(next.VersionVector, knownIDs)
	next.GlobalLamportClock++
	next.VersionVector.Increment(nodeID)
	next.ActiveCoordinator = nodeID
	next.LastModifiedBy = nodeID
	return next
}

func ensureVectorKeys(vector VersionVector, nodeIDs []NodeID) VersionVector {
	result := vector.Clone()
	if result == nil {
		result = NewVersionVector(nodeIDs)
	}
	for _, nodeID := range nodeIDs {
		if _, exists := result[nodeID.Key()]; !exists {
			result[nodeID.Key()] = 0
		}
	}
	return result
}

func (m ClusterMetadata) ConflictsWith(other ClusterMetadata) bool {
	return m.VersionVector.Compare(other.VersionVector) == VersionConcurrent
}

// ResolveConflict conserva la metadata de la versión causalmente posterior.
// Para concurrencia gana el Lamport mayor y, en empate, el NodeID menor.
func ResolveClusterMetadataConflict(left, right ClusterMetadata) (ClusterMetadata, error) {
	if err := left.Validate(); err != nil {
		return ClusterMetadata{}, err
	}
	if err := right.Validate(); err != nil {
		return ClusterMetadata{}, err
	}

	winner := left
	switch left.VersionVector.Compare(right.VersionVector) {
	case VersionBefore:
		winner = right
	case VersionAfter:
		winner = left
	case VersionConcurrent, VersionEqual:
		if right.GlobalLamportClock > left.GlobalLamportClock ||
			(right.GlobalLamportClock == left.GlobalLamportClock && right.LastModifiedBy < left.LastModifiedBy) {
			winner = right
		}
	}
	winner = winner.Clone()
	winner.GlobalLamportClock = maxLamport(left.GlobalLamportClock, right.GlobalLamportClock)
	winner.VersionVector = left.VersionVector.Merge(right.VersionVector)
	return winner, nil
}

func maxLamport(left, right LamportTime) LamportTime {
	if left > right {
		return left
	}
	return right
}
