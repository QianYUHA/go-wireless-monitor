// Package model contains the small, shared data types used by the service.
package model

import (
	"errors"
	"time"
)

// Telemetry is one measurement reported by a wireless access point.
// RSSI is measured in dBm, latency in milliseconds, and packet loss as a
// percentage from 0 through 100.
type Telemetry struct {
	APID             string    `json:"ap_id"`
	RSSI             int       `json:"rssi_dbm"`
	LatencyMS        float64   `json:"latency_ms"`
	PacketLossPct    float64   `json:"packet_loss_pct"`
	ConnectedClients int       `json:"connected_clients"`
	Timestamp        time.Time `json:"timestamp"`
}

// Validate checks the fields that must be valid before telemetry is accepted.
func (t Telemetry) Validate() error {
	if t.APID == "" {
		return errors.New("telemetry AP ID must not be empty")
	}
	if t.LatencyMS < 0 {
		return errors.New("telemetry latency must not be negative")
	}
	if t.PacketLossPct < 0 || t.PacketLossPct > 100 {
		return errors.New("telemetry packet loss must be between 0 and 100")
	}
	if t.ConnectedClients < 0 {
		return errors.New("telemetry connected client count must not be negative")
	}
	if t.Timestamp.IsZero() {
		return errors.New("telemetry timestamp must not be empty")
	}
	return nil
}

// AP describes a configured access point. It is separate from Telemetry:
// configuration changes infrequently, while measurements arrive repeatedly.
type AP struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Location string `json:"location"`
}

// APState is the latest known view of an access point. The monitor will own
// and update this model in Stage 4.
type APState struct {
	AP        AP        `json:"ap"`
	Telemetry Telemetry `json:"telemetry"`
}
