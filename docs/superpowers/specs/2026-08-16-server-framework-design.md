# Server framework design

- Status: Draft for review
- Date: 2026-08-16
- Repository: `server`
- Milestone: M11, which this document subdivides into M11.1 through M11.7

## Context

`server` is 13,708 lines of Go that run a playable 1.8.9 server. M3 moved every
connection onto `minecraft-protocol` and deleted the repository's own wire code,
and M6.1 finishes that by replacing the last hand-written play packet structs in
`pkg/gamedata/versions/pc_1_8`.

What comes after M6.1 has never been designed. `docs/todo.md` holds eight items
covering per-player load measurement, per-feature load measurement, item
provenance, a vanilla command list, save formats, world generation, performance
metrics, and chunk loading. None of them fit any plan on disk, because every
server plan is protocol migration. `MASTER_PLAN.md` has no milestone for any of
them either: M0 through M10 covers protocol, simulation, headless, and
conformance, and nothing covers the server becoming a thing people build with.

This document defines that track. It does not design the sub-milestones. Each
gets its own focused design and implementation plan, the way M8 subdivided into
M8.1 through M8.8.

## What the server is

`server` is a framework: composable pieces someone uses to build and customize
their own server. It is also, and first, the test harness that proves
`minecraft-protocol` against real clients and gives `headless-minecraft` and
`minecraft-simulation` something to connect to. The harness role is the one that
must never break, because M6.1, M9, and M10 all depend on it.

`cmd/server` stops being the product. It becomes one entry under `examples/`,
where pieces get combined for tests and demonstrations.

That reframing changes what the eight items are. Each stops being a feature and
becomes a seam with a default implementation someone can replace.

## Goals

- One internal world model that is neutral about protocol version, adapted per
  connection.
- Every subsystem reachable through an interface, with a working default, wired
  by the application rather than discovered by the framework.
- A server that keeps ticking while it saves.
- Enough recorded history to answer who placed a block, who broke it, and where
  a specific item came from, across a restart.
- Item duplication detected when it happens, not reconstructed afterwards.

## Non-goals

- Dynamic plugin loading. Go has no usable mechanism for it, and pretending
  otherwise buys a familiar shape without the property that made it worth
  having in Java.
- Rollback. Provenance is audit only. Decision 6 keeps the record format able to
  support rollback later without a migration, and nothing more.
- Vanilla parity. The framework ships stubs and defaults, and a server that
  wants vanilla behavior implements it.
- Any change to `minecraft-protocol`. The version boundary lives here.

## Decision 1: interfaces and options, wired by the application

```go
srv, err := server.New(
    server.WithStore(anvil.Open("world")),
    server.WithGenerator(gen.Default(seed)),
    server.WithCommands(vanilla.Stubs()),
    server.WithObserver(prom.New()),
)
```

Plain Go, compile-time checked, no registry and no lifecycle magic. It matches
how `minecraft-protocol` already works, with `NewSession(Role, Limits)`,
`WireProfile`, and explicit seams, so someone moving between the two
repositories meets one idea rather than two.

An ordered middleware layer and a domain event taxonomy were both considered.
They are what `minecraft-protocol` M5 and `headless-minecraft` M6.3 build, and
adding them here would be consistent. They are deferred because no seam in this
track needs interception, only replacement, and a middleware chain that nothing
intercepts is machinery with no user.

`examples/` carries at least three: `minimal`, which accepts a login into an
empty world with no storage; `flat`, superflat and in-memory; and `vanilla`,
which is today's `cmd/server`. A framework whose only example is the full server
has not shown that its pieces come apart.

**`examples/` is its own Go module.** The library keeps the dependency list it
has, and examples pull whatever they need to be realistic, including the
observability sink in Decision 5 that nobody wants in the core. The cost is a
second CI step, because `go test ./...` from the root does not descend into a
nested module.

