# M11.2 world model and chunk ownership design

- Status: Draft for review
- Date: 2026-08-17
- Repository: `server`
- Milestone: M11.2, the second sub-milestone of
  [the server framework track](2026-08-16-server-framework-design.md)

## Context

M11.1 made the server importable and declared the `Store` and `Observer`
seams. It changed no data structure. This milestone changes the one every
other sub-milestone sits on.

What exists today:

- `pkg/world/gen.Section` is `Blocks [4096]uint16`, each entry
  `blockID<<4 | metadata`, which is the protocol 47 wire encoding written
  directly into memory (`pkg/world/gen/generator.go:8`).
- `gen.ChunkData` is `Sections [16]*Section` plus `Biomes [256]byte`, so the
  world is 0..255 blocks tall by construction.
- `world.World` keeps a **second** source of truth: `blocks map[BlockPos]int32`
  holding every player edit, separate from the chunks
  (`pkg/world/world.go:17`). Three call sites merge the two — `GetBlock`,
  `EncodeChunk`, and `anvil.EncodeChunkNBT` — and each merges differently.
- `EncodeChunk` builds the protocol 47 payload by hand and re-derives the
  section bitmap from the override map on every send
  (`pkg/world/chunk.go:16`).
- Every write takes `World.mu`, a single mutex over the whole world.

Two facts from `minecraft-protocol` v0.2.0 bound the design. Java 1.8 has 198
blocks addressed as `id<<4 | metadata`. Java 26.1 has 1168 blocks whose states
run to ID 29872, and its `PlayClientboundMapChunk` carries paletted section
data, heightmaps, and light masks rather than a bitmap and 2 bytes per block.
Both versions ship in the same released module, so both are checkable here
without waiting for a protocol release.

## Goals

- One in-memory block representation that names neither wire encoding.
- One source of truth per block. The override map goes away.
- Concurrent writers to one chunk that do not race, and a save that does not
  stop the tick.
- A world height that is a property of the dimension rather than a constant.

## Non-goals

- Serving protocol 775. Nothing in this repository speaks it, and M11.2 proves
  version neutrality with a registry round-trip rather than by shipping a
  second play implementation.
- Lighting. Sky and block light stay the `0xFF` fill they are today; a real
  light engine is its own milestone and nothing here forecloses it.
- Changing the storage format. M11.3 owns that. M11.2 keeps writing what is
  written today, through a shim that is deleted there.

## Decision 1: a block state is an interned handle, and handles never leave memory

```go
// State is an opaque handle to a block state. Its numeric value is assigned at
// registry construction and is meaningful only to the process that built it.
type State uint32

// StateRegistry interns block states by canonical identity and resolves them
// back to it. It is built once from a data.Set and is immutable afterwards.
type StateRegistry interface {
    // Intern returns the handle for a canonical name and property set,
    // minting one if this is the first time it is seen.
    Intern(name string, properties Properties) State
    // Lookup resolves a handle to its canonical identity.
    Lookup(s State) (name string, properties Properties, ok bool)
    // Air is the handle every empty section reads as.
    Air() State
}
```

The canonical identity is the block's registry name plus its property set,
because that is the only thing both versions agree on: `minecraft:oak_log` with
`axis=y` is `17:0` on 1.8 and one of 1168 blocks' states on 26.1.

**Handles are process-local and are never persisted or transmitted.** A saved
world holds canonical names, a wire packet holds the version's own encoding,
and the handle exists only between them. This is the property that keeps the
model honest: nothing downstream can come to depend on a numeric value, so
adding a block to a registry cannot corrupt a world or a fixture. The
alternative — a stable global state ID assigned by this repository — was
rejected because it creates a third numbering nobody else uses, which then
needs its own migration story the first time it is wrong.

The cost is one map lookup per name resolution at generator construction and
one array index per block on the encode path. Neither is on a hot loop:
generators resolve names once, and the encode path is memoized by Decision 3.

## Decision 2: sections are immutable and a write swaps a pointer

```go
// Section is 16×16×16 block states. It is immutable once constructed: every
// method returning a Section returns a new one.
type Section struct {
    states [4096]State
}

// With returns a copy of the section with one block changed. The receiver is
// unchanged, so a reader holding it sees a consistent 4096 blocks.
func (s *Section) With(index int, state State) *Section

// Chunk is one column. Its section pointers are immutable; a write builds a
// new Chunk sharing every section it did not touch.
type Chunk struct {
    Pos      ChunkPos
    Sections []*Section // len == dimension height / 16; nil means all air
    Biomes   [256]Biome
    Gen      Generation // increments on every write, for M11.3's dirty tracking
}
```

