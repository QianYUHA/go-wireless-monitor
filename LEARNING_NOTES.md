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

## Stage 5: stateless alert detection

### Architecture

```text
collector --> telemetry --> monitor --> latest AP state --> future API
                 |
                 +-------> alert.Detect --> []alert.Alert --> future API/history
```

Alert detection is independent in this stage. A future application coordinator
can give the same validated telemetry sample to both the monitor and detector.
The monitor remains responsible only for latest state; it does not yet own an
alert-history collection.

### Alert model

`Alert` contains the AP ID, typed `AlertType`, typed `Severity`, message,
observed numeric value, threshold, and the telemetry timestamp. The types are
easy to serialize later because they have underlying type `string`.

`AlertWeakSignal`, `AlertHighLatency`, and `AlertHighPacketLoss` are typed
constants, not arbitrary string literals scattered through callers. Typed
constants prevent accidental mixing of unrelated string values and make the
public API self-documenting. Weak signal and high latency are warnings; high
packet loss is marked critical in this initial policy.

### Detector and validation

`alert.Detect(telemetry) ([]Alert, error)` is a stateless function. It calls
`Telemetry.Validate` itself, returning no alerts and an error for invalid
input. Monitor validation also protects its own boundary, but the detector is
an independently callable component, so validating here prevents malformed
telemetry from becoming a misleading alert. The small duplicate validation
cost is preferable to relying on every future caller to remember it.

Each rule is an independent `if`, so one valid sample can append zero through
three alerts. The detector allocates a fresh result slice for every call and
keeps no history or mutable package state, making concurrent calls naturally
safe and requiring no mutex.

### Threshold behavior

- RSSI triggers only when `RSSI < -70`; exactly `-70` does not alert.
- Latency triggers only when `LatencyMS > 100`; exactly `100` does not alert.
- Loss triggers only when `PacketLossPct > 5`; exactly `5` does not alert.

The `Alert.Timestamp` is the telemetry timestamp, which identifies when the
condition was observed rather than when the detector happened to execute.

### Data journey

For AP-02 with RSSI -82 dBm, latency 140 ms, and loss 7%, `alert.Detect`
validates the sample then appends a weak-signal alert, a high-latency alert,
and a high-packet-loss alert. Each has AP ID `ap-02`, its rule-specific type,
the observed value, its threshold, and the sample timestamp.

### Design tradeoffs

Thresholds are private constants to keep the current learning project obvious
and avoid configuration parsing. A later configuration struct or file could
inject thresholds into a detector. We do not add a rule engine because three
fixed numeric comparisons do not justify dynamic rule registration, complex
state, or additional failure modes.

### Stage 5 interview questions

**What is a stateless service?** It retains no request-specific or historical
data between calls. It is simpler to test and naturally safe for concurrent
use when its inputs are values.

**Why use typed constants?** They constrain intended values, make APIs clear,
and avoid repeatedly writing arbitrary strings that can contain typos.

**Why are threshold operators strict?** The policy says only values beyond the
limit are abnormal, so equality must remain healthy and tests protect that
boundary.

**Can one event create multiple alerts?** Yes. Metrics represent independent
conditions, so every violated rule emits its own explanatory alert.

**Why validate in the detector when the monitor validates too?** The detector
is usable independently; validating at its public boundary prevents invalid
input from producing alert output.

**Who owns alert history?** No component does in Stage 5. Detection returns
values, and a later API/application layer can choose a bounded history policy.

**How are responsibilities separated?** The collector schedules, the monitor
stores latest state, and the detector evaluates one telemetry value.

**How would you extend the rules?** Add a typed alert type, threshold, and
independent condition, or later inject thresholds through a configuration
struct without requiring a full rule engine.

## Stage 6: standard-library HTTP API

### Architecture

