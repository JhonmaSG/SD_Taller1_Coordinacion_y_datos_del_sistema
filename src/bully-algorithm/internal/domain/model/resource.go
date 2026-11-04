package model

import (
	"errors"
	"strings"
)

// Resource representa una unidad de inventario replicada entre los nodos.
type Resource struct {
	ID       string
	Name     string
	Quantity int64
	Metadata Metadata
}

// Validate comprueba las invariantes mínimas del recurso.
func (r Resource) Validate() error {
	if strings.TrimSpace(r.ID) == "" {
		return errors.New("resource id must not be empty")
	}
	if r.Metadata.UpdatedBy == 0 {
		return errors.New("resource metadata must identify the updating node")
	}

	return nil
}

// ConflictsWith detecta dos versiones concurrentes del mismo recurso.
func (r Resource) ConflictsWith(other Resource) bool {
	return r.ID == other.ID && r.Metadata.CompareVersion(other.Metadata) == VersionConcurrent
}

// ResolveConflict devuelve una versión determinista y fusiona sus vectores.
// Para versiones concurrentes gana el Lamport mayor. Si ambos timestamps son
// iguales, gana el ID de nodo menor, tal como exige la regla de negocio.
func ResolveConflict(left, right Resource) (Resource, error) {
	if err := left.Validate(); err != nil {
		return Resource{}, err
	}
	if err := right.Validate(); err != nil {
		return Resource{}, err
	}
	if left.ID != right.ID {
		return Resource{}, errors.New("cannot resolve resources with different ids")
	}

	relation := left.Metadata.CompareVersion(right.Metadata)
	winner := left

	switch relation {
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

	winner.Metadata = winner.Metadata.Clone()
	winner.Metadata.Lamport = maxLamport(left.Metadata.Lamport, right.Metadata.Lamport)
	winner.Metadata.VersionVector = left.Metadata.VersionVector.Merge(right.Metadata.VersionVector)

	return winner, nil
}

func maxLamport(left, right LamportTime) LamportTime {
	if left > right {
		return left
	}

	return right
}