**Examples are the integration test surface, not documentation.** The M3
byte-parity fixtures and the pinned Node client lane point at
`examples/vanilla` after M11.1 rather than at `cmd/server`. An example that only
demonstrates rots quietly. An example CI runs cannot, and in a repository where
most plans are written well ahead of the code, that difference is worth the
extra CI step on its own.

This is a project convention rather than a server one. `headless-minecraft` is a
client toolkit under the same rule, and `MASTER_PLAN.md` records it once for
both.

## Decision 2: a block state is an interned handle

The core stores `type State uint32`, an opaque handle resolved through a
`StateRegistry`. Each version adapter maps between handles and wire encoding:
protocol 47 packs `id<<4 | metadata`, protocol 775 indexes a global palette, and
neither representation enters the core.

`pkg/world/gen` today is `Blocks [4096]uint16` with `blockID<<4 | metadata`
written into it, and `pkg/world/anvil` reads 1.8 regions. Neither survives this
decision unchanged, which is why M11.2 comes before world generation and
storage rather than after.

## Decision 3: sections are immutable, and a block write swaps one

A section's contents are immutable once stored. A block write does not mutate a
section, it builds a replacement and swaps it in. A snapshot of the world is a
pointer copy.

This answers three of the eight items at once:

- **Overlapping writers.** Two players changing the same chunk stop racing,
  because nothing is mutated in place.
- **Saving must not freeze.** The save goroutine serializes a pointer-copied
  snapshot while the tick keeps running. Snapshot saving is not an extra
  mechanism bolted on, it is what this model gives for free.
- **Chunk loading and unloading.** Ownership is stated once here rather than
  answered differently by the storage design, the generation design, and the
  chunk design.

The cost is one section copy per block write, bounded at 4096 entries, and a
`MultiBlockChange` addresses one section by definition so it also costs one.

`headless-minecraft` M7 adopts the same model for its observed chunk store,
decided independently and for the same reason. Keeping both repositories on one
chunk representation is worth stating as an intent rather than a coincidence.

## Decision 4: the world store holds vanilla data, and everything else sits beside it

```go
type WorldStore interface {
    LoadChunk(ctx context.Context, pos ChunkPos) (*Chunk, error)
    SaveSnapshot(ctx context.Context, snap Snapshot) error
    Close() error
}

// SideStore holds what the world format has no field for. It is written from
// the same snapshot as the world, and stamped with the same generation, so a
// mismatched pair is detected at load rather than trusted.
type SideStore interface {
    SaveSnapshot(ctx context.Context, snap Snapshot, gen Generation) error
    Load(ctx context.Context, gen Generation) (Sidecar, error)
    Close() error
}
```

A version-neutral core cannot have a version-specific native format, so vanilla
Anvil is one adapter among several rather than the store. `pkg/world/anvil`
already reads and writes 1.8 regions and becomes the first adapter.

**Nothing non-vanilla goes into the vanilla file.** Block identity, item
identity, and the provenance log all live beside it. The alternative, custom NBT
tags inside Anvil, was rejected: NBT is extensible so it would work, and any
vanilla tool or other server reading that world drops the tags silently, which
breaks the chain at the first external touch and gives no signal that it
happened.

The split is by data shape, and each kind lands differently:

- **The provenance log** is append-only, large, and time-ordered. It has no
  natural home in a chunk format and gets its own store, which M11.5 designs.
- **Block identity** is keyed by `(world, x, y, z)`, which is stable because
  blocks do not move. A chunk-keyed sidecar alongside each region file needs no
  re-keying, ever.
- **Item identity** has no stable address, because an item moves between a
  chest slot, a hotbar, and a dropped entity. Keying a sidecar by location
  breaks on the first move. Keying it by ID works, and that structure already
  exists: the ID-to-location index from Decision 7 **is** the item sidecar.

Because vanilla data stays in the vanilla file, the native format stops being
required and becomes a performance choice. M11.3's research question changes
from "which format can hold everything" to "which format is fastest for the
vanilla world", which is a smaller question with an easier exit.

### Reconciliation on load