`World` holds `map[ChunkPos]*atomic.Pointer[Chunk]`. A block write loads the
pointer, builds the replacement chunk, and compare-and-swaps; a lost race
retries against the new value. Readers never block and never lock.

This is the parent design's Decision 3, and it answers three of the original
eight `docs/todo.md` items at once: overlapping writers, saving without
freezing the tick, and chunk ownership. It is also the same model
`headless-minecraft` M7 reached for its observed chunk store, so both
repositories describe a chunk the same way.

The cost is 8 KB copied per block write on 1.8-sized sections. A
`MultiBlockChange` addresses one section by definition, so a batch of edits in
one section costs one copy through a `Section.WithMany` variant. Mining a
tunnel one block at a time is the worst case and it is bounded by the client's
own dig rate.

## Decision 3: encoded section bytes are memoized on the section pointer

Immutability buys a cache that a mutable model could not have. The wire bytes
for a section depend only on the section's contents and the adapter, and a
section pointer never changes contents, so the encoding can be computed once:

```go
type Adapter interface {
    Registry() StateRegistry
    // EncodeChunk renders one chunk for this protocol version.
    EncodeChunk(c *Chunk, groundUp bool) (protocol.Packet, error)
}
```

The adapter keeps a bounded cache keyed by section pointer. Today
`EncodeChunk` rebuilds 8 KB per section per player per send, and a player
crossing a chunk boundary with a view distance of 12 triggers 25 columns of
that work. Sixteen sections at 8 KB each is 128 KB per column rebuilt for
every player who sees it.

The cache is dropped when a section is replaced, because nothing holds the old
pointer. It is bounded by section count and evicted by the same view-distance
bookkeeping that already tracks loaded chunks, so it cannot grow past the
resident world.

## Decision 4: the override map is deleted and the chunk is the only truth

`World.blocks` exists because chunks were generated values nobody wanted to
mutate. Immutable sections with a swap make mutation safe, so the chunk holds
the player's edits directly and `GetBlock` becomes one lookup instead of a
merge.

Three consequences, each of which is a bug class that disappears:

- `EncodeChunk` stops re-deriving the section bitmap from the override map.
  The fix in `0a7fc68` — send the sections a player built in — exists only
  because those two disagreed.
- `anvil.EncodeChunkNBT` stops taking a pre-filtered override map and stops
  synthesizing sections for overrides with no base section.
- "Which of the two is right" stops being a question at load, because
  `overrides.json` stops being the source of truth.

**Sequencing.** M11.3 owns the storage format, so M11.2 must not change what is
on disk. It ships a load-time shim that folds `overrides.json` into the chunks
it belongs to and a save-time shim that extracts them back out by diffing
against the generator's output for that chunk. Both shims are deleted in
M11.3, and the plan says so in the same commit that adds them. A shim nobody
records the removal of is how a temporary format becomes permanent.

## Decision 5: height and biomes come from a dimension descriptor

```go
type Dimension struct {
    Name     string // "minecraft:overworld"
    MinY     int    // 0 on 1.8, -64 on 26.1
    Height   int    // 256 on 1.8, 384 on 26.1
    Biomes   BiomeRegistry
}
```

`Sections` is indexed from `MinY>>4`, and every position calculation goes
through the descriptor rather than through `y >= 0 && y < 256`, which appears
in four places today.

Biomes stay per column, 256 per chunk, as interned handles. Java 26.1 stores
them per 4×4×4 cell, and its adapter expands each column into the 64 cells
above it. Storing 3D biomes in the core was rejected for now because no
generator in this repository produces them and no consumer reads them, so it
would be 64× the memory for a value that is constant down every column. The
descriptor is where that changes when a generator wants it.

## Decision 6: adapters live behind the seam, and 775 is proved by registry, not by a server

M11.2 ships one adapter, protocol 47, built from `v1_8.Data()`. It produces
the same bytes `pkg/world/chunk.go` produces today, and the M3 byte-parity
fixtures are what says so.

Version neutrality is proved rather than asserted, but proved at the level
this milestone can honestly reach: a second registry is built from
`v26_1.Data()` — which ships in the same `minecraft-protocol` v0.2.0 the
server already depends on — and every one of its states round-trips from
canonical identity to handle and back. A 775 chunk encoder is not built,
because a chunk encoder with no play implementation behind it is untested code
pretending to be a feature.

What that proves: no type in `pkg/world` names a version, and a registry with
29,872 states fits the handle space and the interning path. What it does not
prove: that a 775 client would accept the output. Nothing in M11 claims to.

