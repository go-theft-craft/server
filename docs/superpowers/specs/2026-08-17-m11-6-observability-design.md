# M11.6 observability design

- Status: Draft for review
- Date: 2026-08-17
- Repository: `server`
- Milestone: M11.6, the sixth sub-milestone of
  [the server framework track](2026-08-16-server-framework-design.md)

## Context

M11.1 shipped the seam and the samples that do not depend on the world model:

- `Observer` with one method, `Observe(Sample)`, and `NopObserver` as the
  default (`server/observer.go`).
- `Sample{Kind, Value, At, Labels}`, where `Labels` is present and nothing
  populates it.
- Four kinds: `cpu`, `memory`, `network_in`, `network_out`.
- Delivery through a bounded queue that drops when full, so a slow observer
  cannot apply backpressure to a stream goroutine.
- `NetworkSink`, which adapts `minecraft-protocol`'s observation points and
  counts `ObservationRawFrame` only.
- `SampleResources`, called from the tick loop every ten seconds and only when
  an observer exists.

What M11.1 deliberately deferred is the part the original `docs/todo.md`
actually asked for: per-player load, per-feature load, and per-player chunk
load and unload timing. Those were sequenced after M11.2 because measuring a
chunk model about to be replaced produces numbers that expire.

The work this measures is concentrated and known. A player joining at the
default view distance of 12 triggers 625 chunk encodes before they can move
(`sendInitialChunks`), and each one is 16 sections of 8 KB rebuilt per send
until M11.2's memoization lands. That single path is the reason per-player and
per-chunk attribution exists.

## Goals

- Attribution: which player, which feature, and which region of the world a
  cost belongs to.
- A measurement API cheap enough to leave in the hot paths and free when
  nobody is observing.
- Bounded label cardinality, chosen deliberately rather than discovered by an
  exporter falling over.
- A demonstration sink in `examples/`, and none in the core.

## Non-goals

- Shipping a Prometheus, OpenTelemetry, or statsd dependency in the library.
  Parent Decision 1 puts the sink in `examples/`, which is its own module for
  exactly this reason.
- Tracing. Spans with parents and propagation are a different tool; this is
  measurement, and a sample carries no context.
- Alerting, dashboards, or retention. Those belong to whatever consumes the
  samples.
- Per-block or per-packet events. Decision 4 aggregates them instead, and says
  why.

## Decision 1: labels are a closed set, and the keys are constants

```go
const (
    LabelPlayer    = "player"    // username, not UUID — see below
    LabelFeature   = "feature"   // one of the Feature constants
    LabelRegion    = "region"    // "r.2.-3", 32×32 chunks
    LabelChunk     = "chunk"     // "12,-40", only when WithChunkDetail is set
    LabelWorld     = "world"
    LabelPacket    = "packet"    // generated packet name, e.g. "map_chunk"
    LabelDirection = "direction" // "in" or "out"
)
```

A closed set is what makes a sample mappable onto a Prometheus label set
without the sink inventing a schema. An open `map[string]string` would let any
call site add a key, and the first one to add a player UUID alongside the
username doubles every series.

**The player label is the username.** A UUID is the stable identity and the
username is the readable one, and a metrics label is read by a human. The pair
would double cardinality for no query anyone runs; a sink that needs the UUID
can ask the server.

Sample construction takes labels as a small typed value rather than a map, so
the hot path allocates nothing:

```go
type Labels struct {
    Player  string
    Feature Feature
    Region  RegionPos
    World   string
    // Packet and Direction are set by the network sink only.
    Packet    string
    Direction string
}
```

`Sample.Labels` changes from `map[string]string` to this struct. It is a
breaking change to a type published in M11.1, made while the only consumers
are in this repository, and the alternative — keeping a map and allocating one
per sample on a path that runs per frame — is worse than the break.

## Decision 2: a feature is a named unit of server work, and the list is fixed

```go
type Feature string

const (
    FeatureChunkGenerate Feature = "chunk_generate"
    FeatureChunkEncode   Feature = "chunk_encode"
    FeatureChunkSend     Feature = "chunk_send"
    FeatureChunkLoad     Feature = "chunk_load"    // from the store
    FeatureChunkSave     Feature = "chunk_save"
    FeatureTick          Feature = "tick"
    FeatureEntitySync    Feature = "entity_sync"
    FeatureInventory     Feature = "inventory"
    FeatureCrafting      Feature = "crafting"
    FeatureCombat        Feature = "combat"
    FeatureCommand       Feature = "command"
    FeatureLogin         Feature = "login"
    FeatureProvenance    Feature = "provenance"
)
```

