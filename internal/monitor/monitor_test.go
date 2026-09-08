package monitor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go-wireless-monitor/internal/model"
)

func TestNewInitializesConfiguredAPs(t *testing.T) {
	aps := []model.AP{
		{ID: "ap-01", Name: "Lobby", Location: "Floor 1"},
		{ID: "ap-02", Name: "Lab", Location: "Floor 2"},
	}
	monitor, err := New(aps)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	states := monitor.List()
	if len(states) != len(aps) {
		t.Fatalf("List() returned %d states, want %d", len(states), len(aps))
	}
	state, ok := monitor.Get("ap-01")
	if !ok {
		t.Fatal("Get(ap-01) found = false, want true")
	}
	if state.AP != aps[0] {
		t.Errorf("Get(ap-01).AP = %+v, want %+v", state.AP, aps[0])
	}
	if !state.Telemetry.Timestamp.IsZero() {
		t.Error("initial telemetry timestamp is set, want zero value")
	}
}

func TestUpdateReplacesLatestTelemetry(t *testing.T) {
	monitor := newMonitor(t)
	first := testTelemetry("ap-01", 10)
	second := testTelemetry("ap-01", 20)

	if err := monitor.Update(first); err != nil {
		t.Fatalf("Update(first) error = %v", err)
	}
	if err := monitor.Update(second); err != nil {
		t.Fatalf("Update(second) error = %v", err)
	}

	state, ok := monitor.Get("ap-01")
	if !ok {
		t.Fatal("Get(ap-01) found = false, want true")
	}
	if state.Telemetry != second {
		t.Errorf("latest telemetry = %+v, want %+v", state.Telemetry, second)
	}
}

func TestUpdateRejectsUnknownAP(t *testing.T) {
	err := newMonitor(t).Update(testTelemetry("ap-unknown", 1))
	if !errors.Is(err, ErrUnknownAP) {
		t.Fatalf("Update() error = %v, want ErrUnknownAP", err)
	}
}

func TestListReturnsCopies(t *testing.T) {
	monitor := newMonitor(t)
	states := monitor.List()
	states[0].AP.Name = "changed outside monitor"

	state, ok := monitor.Get(states[0].AP.ID)
	if !ok {
		t.Fatalf("Get(%q) found = false", states[0].AP.ID)
	}
	if state.AP.Name == "changed outside monitor" {
		t.Error("mutating List() result changed monitor state")
	}
}

func TestRunProcessesInputUntilChannelCloses(t *testing.T) {
	monitor := newMonitor(t)
	input := make(chan model.Telemetry, 1)
	telemetry := testTelemetry("ap-02", 5)
	input <- telemetry
	close(input)

	if err := monitor.Run(context.Background(), input); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	state, ok := monitor.Get("ap-02")
	if !ok || state.Telemetry != telemetry {
		t.Errorf("Get(ap-02) = %+v, %t; want telemetry %+v, true", state, ok, telemetry)
	}
}

func TestRunStopsOnContextCancellation(t *testing.T) {
	monitor := newMonitor(t)
	input := make(chan model.Telemetry)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := monitor.Run(ctx, input); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
}

func TestConcurrentReadsAndWrites(t *testing.T) {
	monitor := newMonitor(t)
	done := make(chan struct{})

	var workers sync.WaitGroup
	workers.Add(5)
	for range 4 {
		go func() {
			defer workers.Done()
			for {
				select {
				case <-done:
					return
				default:
					monitor.Get("ap-01")
					monitor.List()
				}
			}
		}()
	}
	go func() {
		defer workers.Done()
		defer close(done)
		for i := 0; i < 1_000; i++ {
			if err := monitor.Update(testTelemetry("ap-01", int64(i))); err != nil {
				t.Errorf("Update() error = %v", err)
				return
			}
		}
	}()
	workers.Wait()
}

func newMonitor(t *testing.T) *Monitor {
	t.Helper()
	monitor, err := New([]model.AP{
		{ID: "ap-01", Name: "Lobby", Location: "Floor 1"},
		{ID: "ap-02", Name: "Lab", Location: "Floor 2"},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return monitor
}

func testTelemetry(id string, seconds int64) model.Telemetry {
	return model.Telemetry{
		APID:             id,
		RSSI:             -55,
		LatencyMS:        25,
		PacketLossPct:    1,
		ConnectedClients: 12,
		Timestamp:        time.Unix(seconds, 0).UTC(),
	}
}
