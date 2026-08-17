# M11.5 provenance design

- Status: Draft for review
- Date: 2026-08-17
- Repository: `server`
- Milestone: M11.5, the fifth sub-milestone of
  [the server framework track](2026-08-16-server-framework-design.md)

## Context

Parent Decisions 6, 7, and 8 settle the shape: an append-only audit log with
its own store, a 64-bit ID on every item with a live ID-to-location index, and
sparse block identity keyed by position. This document turns that into
interfaces, arithmetic, and a write path.

One correction to the parent design, which cited an open case as motivation.
It says the M3 session findings "record a survival block duplication that is
not caused by the migrated drop data and has no explanation yet". That is no
longer true. Both duplications from that session were explained and fixed
before this design was written:

- The inventory duplication was `tryAddToSection` depositing part of a stack
  and then returning false, so callers left the source stack intact
  (`docs/verification/2026-08-15-m3-session-findings.md:77`).
- The survival block duplication was the **placement**, not the break:
  `handleBlockPlace` never consumed the held item, fixed in `e67ec09`.

So the ID index has no open case waiting for it. It is a prospective
instrument, and this design should be read as buying detection for the next
one rather than as solving a live bug. That is a weaker argument than the
parent design made, and it is the honest one.

What the item model looks like today: `player.Slot` is
`{BlockID int16, ItemCount int8, ItemDamage int16}`, `world.ItemStack` is the
same three fields for container contents, and `player.ItemEntity` wraps a
`Slot` for a dropped item. Items are created and destroyed in at least six
places — creative slot set, crafting output, block drops, mob-less death
drops, chest transfer, and inventory clicks — and nothing correlates them.

## Goals

- Every item in the world carries an identity that survives a restart.
- A write that would place a live ID in a second location is detected at that
  write.
- Placement, destruction, drop, and transfer of one item answer as one chain,
  across restarts.
- The audit path never touches the tick, and never drops a record silently.

## Non-goals

- Rollback. Records carry old and new state so a later milestone can build it;
  nothing here consumes them that way.
- Universal block identity. It stays behind the flag parent Decision 8
  describes, with the key space ready for it.
- Cross-server identity. IDs are unique within one server forever, which is
  the scope detection needs.
- Querying at scale. Decision 5 states the query limits the default store has
  and where a different store takes over.

## Decision 1: an ItemID is an epoch and a counter, and the split is 24/40

```go
// ItemID identifies one item for the life of a server. The high 24 bits are
// the run epoch and the low 40 bits are a counter within that run.
type ItemID uint64

const (
    epochBits   = 24            // 16,777,215 server starts
    counterBits = 40            // 1,099,511,627,775 items per run
)
```

The epoch is a counter in `level.dat`, incremented once at startup before the
first item is minted, not a timestamp. A clock that moves backwards — a
restored VM, an NTP correction, a container without a battery-backed clock —
would mint colliding IDs, and the whole value of an ID is that it never
collides.

At the limits: a server would have to restart every second for six months to
exhaust the epoch, and mint a million items a second for twelve days to
exhaust a run's counter. Both exits are the same: minting refuses, the failure
is logged at error, and the server keeps running with un-identified items
rather than stopping. An audit gap is bad; a server that will not start
because it once ran for twelve days is worse.

64 bits rather than a UUID is the parent design's call, and the reason holds:
half the memory and half the storage of a UUID in the structure that holds
every live item in the world.

## Decision 2: identity attaches to the stack, and the stack is one type

M11.3 makes `world.ItemStack` the public item type and `PlayerData` carries
it, which means identity attaches in one place:

```go
type ItemStack struct {
    Name   string   // "minecraft:diamond", the canonical identity from M11.2
    Count  int8
    Damage int16
    IDs    []ItemID // len(IDs) == Count when identity is on; nil when off
}
```

Split, merge, and craft are the three operations that have to be right:

- **Split** 64 into 32 and 32: the ID slice splits at the same index. Which
  half keeps which IDs is arbitrary and must be deterministic — the first *n*
  go with the first stack — so a replay produces the same answer.
- **Merge**: concatenate. A merge that would exceed the stack size splits
  again, and the leftover keeps the tail.
- **Craft**: input IDs are retired with a record naming the recipe, and output
  IDs are minted with a record naming the same recipe, so the chain is
  connected across a transformation rather than broken by it.

The wire never carries IDs. `player.ToGeneratedSlot` already drops everything
the protocol has no field for, and no 1.8 client has a use for them.

## Decision 3: the index is the write path, not a mirror of it

Every item movement goes through one API, and the index is updated inside it:

```go
// Location is where an item is. A stack is at exactly one location.
type Location struct {
    Kind      LocationKind // player, container, entity, cursor, crafting, world
    Player    uuid.UUID    // Kind == player
    Container BlockPos     // Kind == container
    Entity    int32        // Kind == entity, the item entity's ID
    Slot      int          // within a player inventory or a container
}

type Index interface {
    // Mint allocates n IDs and records them at loc.
    Mint(n int, loc Location, by Actor) ([]ItemID, error)
    // Move records that ids left from and arrived at to. It returns
    // ErrDuplicate when an ID is live somewhere other than from.
    Move(ids []ItemID, from, to Location, by Actor) error
    // Retire records that ids ceased to exist.
    Retire(ids []ItemID, at Location, by Actor) error
    // Where answers "where is this item now".
    Where(id ItemID) (Location, bool)
}
```

