package main

import (
	"fmt"
	"testing"
	"time"
)

func TestMeasureRTTTCPWithMockProbe(t *testing.T) {
	cfg := &Config{
		MaxConcurrentProbes: 2,
		MinHosts:            1,
		TCPConnectTimeout:   1,
		DLInterface:         "lo",
		ULInterface:         "lo",
	}
	s, err := NewCakeAutoRTTService(cfg)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	// Inject a fake probe function: h1 -> 10ms, h2 -> fail, h3 -> 50ms
	s.ProbeFunc = func(host string, timeoutSec int) (time.Duration, error) {
		switch host {
		case "h1":
			return 10 * time.Millisecond, nil
		case "h2":
			return 0, fmt.Errorf("unreachable")
		case "h3":
			return 50 * time.Millisecond, nil
		default:
			return 0, fmt.Errorf("unknown host")
		}
	}

	hosts := []string{"h1", "h2", "h3"}
	worst, alive, err := s.measureRTTTCP(hosts)
	if err != nil {
		t.Fatalf("unexpected error from measureRTTTCP: %v", err)
	}
	if alive != 2 {
		t.Fatalf("expected 2 alive hosts, got %d", alive)
	}
	if int(worst+0.5) != 50 {
		t.Fatalf("expected worst RTT ~50ms, got %.2f", worst)
	}
}

func TestMeasureRTTTCPAllFail(t *testing.T) {
	cfg := &Config{
		MaxConcurrentProbes: 2,
		MinHosts:            1,
		TCPConnectTimeout:   1,
		DLInterface:         "lo",
		ULInterface:         "lo",
	}
	s, err := NewCakeAutoRTTService(cfg)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	// All probes fail
	s.ProbeFunc = func(host string, timeoutSec int) (time.Duration, error) {
		return 0, fmt.Errorf("down")
	}

	hosts := []string{"a", "b"}
	_, alive, err := s.measureRTTTCP(hosts)
	if err == nil {
		t.Fatalf("expected error when all probes fail")
	}
	if alive != 0 {
		t.Fatalf("expected 0 alive hosts, got %d", alive)
	}
}

func TestCompletedProbesBuffer(t *testing.T) {
	cfg := &Config{
		MaxConcurrentProbes: 2,
		MinHosts:            1,
		TCPConnectTimeout:   1,
		DLInterface:         "lo",
		ULInterface:         "lo",
	}
	s, err := NewCakeAutoRTTService(cfg)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	// Shorten retention and max entries for test clarity
	s.completedRetentionSec = 1
	s.completedMaxEntries = 2

	s.ProbeFunc = func(host string, timeoutSec int) (time.Duration, error) {
		return 5 * time.Millisecond, nil
	}

	hosts := []string{"x", "y", "z"}
	worst, alive, err := s.measureRTTTCP(hosts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if alive != 3 {
		t.Fatalf("expected 3 alive hosts, got %d", alive)
	}
	if worst <= 0 {
		t.Fatalf("expected positive worst RTT, got %.2f", worst)
	}

	completed := s.GetRecentCompletedProbes()
	if len(completed) != 2 {
		t.Fatalf("expected completed buffer trimmed to 2 entries, got %d", len(completed))
	}
}
