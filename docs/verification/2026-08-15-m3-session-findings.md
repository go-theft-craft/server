# Gameplay findings from the M3 client sessions

Recorded 2026-08-15, alongside the
[M3 client verification](2026-08-15-m3-client-checks.md).

Three gameplay problems surfaced while running a real 1.8.9 client against the
migrated server. None of them is a protocol or codec failure: the sessions
logged zero decode errors in both offline and online mode, which is what M3's
client checks existed to test.

They are recorded here rather than fixed in M3, because the milestone's rule is
to add newly discovered work to a later milestone instead of silently expanding
the active one. Each entry says what was ruled out and how, so whoever picks it
up does not repeat the elimination.

Finding 2 is now settled and fixed, ahead of M6, because M6's checklist could
not close without it and the answer was a day's evidence rather than a
migration. Findings 1 and 3 remain open, owned by M6 and M9.

## 1. The crafting table does not work

**Status:** missing feature, predates M3. Confirmed by reading the code.

Right-clicking a crafting table opens nothing. The server has no 3x3 matcher and
no `OpenWindow` anywhere in the codebase — `grep -rn "OpenWindow\|3x3" internal/`
returns nothing. Only `matchRecipe2x2` exists, against inventory slots 1–4.

Implementing it needs a window to open, the 3x3 grid slots, a 3x3 matcher, and
the shift-click and result-slot behavior that goes with them.

## 2. Inventory crafting works only partially

**Status:** fixed. **Not an M3 regression** — the registry swap is ruled out.

**Ruled out — the migrated recipe data.** The suspicion was that the shared
data encoded a wildcard ingredient as `0` where the old local data used `-1`,
which would make the matcher demand an exact variant and silently drop every
recipe accepting one. It does not. `schema.ParseIngredient` normalizes both a
bare integer ingredient and an object with a null `metadata` to `-1`, and the
generated registry shows wildcards intact — `{ID: 4, Metadata: -1}` for the
cobblestone in a furnace, for one.

The earlier `Metadata: -1` count difference — 3609 old against 956 shared — was
the empty-cell padding the note already suspected, not lost wildcards.

**Ruled out — the matcher.** `matchRecipe2x2` was run against the real registry
across fourteen cases covering exact-metadata ingredients (each wood variant),
wildcard ingredients (planks to sticks, both coal variants to torches), shaped
recipes at every grid offset, full-grid and shapeless recipes, and two
non-recipes. All fourteen produce the correct result, including the correct
result metadata and count. The registry is a map, so the matcher was also run
200 times per grid to confirm no grid matches two recipes.

**The actual cause: shift-click crafted once.** `handleShiftClick` on the
output slot ran a single craft and stopped, where vanilla repeats until the
grid runs out. A player shift-clicking eight logs got four planks instead of
thirty-two, which is exactly "works partially" from the client side. Normal
clicking the output always worked, one result stack per click.

**A duplication bug found on the fix path.** `tryAddToSection` places what fits
and *then* returns false, so its callers, which all treated false as "nothing
moved", left the source stack intact after part of it had already been
deposited. Shift-clicking a full stack into an inventory with room for ten
created ten items from nothing. It is split into `addToSection`, returning the
leftover count, and every shift-click path now keeps the remainder in the
source slot. The output path uses a non-mutating `spaceForItem` check so a
craft that cannot be deposited whole is refused before any ingredient is
consumed, matching vanilla.

**Coverage gap, closed.** No test exercised `matchRecipe2x2` against the real
registry, and worse, `newTestConnWithCapture` left `gameData` nil — so every
crafting path in the test suite silently produced nothing, and no assertion
noticed. The harness now supplies the real `v1_8.Data()`, and
`internal/server/conn/crafting_test.go` covers the matcher against the shared
registry plus the click, shift-click, refusal, and partial-move paths.

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
encryption, or the generated codecs. Finding 2 was the only one that could have
been caused by the migration, and it was not: the shared registry carries the
wildcard ingredients intact, and the defect was pre-existing game logic in the
server's own shift-click handler. M3 changed no crafting behavior.
