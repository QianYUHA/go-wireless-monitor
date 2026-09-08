# go-wireless-monitor learning notes

This project is a deliberately small wireless access point (AP) telemetry
service. Each stage adds one understandable building block.

## Architecture (planned)

```text
+------------------+       +-------------------+       +----------------+
| AP simulators    | ----> | telemetry channel | ----> | monitor        |
| (goroutines)     |       | / UDP receiver    |       | latest AP map  |
+------------------+       +-------------------+       +-------+--------+
                                                                  |
                                                         +--------v--------+
                                                         | alert detector  |
                                                         +--------+--------+
                                                                  |
                                                         +--------v--------+
                                                         | HTTP API        |
                                                         | /health /aps    |
                                                         | /alerts         |
                                                         +-----------------+
```

## Stage 1: telemetry and AP data models

### Responsibility

`internal/model` defines the vocabulary shared by every later component. A
telemetry sample represents one observation; an AP represents relatively
stable AP metadata; an AP state combines the AP and its most recent sample.

### Files

- `go.mod` declares this project as the `go-wireless-monitor` Go module.
- `internal/model/telemetry.go` contains the data models and validation.
- `LEARNING_NOTES.md` records the design and interview explanations.

### Important Go syntax and concepts

`type Telemetry struct { ... }` creates a struct: a named group of related
fields. JSON tags, for example ``json:"ap_id"``, tell `encoding/json` what
field name to use when the HTTP and UDP stages serialize a value.

`time.Time` represents the instant at which the AP produced the sample.
`Telemetry.Validate() error` is a value-receiver method. It returns `nil` on
success and an `error` on invalid input, which is Go's usual explicit error
handling style.

`APState` embeds named fields rather than duplicating telemetry fields. This
keeps the distinction clear between static AP information and changing data.

### Data flow

Later, a simulator or UDP receiver will create a `Telemetry` value, assign its
timestamp, call `Validate`, and pass it to the monitor. The monitor will pair
it with the matching `AP` and save an `APState` as that AP's latest state.

### Concurrency status

Stage 1 starts no goroutines and creates no channels. `Telemetry`, `AP`, and
`APState` are plain values and own no shared mutable state, so mutexes are not
needed yet. A future monitor must synchronize writes and reads of its map;
that concern intentionally belongs to Stage 4.

### Interview questions and short answers

**Why separate `AP` from `Telemetry`?** AP identity and location are stable
configuration, whereas RSSI, latency, loss, client count, and timestamp are
repeated measurements. Separating them avoids repeating the design concept in
every component.

**Why return an error instead of panicking on invalid telemetry?** Invalid
network input is expected operationally. Returning an error lets the caller
log or reject that single message while the service continues running.

**Why use JSON tags?** They make the external HTTP/UDP representation explicit
and decouple Go field capitalization from the API contract.