Two stores can disagree, and the honest response is to say so rather than to
trust either. Both are written from one snapshot under Decision 3, so they
capture the same instant, and the generation stamp catches a mismatched pair at
load.

When they disagree anyway, which means something outside this server edited the
world, a reconciliation pass records it:

- An item present in the world with no ID gets one minted, with a provenance
  record saying its history begins at this load.
- An ID in the index with no item behind it is retired, with a record saying it
  was unaccounted at load.

That turns an external edit into a detected, recorded event. An external edit is
also one of the ways a duplication can enter a world, so making it visible
serves Decision 7 rather than merely tolerating it.

## Decision 5: one observer taking typed samples

Per-player load, per-feature load, per-player chunk load and unload timing, CPU,
memory, and network traffic are all samples through one interface, not four
systems. `minecraft-protocol` M1 already publishes lossless observation points
for the same reason, so a sink written for one consumes the other.

Item provenance is deliberately not part of this interface. Decision 6 explains
why.

Per-chunk and per-feature attribution is sequenced after M11.2, because
measuring a chunk model that is about to be replaced produces numbers that
expire. The plain CPU, memory, and network counters do not expire and can land
in M11.1.

## Decision 6: provenance is a durable audit log, not an observability hook

"I placed a block, the server restarted, a creeper broke it, and I still want to
know" is not a metric. It is an append-only record with its own store, its own
query surface, and its own retention policy, so it becomes M11.5 sitting on
M11.3 rather than a hook beside the metrics interface.

**The actor is not a player.**

```go
type Actor struct {
    Kind   ActorKind // player, mob, explosion, fire, liquid, piston, worldgen, command, extension
    Player uuid.UUID // when Kind is player
    Entity EntityRef // the mob, or the explosion's source
    Cause  *Actor    // the creeper was lit by someone
}
```

`Cause` is what makes the creeper case answerable instead of ending at "a creeper
did it".

A block record holds world, position, action, old state, new state, actor, and
time. An item record holds action, item, source holder, destination holder,
actor, and time, where a holder is a player, a container at a position, or the
world.

The query surface is three questions: what happened at this position, what did
this actor do in this window, and what happened to this item.

**The write path never touches the tick.** The tick appends to a bounded queue
and a writer goroutine drains it. On overflow the default drops, counts, and
warns loudly, with an option to block instead for anyone who needs a complete
audit. Silently dropping audit records is the one behavior that makes the
feature worse than not having it.

Provenance is off by default. Retention belongs to the store implementation
rather than the interface, so the framework ships a file-based default with a
configurable window and a heavier adapter stays out of the core dependency
list.

Rollback is out of scope. The record format keeps old and new state, which is
what a later rollback milestone would need, so adding it costs a design and a
plan rather than a migration.

## Decision 7: every item carries an ID, and the ID index is the duplication detector

Each item gets a 64-bit ID, a server epoch plus a monotonic counter, minted when
the item comes into existence and retired when it is destroyed. Unique within a
server forever, which is the scope duplication detection needs, at half the
storage and half the memory of a UUID.

Stacking is not an obstacle. The server owns its inventory model, so a stack
carries a count and a slice of IDs. Splitting 64 into 32 and 32 splits the
slice, merging concatenates, and crafting consumes input IDs and mints new ones.
The wire protocol never carries them, because no client has a use for them.

The important part is the index. **A live ID-to-location index makes duplication
detectable rather than forensic.** Any write that would place an existing ID in
a second location without removing it from the first is a duplication, caught
where it happens. The same index answers "where is this item now", and
persisting it is what Decision 4 calls the item sidecar, so one structure does
three jobs and there is no fourth place item identity lives.

This is also the instrument for an open question. The M3 session findings record
a survival block duplication that is not caused by the migrated drop data and
has no explanation yet.

## Decision 8: block identity is sparse, with the key space ready for universal

A block gets an ID when a non-worldgen actor places it, held in a side-table
keyed by position and released when the block is broken. When a placed block
breaks, its ID links to the ID of the item that drops, so one chain runs from
placement through destruction, through the drop, and through every transfer
afterwards.

