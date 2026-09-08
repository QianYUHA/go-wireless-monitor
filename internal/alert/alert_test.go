package alert

import (
	"strings"
	"testing"
	"time"

	"go-wireless-monitor/internal/model"
)

func TestDetectHealthyTelemetryProducesNoAlerts(t *testing.T) {
	alerts, err := Detect(testTelemetry())
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if len(alerts) != 0 {
		t.Errorf("Detect() returned %d alerts, want 0", len(alerts))
	}
}

func TestDetectIndividualRules(t *testing.T) {
	tests := []struct {
		name      string
		telemetry model.Telemetry
		wantType  AlertType
		severity  Severity
		value     float64
		threshold float64
	}{
		{
			name:      "weak signal",
			telemetry: telemetryWith(-71, 50, 1),
			wantType:  AlertWeakSignal,
			severity:  SeverityWarning,
			value:     -71,
			threshold: weakSignalThreshold,
		},
		{
			name:      "high latency",
			telemetry: telemetryWith(-50, 101, 1),
			wantType:  AlertHighLatency,
			severity:  SeverityWarning,
			value:     101,
			threshold: highLatencyThresholdMS,
		},
		{
			name:      "high packet loss",
			telemetry: telemetryWith(-50, 50, 5.1),
			wantType:  AlertHighPacketLoss,
			severity:  SeverityCritical,
			value:     5.1,
			threshold: highPacketLossThreshold,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			alerts, err := Detect(test.telemetry)
			if err != nil {
				t.Fatalf("Detect() error = %v", err)
			}
			if len(alerts) != 1 {
				t.Fatalf("Detect() returned %d alerts, want 1", len(alerts))
			}
			alert := alerts[0]
			if alert.APID != test.telemetry.APID || alert.Type != test.wantType || alert.Severity != test.severity {
				t.Errorf("alert identity = %+v, want APID %q, type %q, severity %q", alert, test.telemetry.APID, test.wantType, test.severity)
			}
			if alert.ObservedValue != test.value || alert.Threshold != test.threshold {
				t.Errorf("alert values = (%v, %v), want (%v, %v)", alert.ObservedValue, alert.Threshold, test.value, test.threshold)
			}
			if alert.Timestamp != test.telemetry.Timestamp || alert.Message == "" {
				t.Errorf("alert timestamp/message = (%v, %q), want (%v, non-empty)", alert.Timestamp, alert.Message, test.telemetry.Timestamp)
			}
		})
	}
}

func TestDetectCanProduceMultipleAlerts(t *testing.T) {
	telemetry := telemetryWith(-82, 150, 8)
	alerts, err := Detect(telemetry)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if len(alerts) != 3 {
		t.Fatalf("Detect() returned %d alerts, want 3", len(alerts))
	}

	wantTypes := []AlertType{AlertWeakSignal, AlertHighLatency, AlertHighPacketLoss}
	for i, wantType := range wantTypes {
		if alerts[i].Type != wantType {
			t.Errorf("alerts[%d].Type = %q, want %q", i, alerts[i].Type, wantType)
		}
	}
}

func TestDetectThresholdValuesDoNotTrigger(t *testing.T) {
	alerts, err := Detect(telemetryWith(-70, 100, 5))
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if len(alerts) != 0 {
		t.Errorf("Detect() returned %d alerts at exact thresholds, want 0", len(alerts))
	}
}

func TestDetectRejectsInvalidTelemetry(t *testing.T) {
	telemetry := testTelemetry()
	telemetry.Timestamp = time.Time{}
	alerts, err := Detect(telemetry)
	if err == nil {
		t.Fatal("Detect() error = nil, want validation error")
	}
	if alerts != nil {
		t.Errorf("Detect() alerts = %+v, want nil for invalid telemetry", alerts)
	}
	if !strings.Contains(err.Error(), "invalid telemetry") {
		t.Errorf("Detect() error = %q, want invalid telemetry context", err)
	}
}

func testTelemetry() model.Telemetry {
	return telemetryWith(-50, 50, 1)
}

func telemetryWith(rssi int, latency, packetLoss float64) model.Telemetry {
	return model.Telemetry{
		APID:             "ap-02",
		RSSI:             rssi,
		LatencyMS:        latency,
		PacketLossPct:    packetLoss,
		ConnectedClients: 12,
		Timestamp:        time.Date(2026, time.September, 8, 12, 0, 0, 0, time.UTC),
	}
}
