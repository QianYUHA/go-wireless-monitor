# go-wireless-monitor

A small, interview-ready Go service that simulates wireless access points (APs),
sends telemetry over UDP, maintains thread-safe latest state, detects network
conditions, and exposes the results through an HTTP API.

## Architecture

```text
AP simulators
    |
    v
UDP senders --> UDP receiver --> telemetry channel --> monitor --> HTTP API
                                                    \
                                                     --> alerts
```

## Features

- Multi-AP telemetry simulation
- Goroutines, channels, contexts, and WaitGroups
- JSON UDP telemetry on `127.0.0.1:9000`
- Telemetry validation and malformed-packet handling
- Thread-safe latest AP state with `sync.RWMutex`
- RSSI, latency, and packet-loss alerts
- REST-style standard-library HTTP API on `:8080`
- Graceful SIGINT/SIGTERM shutdown
- Unit, integration, race-detector, and vet coverage

## API

```bash
curl http://localhost:8080/health
curl http://localhost:8080/aps
curl http://localhost:8080/aps/ap-01
curl http://localhost:8080/alerts
```

Endpoints:

- `GET /health`
- `GET /aps`
- `GET /aps/{id}`
- `GET /alerts`

## Run

```bash
go run ./cmd/server
```

Press `Ctrl+C` to stop producers, UDP reception, processing goroutines, and
the HTTP server cleanly.

## Test

```bash
go test ./...
go test -race ./...
go vet ./...
```

## Key engineering concepts

- **Goroutines:** independent AP senders, UDP reception, telemetry processing,
  and HTTP serving.
- **Channels:** transfer validated telemetry between transport, monitor, and
  alert processing with explicit ownership.
- **WaitGroup:** main waits for all AP producer goroutines before stopping the
  UDP receiver.
- **RWMutex:** protects concurrent reads and writes of latest AP state and
  alert history.
- **Context:** coordinates cancellation from SIGINT/SIGTERM.
- **UDP vs TCP:** UDP is low-overhead and best-effort, suitable for frequent
  telemetry; TCP is preferred when reliable ordered delivery is required.
- **Graceful shutdown:** producers stop first, downstream channels close in
  ownership order, then `http.Server.Shutdown` drains active HTTP requests.
