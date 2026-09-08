// Package simulator generates realistic-looking telemetry for one access point.
package simulator

import (
	"errors"
	"math/rand"
	"time"

	"go-wireless-monitor/internal/model"
)

const (
	minRSSI             = -90
	maxRSSI             = -30
	minLatencyMS        = 5
	maxLatencyMS        = 150
	maxPacketLossPct    = 10.0
	maxConnectedClients = 50
)

// Simulator produces one telemetry sample at a time for a configured AP.
type Simulator struct {
	ap     model.AP
	random *rand.Rand
}

// New creates a simulator for ap. If random is nil, New creates a time-seeded
// random source. Supplying a source is useful when a caller needs repeatable
// samples, such as in a test.
func New(ap model.AP, random *rand.Rand) (*Simulator, error) {
	if ap.ID == "" {
		return nil, errors.New("simulator AP ID must not be empty")
	}
	if random == nil {
		random = rand.New(rand.NewSource(time.Now().UnixNano()))
	}

	return &Simulator{ap: ap, random: random}, nil
}

// Next generates the next valid telemetry measurement for the simulator's AP.
func (s *Simulator) Next() model.Telemetry {
	return model.Telemetry{
		APID:             s.ap.ID,
		RSSI:             randomInt(s.random, minRSSI, maxRSSI),
		LatencyMS:        float64(randomInt(s.random, minLatencyMS, maxLatencyMS)),
		PacketLossPct:    s.random.Float64() * maxPacketLossPct,
		ConnectedClients: randomInt(s.random, 0, maxConnectedClients),
		Timestamp:        time.Now().UTC(),
	}
}

// randomInt returns an integer in the inclusive range [min, max].
func randomInt(random *rand.Rand, min, max int) int {
	return min + random.Intn(max-min+1)
}