Thirteen values, added to by editing this list. A free-form string would let
a call site coin a feature per block type, and per-feature attribution is only
useful if the features are comparable across servers and across releases.

The list is chosen to match where the work actually is, not where the code is
organized: `chunk_encode` and `chunk_send` are separate because one is CPU in
`pkg/world` and the other is bytes through a stream, and the original todo
item asked about both.

## Decision 3: measurement is a span helper that is free when unobserved

```go
// Measure starts a timing span. The returned function records the elapsed
// duration as a SampleDuration when it is called, normally through defer.
//
// When no observer is configured it returns a shared no-op closure, so an
// unobserved server pays one nil check and no allocation.
func (s *Server) Measure(f Feature, l Labels) func()
```

Usage at the call site:

```go
defer c.server.Measure(FeatureChunkEncode, Labels{Player: name, Region: r})()
```

Two properties matter more than the syntax. First, the unobserved path is one
predictable branch returning a package-level closure, which is what makes it
acceptable to leave in `sendInitialChunks`'s 625-iteration loop. Second, the
timing uses monotonic duration from `time.Now`, which on Linux is a vDSO call
of tens of nanoseconds against a chunk encode measured in hundreds of
microseconds — three orders of magnitude of headroom.

New sample kinds:

```go
SampleDuration   SampleKind = "duration"    // seconds, from Measure
SampleCount      SampleKind = "count"       // an event happened n times
SampleBytes      SampleKind = "bytes"       // payload size
SampleGauge      SampleKind = "gauge"       // a level: players online, chunks resident
SampleDropped    SampleKind = "dropped"     // samples the dispatcher discarded
```

`SampleDropped` closes a gap M11.1 left: the dispatcher drops when its queue is
full and nothing reports it, so an observer under load silently sees less than
it thinks. The drop counter is emitted on the same ten-second cadence as the
resource samples, from the dispatcher's own goroutine.

## Decision 4: hot paths aggregate per tick, and only the aggregate is emitted

A block write, a packet, and an entity position update all happen thousands of
times a second. Emitting a sample per event would make the measurement the
load — the failure mode where observability changes what it observes.

The rule: anything that can happen more than once per tick per player is
counted in a per-tick accumulator and flushed as one sample at the end of the
tick. Anything rarer is sampled directly.

| Path | Frequency | Treatment |
| --- | --- | --- |
| Chunk generate, encode, load, save | Tens per second | Direct `Measure` |
| Tick duration | 20/s | Direct |
| Login, command, crafting | Rare | Direct |
| Block write, entity sync, packet in/out | Thousands per second | Per-tick accumulator |

The accumulator is a plain struct on the tick goroutine, so it needs no
synchronization, and the flush is one sample per (feature, player) pair that
saw activity in that tick.

The network sink is the exception that stays per-frame, because it already
exists and already runs on the stream's goroutine. It gains the packet and
direction labels and keeps counting bytes per raw frame.

## Decision 5: chunk attribution is per region by default, per chunk behind an option

A chunk label is unbounded: a world of 10,000 resident chunks is 10,000 label
values, and a Prometheus sink with a per-chunk histogram is a memory incident.

The default label is the region — 32×32 chunks, the same grouping the Anvil
files use — which divides cardinality by 1,024 and still answers "which part of
the world is expensive". `WithChunkDetail()` switches to exact chunk
coordinates for someone debugging a specific column, with the cardinality cost
stated in its documentation.

The region grouping is not an arbitrary bucket size. It matches the storage
granularity from M11.3, so a slow region in the metrics and a slow region file
on disk are the same region.

## Decision 6: the reference sink lives in examples, and prints or exports

`examples/observed` joins `minimal`, `flat`, and `vanilla`: the vanilla server
plus an observer, exposing a `/metrics` endpoint. It is where a Prometheus
client dependency is allowed to exist, and it is the working answer to "how do
I wire this up" that a README section cannot be.

The core keeps `NopObserver` and nothing else. A logging observer that formats
samples into `slog` is small enough to belong in the example rather than the
library.

Because `examples/` is a test surface rather than documentation
(parent Decision 1), the observed example is started by
`task test:examples` like the others, which is what keeps the sink compiling.

## Decision 7: the off profile is a benchmark, not a claim

