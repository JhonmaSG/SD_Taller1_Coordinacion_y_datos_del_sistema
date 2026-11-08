package system

import (
	"context"
	"errors"
	"testing"

	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/domain/model"
)

func TestMachineCapacityReaderReadUsesAllSources(t *testing.T) {
	reader := &MachineCapacityReader{
		cpu:       func() int { return 12 },
		memory:    func(context.Context) (int64, error) { return 31, nil },
		bandwidth: func(context.Context) int64 { return 100000 },
	}

	resources, err := reader.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := model.NodeResources{CPUCores: 12, MemoryGB: 31, BandwidthMbps: 100000}
	if resources != want {
		t.Fatalf("resources = %#v, want %#v", resources, want)
	}
}

func TestMachineCapacityReaderReadRejectsNilContext(t *testing.T) {
	reader := NewMachineCapacityReader()
	if _, err := reader.Read(nil); err == nil {
		t.Fatal("expected nil context error")
	}
}

func TestMachineCapacityReaderReadPropagatesMemoryError(t *testing.T) {
	readErr := errors.New("memory unavailable")
	reader := &MachineCapacityReader{
		cpu:       func() int { return 4 },
		memory:    func(context.Context) (int64, error) { return 0, readErr },
		bandwidth: func(context.Context) int64 { return 1000 },
	}

	if _, err := reader.Read(context.Background()); !errors.Is(err, readErr) {
		t.Fatalf("error = %v, want %v", err, readErr)
	}
}
