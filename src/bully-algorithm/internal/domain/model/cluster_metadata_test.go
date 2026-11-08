package model

import "testing"

func TestClusterMetadataAdvanceLeadershipSequence(t *testing.T) {
	known := []NodeID{1, 2, 3}
	metadata := ClusterMetadata{VersionVector: NewVersionVector(known)}
	sequence := []NodeID{1, 2, 3, 2, 1, 2}
	want := []VersionVector{
		{"node_1": 1, "node_2": 0, "node_3": 0},
		{"node_1": 1, "node_2": 1, "node_3": 0},
		{"node_1": 1, "node_2": 1, "node_3": 1},
		{"node_1": 1, "node_2": 2, "node_3": 1},
		{"node_1": 2, "node_2": 2, "node_3": 1},
		{"node_1": 2, "node_2": 3, "node_3": 1},
	}

	for index, nodeID := range sequence {
		metadata = metadata.Advance(nodeID, known)
		if metadata.VersionVector.Compare(want[index]) != VersionEqual {
			t.Fatalf("step %d vector = %#v, want %#v", index, metadata.VersionVector, want[index])
		}
		if metadata.ActiveCoordinator != nodeID || metadata.LastModifiedBy != nodeID {
			t.Fatalf("step %d coordinator metadata = %#v", index, metadata)
		}
	}
	if metadata.GlobalLamportClock != LamportTime(len(sequence)) {
		t.Fatalf("lamport = %d, want %d", metadata.GlobalLamportClock, len(sequence))
	}
}

func TestResolveClusterMetadataConflictUsesLamportThenLowerNodeID(t *testing.T) {
	left := ClusterMetadata{GlobalLamportClock: 5, VersionVector: VersionVector{"node_1": 2, "node_2": 1}, ActiveCoordinator: 1, LastModifiedBy: 1}
	right := ClusterMetadata{GlobalLamportClock: 7, VersionVector: VersionVector{"node_1": 1, "node_2": 2}, ActiveCoordinator: 2, LastModifiedBy: 2}
	winner, err := ResolveClusterMetadataConflict(left, right)
	if err != nil {
		t.Fatal(err)
	}
	if winner.ActiveCoordinator != 2 {
		t.Fatalf("winner coordinator = %d, want 2", winner.ActiveCoordinator)
	}

	right.GlobalLamportClock = 5
	right.LastModifiedBy = 2
	winner, err = ResolveClusterMetadataConflict(left, right)
	if err != nil {
		t.Fatal(err)
	}
	if winner.ActiveCoordinator != 1 {
		t.Fatalf("tie winner coordinator = %d, want 1", winner.ActiveCoordinator)
	}
}
