// Package udp transports telemetry as JSON UDP datagrams.
package udp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"time"

	"go-wireless-monitor/internal/model"
)

const (
	// DefaultMaxDatagramSize is ample for one JSON telemetry sample.
	DefaultMaxDatagramSize = 4 * 1024
	outputBufferSize       = 16
	readPollInterval       = 50 * time.Millisecond
)

// Sender sends telemetry to one configured UDP destination.
type Sender struct {
	conn *net.UDPConn
}

// NewSender creates a connected UDP socket for destination.
func NewSender(destination string) (*Sender, error) {
	address, err := net.ResolveUDPAddr("udp", destination)
	if err != nil {
		return nil, fmt.Errorf("resolve UDP destination: %w", err)
	}
	conn, err := net.DialUDP("udp", nil, address)
	if err != nil {
		return nil, fmt.Errorf("dial UDP destination: %w", err)
	}
	return &Sender{conn: conn}, nil
}

// Send validates, JSON-encodes, and sends one telemetry datagram.
func (s *Sender) Send(telemetry model.Telemetry) error {
	if err := telemetry.Validate(); err != nil {
		return fmt.Errorf("invalid telemetry: %w", err)
	}
	payload, err := json.Marshal(telemetry)
	if err != nil {
		return fmt.Errorf("marshal telemetry: %w", err)
	}
	if _, err := s.conn.Write(payload); err != nil {
		return fmt.Errorf("write UDP telemetry: %w", err)
	}
	return nil
}

// Close releases the sender socket.
func (s *Sender) Close() error {
	return s.conn.Close()
}

// Receiver receives JSON telemetry datagrams on one bound UDP socket.
type Receiver struct {
	conn            *net.UDPConn
	maxDatagramSize int
	done            chan struct{}
}

// Listen binds a UDP socket to address. A port of 0 asks the OS for an
// available ephemeral port, which is useful in tests.
func Listen(address string, maxDatagramSize int) (*Receiver, error) {
	if maxDatagramSize <= 0 {
		return nil, errors.New("UDP maximum datagram size must be positive")
	}
	udpAddress, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return nil, fmt.Errorf("resolve UDP listen address: %w", err)
	}
	conn, err := net.ListenUDP("udp", udpAddress)
	if err != nil {
		return nil, fmt.Errorf("listen for UDP telemetry: %w", err)
	}
	return &Receiver{conn: conn, maxDatagramSize: maxDatagramSize, done: make(chan struct{})}, nil
}

// Address returns the local address currently bound by the receiver.
func (r *Receiver) Address() string {
	return r.conn.LocalAddr().String()
}

// Done closes after the receive loop has released its socket and output channel.
func (r *Receiver) Done() <-chan struct{} {
	return r.done
}

// Start launches the receive loop and returns its output channel. The receive
// loop owns and closes the channel when ctx is cancelled or the socket closes.
func (r *Receiver) Start(ctx context.Context) <-chan model.Telemetry {
	out := make(chan model.Telemetry, outputBufferSize)
	go r.receive(ctx, out)
	return out
}

func (r *Receiver) receive(ctx context.Context, out chan<- model.Telemetry) {
	defer close(r.done)
	defer close(out)
	defer r.conn.Close()

	buffer := make([]byte, r.maxDatagramSize)
	for {
		if err := r.conn.SetReadDeadline(time.Now().Add(readPollInterval)); err != nil {
			log.Printf("UDP receiver deadline error: %v", err)
			return
		}

		n, senderAddress, err := r.conn.ReadFromUDP(buffer)
		if err != nil {
			if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				return
			}
			if networkError, ok := err.(net.Error); ok && networkError.Timeout() {
				continue
			}
			log.Printf("UDP receiver read error: %v", err)
			continue
		}

		var telemetry model.Telemetry
		if err := json.Unmarshal(buffer[:n], &telemetry); err != nil {
			log.Printf("UDP receiver discarded malformed packet from %s: %v", senderAddress, err)
			continue
		}
		if err := telemetry.Validate(); err != nil {
			log.Printf("UDP receiver discarded invalid telemetry from %s: %v", senderAddress, err)
			continue
		}

		select {
		case out <- telemetry:
		case <-ctx.Done():
			return
		}
	}
}
