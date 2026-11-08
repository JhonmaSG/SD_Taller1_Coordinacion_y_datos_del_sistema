package model

import "testing"

func TestNodeDocumentValidation(t *testing.T) {
	document := NodeDocument{ID: 1, Role: RoleAgent, Status: StatusOnline, Resources: NodeResources{CPUCores: 2, MemoryGB: 3, BandwidthMbps: 1000}}
	if err := document.Validate(); err != nil {
		t.Fatal(err)
	}

	document.Resources.MemoryGB = -1
	if err := document.Validate(); err == nil {
		t.Fatal("expected negative resource validation error")
	}
}
