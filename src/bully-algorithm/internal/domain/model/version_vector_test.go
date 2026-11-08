package model

import "testing"

func TestVersionVectorCompareRelations(t *testing.T) {
	tests := []struct {
		name     string
		left     VersionVector
		right    VersionVector
		relation VersionRelation
	}{
		{"equal", VersionVector{"node_1": 2}, VersionVector{"node_1": 2}, VersionEqual},
		{"before", VersionVector{"node_1": 1}, VersionVector{"node_1": 2}, VersionBefore},
		{"after", VersionVector{"node_1": 2}, VersionVector{"node_1": 1}, VersionAfter},
		{"concurrent", VersionVector{"node_1": 2, "node_2": 1}, VersionVector{"node_1": 1, "node_2": 2}, VersionConcurrent},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.left.Compare(test.right); got != test.relation {
				t.Fatalf("got %v, want %v", got, test.relation)
			}
		})
	}
}

func TestVersionVectorNewMergeAndIncrement(t *testing.T) {
	vector := NewVersionVector([]NodeID{1, 2, 3})
	vector.Increment(2)
	merged := vector.Merge(VersionVector{"node_1": 4, "node_2": 1})

	want := VersionVector{"node_1": 4, "node_2": 1, "node_3": 0}
	if merged.Compare(want) != VersionEqual {
		t.Fatalf("merged vector = %#v, want %#v", merged, want)
	}
}
