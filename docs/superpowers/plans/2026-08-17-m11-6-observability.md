# M11.6 Observability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> **Status: complete, 2026-08-18.** All eight tasks landed. Two deviations are
> recorded in `MASTER_PLAN.md`: the feature list is fourteen rather than
> thirteen, because the per-tick accumulator needed somewhere to put a block
> write, and the accumulator is a map behind a mutex rather than the lock-free
> struct the design described, because a block write happens on the
> connection's goroutine rather than the tick's. Task 6's wall-time tolerance
> was demoted to a recorded number before it was written.

**Goal:** Turn M11.1's four process-wide counters into per-player, per-feature, and per-region attribution, with a measurement API that allocates nothing when nobody is observing, bounded label cardinality, a reference sink in `examples/`, and a CI benchmark that keeps the off path free.

**Architecture:** `Sample.Labels` becomes a closed struct instead of a map, so no call site can coin a key and no sample allocates. `Measure(feature, labels)` returns a closure that records a duration on call and is a shared no-op when unobserved. Paths that can run more than once per tick per player accumulate on the tick goroutine and flush one sample; everything rarer samples directly. Chunk attribution is per 32×32 region by default.

**Tech Stack:** Go 1.26.6 via `openserbia/go-flake`, Devbox, Task, `minecraft-protocol` v0.2.0, vendored dependencies. The Prometheus client lives in the `examples` module only.

## Global Constraints

- Work in `/home/ocharnyshevich/pet.projects/go-theft-craft/server`.
- Run every command as `devbox run -- task <name>`. Never call `go` directly.
- This repository is public. Do not name the private proxy project, its protocol, or its codename in any file or commit message.
- **The core module's dependency list does not grow.** No metrics client, no exporter. `go.mod` in the root is unchanged by this milestone; `examples/go.mod` is where a client may appear.
- **An unobserved server must allocate nothing.** Every task that adds a call site is followed by the allocation test, not only by the unit tests.
- Never add the `Co-Authored-By` or `Claude-Session` trailer to a commit message.
- Run `devbox run -- task lint` and `devbox run -- task test` before every commit.

## Dependencies

**M11.2**, because instrumenting the old chunk path measures code that is being
deleted. Tasks 1 and 2 here are independent of it and can land earlier if
there is capacity; Tasks 3 onward cannot.

**M11.5** is not a dependency, but its `Recorder` drop counter is emitted as a
sample from this milestone's `SampleDropped` kind. Whichever lands second
wires that line.

The design this plan implements is
[the M11.6 observability design](../specs/2026-08-17-m11-6-observability-design.md),
2026-08-17.

## What M11.1 left

| Piece | State |
| --- | --- |
| `Observer`, `NopObserver`, `WithObserver` | Shipped, unchanged by this milestone |
| `Sample{Kind, Value, At, Labels}` | `Labels map[string]string`, populated by nothing |
| Kinds `cpu`, `memory`, `network_in`, `network_out` | Shipped |
| `dispatcher`, bounded at 1024, drops when full | Shipped; **the drops are silent** |
| `NetworkSink` on `ObservationRawFrame` | Shipped, unlabelled |
| `SampleResources` every 10s from the tick loop | Shipped |

The work this measures is concentrated: a player joining at the default view
distance of 12 triggers 625 chunk encodes in `sendInitialChunks` before they
can move.

## File Structure

**Created:**

| File | Responsibility |
| --- | --- |
| `server/labels.go` | `Labels`, `Feature`, `RegionPos`, and the constant list |
| `server/labels_test.go` | Region arithmetic including negatives; the feature list |
| `server/measure.go` | `Measure`, `Count`, `Gauge`, and the no-op path |
| `server/measure_test.go` | Zero allocation unobserved; correct durations observed |
| `server/tickstats.go` | The per-tick accumulator and its flush |
| `server/tickstats_test.go` | One sample per (feature, player) per tick |
| `server/observe_bench_test.go` | The three-configuration benchmark and the allocation test |
| `examples/observed/main.go` | Vanilla plus an observer and `/metrics` |

**Modified:**

| File | Change |
| --- | --- |
| `server/observer.go` | `Labels` type change; `SampleDropped`; dispatcher reports its drops |
| `server/metrics.go` | `NetworkSink` labels packets and direction |
| `server/server.go` | Tick timing, chunk-path instrumentation, gauges |
| `internal/server/conn/handler_play.go` | Chunk send and load spans, per player |
| `pkg/world/world.go` | Chunk generate and load spans |
| `internal/server/storage/*` | Chunk save spans |
| `examples/examples_test.go`, `Taskfile.yml` | The new example joins the lane |
| `examples/go.mod` | Prometheus client, examples module only |

