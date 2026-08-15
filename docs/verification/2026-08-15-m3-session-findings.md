# Gameplay findings from the M3 client sessions

Recorded 2026-08-15, alongside the
[M3 client verification](2026-08-15-m3-client-checks.md).

Three gameplay problems surfaced while running a real 1.8.9 client against the
migrated server. None of them is a protocol or codec failure: the sessions
logged zero decode errors in both offline and online mode, which is what M3's
client checks existed to test.

They are recorded here rather than fixed in M3, because the milestone's rule is
to add newly discovered work to a later milestone instead of silently expanding
the active one. All three belong to M6 and M9, and finding 2 is named in M6's
checklist in the master plan. Each entry says what was ruled out and how, so
whoever picks it up does not repeat the elimination.

## 1. The crafting table does not work

**Status:** missing feature, predates M3. Confirmed by reading the code.

Right-clicking a crafting table opens nothing. The server has no 3x3 matcher and
no `OpenWindow` anywhere in the codebase — `grep -rn "OpenWindow\|3x3" internal/`
returns nothing. Only `matchRecipe2x2` exists, against inventory slots 1–4.

Implementing it needs a window to open, the 3x3 grid slots, a 3x3 matcher, and
the shift-click and result-slot behavior that goes with them.

## 2. Inventory crafting works only partially

**Status:** open, owned by M6. **May be an M3 regression.**

M3 closed on its own scope — the connection path — with every gate green and
both client checks passed. This is game logic reading migrated data, so it
carries into M6, which finishes the consumer migration and is required to
settle it. It is listed there in the master plan rather than left here alone.

Task 4 moved the recipe registry from the server's own generated data to
`minecraft-protocol/data`. The matcher treats a negative ingredient metadata as
"any variant":

```go
if expected.Metadata >= 0 && data.Metadata(gridSlot.ItemDamage) != expected.Metadata {
    return false
}
```

If the shared data encodes a wildcard ingredient as `0` where the old local data
used `-1`, every recipe accepting a variant — planks from any log, torches from
any coal — silently stops matching, while exact-metadata recipes keep working.
That is the shape of "works partially".

**Partly checked, not settled.** The two datasets carry very different numbers
of `Metadata: -1` entries — 3609 in the old generated recipes, 956 in the
shared ones. That is suggestive but not conclusive: the old generator most
likely padded *empty* shape cells with `{ID: 0, Metadata: -1}` where the shared
data uses `0`, and the matcher ignores the metadata of an empty cell anyway
because of its `expected.ID > 0` guard.

What still has to be compared is a cell with `ID > 0` that accepts any
variant — planks from any log is the clearest case — in both datasets. If the
shared data gives that ingredient `Metadata: 0`, the matcher demands an exact
variant and the recipe stops matching.

**Coverage gap either way:** no test exercises `matchRecipe2x2` against the real
registry. The existing tests build recipe structs by hand, so the data
underneath the matcher can change without a single test failing. That gap let
this reach a play session, and closing it belongs with the fix.

## 3. Breaking a placed block returns two items in survival

**Status:** open. The migrated drop data is **ruled out**; the pickup path is
the remaining suspect.

Reported: place a block (stack decrements, correct for survival), break it, and
the stack comes back up by two rather than one.

**Ruled out — the drop data.** Dirt's drop entry is byte-for-byte the same
before and after the migration:

| Source | Entry |
| --- | --- |
| Old, `pkg/gamedata/versions/pc_1_8/blocks.go` | `{ID: 3, Metadata: 0, MinCount: 0, MaxCount: 0}` |
| New, `minecraft-protocol` `v1_8` | `{ID: 3, Metadata: 0, MinCount: 0, MaxCount: 0, HasMinCount: false, HasMaxCount: false}` |

`blockDrops` defaults a `0/0` count to one item, and that default was
deliberately kept on the *values* rather than switched to the new
`HasMinCount`/`HasMaxCount` flags precisely so the behavior would not shift. So
one dug block yields one drop, as before.

**Ruled out — creative-mode rules.** `breakBlock` already guards drops behind
`GetGameMode() != packet.GameModeCreative`, and the session was in survival, so
this is ordinary survival drop handling.

**Remaining suspect — the pickup path.** `SpawnBlockDrop` spawns an item entity
which is then collected. A scripted session that dug one block observed
`spawn_entity: 3` alongside `collect: 3`, which is consistent with an item being
credited more than once, or with an entity being collected by both the tracker
broadcast and the owning connection. `SpawnBlockDrop` and the collect handler
have not been read yet; that is where to start.

## What these say about M3

Nothing here implicates the transport, the framing, the compression, the
encryption, or the generated codecs. Finding 2 is the only one that could be
caused by the migration, and it is a data-shape question in a game-logic
matcher rather than a wire problem.
