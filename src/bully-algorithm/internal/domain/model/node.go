package model

import "errors"

// NodeID identifica de forma única un nodo del sistema distribuido.
type NodeID uint64

// Node contiene únicamente la identidad y el endpoint lógico de un nodo.
// Address se mantiene como string para que el dominio no dependa de TCP, gRPC
// ni de otra tecnología de transporte.
type Node struct {
	ID      NodeID
	Address string
}

// Validate comprueba las invariantes que el dominio necesita para ordenar nodos.
func (n Node) Validate() error {
	if n.ID == 0 {
		return errors.New("node id must be greater than zero")
	}

	return nil
}

// HasHigherPriorityThan implementa la prioridad del algoritmo Bully: el nodo
// con el ID más alto tiene mayor prioridad para ser coordinador.
func (n Node) HasHigherPriorityThan(other Node) bool {
	return n.ID > other.ID
}

// SameIdentity indica si dos representaciones apuntan al mismo nodo lógico.
func (n Node) SameIdentity(other Node) bool {
	return n.ID == other.ID
}
