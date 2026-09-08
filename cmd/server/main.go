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
	"sync"
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
	shutdownGrace = 5 * time.Second
)

func main() {
	rootCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	producerCtx, cancelProducers := context.WithCancel(context.Background())
	receiverCtx, cancelReceiver := context.WithCancel(context.Background())
	defer cancelProducers()
	defer cancelReceiver()

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

	telemetry := udpReceiver.Start(receiverCtx)
	monitorInput, alertInput, fanoutDone := splitTelemetry(context.Background(), telemetry)
	monitorDone := make(chan struct{})
	alertsDone := make(chan struct{})
	go runMonitor(context.Background(), mon, monitorInput, monitorDone)
	go recordAlerts(context.Background(), alertInput, history, alertsDone)

	var producers sync.WaitGroup
	producers.Add(len(simulators))
	for _, sim := range simulators {
		sender, err := udp.NewSender(udpAddress)
		if err != nil {
			log.Fatal(err)
		}
		go runAPSender(producerCtx, sim, sender, time.Second, &producers)
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
	serverStopped := false
	select {
	case err := <-serverErrors:
		serverStopped = true
		if !errors.Is(err, http.ErrServerClosed) {
			log.Printf("HTTP server error: %v", err)
		}
	case <-rootCtx.Done():
		log.Print("shutdown signal received")
	}

	// Stop sources before the UDP receiver so producers do not generate avoidable
	// write errors against a socket that is shutting down.
	cancelProducers()
	producers.Wait()
	cancelReceiver()
	<-udpReceiver.Done()
	<-fanoutDone
	<-monitorDone
	<-alertsDone

	if !serverStopped {
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), shutdownGrace)
		if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("HTTP server shutdown error: %v", err)
		}
		cancelShutdown()
		<-serverErrors
	}
	log.Print("shutdown complete")
}

type telemetrySender interface {
	Send(model.Telemetry) error
	Close() error
}

func runAPSender(
	ctx context.Context,
	sim *simulator.Simulator,
	sender telemetrySender,
	interval time.Duration,
	producers *sync.WaitGroup,
) {
	defer producers.Done()
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
) (<-chan model.Telemetry, <-chan model.Telemetry, <-chan struct{}) {
	monitorOutput := make(chan model.Telemetry, 16)
	alertOutput := make(chan model.Telemetry, 16)
	done := make(chan struct{})
	go func() {
		defer close(done)
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
	return monitorOutput, alertOutput, done
}

func runMonitor(ctx context.Context, mon *monitor.Monitor, input <-chan model.Telemetry, done chan<- struct{}) {
	defer close(done)
	if err := mon.Run(ctx, input); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("monitor telemetry error: %v", err)
	}
}

func recordAlerts(
	ctx context.Context,
	input <-chan model.Telemetry,
	history *alert.History,
	done chan<- struct{},
) {
	defer close(done)
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
