package tools

import "testing"

func TestComputeDynamicActiveSlots_ReservesTwentyPercent(t *testing.T) {
	got := computeDynamicActiveSlots(10, 0.20, 0.0, 12)
	if got != 8 {
		t.Fatalf("expected 8 active slots with 20%% reserve on 10 CPU, got %d", got)
	}
}

func TestComputeDynamicActiveSlots_ReducesWithSystemUsage(t *testing.T) {
	got := computeDynamicActiveSlots(10, 0.20, 0.5, 12)
	if got != 3 {
		t.Fatalf("expected 3 active slots when system usage is 50%%, got %d", got)
	}
}

func TestComputeDynamicActiveSlots_AlwaysKeepsOne(t *testing.T) {
	got := computeDynamicActiveSlots(8, 0.20, 0.95, 12)
	if got != 1 {
		t.Fatalf("expected at least 1 active slot under high system usage, got %d", got)
	}
}
