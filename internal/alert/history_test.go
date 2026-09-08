package alert

import (
	"sync"
	"testing"
)

func TestHistoryRecordAndMaximum(t *testing.T) {
	history, err := NewHistory(2)
	if err != nil {
		t.Fatalf("NewHistory() error = %v", err)
	}

	history.Add([]Alert{{Type: AlertWeakSignal}, {Type: AlertHighLatency}, {Type: AlertHighPacketLoss}})
	alerts := history.List()
	if len(alerts) != 2 {
		t.Fatalf("List() returned %d alerts, want 2", len(alerts))
	}
	if alerts[0].Type != AlertHighLatency || alerts[1].Type != AlertHighPacketLoss {
		t.Errorf("List() = %+v, want the two newest alerts", alerts)
	}

	alerts[0].Message = "changed outside history"
	if history.List()[0].Message == "changed outside history" {
		t.Error("mutating List() result changed stored history")
	}
}

func TestHistoryRecordDetectsAlerts(t *testing.T) {
	history, err := NewHistory(3)
	if err != nil {
		t.Fatalf("NewHistory() error = %v", err)
	}
	if err := history.Record(telemetryWith(-82, 150, 8)); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	if got := len(history.List()); got != 3 {
		t.Errorf("List() returned %d alerts, want 3", got)
	}
}

func TestHistoryConcurrentAccess(t *testing.T) {
	history, err := NewHistory(20)
	if err != nil {
		t.Fatalf("NewHistory() error = %v", err)
	}

	var workers sync.WaitGroup
	workers.Add(5)
	for range 4 {
		go func() {
			defer workers.Done()
			for i := 0; i < 1_000; i++ {
				history.List()
			}
		}()
	}
	go func() {
		defer workers.Done()
		for i := 0; i < 1_000; i++ {
			if err := history.Record(telemetryWith(-82, 150, 8)); err != nil {
				t.Errorf("Record() error = %v", err)
				return
			}
		}
	}()
	workers.Wait()
}

func TestNewHistoryRejectsNonPositiveMaximum(t *testing.T) {
	if _, err := NewHistory(0); err == nil {
		t.Error("NewHistory(0) error = nil, want error")
	}
}