```text
simulator.Next -> collector producer -> telemetry channel -> consumeTelemetry
                                                            |          |
                                                            v          v
                                                     monitor.Update  history.Record
                                                            |          |
                                                            +----+-----+
                                                                 |
                                                           HTTP API
                                                    /health /aps /aps/{id} /alerts
```

`cmd/server/main.go` creates three configured APs, one simulator per AP, a
collector, monitor, bounded alert history, and HTTP handler. `consumeTelemetry`
is the single application-level consumer: it updates the monitor then detects
and records any alerts for the same sample.

### HTTP server and routing

The standard library's `net/http` supplies `Server`, `ServeMux`, request and
response types, and concurrent request serving. Main starts `http.Server` on
`:8080` using `ListenAndServe`. When a request arrives, the mux chooses a
matching handler and the HTTP server runs it independently from other requests.

`Handler.Routes` registers Go 1.22+ method-qualified patterns:

- `GET /health`
- `GET /aps`
- `GET /aps/{id}`
- `GET /alerts`

The exact `/aps` pattern is distinct from `/aps/{id}`. For the latter,
`r.PathValue("id")` extracts the standard-library path parameter. Since the
patterns include `GET`, `ServeMux` responds with 405 for unsupported methods
on matching paths.

### Handlers and JSON

`handleHealth` returns `{"status":"ok"}` with 200. `handleAPs` calls
`Monitor.List` and returns all state values with 200. `handleAP` calls
`Monitor.Get`; it returns an AP state with 200 or a JSON error with 404.
`handleAlerts` calls `History.List` and returns recent alerts with 200.

`writeJSON` encodes values using `encoding/json` into a buffer before sending
headers. This allows an actual encoding failure to become 500 before a
response is committed. Successful responses set `Content-Type:
application/json`, write the desired status, then write the encoded body.
JSON tags on Stage 1 models determine field names such as `ap_id` and
`latency_ms` in these responses.

### Concurrency and alert history

`net/http` can run several handlers concurrently. A telemetry goroutine may
call `Monitor.Update` with `Lock` while request A calls `Get` with `RLock` and
request B calls `List` with `RLock`; the monitor's `RWMutex` coordinates them.
Handlers add no redundant lock.

`alert.History` is new shared state. It owns a bounded `[]Alert`, protected by
its own `RWMutex`. `consumeTelemetry` writes through `Record`/`Add`; HTTP
handlers read through `List`. When the configured maximum is exceeded, `Add`
discards oldest entries and keeps the newest alerts. `List` returns a copied
slice so handlers cannot mutate stored history.

### HTTP request lifecycle

For `GET /aps/ap-02`: a client uses an HTTP request over a TCP connection; the
`net/http` server accepts it and passes it to `ServeMux`; the `GET /aps/{id}`
handler reads `id` via `PathValue`; `Monitor.Get("ap-02")` obtains `RLock` and
copies its `APState`; the handler encodes the copy as JSON and sends a 200 HTTP
response. Unknown IDs instead produce a 404 JSON error. Internal errors are
not exposed verbatim, avoiding leakage of implementation details.

### Testing and lifecycle

`httptest.NewRequest` and `httptest.NewRecorder` invoke the router entirely in
memory, so tests need no listener on port 8080. They verify status codes,
content type, and JSON bodies for all endpoints, including 404 and 405.

Main cancels the collector and consumer on SIGINT/SIGTERM and closes the HTTP
server. Stage 8 will add deadline-aware `Server.Shutdown` and coordinated
waiting; that fuller graceful-shutdown work is intentionally deferred.

### Stage 6 interview questions

**How does Go handle concurrent HTTP requests?** `net/http` serves requests
concurrently, so handlers must treat shared dependencies as concurrent.

**What is a handler?** A function or object that receives `ResponseWriter` and
`Request`, performs application work, and writes an HTTP response.

**Why use JSON tags?** They define stable external field names without tying
the API format to Go's exported field naming.

**Why use 404 for an unknown AP?** The requested resource does not exist in
the configured monitor state.

