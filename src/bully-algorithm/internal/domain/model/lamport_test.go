package model

import "testing"

func TestLamportClockTickAndObserve(t *testing.T) {
	clock := NewLamportClock(3)
	if got := clock.Tick(); got != 4 {
		t.Fatalf("tick = %d, want 4", got)
	}
	if got := clock.Observe(9); got != 10 {
		t.Fatalf("observe = %d, want 10", got)
	}
	if got := clock.Observe(2); got != 11 {
		t.Fatalf("observe lower = %d, want 11", got)
	}
}
