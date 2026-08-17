# M11.3 storage design

- Status: Draft for review
- Date: 2026-08-17
- Repository: `server`
- Milestone: M11.3, the third sub-milestone of
  [the server framework track](2026-08-16-server-framework-design.md)

## Context

M11.1 extracted a `Store` seam covering the calls the server makes today.
M11.2 replaces the chunk model underneath it. M11.3 is where persistence stops
being four JSON files and a write-only region writer.

What the server writes today, per `internal/server/storage/storage.go`:

| Path | Contents | Read back? |
| --- | --- | --- |
| `data/config.json` | Effective settings | Yes, by the vanilla example |
| `data/world/world.json` | Age and time of day | Yes |
| `data/world/overrides.json` | Every player block edit, whole map | Yes — **this is the world** |
| `data/world/chests.json` | Every container's contents | Yes |
| `data/world/region/r.X.Z.mca` | Anvil regions | **No** |
| `data/players/<uuid>.json` | Position, gamemode, inventory | Yes |

Two facts drive this design:

**The server writes a format nothing reads and reads a format nothing else
can.** `pkg/world/anvil` has `SaveRegion` and `EncodeChunkNBT` and no reader;
`pkg/world/nbt` has a `Writer` and no `Reader`. Every restart regenerates
terrain from the seed and replays `overrides.json` over it. The `.mca` files
are output only, which is why "Anvil chunk loading" is still item 6 on the
README roadmap.

**Every autosave re-encodes every resident chunk.** `SaveWorldAnvil` walks all
chunks, encodes each to NBT, and rewrites every region file it touches, on a
five-minute timer, whether or not a single block changed. With the default
500-chunk radius that is a lot of work to write bytes identical to the ones
already on disk.

## Goals

- One durable representation of the world that the server both writes and
  reads, so a restart restores what was there rather than regenerating and
  patching.
- A save that copies pointers, runs off the tick, and writes only what changed.
- Vanilla data in the vanilla file, readable by any external tool, with
  everything else beside it (parent Decision 4).
- A `Store` seam that no longer leaks internal types, closing M11.1's first
  narrowing.

## Non-goals

- A new native format. See Decision 4: the research question is settled
  against building one now, with the measurement that would reopen it stated.
- Provenance storage. The audit log and the ID index are M11.5, which sits on
  this.
- Multi-world. One dimension, one store. The interfaces take a world name so
  that adding a second is not a signature change, and nothing else here
  assumes more than one.

## Decision 1: the seam splits into a world store, a side store, and a player store

```go
// WorldStore holds the vanilla world: blocks, biomes, and the tile entities
// the vanilla format has fields for.
type WorldStore interface {
    LoadChunk(ctx context.Context, world string, pos ChunkPos) (*Chunk, error)
    SaveSnapshot(ctx context.Context, snap Snapshot) error
    Level(ctx context.Context, world string) (LevelData, error)
    SaveLevel(ctx context.Context, world string, data LevelData) error
    Close() error
}

// SideStore holds what the vanilla format has no field for. It is written
// from the same snapshot as the world and stamped with the same generation,
// so a mismatched pair is detected at load rather than trusted.
type SideStore interface {
    SaveSnapshot(ctx context.Context, snap Snapshot, gen Generation) error
    Load(ctx context.Context, world string, pos ChunkPos, gen Generation) (Sidecar, error)
    Close() error
}

// PlayerStore holds per-player state. PlayerData is a public value type, so
// an external implementation can satisfy this — which the M11.1 seam could
// not offer, because SavePlayer named *player.Player.
type PlayerStore interface {
    LoadPlayer(ctx context.Context, id uuid.UUID) (*PlayerData, bool, error)
    SavePlayer(ctx context.Context, data PlayerData) error
    Close() error
}
```

`LoadChunk` returning `(nil, nil)` means "no chunk here, generate one", which
is the case a fresh world hits for every chunk. An error means the store
failed and the server must not silently regenerate over data it could not
read: a world that quietly regenerates on a disk error looks like a world that
was deleted.

`PlayerData` moves out of `internal/server/storage` and becomes public with a
stable JSON shape. It carries position, gamemode, and inventory as
`world.ItemStack` values, which is the same type containers already use
(`pkg/world/container.go:16`), so item identity in M11.5 attaches in one place
rather than two.

The M11.1 `Store` interface, with its `SaveWorldAnvil` naming a format, is
deleted. It survived one milestone as an honest extraction of what existed,
and that was its whole purpose.