`Move` is what makes duplication detectable rather than forensic: an ID that
is live at a location other than `from` is being copied, and the write that
copies it is the one that reports it. `ErrDuplicate` carries both locations and
the actor.

**The default policy is to record and allow.** A detector that refuses the
write turns a duplication bug into an item-loss bug, and item loss on a false
positive is worse for the player than the duplication it prevented. An option
selects refusal for anyone who would rather stop the write; both paths record.

The index is the durable item sidecar from parent Decision 4. It is keyed by
ID, so nothing re-keys when an item moves, and it is written from the same
snapshot as the world with the same generation stamp.

## Decision 4: records are typed, actors carry cause, and the log is append-only

```go
type Record struct {
    At     time.Time
    Kind   RecordKind // block or item
    Actor  Actor
    World  string

    // Kind == block
    Pos      BlockPos
    OldState string // canonical name, not a handle: handles are process-local
    NewState string

    // Kind == item
    Item     string   // canonical name
    IDs      []ItemID
    From, To Location
    Reason   Reason   // place, break, craft, pickup, drop, transfer, death,
                      // creative_set, reconcile, mint, retire
}

type Actor struct {
    Kind   ActorKind // player, mob, explosion, fire, liquid, piston, worldgen,
                     // command, extension, reconcile
    Player uuid.UUID
    Entity EntityRef
    Cause  *Actor    // the creeper was lit by someone
}
```

Block states are recorded as canonical names rather than M11.2 handles,
because a handle means nothing to the process that reads the log next week.
This is the same rule M11.2 states for storage, applied to the audit log.

`Cause` is what makes "a creeper broke it, but who lit the creeper" answerable.
It is a pointer chain, bounded at a small depth on write so a cycle or a long
chain cannot make a record unbounded.

## Decision 5: the default store is a rotating file of length-prefixed records, and its query limits are stated

The default `ProvenanceStore` writes newline-delimited JSON, one record per
line, rotating by size and by day, with a manifest naming each file's time
range.

JSON because it adds no dependency and a human with `grep` can read an audit
log, which is most of what an audit log is for. The cost is size, roughly
200–300 bytes per record against about 60 for a packed binary encoding, and a
server logging every block placement in a busy world will notice. The
interface exists so a heavier adapter — a column store, a database — is a
different implementation rather than a different design.

The three queries from the parent design:

```go
type ProvenanceStore interface {
    Append(ctx context.Context, r Record) error
    AtPosition(ctx context.Context, world string, pos BlockPos, window TimeRange) ([]Record, error)
    ByActor(ctx context.Context, a Actor, window TimeRange) ([]Record, error)
    ForItem(ctx context.Context, id ItemID) ([]Record, error)
    Close() error
}
```

The default store answers them by scanning the files whose manifest range
overlaps the window. **That is a linear scan, and the design says so rather
than implying an index it does not have.** The budget: a query over a
seven-day window on a server producing 10 records a second scans about 6
million records, which at 250 bytes each is 1.5 GB of I/O. Acceptable for an
operator investigating an incident, not acceptable on a hot path, and nothing
in the server calls a query on a hot path.

`ForItem` is the exception worth optimizing, because "where did this item come
from" is the question someone asks interactively. The ID index already knows
where an item is now; the store keeps a per-file bloom filter over item IDs so
a scan skips files that cannot contain one. A bloom filter is 1.2 MB per
million records at a 1% false positive rate, and it turns most `ForItem`
queries into one or two file reads.

Retention belongs to the implementation: the file store takes a window and a
size cap and deletes whole files, oldest first, never partial ones.

## Decision 6: the write path is a bounded queue, and overflow is loud

```go
// Recorder is what the tick touches. Every method is non-blocking by default.
type Recorder interface {
    Record(r Record)
}
```

The tick appends to a bounded channel and a writer goroutine drains it into
the store. The default on overflow is to drop, count, and warn — with a rate
limit on the warning, because the condition that produces overflow also
produces a warning per record — and an option to block instead for anyone who
needs a complete audit.

Dropping silently is the one behavior that makes the feature worse than not
having it, which is why the drop counter is itself a sample through M11.6's
`Observer` rather than only a log line.

Queue depth defaults to 8192 records, which absorbs a burst of a full
inventory transfer plus an explosion without touching the tick, and costs
about 2 MB at the record sizes above.

**Provenance is off by default**, per the parent design. With it off,
`Recorder` is a no-op, `ItemStack.IDs` stays nil, and nothing is minted, which
is the configuration the M6.1 resource profile is measured against.

## Decision 7: block identity is sparse and links to the item that drops

A block placed by a non-worldgen actor gets an ID, held in the chunk sidecar
M11.3 defines, released when the block breaks. On break, the block's ID and the
minted drop's item ID are written into the same record, which is the join that
makes one chain run from placement through destruction, through the drop, and
through every transfer afterwards.

