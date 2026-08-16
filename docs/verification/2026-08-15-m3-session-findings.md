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
migration. Findings 1 and 3 were fixed on 2026-08-16, so all three are closed —
each under a test that fails against the behavior reported here.

## 1. The crafting table does not work

**Status:** fixed, 2026-08-16. It was a missing feature, and it predated M3.

Right-clicking a crafting table opened nothing. The server had no 3x3 matcher
and no `OpenWindow` anywhere in the codebase; only `matchRecipe2x2` existed,
against inventory slots 1–4.

What it took: a window layout the click handlers read their slot ranges from,
rather than the player window's constants they had hard-coded. A crafting table
has no armor slots, so its inventory section sits one slot lower — every click,
shift-click, number-key, drag, and double-click had to work in the coordinates
of whichever window is open, or a click would move the item next to the one the
player aimed at. With that in place the 3x3 is the same code as the 2x2 with a
different grid size, and the matcher generalized to a square grid of any side.

Right-clicking a table opens the window unless the player is sneaking, which is
how vanilla lets you build against one. Opening a window returns whatever the
2x2 still held, because that grid stops being reachable. A click carrying a
window ID the server does not have open is refused rather than applied to slot
numbers that no longer mean what the client meant.

Not covered: the table is not a block entity, so two players opening the same
table get separate grids, and a table broken while open leaves the window up
until the player closes it. Both are invisible in single-player use and neither
loses items — the grid is returned to whoever holds it.

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

**Status:** fixed, 2026-08-16. The cause was the *placement*, not the break:
`handleBlockPlace` never consumed the held item in survival.

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

**Ruled out — the pickup path.** This was the standing suspect, and it is
innocent. `SpawnBlockDrop` spawns one entity per drop, `TryPickupItems` removes
the entity under the same lock that credits it, and `Inventory.AddItem` merges
and places without double-counting. Each of those was read and is covered by
`TestPlaceThenBreak_ReturnsExactlyWhatWasPlaced`, which collects exactly one
item.

**The actual cause: placing a block cost nothing.** `handleBlockPlace` set the
world block and broadcast the change, and never touched the inventory. The
client decrements its own stack the moment it predicts a placement, so after
one place the server held one item more than the client showed. Breaking the
block credited the drop on top of that, and the next inventory sync handed the
client both — the stack rising by two against the number the player had been
looking at. Nothing about it was specific to breaking: any place followed by
any inventory sync showed it.

Fixed by consuming one item from the held slot on a survival placement, then
syncing that slot and the held-item equipment. Two related defects on the same
path went with it, because the fix had to read the held item to consume it:

- The block placed came from the packet, so a client could name a block it did
  not hold and build with it for free. The server now places what it has in
  hand and refuses anything that is not a block, reverting the client's
  predicted block rather than leaving a ghost.
- The placed state dropped the item's metadata, so every variant became the
  default — red wool placed as white. The state now carries it.

## What these say about M3

Nothing here implicates the transport, the framing, the compression, the
encryption, or the generated codecs. Finding 2 was the only one that could have
been caused by the migration, and it was not: the shared registry carries the
wildcard ingredients intact, and the defect was pre-existing game logic in the
server's own shift-click handler. M3 changed no crafting behavior.