---

## Stage A — The label surface

### Task 1: Labels become a struct, and drops become visible

**Files:**
- Create: `server/labels.go`, `server/labels_test.go`
- Modify: `server/observer.go`, `server/metrics.go`, `server/observer_test.go`, `server/metrics_test.go`

**Interfaces:**
- Produces: `server.Labels`, `server.Feature` and its constants, `server.RegionPos`, `server.SampleDropped`, `SampleDuration`, `SampleCount`, `SampleBytes`, `SampleGauge`.

- [x] **Step 1: Write the failing tests**

```go
func TestRegionOfNegativeChunkCoordinates(t *testing.T)
func TestTheDispatcherReportsItsOwnDrops(t *testing.T)
func TestASampleCarriesNoMapAndAllocatesNothing(t *testing.T)
```

`TestRegionOfNegativeChunkCoordinates` is not filler: `cx / 32` and `cx >> 5`
disagree for negatives, the Anvil layout uses the shift, and a metrics label
that disagrees with the region file it names is worse than no label.

- [x] **Step 2: Implement the label struct and the feature list**

Exactly the thirteen features the design names. `Feature` is a string type
with unexported-by-convention discipline: the list is the API, and adding one
means editing `labels.go`.

- [x] **Step 3: Change `Sample.Labels`**

From `map[string]string` to `Labels`. This is a breaking change to a type
published in M11.1; the only consumers are in this repository and its
examples, and a map allocated per sample on a per-frame path is the
alternative. Say so in the commit message.

- [x] **Step 4: Report the dispatcher's drops**

The dispatcher counts what it discards and emits `SampleDropped` on the same
ten-second cadence as `SampleResources`, from its own goroutine. Without this,
an observer under load silently sees less than it thinks — the gap M11.1 left.

- [x] **Step 5: Label the network sink**

`NetworkSink` gains packet name and direction. The packet name comes from the
observation's `Packet` metadata when the stage carries one; a raw frame record
does not, so raw frames carry direction only and the packet label is empty.
Do not synthesize a name that the record does not have.

- [x] **Step 6: Gate and commit**

```bash
devbox run -- task lint
devbox run -- task test
devbox run -- task test:race
git add -A
git commit -m "feat(server): close the label set and report dropped samples"
```

### Task 2: The measurement API

**Files:**
- Create: `server/measure.go`, `server/measure_test.go`

**Interfaces:**
- Produces: `(*Server).Measure(Feature, Labels) func()`, `(*Server).Count(Feature, Labels, float64)`, `(*Server).Gauge(SampleKind, Labels, float64)`.

- [x] **Step 1: Write the failing tests**

```go
func TestMeasureAllocatesNothingWhenUnobserved(t *testing.T)   // AllocsPerRun == 0
func TestMeasureDoesNotReadTheClockWhenUnobserved(t *testing.T)
func TestMeasureRecordsAPlausibleDuration(t *testing.T)
func TestCountAndGaugeCarryTheirLabels(t *testing.T)
```

`TestMeasureDoesNotReadTheClockWhenUnobserved` is checkable by injecting the
clock through an unexported function variable in the test build. It matters
because the whole argument for leaving `Measure` in a 625-iteration loop is
that the unobserved path does nothing.

- [x] **Step 2: Implement**

```go
// noopSpan is returned by Measure on an unobserved server. It is a
// package-level value, so the unobserved path allocates nothing and the
// closure it returns is shared.
var noopSpan = func() {}

func (s *Server) Measure(f Feature, l Labels) func() {
    if !s.observed() {
        return noopSpan
    }
    start := time.Now()
    return func() {
        s.Observe(Sample{
            Kind:   SampleDuration,
            Value:  time.Since(start).Seconds(),
            Labels: l.withFeature(f),
        })
    }
}
```

- [x] **Step 3: Gate and commit**

```bash
devbox run -- task lint
devbox run -- task test
git add -A
git commit -m "feat(server): add a measurement span that is free when unobserved"
```

---

## Stage B — Attribution

### Task 3: Instrument the chunk path

**Files:**
- Modify: `internal/server/conn/handler_play.go`, `pkg/world/world.go`, `internal/server/storage/*`, `server/server.go`

