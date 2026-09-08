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

## Stage 3: concurrent telemetry collector

### Responsibility

`internal/collector` schedules repeated sampling. It owns the concurrent
behavior, while each `simulator.Simulator` remains responsible only for
creating one sample through `Next`.

### Architecture

```text
AP-01 Simulator -- producer goroutine --+
AP-02 Simulator -- producer goroutine --+--> telemetry channel --> consumer
AP-03 Simulator -- producer goroutine --+         (buffer: 16)
                                                    ^
                         coordinator waits for all producers, then closes it
```

### Files

- `internal/collector/collector.go` adds ticker scheduling, context-driven
  shutdown, a shared output channel, and coordinated closure.
- `internal/collector/collector_test.go` checks concurrent production,
  validation, cancellation, closure, and invalid configuration.

### Goroutines and lifecycle

`Collector.Start` starts `c.produce` once per simulator. Each producer owns a
`time.Ticker`, waits for either a tick or cancellation, calls `sim.Next`, then
sends to the common channel. It can block waiting for a tick or waiting for
channel buffer space. Cancellation ends it even when a send is blocked.

`Start` also starts one coordinator goroutine. It blocks in `WaitGroup.Wait`.
After every producer has returned, it closes the output channel and exits.
No producer closes the shared channel.

### Channel ownership and backpressure

`Start` creates `chan model.Telemetry` with buffer size 16 and returns it as a
receive-only `<-chan model.Telemetry`. Producers send through a send-only
`chan<- model.Telemetry`; the caller is the consumer. The coordinator owns
closure after `Wait` confirms no producer can send again.

The small buffer absorbs short consumer delays. When full, producers block;
this applies backpressure and preserves telemetry instead of silently dropping
it. A consumer that never reads will stall a producer, but cancellation still
unblocks it and permits clean shutdown.

### WaitGroup, context, select, and ticker

`Start` calls `producers.Add` before launching workers. Each `produce` defers
`producers.Done`; the coordinator calls `producers.Wait` before `close(out)`.

`context.Context` carries request-scoped cancellation. `ctx.Done()` returns a
channel that is closed on cancellation. The first `select` waits for a ticker
event or that cancellation signal. After a tick, the second `select` waits to
send telemetry or to observe cancellation, preventing a blocked send from
leaking the goroutine. A context is preferable to a global boolean because it
is composable, can have deadlines, and safely broadcasts cancellation.

Each producer creates and owns its ticker. `ticker.Stop()` is deferred so the
runtime releases ticker resources when the producer exits.

### Concurrency risks

- **Leaks:** context cancellation returns every producer; `Wait` lets the
  coordinator finish.
- **Deadlock from a full channel:** a producer's send `select` also listens to
  `ctx.Done`.
- **Premature close / send-on-closed panic:** only the coordinator closes the
  channel, after `Wait` proves all senders stopped.
- **Races:** every producer receives a distinct simulator, so no two workers
  mutate the same `rand.Rand`. The collector has no mutable shared map.

A caller must start a collector only once, as documented; starting the same
collector twice would make its simulator instances shared between two groups
of producers.

### One sample's journey

For AP-01, `Collector.Start` launches `c.produce`. On its ticker event,
`produce` calls `(*simulator.Simulator).Next`, receives a `model.Telemetry`,
and sends it to `out`. The consumer receives it from the `<-chan
model.Telemetry` returned by `Start`.

### Stage 3 interview questions

**How is a goroutine different from an OS thread?** A goroutine is a lightweight
unit scheduled by the Go runtime; the runtime multiplexes many goroutines onto
a smaller set of OS threads.

**Why use goroutines here?** APs generate independently, so one lightweight
worker per AP models that behavior without blocking other APs.

**Who owns channel closure?** The coordinator that knows all producers have
finished. Senders must not independently close a channel shared by others.

**Why use a buffered channel?** Its 16 slots absorb a short burst. Once full,
senders block, which preserves data and makes slow-consumer pressure visible.

**What happens when a channel send blocks?** The goroutine pauses until a
receiver/buffer slot is available. Here `select` also allows cancellation to
end that wait.

**What does `select` do?** It waits until one of multiple channel operations
can proceed. Here it chooses between sampling or cancellation, then sending or
cancellation.

