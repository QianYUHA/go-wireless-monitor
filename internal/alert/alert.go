// Package alert detects abnormal conditions in AP telemetry.
package alert

import (
	"fmt"
	"time"

	"go-wireless-monitor/internal/model"
)

const (
	weakSignalThreshold     = -70
	highLatencyThresholdMS  = 100
	highPacketLossThreshold = 5.0
)

// AlertType identifies the abnormal condition that triggered an alert.
type AlertType string

const (
	AlertWeakSignal     AlertType = "weak_signal"
	AlertHighLatency    AlertType = "high_latency"
	AlertHighPacketLoss AlertType = "high_packet_loss"
)

// Severity expresses how urgently an alert should be treated.
type Severity string

const (
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// Alert explains one abnormal telemetry measurement.
type Alert struct {
	APID          string    `json:"ap_id"`
	Type          AlertType `json:"type"`
	Severity      Severity  `json:"severity"`
	Message       string    `json:"message"`
	ObservedValue float64   `json:"observed_value"`
	Threshold     float64   `json:"threshold"`
	Timestamp     time.Time `json:"timestamp"`
}

// Detect validates telemetry and returns one alert for each independent rule
// whose threshold the sample exceeds.
func Detect(telemetry model.Telemetry) ([]Alert, error) {
	if err := telemetry.Validate(); err != nil {
		return nil, fmt.Errorf("invalid telemetry: %w", err)
	}

	alerts := make([]Alert, 0, 3)
	if telemetry.RSSI < weakSignalThreshold {
		alerts = append(alerts, Alert{
			APID:          telemetry.APID,
			Type:          AlertWeakSignal,
			Severity:      SeverityWarning,
			Message:       fmt.Sprintf("AP %s has weak signal: RSSI %.0f dBm is below %.0f dBm", telemetry.APID, float64(telemetry.RSSI), float64(weakSignalThreshold)),
			ObservedValue: float64(telemetry.RSSI),
			Threshold:     weakSignalThreshold,
			Timestamp:     telemetry.Timestamp,
		})
	}
	if telemetry.LatencyMS > highLatencyThresholdMS {
		alerts = append(alerts, Alert{
			APID:          telemetry.APID,
			Type:          AlertHighLatency,
			Severity:      SeverityWarning,
			Message:       fmt.Sprintf("AP %s has high latency: %.1f ms is above %.0f ms", telemetry.APID, telemetry.LatencyMS, float64(highLatencyThresholdMS)),
			ObservedValue: telemetry.LatencyMS,
			Threshold:     highLatencyThresholdMS,
			Timestamp:     telemetry.Timestamp,
		})
	}
	if telemetry.PacketLossPct > highPacketLossThreshold {
		alerts = append(alerts, Alert{
			APID:          telemetry.APID,
			Type:          AlertHighPacketLoss,
			Severity:      SeverityCritical,
			Message:       fmt.Sprintf("AP %s has high packet loss: %.1f%% is above %.1f%%", telemetry.APID, telemetry.PacketLossPct, highPacketLossThreshold),
			ObservedValue: telemetry.PacketLossPct,
			Threshold:     highPacketLossThreshold,
			Timestamp:     telemetry.Timestamp,
		})
	}

	return alerts, nil
}
