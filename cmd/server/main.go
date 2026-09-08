// Command server runs the wireless AP telemetry monitoring service.
package main

import (
	"context"
	"errors"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go-wireless-monitor/internal/alert"
	"go-wireless-monitor/internal/api"
	"go-wireless-monitor/internal/collector"
	"go-wireless-monitor/internal/model"
	"go-wireless-monitor/internal/monitor"
	"go-wireless-monitor/internal/simulator"
)

const (
	serverAddress = ":8080"
	alertLimit    = 100
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	aps := []model.AP{
		{ID: "ap-01", Name: "Lobby", Location: "Floor 1"},
		{ID: "ap-02", Name: "Lab", Location: "Floor 2"},
		{ID: "ap-03", Name: "Office", Location: "Floor 3"},
	}
	simulators := make([]*simulator.Simulator, 0, len(aps))
	for i, ap := range aps {
		sim, err := simulator.New(ap, rand.New(rand.NewSource(time.Now().UnixNano()+int64(i))))
		if err != nil {
			log.Fatal(err)
		}
		simulators = append(simulators, sim)
	}
	telemetryCollector, err := collector.New(simulators, time.Second)
	if err != nil {
		log.Fatal(err)
	}
	mon, err := monitor.New(aps)
	if err != nil {
		log.Fatal(err)
	}
	history, err := alert.NewHistory(alertLimit)
	if err != nil {
		log.Fatal(err)
	}

	telemetry := telemetryCollector.Start(ctx)
	go consumeTelemetry(ctx, telemetry, mon, history)

	server := &http.Server{
		Addr:    serverAddress,
		Handler: api.NewHandler(mon, history).Routes(),
	}
	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- server.ListenAndServe()
	}()

	log.Printf("wireless monitor listening on %s", serverAddress)
	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	case <-ctx.Done():
		// Stage 8 will replace Close with deadline-aware graceful shutdown.
		if err := server.Close(); err != nil {
			log.Printf("HTTP server close error: %v", err)
		}
		<-serverErrors
	}
}

func consumeTelemetry(
	ctx context.Context,
	input <-chan model.Telemetry,
	mon *monitor.Monitor,
	history *alert.History,
) {
	for {
		select {
		case <-ctx.Done():
			return
		case telemetry, ok := <-input:
			if !ok {
				return
			}
			if err := mon.Update(telemetry); err != nil {
				log.Printf("monitor telemetry error: %v", err)
				continue
			}
			if err := history.Record(telemetry); err != nil {
				log.Printf("alert telemetry error: %v", err)
			}
		}
	}
}
