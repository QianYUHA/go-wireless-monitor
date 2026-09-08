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

## Stage 2: single-AP telemetry simulator

### Responsibility

`internal/simulator` models one AP producing one sample when its caller asks.
It does not run a background loop: `Next` returns exactly one new,
realistic-looking `model.Telemetry` value.

### Files

- `internal/simulator/simulator.go` adds the simulator implementation.
- `internal/simulator/simulator_test.go` verifies generated values by their
  properties rather than particular random values.

### Important Go syntax and concepts

`Simulator` has unexported fields (`ap` and `random`) so callers use its small
public API instead of changing its configuration accidentally. `New` is a
constructor function: it validates the AP ID and returns `(*Simulator, error)`.

`Next` has a pointer receiver, `func (s *Simulator) Next()`. The pointer means
the method uses the same simulator and its same random-number stream; it also
avoids copying the `rand.Rand` state. `New` returns `&Simulator{...}`, a
pointer to the constructed struct.

`math/rand` produces pseudo-random values from a seed. A caller may supply a
seeded `*rand.Rand` to get a repeatable sequence, or pass `nil` for a source
seeded with the current time. The simulator maps random values into bounded
ranges: RSSI -90 through -30 dBm, latency 5 through 150 ms, packet loss from
0 up to 10%, and 0 through 50 clients.

### Data flow

```text
model.AP (including ID) -> simulator.New -> Simulator.Next()
    -> model.Telemetry -> caller
```

`Next` copies the AP ID into telemetry, generates each metric, and records the
current UTC time. It returns a value; the caller can then call `Validate`.

### Concurrency status

There are still no goroutines, channels, mutexes, or shared maps. The caller
synchronously invokes `Next` and receives its result directly. A `rand.Rand`
is stateful and should not be called concurrently without synchronization, but
that is not a risk in this single-caller Stage 2 design. Stage 3 will give
each concurrent AP simulator clear ownership of its own simulator instance.

### Testing approach

The test injects a deterministic random source so its execution is stable,
but it never asserts the exact output sequence. It creates 100 samples and
checks invariant properties: preserved AP ID, non-zero timestamp, each metric
within its documented bounds, and successful `Telemetry.Validate()`.

### Stage 2 interview questions

**Why make the simulator return one sample instead of running a loop?** A
single-operation API is easy to test and gives the caller control of timing;
Stage 3 can add looping and concurrency around it.

**Why inject `*rand.Rand`?** It permits repeatable tests and lets production
choose a time-seeded source without coupling the generator to global state.

**Why use a pointer receiver for `Next`?** `rand.Rand` maintains sequence
state. A pointer receiver preserves use of the same state and avoids copying
the simulator.

**Are the generated values really random?** They are pseudo-random: generated
by a deterministic algorithm from a seed. That is suitable for simulation,
but not for security-sensitive values.

**Why test ranges instead of exact values?** Exact random outputs are an
implementation detail. Range and validation checks test the public behavior
that must remain true if the generator implementation changes.