**What is dependency injection here?** Main constructs monitor and history,
then passes them into `api.NewHandler` rather than using global variables.

**What does `httptest` provide?** In-memory requests and recorders for testing
handlers without binding a real TCP port.

**How do HTTP and TCP relate?** HTTP defines request/response semantics at the
application layer; the server usually carries those messages over TCP.

**Why are API reads thread-safe?** Handlers call monitor methods that use
`RWMutex`; they never access the map directly.

**Why should handlers not access maps directly?** It would bypass locking,
encapsulation, validation rules, and future storage changes.

**Why choose standard `net/http` over a framework?** This API is small and
needs only routing and JSON. A framework can help with larger applications,
middleware ecosystems, or advanced binding, but adds concepts unnecessary
for this project.

## Stage 7: UDP telemetry transport

### Architecture

```text
Simulator.Next -> runAPSender -> udp.Sender.Send -> UDP socket
                                                   |
                                                   v
                                           localhost UDP receiver
                                                   |
                                                   v
                                      Receiver output channel (validated telemetry)
                                                   |
                                                   v
                                             splitTelemetry
                                             /              \
                                            v                v
                                  Monitor.Run -> state     alert History.Record
                                            \                /
                                             v              v
                                                  HTTP API
```

The runnable server binds its UDP receiver to `127.0.0.1:9000` and its HTTP
server to `:8080`. Stage 3's in-process collector remains in the repository
for learning and tests, but `cmd/server` no longer creates or starts it.

### UDP sender

`udp.Sender` holds a `*net.UDPConn`. `udp.NewSender(destination string)`
resolves an address such as `127.0.0.1:9000` and calls `net.DialUDP`. This is a
*connected UDP* socket: it sets one default peer address for `Write`, but does
not create a TCP-style reliable connection or handshake.

`(*Sender).Send` validates `model.Telemetry`, marshals it with `json.Marshal`,
then calls `UDPConn.Write` to send one datagram. It returns address-resolution,
validation, JSON-marshalling, or socket-write errors to its caller. Each AP
sender goroutine owns a sender socket and logs send errors while continuing its
periodic loop.

### UDP receiver

`udp.Listen(address, maxDatagramSize)` resolves and binds a socket with
`net.ListenUDP`. `Receiver.Start(ctx)` creates the receive-only telemetry
channel and starts one `receive` goroutine. The loop's blocking operation is
`UDPConn.ReadFromUDP(buffer)`, which returns the byte count `n`, the sending
`*net.UDPAddr`, and an error.

Before each read, the receiver sets a 50 ms read deadline. UDP reads do not
accept a context directly, so a deadline wakes the loop periodically; it sees
`ctx.Err()` and exits on cancellation. The receive goroutine defers both
socket closure and output-channel closure. Invalid packet errors are logged
and skipped rather than terminating the service.

### Datagram lifecycle

For AP-01, `(*simulator.Simulator).Next` creates `model.Telemetry`.
`runAPSender` passes it to `(*udp.Sender).Send`, which JSON-marshals it and
calls `UDPConn.Write`. The OS puts the datagram through the localhost UDP
network stack to the bound receiver socket. `(*udp.Receiver).receive` returns
from `ReadFromUDP`, unmarshals `buffer[:n]`, calls `Telemetry.Validate`, and
sends it to its output channel. `splitTelemetry` forwards one copy to
`(*monitor.Monitor).Run`, which calls `(*Monitor).Update` to update state.

### Buffer handling

The receiver goroutine allocates one 4 KiB byte buffer before its loop. The
`n` returned by `ReadFromUDP` tells exactly how many bytes in that buffer
belong to the datagram, so JSON must decode `buffer[:n]`, not unused bytes
left from previous reads. A datagram larger than the supplied buffer is
truncated by the receive operation and its remainder is discarded; its JSON
will normally fail to decode and be logged as malformed. Four KiB is ample for
this small JSON telemetry format.

