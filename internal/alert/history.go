package alert

import (
	"errors"
	"sync"

	"go-wireless-monitor/internal/model"
)

// History stores a bounded list of the most recently detected alerts.
type History struct {
	mu     sync.RWMutex
	max    int
	alerts []Alert
}

// NewHistory creates an alert history that retains at most max alerts.
func NewHistory(max int) (*History, error) {
	if max <= 0 {
		return nil, errors.New("alert history maximum must be positive")
	}
	return &History{max: max}, nil
}

// Record detects alerts for telemetry and saves any resulting alerts.
func (h *History) Record(telemetry model.Telemetry) error {
	alerts, err := Detect(telemetry)
	if err != nil {
		return err
	}
	h.Add(alerts)
	return nil
}

// Add saves alerts, discarding the oldest entries when the history is full.
func (h *History) Add(alerts []Alert) {
	if len(alerts) == 0 {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	h.alerts = append(h.alerts, alerts...)
	if excess := len(h.alerts) - h.max; excess > 0 {
		h.alerts = append([]Alert(nil), h.alerts[excess:]...)
	}
}

// List returns copies of the saved alerts from oldest to newest.
func (h *History) List() []Alert {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return append([]Alert(nil), h.alerts...)
}