**Why use context cancellation?** It composes across components, supports
deadlines, and broadcasts shutdown without unsafe global mutable state.

**What is a goroutine leak?** A goroutine that can never finish, often because
it blocks forever. This design gives every worker a context cancellation path.

## Stage 4: thread-safe latest-state monitor

### Architecture

```text
collector output channel --> Monitor.Run --> Monitor.states map --> future API readers
                                  |                 ^
                                  +-- Update -------+  (protected by RWMutex)
```

### Data structure and responsibility

`Monitor` owns `states map[string]model.APState`. `New` populates one entry per
configured `model.AP`, so static AP metadata lives in `APState.AP` and the
latest received sample lives in `APState.Telemetry`. `Update` only accepts
known AP IDs; it returns `ErrUnknownAP` otherwise rather than silently turning
untrusted input into configuration.

### Locking

The map is shared mutable state. Without a mutex, a collector writer could
assign `states["ap-02"]` while a future HTTP handler reads it. Go maps are not
safe for simultaneous reads and writes: this can produce a data race or a
fatal concurrent-map access error.

`Update` validates before locking, then uses `mu.Lock` and `mu.Unlock` while it
looks up and replaces one map value. `Get` uses `mu.RLock`/`mu.RUnlock` while
copying one value. `List` uses `RLock` only while copying all values to a new
slice, then unlocks before sorting.

`Lock` grants exclusive write access; `Unlock` releases it. `RLock` grants a
shared read lock; multiple readers may hold it together, but writers cannot.
`RUnlock` releases a read lock. `RWMutex` fits this anticipated workload:
many API reads with periodic telemetry writes.

### Run loop and channel ownership

`Run(ctx, input)` is synchronous; its caller chooses whether it runs in a
goroutine. Its `select` waits for either `ctx.Done()` (then returns
`ctx.Err()`) or a receive from `input`. The two-value receive detects channel
closure through `ok == false`, then returns nil. For each received sample it
calls `Update` and returns an error for invalid or unknown telemetry.

The collector creates, sends to, and—after its producers exit—closes the
telemetry channel. The monitor only receives through `<-chan model.Telemetry`
and never closes it, because receivers do not own producer channels.

### Encapsulation and tests

The map is never returned. `Get` returns an `APState` value copy. `List`
creates a new slice and copies each value before releasing the lock; sorting
the returned slice cannot alter the monitor. Struct fields are values (strings,
numbers, and `time.Time`), so those copies expose no map-backed mutable data.

`go test -race ./...` instruments memory access and reports unsynchronized
conflicting reads/writes. The concurrent test runs telemetry updates alongside
`Get` and `List`; removing the locks would cause it to report map/value races.

### One sample's journey

AP-02 telemetry is sent by `collector.(*Collector).produce` to the channel
from `collector.(*Collector).Start`. `monitor.(*Monitor).Run` receives it and
calls `monitor.(*Monitor).Update`, which replaces `states["ap-02"].Telemetry`.
A future caller retrieves the copied entry with `monitor.(*Monitor).Get("ap-02")`.

### Stage 4 interview questions

**What is a race condition?** Conflicting concurrent access to shared data
where at least one access writes and ordering is not synchronized.

**When use a mutex versus a channel?** Use a mutex to protect shared in-memory
state; use channels to transfer values or coordinate ownership between
goroutines. They can be used together, as here.

**Why `RWMutex` rather than `Mutex`?** It permits multiple concurrent readers
while still requiring exclusive access for updates, which suits read-heavy API
access.

**What is the difference between read and write locks?** Read locks can be
held together by several readers; a write lock excludes both readers and other
writers.

**What is lock granularity?** The amount of work protected by one lock. This
monitor holds its lock only for map access and copies, not validation or
sorting.

**How can deadlocks occur?** For example, two goroutines acquire two locks in
opposite order. This monitor has one mutex and no nested locking, avoiding
that pattern.

**Who owns the shared state?** The `Monitor` owns its `states` map; callers
interact through `Update`, `Get`, and `List`.

**Why not return the map directly?** A caller could mutate it without locking,
bypass validation, and create races or corrupt monitor state.
