// Package collector schedules telemetry generation from multiple AP simulators.
package collector

import (
	"context"
	"errors"
	"sync"
	"time"

	"go-wireless-monitor/internal/model"
	"go-wireless-monitor/internal/simulator"
)

const (
	// DefaultInterval is used when New receives an interval of zero.
	DefaultInterval = time.Second

	// outputBufferSize permits a short burst while still applying backpressure.
	outputBufferSize = 16
)

// Collector repeatedly requests telemetry from each of its simulators.
// A Collector is intended to be started once with Start.
type Collector struct {
	simulators []*simulator.Simulator
	interval   time.Duration
}

// New creates a collector. An interval of zero selects DefaultInterval.
func New(simulators []*simulator.Simulator, interval time.Duration) (*Collector, error) {
	if interval < 0 {
		return nil, errors.New("collector interval must not be negative")
	}
	if interval == 0 {
		interval = DefaultInterval
	}
	for _, sim := range simulators {
		if sim == nil {
			return nil, errors.New("collector simulator must not be nil")
		}
	}

	return &Collector{simulators: simulators, interval: interval}, nil
}

// Start launches one producer goroutine per simulator and returns their shared
// output channel. Cancelling ctx stops the producers and eventually closes out.
func (c *Collector) Start(ctx context.Context) <-chan model.Telemetry {
	out := make(chan model.Telemetry, outputBufferSize)

	var producers sync.WaitGroup
	producers.Add(len(c.simulators))
	for _, sim := range c.simulators {
		go c.produce(ctx, sim, out, &producers)
	}

	go func() {
		producers.Wait()
		close(out)
	}()

	return out
}

func (c *Collector) produce(
	ctx context.Context,
	sim *simulator.Simulator,
	out chan<- model.Telemetry,
	producers *sync.WaitGroup,
) {
	defer producers.Done()

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			telemetry := sim.Next()
			select {
			case <-ctx.Done():
				return
			case out <- telemetry:
			}
		}
	}
}