## Decision 2: Anvil becomes bidirectional and the JSON world files are retired

`pkg/world/anvil` gains a reader, and `pkg/world/nbt` gains the `Reader` its
`Writer` has always implied. The Anvil adapter becomes the default
`WorldStore`.

`overrides.json` and `chests.json` are retired. A world's blocks come from its
region files, and chest contents move into the region file as `TileEntities`,
which is where the vanilla format puts them and where any external tool
expects to find them. Chests are vanilla data; keeping them in a private JSON
file was the expedient choice, not the right one.

`world.json` becomes `level.dat`-shaped `LevelData` — age, time of day, seed,
generator name and parameters, and the world's format generation. Whether it
is written as vanilla `level.dat` NBT or as the store's own file is a detail
of the adapter, and the interface does not say.

**Migration is one-way and explicit.** On first load, an existing
`overrides.json` and `chests.json` are folded into the chunks they belong to,
written through, and the source files renamed to `*.migrated` rather than
deleted. A migration that deletes its input leaves nobody anything to go back
to, and a rename is one command to undo. The migration is logged at info with
counts, and it runs once because the renamed files no longer match.

## Decision 3: saving is snapshot-driven, incremental, and off the tick

M11.2's chunks carry a `Generation` that increments on write. The store keeps
the generation it last wrote per chunk, and a save writes the chunks whose
generation moved. A world where nobody built anything writes nothing.

```go
func (s *Server) save(ctx context.Context) error {
    snap := s.world.Snapshot()          // pointer copy, no lock held afterwards
    if err := s.world_store.SaveSnapshot(ctx, snap); err != nil { ... }
    return s.side_store.SaveSnapshot(ctx, snap, snap.Gen)
}
```

The tick never blocks: `Snapshot` is a map copy of pointers, and everything
after it runs on the save goroutine against values nothing can mutate. This is
the property Decision 3 of the parent design bought, and this is the milestone
that spends it.

Region files are written to a temporary file in the same directory and
renamed, so an interrupted save leaves the previous region intact. Anvil's
sector layout makes an in-place partial write a corrupt region, and a crash
during autosave is not an exotic case on a five-minute timer.

Ordering is world first, then sidecar, both stamped with the snapshot's
generation. A crash between them leaves a sidecar older than the world, which
the load-time reconciliation in Decision 5 detects and records rather than
trusts.

## Decision 4: the native format research is answered "not yet", with the number that reopens it

The parent design narrowed the question from "which format can hold
everything" to "which format is fastest for the vanilla world", because
non-vanilla data no longer needs a home inside it.

The answer for M11.3 is to keep Anvil, for three reasons:

- It is the format every external tool reads, and parent Decision 4 makes
  external readability a property worth keeping rather than a nice-to-have.
- Half of it is already written here. A reader is the missing half either way,
  and writing a reader for a documented format is cheaper than designing an
  undocumented one.
- The performance problem in front of us is not the format. It is that every
  save re-encodes every chunk, which Decision 3 fixes without touching bytes
  on disk.

The measurement that would reopen it, taken after Decision 3 lands, on a world
of 10,000 resident chunks:

| Metric | Threshold to reopen |
| --- | --- |
| Incremental save of 100 dirty chunks | > 250 ms |
| Cold load of a 25-chunk view (view distance 12, one player joining) | > 500 ms |
| Bytes on disk per chunk, mean | > 3× the same chunk's in-memory section data |

Those are recorded so the question has an exit rather than an opinion. If none
of them trips, the native format stays unbuilt and M11.3 closes.

## Decision 5: the generation stamp is checked at load, and disagreement is recorded

Both stores are written from one snapshot and stamped with its generation. At
load:

- Stamps equal: the pair is consistent, load proceeds.
- Sidecar older than the world: the crash-between-writes case. The sidecar's
  missing entries are reconciled per parent Decision 4 — block identity absent
  for a block that exists is simply absent, and the load records how many.
- Sidecar newer than the world, or a chunk in the sidecar with no chunk in the
  world: something outside this server rewrote the region file. Each
  discrepancy is recorded as an event, not absorbed.

Reconciliation records go to the log in M11.3 and to the provenance store in
M11.5, which is why the record shape is defined here and the durable
destination arrives there.

## Interfaces