Track exit criterion 9 is that turning provenance and observability off
returns the server to its M6.1 resource profile. That needs a measurement, and
this milestone owns half of it.

The benchmark: a fixed workload — one connection, join, 625 chunks sent, 1,000
block writes, disconnect — run three ways.

| Configuration | What it establishes |
| --- | --- |
| No observer | The floor |
| Observer set, `NopObserver`-equivalent sink that discards | The cost of the machinery |
| Observer set, sink that records every sample | The cost of the machinery plus a real consumer |

Reported as allocations per operation and wall time, with a documented
tolerance: the unobserved path must allocate zero samples and stay within
noise of the floor. A regression here is the failure this whole design is
arranged to avoid, so it belongs in CI rather than in a one-time report.

## Interfaces

```go
package server

type Observer interface{ Observe(Sample) }

type Sample struct {
    Kind   SampleKind
    Value  float64
    At     time.Duration
    Labels Labels
}

func WithObserver(Observer) Option        // unchanged from M11.1
func WithChunkDetail() Option             // exact chunk labels
func (s *Server) Observe(Sample)          // unchanged
func (s *Server) Measure(Feature, Labels) func()
func (s *Server) Count(Feature, Labels, float64)
func (s *Server) Gauge(SampleKind, Labels, float64)
```

The connection carries its player's label value once, set at login, so no call
site formats a username per sample.

## Migration

1. `Labels` becomes a struct, `SampleDropped` lands, and the dispatcher reports
   its own drops. Nothing else changes.
2. `Measure`, `Count`, `Gauge`, and the feature list, with no call sites.
3. Chunk paths instrumented: generate, encode, load, save, send. These are the
   ones the original todo asked for and the ones M11.2 changed.
4. The per-tick accumulator and the high-frequency paths.
5. `examples/observed`, and the off-profile benchmark in CI.

Steps 3 and 4 depend on M11.2 having landed, because instrumenting the old
chunk path measures code that is being deleted.

## Testing

- An unobserved server allocates zero samples across the fixed workload, by
  `testing.AllocsPerRun`.
- `Measure` on an unobserved server returns the shared closure and does not
  read the clock.
- Every sample emitted carries a feature from the constant list, checked by a
  test that walks the call sites — a linter-style test rather than a runtime
  one, because the point is to catch a coined feature name at review.
- Region labels are correct for negative coordinates, which is where the
  `>>5` arithmetic goes wrong if it is written as division.
- Under a blocking observer, the tick period does not move and `SampleDropped`
  reports a non-zero count. This extends M11.1's non-blocking test to assert
  the loss is visible rather than only that it is survivable.
- The per-tick accumulator emits one sample per (feature, player) pair per
  tick, not one per event, verified by counting samples for a known number of
  block writes.
- `examples/observed` starts, serves `/metrics`, and the output contains the
  chunk-encode series after a client joins.

## Risks

**Instrumentation goes stale the moment a path moves.** A feature constant
with no call site and a call site with no feature are both silent. The
call-site test in the testing section is the only mechanism proposed against
it, and it is weaker than it sounds — it catches unknown names, not missing
ones.

**Cardinality is a foot-gun the framework cannot fully prevent.** The label
struct bounds the keys, the region default bounds the values, and
`WithChunkDetail` hands the operator a way to blow it up on purpose. That is
the right split, and it should be documented at the option rather than in a
design nobody reads at 2 a.m.

**The per-tick accumulator adds a fixed cost to every tick, observed or
not**, unless it is behind the same nil check as everything else. It is, and
the benchmark in Decision 7 is what proves it stayed that way.

**Per-player attribution names people.** The label is a username, which is
already broadcast to every client on the server, so this adds no exposure that
joining does not. It is still worth stating that a metrics endpoint is not a
public one, and `examples/observed` binds loopback by default.

## Exit criteria

| | Criterion |
| --- | --- |
| 1 | Chunk generate, encode, load, save, and send are attributed per player and per region |
| 2 | Per-feature durations exist for every feature in the constant list that the server can reach |
| 3 | An unobserved server allocates zero samples and stays within noise of the no-observer floor |
| 4 | Dropped samples are counted and reported rather than silent |
| 5 | Label cardinality is bounded by default, and the unbounded mode is opt-in and documented |
| 6 | `examples/observed` runs in `task test:examples` and exposes the chunk series |
| 7 | The off-profile benchmark runs in CI, with a recorded tolerance |