### UDP versus TCP and packet loss

UDP is connectionless and has low per-message overhead, but provides no
delivery, ordering, or duplicate-suppression guarantee. TCP is a
connection-oriented, reliable, ordered byte stream with retransmission and
additional protocol/state overhead. TCP is preferable when every event must
arrive in order, such as a financial command or configuration update.

If one frequent telemetry datagram is lost, the monitor keeps the most recent
previous state until the next sample arrives. That is acceptable for this
latest-state dashboard because new samples replace old ones quickly. It would
not be acceptable for a one-time safety shutdown command or billing event,
where loss must be detected and recovered.

### Concurrency and channel ownership

Stage 7 networking starts one `runAPSender` goroutine per AP in `main`; each
blocks on ticker events and exits on `ctx.Done`, closing its own sender socket.
`Receiver.Start` starts one `receive` goroutine; it blocks in `ReadFromUDP`
until a packet or deadline, exits after cancellation is observed, closes its
socket, then closes its output channel. `splitTelemetry` starts one fan-out
goroutine and closes its two downstream channels when input closes or context
is cancelled.

The receiver creates `chan model.Telemetry`, is its only sender, and is its
only closer. `splitTelemetry` receives from it. The monitor receives one
downstream channel; alert recording receives the other. Neither receiver nor
monitor closes an upstream channel, which makes ownership safe and prevents
send-on-closed panics.

### Error handling and JSON wire format

For a malformed UDP packet, `ReadFromUDP` succeeds, `json.Unmarshal` fails,
the receiver logs the sender address and error, then continues its loop. A
valid JSON packet with invalid telemetry similarly fails `Validate`, is logged,
and is not forwarded. A later valid packet is still decoded and sent.

JSON uses the Stage 1 struct tags as the wire contract. It is readable with
tools, easy to debug, and fully supported by Go's standard library. Its costs
are larger datagrams and parsing overhead versus binary formats. Protobuf or a
similar binary schema could reduce size and improve efficiency later, but is
not needed for this learning service.

### Socket-level and HTTP boundary

The application calls Go's `net` APIs, which use operating-system sockets. A
UDP sender hands one datagram to the OS network stack; a receiver socket bound
to an IP and port receives datagrams addressed to that endpoint. The receiver
turns transport bytes back into telemetry before the monitor sees them.

HTTP handlers only call `Monitor.Get` and `Monitor.List`; they do not know or
care whether updates originated from UDP, a simulator, or a future transport.
This `UDP -> monitor state` and `HTTP -> monitor state` boundary keeps network
parsing out of handlers and map management out of transport code.

### Design tradeoffs

UDP is isolated in `internal/transport/udp`, so socket handling, JSON wire
format, and datagram errors do not leak into simulators or the monitor. The
simulator generates a value; it does not update shared state. The monitor owns
state; it does not parse packets. No acknowledgements, retries, sequence
numbers, or TCP fallback are added: these would be a reliability protocol
beyond this project's deliberately lossy telemetry use case.

### Stage 7 interview questions

**How does UDP differ from TCP?** UDP sends independent, best-effort datagrams;
TCP provides a reliable ordered byte stream with connection state.

**What is a datagram?** A self-contained message with boundaries preserved by
UDP, unlike a TCP stream where applications frame messages themselves.

**What does binding a socket mean?** Reserving a local IP address and port so
the OS can deliver matching incoming traffic to the process.

**What happens when a UDP packet is lost?** It is not retransmitted by UDP;
the application receives no error for that particular missing datagram.

**Why use JSON for telemetry?** It is readable, debuggable, and standard
library supported, though larger and slower than binary formats.

**Why can `ReadFromUDP` block?** It waits for an incoming datagram. This
receiver uses read deadlines so cancellation is observed promptly.

**How does the receiver shut down?** Context cancellation is seen after a
short read deadline; the receive goroutine returns, closes the socket and
output channel.