Position is a stable key, so unlike item identity this needs no index and no
reconciliation: a chunk's block IDs load and unload with the chunk. Blocks
cannot duplicate, because a position holds one block, so the detection argument
applies to items only.

Universal block identity — an ID for worldgen blocks too — stays behind a flag.
The parent design's table is the reason: a 2000×2000 world is 1.02 billion
blocks and 8.2 GB of identity alone, nearly all of it air and stone no query
would reach.

## Decision 8: reconciliation at load turns an external edit into a record

Parent Decision 4 defines the pass. Concretely, at load, for each chunk whose
sidecar generation matches the world's:

- An item in the world with no ID gets one minted, with a record whose reason
  is `reconcile` and whose actor kind is `reconcile`. Its history begins at
  this load and the record says so.
- An ID in the index whose location holds no such item is retired, with a
  record saying it was unaccounted at load.

Both counts are logged and sampled. An external edit is one of the ways a
duplication enters a world, so making it visible serves detection rather than
merely tolerating it.

## Privacy

Records hold player UUIDs and usernames. They are local runtime data under
`data/`, which `.gitignore` already excludes (`.gitignore:41`), and the
provenance directory lands inside it rather than beside it. The design adds
one rule the repository does not have yet: **no test fixture may contain a
real player UUID or username**, and the fixtures this milestone adds use
generated ones.

## Interfaces

```go
package server

func WithProvenance(store ProvenanceStore, opts ...ProvenanceOption) Option
func FileProvenance(dir string, window time.Duration, cap int64) (ProvenanceStore, error)

func WithItemIdentity(index Index) Option   // Mint/Move/Retire; off when unset
func ProvenanceOverflowBlocks() ProvenanceOption
func ProvenanceDuplicateRefuses() ProvenanceOption
```

## Migration

1. `ItemID`, the epoch in `level.dat`, and the minting arithmetic, with no
   consumer.
2. `world.ItemStack.IDs` and the split, merge, and craft rules, with the
   inventory paths moved onto them one at a time. This is the step with the
   most surface: `internal/server/conn/inventory.go` is 1,097 lines and every
   click path touches a stack.
3. The index and `Move`, with duplication detection, recording only.
4. `Recorder`, the queue, and the file store with its three queries.
5. Block identity in the sidecar, and the break-to-drop join.
6. Reconciliation at load.

Each step is off by default until the last, so a half-built audit trail never
becomes a half-true one.

## Testing

- Mint, split, merge, and craft preserve the invariant `len(IDs) == Count` on
  every stack, checked by a property test over random click sequences.
- A deliberately duplicated item — the same ID written to two slots — is
  reported at the second write, with both locations and the actor, and not by
  a later sweep.
- The `tryAddToSection` bug from M3, reconstructed: deposit part of a stack and
  leave the source intact, and assert the detector catches it. The bug is fixed
  and the test is about the instrument, not the bug.
- Restart chain: place a block, stop the server, start it, break the block with
  a mob lit by a player, and one `ForItem` query returns placement,
  destruction, drop, and pickup with the creeper's `Cause` naming the player.
- Reconciliation: edit the world between two runs by adding an item and
  removing another, and assert both discrepancies are recorded rather than
  absorbed.
- Overflow: a store that blocks forever, a full queue, and the assertion that
  the tick period does not move and the drop counter does.
- Off by default: with no provenance option, no ID is minted, no file is
  created, and the item paths allocate nothing extra — checked with
  `testing.AllocsPerRun` rather than by inspection.
- Epoch exhaustion: a store forced to the last epoch refuses to mint, logs, and
  keeps serving.

## Risks

**The instrument has no open case to prove itself against.** Both M3
duplications are fixed, so the first real test of the detector is a bug nobody
has found yet. The `tryAddToSection` reconstruction is the closest thing to
evidence available, and it should be written first rather than last.

**The ID index holds every live item in the world.** Eight bytes per item is
small until a server has many chests. It should be measured on a populated
world rather than a fresh one, per the parent design's risk, and M11.6's
counters are how.

**Every inventory path has to be moved, and one missed path is a silent gap.**
The property test over random click sequences is the mitigation, because it
exercises paths a hand-written test would not think to combine.

**JSON records are large.** A busy server writing every block placement will
produce gigabytes a week. Retention defaults exist for that reason, and the
size is stated here so the first person to notice is not surprised.

## Exit criteria

| | Criterion |
| --- | --- |
| 1 | Every item in a running world has an ID, and the invariant holds under random click sequences |
| 2 | A duplicated ID is reported at the write that duplicates it, with both locations |
| 3 | Placement, restart, destruction by a mob, drop, and transfer answer as one chain from one query |
| 4 | An external edit between two runs is reconciled with every discrepancy recorded |
| 5 | A full queue drops, counts, warns, and leaves the tick period unchanged |
| 6 | Provenance off returns the server to its M6.1 allocation profile on the item paths |
| 7 | No fixture contains a real player UUID or username |