## Interfaces

```go
package world

func NewWorld(dim Dimension, gen Generator, reg StateRegistry) *World

func (w *World) Block(pos BlockPos) State
func (w *World) SetBlock(pos BlockPos, state State) (changed bool)
func (w *World) Chunk(pos ChunkPos) *Chunk          // nil when not resident
func (w *World) LoadChunk(ctx context.Context, pos ChunkPos) (*Chunk, error)
func (w *World) Snapshot() Snapshot                  // pointer copy, no locks held

// Snapshot is a consistent view of every resident chunk at one instant. It is
// what M11.3 saves and what a long read walks without holding the world.
type Snapshot struct {
    Dimension Dimension
    Chunks    map[ChunkPos]*Chunk
    Gen       Generation
}
```

`Generator` changes to produce the new chunk type:

```go
type Generator interface {
    Generate(pos ChunkPos, into *Builder) error
    HeightAt(x, z int) int
}
```

`Builder` is a mutable staging buffer that becomes an immutable `Chunk` at the
end of generation, so a generator writing 65,536 blocks does not pay for
65,536 section copies. Generation is the one place where mutation is correct,
because nothing else can see the chunk yet. M11.4 designs what a generator is
configured with; M11.2 changes only what it writes into.

## Migration

1. Registry and `State`, with no consumer. Round-trip tests for both versions.
2. `Section`, `Chunk`, `Builder`, `Snapshot`, and the world's atomic map,
   alongside the existing types, with the concurrency tests.
3. Generators emit `Builder` output. Block constants in `flat.go`,
   `surface.go`, `ores.go`, and `trees.go` become names resolved once at
   construction, which is the change M11.4 builds parameters on top of.
4. The protocol 47 adapter, checked against the byte-parity fixtures.
5. The override map is deleted and the two shims land with it.
6. `pkg/world/anvil` moves onto the new chunk type, still writing 1.8 regions.

Each step keeps `task test`, the byte-parity fixtures, and the pinned Node
client lane green. The lane is the only check that a rewrite of the chunk path
preserved the wire.

## Testing

- Every state in `v1_8.Data()` and every state in `v26_1.Data()` round-trips
  canonical identity → handle → canonical identity.
- The protocol 47 adapter reproduces the existing byte-parity fixtures with no
  `-update`. A fixture that changes here is a bug, not a new baseline.
- Concurrent writers: N goroutines writing distinct blocks in one section, M
  readers walking it, race detector on, and every write observable afterwards.
- A save-shaped test: take a snapshot, keep writing for a second, and assert
  the snapshot's contents are exactly what they were at the instant it was
  taken.
- A placed block above the generator's terrain is present in the encoded chunk
  on the first send, the second send, and after a reload — the regression
  `0a7fc68` fixed, now expressed against the model rather than against the
  merge.
- Encode memoization: encoding one chunk twice performs the section work once,
  and a block write invalidates exactly one section's entry.

## Risks

**This rewrites two working packages.** `pkg/world/gen` and `pkg/world/anvil`
both encode 1.8 assumptions in their types, and the harness has to stay green
across the change. The byte-parity fixtures and the Node lane are the only
things that will say so, which is why they run at every step rather than at
the end.

**The atomic-swap write path is easy to get subtly wrong.** A retry loop that
rebuilds from a stale chunk silently loses writes. The concurrency test has to
assert every write is present, not merely that the race detector stayed quiet.

**Memory goes up before it goes down.** A resident chunk now holds its player
edits inline rather than in a shared map, and the encode cache holds bytes.
Against that, the override map and its per-send filtered copies go away.
M11.6's counters are what settle it; until then this is an unmeasured claim
and should be recorded as one.

**Interning is a shared mutable map if it is built lazily.** The registry is
constructed once from a `data.Set` and frozen before the first connection, so
`Intern` on the hot path is a read. A registry that mints handles during play
would need a lock on every block write, which is exactly what this design
avoids.

## Exit criteria

| | Criterion |
| --- | --- |
| 1 | No type in `pkg/world` names a protocol version or a wire encoding |
| 2 | Both versions' registries round-trip every state |
| 3 | The byte-parity fixtures and the Node lane pass unchanged |
| 4 | `World.blocks` is gone and `GetBlock` reads one place |
| 5 | Concurrent writers and readers on one chunk pass under the race detector, with every write observable |
| 6 | A snapshot taken during sustained writes is internally consistent |
| 7 | World height comes from the dimension descriptor, and no `0..255` literal remains in the block path |