**How is malformed input handled?** Decode/validation errors are logged and
dropped, while the receive loop continues for later packets.

**How does UDP integrate with channels?** The receiver translates each valid
datagram into a typed value and sends it to a Go channel for downstream logic.

**Why is network concurrency needed here?** Reads block waiting for packets,
so a receiver goroutine permits HTTP serving and AP senders to continue.

**Why is telemetry suitable for UDP?** New measurements arrive often, so an
occasional missing sample can be superseded by the next one.

**Why separate transport from monitor state?** It keeps socket parsing and
shared-map synchronization independently testable and replaceable.

## Stage 8: graceful shutdown and final project summary

### Shutdown model

`main` uses `signal.NotifyContext` to convert SIGINT and SIGTERM into root
context cancellation. On either a signal or HTTP server failure, it performs
shutdown in this order:

```text
cancel producer context -> WaitGroup.Wait
    -> cancel receiver context -> Receiver.Done
    -> fan-out input closes -> monitor and alert consumers finish
    -> http.Server.Shutdown (five-second deadline)
    -> process exits
```

Stopping producers before the receiver avoids avoidable UDP write errors during
intentional shutdown. The receiver's 50 ms read deadline wakes `ReadFromUDP` so
it can observe cancellation; its goroutine closes the transport channel. The
fan-out owns and closes its two downstream channels, so `Monitor.Run` and alert
recording exit from normal channel closure. Main waits for each completion
signal before calling `Server.Shutdown`.

`Server.Shutdown` stops new HTTP connections and waits for in-flight handlers
up to the deadline. `Server.Close` stops listeners/connections immediately;
it is more abrupt and remains useful only when a hard stop is required. An
expected `http.ErrServerClosed` during shutdown is not logged as a failure.

### Complete architecture and responsibilities

```text
AP config -> simulator.Next -> UDP Sender -> UDP socket -> UDP Receiver
                                                        |
                                                        v
                                                 telemetry channel
                                                        |
                                                        v
                                                    splitTelemetry
                                                   /              \
                                                  v                v
                                          Monitor.Run        History.Record
                                              |                   |
                                              +----- HTTP API -----+
```

- `model`: shared AP, telemetry, and latest-state data types plus validation.
- `simulator`: produces one realistic sample per explicit `Next` call.
- `collector`: retains the Stage 3 in-process concurrency lesson and tests;
  the Stage 7/8 runnable server instead uses UDP transport.
- `transport/udp`: encodes/sends and receives/decodes UDP JSON datagrams.
- `monitor`: owns the thread-safe latest-state map.
- `alert`: evaluates rules and owns bounded, thread-safe alert history.
- `api`: maps monitor/history reads to standard-library HTTP JSON handlers.
- `cmd/server`: constructs components, owns goroutine lifecycles, and performs
  ordered shutdown.

### Full data journey

For AP-01, `(*simulator.Simulator).Next` returns a `model.Telemetry`.
`runAPSender` passes it to `(*udp.Sender).Send`, which calls `json.Marshal` and
`UDPConn.Write`. `(*udp.Receiver).receive` gets bytes through `ReadFromUDP`,
uses `json.Unmarshal`, calls `Telemetry.Validate`, and sends the typed value to
its output channel. `splitTelemetry` forwards it to `(*monitor.Monitor).Run`,
which calls `(*Monitor).Update`, and separately to `(*alert.History).Record`,
which invokes `alert.Detect`. HTTP handlers later read the copied state through
`Monitor.Get`/`List` and alerts through `History.List`.

### Concurrency and shared state

Important goroutines are AP `runAPSender` workers (created by main; ticker
blocks; producer context ends them), the UDP receiver (created by `Start`;
socket read/deadline blocks; receiver context ends it), fan-out (created by
`splitTelemetry`; input receive blocks; closes downstream on source closure),
monitor and alert consumers (created by main; receive blocks; downstream
closure ends them), and the `net/http` serving goroutine (created by main;
server accept blocks; `Shutdown` ends it).

