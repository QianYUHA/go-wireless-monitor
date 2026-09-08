package simulator

import (
	"math/rand"
	"testing"

	"go-wireless-monitor/internal/model"
)

func TestNewRejectsAPWithoutID(t *testing.T) {
	_, err := New(model.AP{}, rand.New(rand.NewSource(1)))
	if err == nil {
		t.Fatal("New() error = nil, want an error for an AP without an ID")
	}
}

func TestNextGeneratesValidTelemetry(t *testing.T) {
	ap := model.AP{ID: "ap-lobby-01", Name: "Lobby AP", Location: "Lobby"}
	sim, err := New(ap, rand.New(rand.NewSource(1)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	for range 100 {
		telemetry := sim.Next()

		if telemetry.APID != ap.ID {
			t.Errorf("APID = %q, want %q", telemetry.APID, ap.ID)
		}
		if telemetry.Timestamp.IsZero() {
			t.Error("Timestamp is zero, want a timestamp")
		}
		if telemetry.RSSI < minRSSI || telemetry.RSSI > maxRSSI {
			t.Errorf("RSSI = %d, want [%d, %d]", telemetry.RSSI, minRSSI, maxRSSI)
		}
		if telemetry.LatencyMS < minLatencyMS || telemetry.LatencyMS > maxLatencyMS {
			t.Errorf("LatencyMS = %v, want [%d, %d]", telemetry.LatencyMS, minLatencyMS, maxLatencyMS)
		}
		if telemetry.PacketLossPct < 0 || telemetry.PacketLossPct > maxPacketLossPct {
			t.Errorf("PacketLossPct = %v, want [0, %v]", telemetry.PacketLossPct, maxPacketLossPct)
		}
		if telemetry.ConnectedClients < 0 || telemetry.ConnectedClients > maxConnectedClients {
			t.Errorf("ConnectedClients = %d, want [0, %d]", telemetry.ConnectedClients, maxConnectedClients)
		}
		if err := telemetry.Validate(); err != nil {
			t.Errorf("Validate() error = %v", err)
		}
	}
}
