package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"go-wireless-monitor/internal/model"
	"go-wireless-monitor/internal/simulator"
)

func TestHTTPServerShutdown(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("Serve() error = %v, want http.ErrServerClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for HTTP server to stop")
	}
}

func TestRunAPSenderStopsAfterCancellation(t *testing.T) {
	sim, err := simulator.New(model.AP{ID: "ap-01"}, nil)
	if err != nil {
		t.Fatalf("simulator.New() error = %v", err)
	}
	sender := &fakeSender{}
	ctx, cancel := context.WithCancel(context.Background())

	var producers sync.WaitGroup
	producers.Add(1)
	go runAPSender(ctx, sim, sender, time.Hour, &producers)
	cancel()

	done := make(chan struct{})
	go func() {
		producers.Wait()
		close(done)
	}()
	select {
	case <-done:
		if !sender.closed {
			t.Error("sender was not closed after producer exit")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for AP producer to stop")
	}
}

func TestSplitTelemetryClosesOutputsAfterInputCloses(t *testing.T) {
	input := make(chan model.Telemetry, 1)
	telemetry := model.Telemetry{APID: "ap-01"}
	input <- telemetry
	close(input)

	monitorOutput, alertOutput, done := splitTelemetry(context.Background(), input)
	if got := <-monitorOutput; got != telemetry {
		t.Errorf("monitor output = %+v, want %+v", got, telemetry)
	}
	if got := <-alertOutput; got != telemetry {
		t.Errorf("alert output = %+v, want %+v", got, telemetry)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for telemetry fan-out to stop")
	}
	if _, ok := <-monitorOutput; ok {
		t.Error("monitor output remains open after input closure")
	}
	if _, ok := <-alertOutput; ok {
		t.Error("alert output remains open after input closure")
	}
}

type fakeSender struct {
	closed bool
}

func (s *fakeSender) Send(model.Telemetry) error { return nil }

func (s *fakeSender) Close() error {
	s.closed = true
	return nil
}