```go
package server

// FileStore returns the framework's default: Anvil regions for the world,
// a chunk-keyed sidecar beside them, and one JSON file per player.
func FileStore(dir string, log *slog.Logger) (*Storage, error)

func (s *Storage) World() WorldStore
func (s *Storage) Side() SideStore
func (s *Storage) Players() PlayerStore

// Options wired through server.New.
func WithWorldStore(WorldStore) Option
func WithSideStore(SideStore) Option
func WithPlayerStore(PlayerStore) Option
```

Three options rather than one keep the three lifetimes separate: an
application that wants its players in a database and its world on disk should
not have to reimplement chunk encoding to get there.

`WithStore(Store)` from M11.1 is removed. It is a breaking change to a seam
one milestone old, published unversioned and consumed by nothing outside this
repository, and the parent design's risk section already names that as the
cost of extracting the interface that existed rather than the one that was
wanted.

## On-disk layout

```
data/
├── config.json                     # the application's, not the store's
├── world/
│   ├── level.dat                   # age, time, seed, generator name and params
│   ├── region/r.X.Z.mca            # vanilla Anvil: blocks, biomes, tile entities
│   ├── sidecar/r.X.Z.side          # block identity, per parent Decision 8
│   ├── overrides.json.migrated     # renamed at first load, then ignored
│   └── chests.json.migrated
└── players/<uuid>.json
```

The sidecar is region-granular so it shares the world's write batching, and
chunk-keyed inside so a chunk's identity data loads and unloads with the
chunk.

## Migration

1. `pkg/world/nbt` gains a reader, tested against the bytes its writer
   produces and against a region file written by vanilla.
2. `pkg/world/anvil` gains chunk and region reading, round-tripped against its
   own writer.
3. `WorldStore`, `SideStore`, and `PlayerStore` land with the Anvil
   implementation; the server still saves the old files as well, so the two
   can be compared on a real world.
4. Incremental, generation-driven saving replaces the full re-encode.
5. Chest contents move into tile entities; the JSON path becomes migration
   only.
6. The old files are retired, the M11.1 `Store` is deleted, and the shims
   M11.2 added for `overrides.json` are deleted in the same commit.

## Testing

- NBT writer and reader round-trip every tag type, including nested lists and
  empty compounds.
- A region file written by this server is read back into chunks equal to the
  ones written, block for block.
- A region file written by **vanilla** is read and re-encoded, and the
  re-encode is compared field by field rather than byte for byte, because
  vanilla's tag ordering is not something to imitate.
- A world saved with a sidecar is opened by a reader that knows nothing about
  sidecars and yields the same blocks. This is the test that keeps parent
  Decision 4 honest.
- Incremental save: touch one block, save, and assert exactly one region file's
  mtime moved and every other file is byte-identical.
- Crash safety: kill the process mid-save (a store fault injected at the write
  between temp file and rename) and assert the previous region loads.
- Migration: a data directory with `overrides.json` and `chests.json` loads
  into the same world state the old code produced, and the second start reads
  regions with no migration.
- Save under load: sustained block writes for the duration of a save, with the
  tick's period measured and asserted not to move.

## Risks

**Reading vanilla NBT is where undocumented reality lives.** Writers are
forgiving and readers are not. The mitigation is a fixture directory of real
region files, including at least one written by vanilla rather than by this
server, and a reader that fails loudly on anything it does not understand
rather than defaulting.

**The migration runs on a world someone cares about.** It renames rather than
deletes, it logs counts, and the test covers the case where it runs against a
world that has both region files and overrides that disagree — which is every
world written by the current code, since the regions were never read back.

**Incremental saving hides bugs that full saving papered over.** A chunk whose
generation does not move when it should is a lost edit that only appears after
a restart. The generation counter is set in exactly one place in M11.2, and
the test that matters writes a block, saves, restarts, and reads it back.

**Two stores can drift**, which the parent design already names. The
generation stamp is the detector and the reconciliation pass is the response;
neither prevents drift, and neither is supposed to.

## Exit criteria

| | Criterion |
| --- | --- |
| 1 | The server reads the world it wrote: place a block, restart, and it is there with no `overrides.json` |
| 2 | A world written here opens in an unmodified external Anvil reader |
| 3 | A vanilla-written region loads without error |
| 4 | An autosave with no edits writes no bytes |
| 5 | A save under sustained writes leaves the tick period unchanged within measurement noise |
| 6 | An interrupted save leaves the previous region readable |
| 7 | `Store` no longer names an internal type, and player persistence runs through the seam |
| 8 | The four thresholds in Decision 4 are measured and recorded, whichever way they fall |