The side-table is chunk-keyed and lives beside the region file, per Decision 4.
Position is a stable key, so unlike item identity this one needs no index and no
reconciliation: a chunk's block IDs load with the chunk and are discarded with
it.

Universal block identity is available behind a flag, and the side-table's key
space is designed to hold worldgen IDs so turning it on needs no format change.
It is not the default because the cost is not proportional to anything a query
needs:

| Area | Blocks | Identity alone, at 8 bytes |
| --- | --- | --- |
| 500×500 | 64 million | ~512 MB |
| 2000×2000 | 1.02 billion | ~8.2 GB |

That is before a single provenance record, and nearly all of it is air and stone
that no query in Decision 6 would ever reach.

Blocks also cannot duplicate the way items can, because a position holds one
block. The detection argument in Decision 7 applies to items only.

## Decision 9: world generation stays in this repository

`gen.Generator` already exists and is the seam. What it needs is parameters,
named world types, and output in the version-neutral model from Decision 2.

A separate repository was considered and rejected. The interface would live in
one repository and every implementation in another, with a version bump on each
side for any change to either, and there is no consumer outside `server` that
wants a generator without wanting the framework it plugs into. A custom
generator belongs in its author's repository or in `examples/`.

## Decision 10: commands are a set, and the version boundary renders it

```go
type Command interface {
    Name() string
    Aliases() []string
    Run(ctx context.Context, caller Caller, args Args) error
}

type Set interface {
    Lookup(name string) (Command, bool)
    All() []Command
}
```

`vanilla.Stubs()` returns every vanilla command, each returning
`ErrNotImplemented` with a message saying so. Someone building a server
overrides them one at a time and the list tells them what is left.

The version boundary is real. On protocol 775 the set renders to a brigadier
tree for `DeclareCommands`. On protocol 47 there is no command tree packet at
all, so the set only feeds tab-complete. The same `Set` produces both.

## The track

| | Sub-milestone | Covers | Depends on |
| --- | --- | --- | --- |
| M11.1 | Framework shape | `server.New` and options, `cmd/server` moves to `examples/`, seams declared, plain resource counters | M6.1 |
| M11.2 | World model and chunk ownership | Interned states, per-version adapters, immutable sections | M11.1 |
| M11.3 | Storage | `WorldStore`, native format research and design, vanilla Anvil adapter, snapshot saving | M11.2 |
| M11.4 | World generation | Parameters, named world types, version-neutral output | M11.2 |
| M11.5 | Provenance | Item and block identity, the ID index, the audit log and its queries | M11.3 |
| M11.6 | Observability | The `Observer` interface and per-player, per-feature, per-chunk attribution | M11.2 |
| M11.7 | Commands | `Command`, `Set`, `vanilla.Stubs()`, brigadier rendering on 775 | M11.1 |

M11.6 and M11.7 touch the world model lightly and can run alongside M11.2 if
there is capacity. M11.3, M11.4, and M11.5 cannot.

Three of the eight `docs/todo.md` items say "do research". In this project
research produces a design and the design produces a plan, which is how M4, M5,
and M8.1 all ran. The storage format research opens M11.3 and the chunk loading
research is answered by Decision 3 in M11.2, not separately.

## Dependencies

M6.1, which deletes the last hand-written play packet structs. Building the
version boundary in Decision 2 while play still runs on 1.8-shaped structs would
mean building it twice.

Nothing here depends on M4, M5, M7, M8, or M9. The harness role means M11 must
not break them, which is what the existing interoperability lanes and byte
parity fixtures are for. They move to point at `examples/vanilla` in M11.1 and
must stay green through every sub-milestone.

## Testing

- The M3 byte-parity fixtures and the pinned Node client lane keep running
  against `examples/vanilla` after M11.1, unchanged. A framework refactor that
  breaks the harness has failed regardless of how clean it is.
