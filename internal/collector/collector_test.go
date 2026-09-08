package collector

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"go-wireless-monitor/internal/model"
	"go-wireless-monitor/internal/simulator"
)

func TestStartProducesValidTelemetryForEachAP(t *testing.T) {
	simulators := []*simulator.Simulator{
		newSimulator(t, "ap-01", 1),
		newSimulator(t, "ap-02", 2),
		newSimulator(t, "ap-03", 3),
	}
	collector, err := New(simulators, time.Millisecond)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	out := collector.Start(ctx)
	defer cancel()

	wantIDs := map[string]bool{"ap-01": true, "ap-02": true, "ap-03": true}
	seen := make(map[string]bool)
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()

	for len(seen) < len(wantIDs) {
		select {
		case telemetry, ok := <-out:
			if !ok {
				t.Fatal("output channel closed before all APs produced telemetry")
			}
			if !wantIDs[telemetry.APID] {
				t.Errorf("unexpected AP ID %q", telemetry.APID)
			}
			if err := telemetry.Validate(); err != nil {
				t.Errorf("telemetry from %q failed validation: %v", telemetry.APID, err)
			}
			seen[telemetry.APID] = true
		case <-deadline.C:
			t.Fatalf("timed out waiting for telemetry from all APs; saw %v", seen)
		}
	}

	cancel()
	waitForClosed(t, out)
}

func TestStartClosesOutputAfterCancellation(t *testing.T) {
	collector, err := New([]*simulator.Simulator{newSimulator(t, "ap-01", 1)}, time.Millisecond)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	out := collector.Start(ctx)
	cancel()

	waitForClosed(t, out)
}

func TestNewValidatesConfiguration(t *testing.T) {
	if _, err := New(nil, -time.Second); err == nil {
		t.Error("New() error = nil, want an error for a negative interval")
	}
	if _, err := New([]*simulator.Simulator{nil}, time.Second); err == nil {
		t.Error("New() error = nil, want an error for a nil simulator")
	}

	collector, err := New(nil, 0)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if collector.interval != DefaultInterval {
		t.Errorf("default interval = %v, want %v", collector.interval, DefaultInterval)
	}
}

func newSimulator(t *testing.T, id string, seed int64) *simulator.Simulator {
	t.Helper()
	sim, err := simulator.New(model.AP{ID: id}, rand.New(rand.NewSource(seed)))
	if err != nil {
		t.Fatalf("simulator.New() error = %v", err)
	}
	return sim
}

func waitForClosed(t *testing.T, out <-chan model.Telemetry) {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()

	for {
		select {
		case _, ok := <-out:
			if !ok {
				return
			}
		case <-deadline.C:
			t.Fatal("timed out waiting for output channel to close")
		}
	}
}
