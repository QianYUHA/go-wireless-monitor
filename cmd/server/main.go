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
	"go-wireless-monitor/internal/model"
	"go-wireless-monitor/internal/monitor"
	"go-wireless-monitor/internal/simulator"
	"go-wireless-monitor/internal/transport/udp"
)

const (
	serverAddress = ":8080"
	udpAddress    = "127.0.0.1:9000"
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
	udpReceiver, err := udp.Listen(udpAddress, udp.DefaultMaxDatagramSize)
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

	telemetry := udpReceiver.Start(ctx)
	monitorInput, alertInput := splitTelemetry(ctx, telemetry)
	go runMonitor(ctx, mon, monitorInput)
	go recordAlerts(ctx, alertInput, history)

	for _, sim := range simulators {
		sender, err := udp.NewSender(udpAddress)
		if err != nil {
			log.Fatal(err)
		}
		go runAPSender(ctx, sim, sender, time.Second)
	}

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

func runAPSender(ctx context.Context, sim *simulator.Simulator, sender *udp.Sender, interval time.Duration) {
	defer sender.Close()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := sender.Send(sim.Next()); err != nil {
				log.Printf("UDP telemetry send error: %v", err)
			}
		}
	}
}

func splitTelemetry(
	ctx context.Context,
	input <-chan model.Telemetry,
) (<-chan model.Telemetry, <-chan model.Telemetry) {
	monitorOutput := make(chan model.Telemetry, 16)
	alertOutput := make(chan model.Telemetry, 16)
	go func() {
		defer close(monitorOutput)
		defer close(alertOutput)
		for {
			select {
			case <-ctx.Done():
				return
			case telemetry, ok := <-input:
				if !ok {
					return
				}
				select {
				case <-ctx.Done():
					return
				case monitorOutput <- telemetry:
				}
				select {
				case <-ctx.Done():
					return
				case alertOutput <- telemetry:
				}
			}
		}
	}()
	return monitorOutput, alertOutput
}

func runMonitor(ctx context.Context, mon *monitor.Monitor, input <-chan model.Telemetry) {
	if err := mon.Run(ctx, input); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("monitor telemetry error: %v", err)
	}
}

func recordAlerts(
	ctx context.Context,
	input <-chan model.Telemetry,
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
			if err := history.Record(telemetry); err != nil {
				log.Printf("alert telemetry error: %v", err)
			}
		}
	}
}
