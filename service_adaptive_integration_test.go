package main

import (
	"context"
	"testing"
	"time"
)

func TestAdaptiveControllerIntegration(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Build a lightweight service instance without running New... to avoid system calls
	s := &CakeAutoRTTService{
		config: &Config{MaxConcurrentProbes: 200},
		ctx:    ctx,
		cancel: cancel,
		// start with 10 workers
		adaptiveWorkers:   10,
		cpuSampleInterval: 10 * time.Millisecond,
	}

	// Simulated cpu samples (total, idle). Initial sample will be read first.
	samples := []struct{ total, idle uint64 }{
		{1000, 900},  // initial low CPU (~10%)
		{1100, 900},  // high CPU (~100%) -> should reduce
		{1200, 1190}, // low CPU (~10%) -> should increase
	}
	idx := 0
	s.cpuReader = func() (uint64, uint64, error) {
		if idx >= len(samples) {
			last := samples[len(samples)-1]
			return last.total, last.idle, nil
		}
		v := samples[idx]
		idx++
		return v.total, v.idle, nil
	}

	// Start the adaptive controller
	go s.startAdaptiveController()

	// Wait enough time for several samples to be processed
	time.Sleep(250 * time.Millisecond)

	got := s.getAdaptiveWorkers()
	if got == 10 {
		t.Fatalf("adaptive workers did not change from initial: got %d", got)
	}

	// Clean up
	cancel()
}