**Interfaces:**
- Consumes: `Measure`, `Labels`, the M11.2 adapter.

- [x] **Step 1: Write the failing tests**

```go
func TestJoiningEmitsOneEncodeSamplePerChunkSent(t *testing.T)
func TestChunkSamplesCarryThePlayerAndTheRegion(t *testing.T)
func TestGenerateLoadAndSaveAreDistinguishable(t *testing.T)
```

The first asserts 625 samples at view distance 12, which is the number that
motivated the milestone.

- [x] **Step 2: Instrument, in this order**

| Feature | Site |
| --- | --- |
| `chunk_generate` | `World.LoadChunk` when the store has no chunk |
| `chunk_load` | `World.LoadChunk` when the store does |
| `chunk_encode` | The adapter's `EncodeChunk`, through the world |
| `chunk_send` | `sendInitialChunks` and `updateLoadedChunks` |
| `chunk_save` | The store's per-region write |

`chunk_encode` is measured where the cache is consulted, not inside it, so a
cache hit shows as a fast encode rather than as no encode at all. A hit that
records nothing would make the cache look like it removed the work rather than
made it cheap.

- [x] **Step 3: Carry the player label without formatting per sample**

The connection stores its `Labels` value once at login. No call site calls
`p.Username` inside a loop.

- [x] **Step 4: Gate and commit**

```bash
devbox run -- task lint
devbox run -- task test
devbox run -- task test:race
git add -A
git commit -m "feat(server): attribute chunk work to a player and a region"
```

### Task 4: The per-tick accumulator

**Files:**
- Create: `server/tickstats.go`, `server/tickstats_test.go`
- Modify: `server/server.go`, `internal/server/conn/*`

**Interfaces:**
- Produces: `tickStats`, flushed at the end of `tick`.

- [x] **Step 1: Write the failing tests**

```go
func TestAThousandBlockWritesProduceOneSamplePerTick(t *testing.T)
func TestTheAccumulatorIsFreeWhenUnobserved(t *testing.T)
func TestFlushEmitsOneSamplePerFeatureAndPlayer(t *testing.T)
```

- [x] **Step 2: Implement**

A plain struct owned by the tick goroutine, so it needs no synchronization.
Block writes, entity syncs, and packet counts accumulate into it; `tick`
flushes at the end.

The design's table is the rule: anything that can happen more than once per
tick per player accumulates, anything rarer samples directly. Deviating from
that in a call site is what turns measurement into load.

- [x] **Step 3: Add the gauges**

Players online, chunks resident, index size when M11.5 is present. Gauges are
emitted on the ten-second cadence, not per tick: a level does not need 20
samples a second.

- [x] **Step 4: Gate and commit**

```bash
devbox run -- task lint
devbox run -- task test
devbox run -- task test:race
git add -A
git commit -m "feat(server): aggregate hot-path samples per tick"
```

### Task 5: Chunk detail as an opt-in

**Files:**
- Modify: `server/options.go`, `server/labels.go`

- [x] **Step 1: Write the failing test**

```go
func TestChunkLabelIsEmptyByDefaultAndSetUnderWithChunkDetail(t *testing.T)
```

- [x] **Step 2: Implement and document the cost at the option**

```go
// WithChunkDetail labels chunk samples with exact chunk coordinates instead
// of the 32×32 region they fall in.
//
// Cardinality: one label value per resident chunk. A world with 10,000
// resident chunks produces 10,000 series per chunk metric, against about 10
// with the region default. Use it to investigate a specific column, not as a
// standing configuration.
func WithChunkDetail() Option
```

The number belongs at the option, where someone reads it at the moment they
are about to turn it on.

- [x] **Step 3: Gate and commit**

```bash
devbox run -- task lint
devbox run -- task test
git add -A
git commit -m "feat(server): allow exact chunk labels, with the cardinality stated"
```

---

## Stage C — Prove it and show it

### Task 6: The off-profile benchmark in CI

**Files:**
- Create: `server/observe_bench_test.go`
- Modify: `Taskfile.yml`

**Interfaces:**
- Produces: `task test:profile`.

- [x] **Step 1: Write the fixed workload**

One connection, join, 625 chunks sent, 1,000 block writes, disconnect, over a
loopback pipe with a discarding client — the same harness shape
`internal/server/conn/conn_test.go` already uses.

- [x] **Step 2: Three configurations**