`Monitor.states map[string]model.APState` is protected by `Monitor.mu
sync.RWMutex`. UDP-driven updates write it; HTTP handlers read it. `History`
protects its bounded `[]Alert` with its own `RWMutex`; alert processing writes
and `/alerts` reads. No handler accesses either private collection directly.

Channel ownership is explicit: `Receiver.Start` creates and its receive loop
closes the UDP telemetry channel; `splitTelemetry` creates, sends, and closes
the monitor and alert downstream channels. The monitor and alert history only
receive. `serverErrors` is created by main, sent by the one HTTP serve
goroutine, and read by main; it is buffered and never needs closure.

Every blocking goroutine has an exit path: ticker/select watches context;
socket reads use a deadline and receiver context; channel sends/selects watch
context; consumers return on channel close; `WaitGroup` waits for producers;
and HTTP shutdown has a five-second deadline. These paths prevent known
goroutine leaks.

### Error handling and tradeoffs

Malformed UDP JSON and invalid telemetry are logged/dropped while the receiver
continues. Unknown AP telemetry is rejected by `Monitor.Update`; in the HTTP
API, an unknown requested AP is a 404. Expected shutdown socket/context errors
are treated as normal exit paths; unexpected server failures are logged.

JSON is readable but larger/slower than protobuf; the service uses in-memory
state rather than a database; UDP suits lossy frequent telemetry while TCP
suits reliable commands; `RWMutex` fits read-heavy state better than routing
every query through an owner goroutine; and `net/http` is sufficient without
Gin for four small endpoints.

### Scalability

Three APs can use one goroutine and UDP sender each. Around 1,000 APs, measure
goroutine, allocation, socket, and HTTP contention; consider shared sender
workers, batching, bounded queues, configurable sampling, and alert-rate
limits. Across physical machines, secure/authenticate transport, configure
addresses, add observability, and consider a broker or durable storage when
loss, replay, multi-consumer processing, or historical queries matter.

### Final interview questions

**Why use goroutines for APs?** Each AP samples independently, and Go
goroutines model that cheaply while avoiding blocking unrelated APs.

**Who closes a channel?** The component that owns all sends and knows sending
has finished; receivers should not close producer-owned channels.

**Why use a WaitGroup?** Main waits until every producer returns before it
stops the UDP receiver, preserving shutdown order.

**What does context provide?** A composable broadcast cancellation signal,
often with deadlines, without global mutable shutdown flags.

**Why use an RWMutex?** It permits concurrent HTTP reads while keeping map
writes exclusive and safe.

**What is a race condition?** Unsynchronized conflicting access to shared
memory where at least one access writes.

**Why run the race detector?** It instruments accesses and can expose missing
locks that ordinary tests may not reliably reproduce.

**UDP versus TCP?** UDP is best-effort datagrams with low overhead; TCP is a
reliable ordered stream with more connection state.

**What does socket binding do?** It registers a local IP/port so the OS can
deliver matching incoming packets to the process.

**How does HTTP serve requests concurrently?** `net/http` handles requests in
separate concurrent execution paths, so handlers need safe dependencies.

**What is graceful shutdown?** Stop accepting new work, let or bound active
work finish, release resources, and wait for goroutines to exit.

**How are malformed packets handled?** They fail JSON decoding or validation,
are logged/dropped, and do not terminate the receiver loop.

**How does backpressure work here?** Small buffered channels absorb bursts;
when full, senders block until consumers progress or cancellation occurs.

**Why keep component boundaries?** It keeps simulation, transport, state,
alerting, and HTTP independently testable and replaceable.

**What changes for 1,000 APs?** Measure first, then consider worker pools,
bounded queues, batching, configuration, observability, and distributed
transport/storage where required.
