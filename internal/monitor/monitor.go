// Package monitor stores the latest telemetry state for configured access points.
package monitor

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"go-wireless-monitor/internal/model"
)

// ErrUnknownAP is returned when telemetry identifies an AP not configured in
// the monitor.
var ErrUnknownAP = errors.New("unknown access point")

// Monitor owns the latest telemetry state for its configured APs.
type Monitor struct {
	mu     sync.RWMutex
	states map[string]model.APState
}

// New creates a monitor with one initial state for every configured AP.
func New(aps []model.AP) (*Monitor, error) {
	states := make(map[string]model.APState, len(aps))
	for _, ap := range aps {
		if ap.ID == "" {
			return nil, errors.New("monitor AP ID must not be empty")
		}
		if _, exists := states[ap.ID]; exists {
			return nil, fmt.Errorf("duplicate AP ID %q", ap.ID)
		}
		states[ap.ID] = model.APState{AP: ap}
	}

	return &Monitor{states: states}, nil
}

// Update saves telemetry as the latest received sample for its configured AP.
func (m *Monitor) Update(telemetry model.Telemetry) error {
	if err := telemetry.Validate(); err != nil {
		return fmt.Errorf("invalid telemetry: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	state, exists := m.states[telemetry.APID]
	if !exists {
		return fmt.Errorf("%w: %s", ErrUnknownAP, telemetry.APID)
	}
	state.Telemetry = telemetry
	m.states[telemetry.APID] = state
	return nil
}

// Get returns a copy of one AP's latest state.
func (m *Monitor) Get(id string) (model.APState, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	state, exists := m.states[id]
	return state, exists
}

// List returns copies of every AP state, sorted by AP ID for a stable result.
func (m *Monitor) List() []model.APState {
	m.mu.RLock()
	states := make([]model.APState, 0, len(m.states))
	for _, state := range m.states {
		states = append(states, state)
	}
	m.mu.RUnlock()

	sort.Slice(states, func(i, j int) bool {
		return states[i].AP.ID < states[j].AP.ID
	})
	return states
}

// Run receives telemetry until input closes or ctx is cancelled. The collector
// owns input and is responsible for closing it.
func (m *Monitor) Run(ctx context.Context, input <-chan model.Telemetry) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case telemetry, ok := <-input:
			if !ok {
				return nil
			}
			if err := m.Update(telemetry); err != nil {
				return err
			}
		}
	}
}
