package udp

import (
	"context"
	"net"
	"testing"
	"time"

	"go-wireless-monitor/internal/model"
)

func TestSenderReceiverIntegration(t *testing.T) {
	receiver := newReceiver(t)
	ctx, cancel := context.WithCancel(context.Background())
	out := receiver.Start(ctx)

	sender, err := NewSender(receiver.Address())
	if err != nil {
		t.Fatalf("NewSender() error = %v", err)
	}
	defer sender.Close()

	want := testTelemetry("ap-01")
	if err := sender.Send(want); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	got := receiveTelemetry(t, out)
	if got != want {
		t.Errorf("received telemetry = %+v, want %+v", got, want)
	}
	if err := got.Validate(); err != nil {
		t.Errorf("received telemetry validation error = %v", err)
	}

	cancel()
	waitForClosed(t, out)
}

func TestReceiverDiscardsInvalidPacketsAndContinues(t *testing.T) {
	receiver := newReceiver(t)
	ctx, cancel := context.WithCancel(context.Background())
	out := receiver.Start(ctx)
	defer cancel()

	raw, err := net.Dial("udp", receiver.Address())
	if err != nil {
		t.Fatalf("net.Dial() error = %v", err)
	}
	defer raw.Close()
	if _, err := raw.Write([]byte("not JSON")); err != nil {
		t.Fatalf("raw malformed write error = %v", err)
	}
	if _, err := raw.Write([]byte(`{"ap_id":"","timestamp":"2026-09-08T12:00:00Z"}`)); err != nil {
		t.Fatalf("raw invalid write error = %v", err)
	}

	sender, err := NewSender(receiver.Address())
	if err != nil {
		t.Fatalf("NewSender() error = %v", err)
	}
	defer sender.Close()
	want := testTelemetry("ap-02")
	if err := sender.Send(want); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if got := receiveTelemetry(t, out); got != want {
		t.Errorf("received telemetry = %+v, want %+v", got, want)
	}
}

func TestReceiverStopsAndClosesOutputOnCancellation(t *testing.T) {
	receiver := newReceiver(t)
	ctx, cancel := context.WithCancel(context.Background())
	out := receiver.Start(ctx)
	cancel()

	waitForClosed(t, out)
}

func newReceiver(t *testing.T) *Receiver {
	t.Helper()
	receiver, err := Listen("127.0.0.1:0", DefaultMaxDatagramSize)
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	return receiver
}

func testTelemetry(id string) model.Telemetry {
	return model.Telemetry{
		APID:             id,
		RSSI:             -55,
		LatencyMS:        25,
		PacketLossPct:    1,
		ConnectedClients: 12,
		Timestamp:        time.Date(2026, time.September, 8, 12, 0, 0, 0, time.UTC),
	}
}

func receiveTelemetry(t *testing.T, out <-chan model.Telemetry) model.Telemetry {
	t.Helper()
	select {
	case telemetry, ok := <-out:
		if !ok {
			t.Fatal("receiver output closed before telemetry arrived")
		}
		return telemetry
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for received telemetry")
		return model.Telemetry{}
	}
}

func waitForClosed(t *testing.T, out <-chan model.Telemetry) {
	t.Helper()
	select {
	case _, ok := <-out:
		if ok {
			t.Fatal("receiver output produced telemetry, want closed channel")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for receiver output to close")
	}
}