- Chunk ownership gets a race test with concurrent writers and readers on one
  chunk, and a save running against a live tick.
- The duplication detector gets a test that deliberately duplicates an item and
  asserts detection at the moment of the second write, not on a later sweep.
- Provenance gets a restart test: place a block, stop the server, start it,
  break the block with a mob, and query the full chain.
- Reconciliation gets a test that edits the vanilla world between two runs, by
  adding an item and removing another, and asserts both discrepancies are
  recorded rather than absorbed.
- A world written with provenance on is opened by an unmodified reader of the
  vanilla format, which is the test that keeps Decision 4 honest.
- Each version adapter round-trips every state in its registry through the wire
  encoding and back.
- Commands get a test asserting every vanilla name is present and that each stub
  reports itself unimplemented rather than failing silently.

## Risks

**M11.2 rewrites two packages that currently work.** `pkg/world/gen` and
`pkg/world/anvil` both encode 1.8 assumptions in their types. The harness has to
stay green across that, and the byte-parity fixtures are the only thing that
will say so.

**Two stores can drift.** Decision 4 keeps non-vanilla data beside the world
rather than inside it, which is what keeps the vanilla file readable by anything
else, and the cost is a consistency problem that did not exist with one store.
The generation stamp and the reconciliation pass are the mitigation, and the
test that matters is not the happy path but the one where an external tool edits
the world between two runs.

**The native storage format is undecided.** It no longer gates M11.5, because
vanilla data stays in the vanilla file and identity lives beside it, so M11.3's
research is now a performance question with an easier exit than it had.

**Per-item identity has a memory cost that no test will reveal early.** Eight
bytes per item is small until a server has many chests. The ID index is the part
to watch, because it holds every live item in the world at once, and M11.5
should measure it on a populated world rather than a fresh one.

**Provenance data is personal.** `server` is public, and the logs hold player
UUIDs and names. They are local runtime data, never committed, and the default
location is added to `.gitignore` in M11.5.

## Exit criteria for the track

| | Criterion |
| --- | --- |
| 1 | `examples/vanilla` passes every gate `cmd/server` passed, including the byte-parity fixtures and the pinned Node client lane |
| 2 | Three examples exist and each composes a different set of framework pieces |
| 3 | One world model serves protocol 47 and protocol 775 with no version type in the core |
| 4 | A save runs to completion with no measurable pause in the tick |
| 5 | A deliberately duplicated item is detected at the write that duplicates it |
| 6 | Placement, restart, destruction by a mob, drop, and transfer produce one connected chain from one query |
| 7 | A world saved with provenance on is readable by an unmodified vanilla tool, and a world edited by one is reconciled at load with every discrepancy recorded |
| 8 | Every vanilla command name resolves, and every unimplemented one says so |
| 9 | Turning provenance and observability off returns the server to its M6.1 resource profile |

## Amendments

**2026-08-17, Decision 7.** This document cites the M3 session findings'
survival block duplication as an open case the ID index would settle. It is not
open. Both duplications from that session were explained and fixed before the
sub-milestone designs were written: the inventory one was `tryAddToSection`
depositing part of a stack and returning false, and the block one was
`handleBlockPlace` never consuming the held item in survival, fixed in
`e67ec09`. The index remains worth building as a prospective instrument, which
is a weaker argument than the one made above.
[The M11.5 design](2026-08-17-m11-5-provenance-design.md) records it.

**2026-08-17, the track table.** Every sub-milestone now has a design:

| | Design |
| --- | --- |
| M11.2 | [World model and chunk ownership](2026-08-17-m11-2-world-model-design.md) |
| M11.3 | [Storage](2026-08-17-m11-3-storage-design.md) |
| M11.4 | [World generation](2026-08-17-m11-4-world-generation-design.md) |
| M11.5 | [Provenance](2026-08-17-m11-5-provenance-design.md) |
| M11.6 | [Observability](2026-08-17-m11-6-observability-design.md) |
| M11.7 | [Commands](2026-08-17-m11-7-commands-design.md) |