| Configuration | Assertion |
| --- | --- |
| No observer | The floor; recorded, not asserted |
| Observer set, discarding sink | Zero sample allocations on the hot paths; wall time within tolerance of the floor |
| Observer set, recording sink | Recorded, not asserted — this is the cost of a real consumer |

The tolerance is written into the test as a constant with a comment giving the
machine it was measured on, because a wall-time assertion with no context is a
flake waiting for a slower CI runner. If it proves flaky, the allocation
assertion stays and the wall-time one becomes a recorded number.

- [x] **Step 3: Add the lane**

```yaml
  test:profile:
    desc: Run the observability off-profile benchmark
    deps: [ deps ]
    cmds:
      - go test -mod vendor -run TestOffProfile -bench BenchmarkObserve -benchtime 3x ./server/
```

- [x] **Step 4: Gate and commit**

```bash
devbox run -- task lint
devbox run -- task test
devbox run -- task test:profile
git add -A
git commit -m "test(server): pin the cost of observability being off"
```

### Task 7: The observed example

**Files:**
- Create: `examples/observed/main.go`
- Modify: `examples/go.mod`, `examples/examples_test.go`, `Taskfile.yml`, `README.md`

- [x] **Step 1: Write the example**

Vanilla's construction plus an `Observer` that maps samples onto Prometheus
collectors and an HTTP server exposing `/metrics`, **bound to loopback by
default**. A metrics endpoint is not a public one, and the default should not
have to be explained after the fact.

The mapping is the interesting part of the example: `Feature` becomes a label,
`SampleDuration` becomes a histogram, `SampleCount` a counter,
`SampleGauge` a gauge. That is the piece someone copying this needs.

- [x] **Step 2: Add it to the lane**

`examples_test.go` gains `"observed"` with port 25705 and asserts `/metrics`
answers after the server starts. That last assertion is what keeps the sink
compiling and wired, which is the whole reason the example exists rather than
a README snippet.

- [x] **Step 3: Gate and commit**

```bash
devbox run -- task lint
devbox run -- task test
devbox run -- task test:examples
git add -A
git commit -m "feat(examples): a server that exports its samples"
```

### Task 8: Documentation and the milestone record

**Files:**
- Modify: `README.md`, `CLAUDE.md`, `../headless-minecraft/MASTER_PLAN.md`

- [x] **Step 1: Document the seam**

A README section listing the sample kinds, the feature list, the label set,
and the cardinality rule, with `examples/observed` as the worked example.

- [x] **Step 2: Record the milestone**

In `MASTER_PLAN.md`, tick M11.6 and record:

- what a join at view distance 12 actually costs, per feature, now that it is
  measurable — this is the first real answer to the original todo item;
- whether M11.2's encode cache showed up in the numbers, which M11.2's own
  record could only assert;
- the off-profile floor and the observed delta, which is half of track exit
  criterion 9;
- whether the wall-time tolerance in Task 6 survived CI or had to be demoted
  to a recorded number.

- [x] **Step 3: Final gate and commit**

```bash
devbox run -- task lint
devbox run -- task test
devbox run -- task test:race
devbox run -- task test:profile
devbox run -- task test:examples
devbox run -- task test:interop
git add -A
git commit -m "docs: record M11.6 and document the observability seam"
```

---

## Self-review notes

- **Task 1 breaks a public type one milestone after publishing it.** `Labels`
  as a map was the wrong call in M11.1 and the cost of keeping it is an
  allocation per sample on a per-frame path. Breaking it now, while the only
  consumers are in this repository, is cheaper than living with it.
- **The instrumentation-goes-stale risk has no good mechanism.** The design
  admits it: a test can catch a coined feature name, not a missing call site.
  This plan does not pretend otherwise, and the honest mitigation is that
  every task that adds a path also adds its sample in the same commit.
- **`chunk_encode` is measured around the cache rather than inside it**, so a
  hit reads as a cheap encode. That is a deliberate reporting choice and it
  will confuse someone comparing sample counts to cache statistics; the doc
  comment on the call site says which.
- **The wall-time assertion in Task 6 is the flakiest thing in this plan.** It
  is written with an explicit escape: if CI cannot hold it, the allocation
  assertion stays and the timing becomes a recorded number. Deciding that in
  advance beats a quarantined test nobody deletes.
- **The Prometheus dependency in `examples/go.mod` is the first real use of
  the nested module's independence.** That was the stated reason for the
  module in the framework design, and until this milestone it was a claim.
