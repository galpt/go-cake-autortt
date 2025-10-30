package main

import "testing"

func TestComputeAdaptiveTarget(t *testing.T) {
	s := &CakeAutoRTTService{}

	// High CPU should reduce workers to ~70% (int truncation)
	if got := s.computeAdaptiveTarget(100, 200, 85.0); got != 70 {
		t.Fatalf("high cpu reduce: got %d want %d", got, 70)
	}

	// Very high CPU with current=1 should not go below 1
	if got := s.computeAdaptiveTarget(1, 100, 95.0); got != 1 {
		t.Fatalf("min worker: got %d want %d", got, 1)
	}

	// Low CPU should increase workers by ~10%% + 1, capped at cfgMax
	if got := s.computeAdaptiveTarget(10, 200, 10.0); got != 12 {
		t.Fatalf("low cpu increase: got %d want %d", got, 12)
	}

	// When increase would exceed cfgMax, it should clamp to cfgMax
	if got := s.computeAdaptiveTarget(190, 200, 10.0); got != 200 {
		t.Fatalf("cap to cfgMax: got %d want %d", got, 200)
	}
}
