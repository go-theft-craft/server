# M11.5 click paths on the index

**Date:** 2026-08-17
**How to reproduce:**

```bash
devbox run -- go test -mod vendor -run 'TestRandomClickSequences|Identity' ./internal/server/conn/
M11_MEASURE=1 devbox run -- go test -mod vendor -run TestItemIndexMemory -v ./server/
```

This records what finishing M11.5's Task 3 Step 3 — routing every item path
through the index — actually cost and actually found. The three questions Task 9
Step 3 asked are answered at the bottom.

## What the conversion turned out to be

The plan expected five conversions, one commit each, ordered easiest to hardest
and ending at `inventory.go`'s seven click handlers. It was one change of shape
instead.

Every click handler was doing its own arithmetic on `ItemCount` — decrement
here, cap at 64 there, write the remainder back — and identity cannot survive
that, because a count that changes without the IDs changing with it breaks the
invariant at the first click. Adding a `Move` beside each of those would have
meant auditing every one of them for whether the IDs it left behind were the
right ones.

So the arithmetic moved into five primitives in
`internal/server/conn/identity.go`, and they are now the only code that changes
how many items a slot holds:

| Primitive | What it is |
| --- | --- |
| `transfer(l, from, to, n)` | up to n items from one slot to another, told to the index |
| `swapSlots(l, a, b)` | two slots exchange, told as two moves |
| `take(l, slot, n)` | n items out of a slot, still live, caller says where they went |
| `consume(l, slot, n)` | n items out of a slot, retired: they stopped existing |
| `dropFromSlot(l, slot, n)` | n items out of a slot and onto the ground |

Two things fell out of that which the plan did not anticipate.

**The cursor became a slot.** `slotCursor` is a negative slot number that
`getSlotIn` and `setSlotIn` understand, so picking a stack up, placing one item,
swapping, dragging, and double-clicking are all the same `transfer` as a chest
transfer is. That is what collapsed the seven handlers into calls rather than
into seven audited conversions. It is also the reason the drag handler lost
forty lines: a left drag and a right drag differ only in how many items each
painted slot gets.

**The crafting output is not items.** An untaken result is an offer. It is
minted at the moment somebody takes it, and the ingredients are retired in the
same breath, so a crafted item is a new item rather than the ingredients wearing
a different name. A result that carried identity while it sat in the output slot
would be an item that exists twice — once as the offer, once as the ingredients
still in the grid — so the invariant deliberately excludes that one slot, and
`TestCraftingMintsTheResultAndRetiresTheIngredients` asserts the exclusion
rather than leaving it implied.

## What the property test found

Nothing. `TestRandomClickSequencesNeverBreakTheInvariant` passed the first time
it ran against the finished conversion, and it has not found a path that was
missed.

That is worth being careful about rather than pleased with, so the test was
checked against a mutant before being believed: deleting the single `moveIDs`
call inside `transfer` — the one line that tells the index anything at all —
fails it in fourteen clicks, on the first round, with the index and the window
disagreeing about where an item is. A test that passes because it is not looking
would have passed that too.

The reason it found nothing is most likely that the conversion did not leave
seven paths to get wrong. There were five primitives to get right, and each has
a hand-written test above it.

## Whether the detector fired

Not once, on legitimate clicks — which is what the property test asserts
directly: every round ends by checking that the index reported no duplication at
all. It has still never fired on real code. It fires on
`TestThePartialDepositBugIsDetected`, which reconstructs the M3
`tryAddToSection` shape deliberately.

## The index's memory cost

The framework design flagged this as the risk no test reveals early: the index
holds every live item in the world at once, and until the click paths routed
through it there was nothing in it to measure.

| Load | Live IDs | Heap | Per item |
| --- | --- | --- | --- |
| 100 players, every one of 45 slots holding a full stack | 288,000 | 42.0 MB | **145 bytes** |

145 bytes per item is a `map[ItemID]Location` at its load factor: the key is 8
bytes and `Location` is the rest — a kind, a player UUID as a string header, a
slot, a `BlockPos`, and an entity ID, sized for whichever kind of place the item
is in. A hundred fully-loaded players is 42 MB, and a million live items would
be about 145 MB.

That is affordable at the scale this server runs at, and it is the number to
watch first if it ever is not. The obvious lever is `Location`: it carries every
field for every kind, and a server that has measured this can pack it. The
second lever is the one the design already names — sharding the map — and it is
about contention rather than size, so M11.6 is what should decide it.

## What is still open

- **Task 6 and Task 7 did not land.** A placed block has no identity, so the
  chain from placement through destruction into the drop is not joined yet, and
  nothing reconciles identity at load.
- **A stack restored from disk gets identity on first use, not at load.**
  `ensureIdentity` mints what a stack is missing at the place it already is, so
  the invariant is true from the first click that moves it rather than from the
  load. Minting at the source cannot invent a duplication — a fresh ID is not
  live anywhere — but it does mean an item that survived a restart without
  identity gets a *new* one rather than being recognised. Task 7 is what makes
  that a reconciliation with a record instead of a silent mint.
- **Chest contents lose identity across a restart, player inventories do not.**
  `PlayerData` is JSON and `ItemStack` marshals its IDs; the Anvil writer has
  nowhere to put them, which is what the sidecar M11.3 wrote empty is for.
  Within one run a chest keeps identity across close and reopen, and
  `TestChestTransfersMoveIdentityIntoTheWorld` asserts it.
- **Two players holding the same chest open still race**, as `closeChest`
  already documented. Each has a working copy and the last writer wins; with
  identity on, the loser's items are now also at a location the index no longer
  agrees with. The detector will say so, which is an improvement on it happening
  silently, but the fix is a shared-container lock rather than anything here.
- **Under `DuplicateRefuse` the click layer has no rollback.** The index refuses
  its own write and the window keeps what the click produced, so the two
  disagree until something moves the item again. The default policy is allow for
  exactly this reason.
