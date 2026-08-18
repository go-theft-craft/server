package storage

import (
	"fmt"
	"log/slog"

	"github.com/go-theft-craft/server/pkg/world"
)

// Reconciliation at load.
//
// Identity is written down in two places and the world is written down in a
// third, so the three can disagree: a crash between a region write and a
// sidecar write, an editor that rewrote a chunk while the server was stopped, a
// backup restored over half of it. An external edit is one of the ways a
// duplication enters a world, so the pass that finds one *records* it rather
// than quietly absorbing it. A discrepancy nobody is told about is the same
// information loss as no audit trail at all.
//
// What the pass does per chunk:
//
//   - An identified block whose position no longer holds a block is retired:
//     the ID was spent on something that is not there.
//   - A stored item with fewer IDs than it has items has the shortfall minted
//     where it already is. Its history begins at this load and the record says
//     so.
//   - A stored item with more IDs than items has the surplus retired.
//   - Every surviving ID is claimed in the index at the location it was found,
//     which is what rebuilds the index a restart emptied — and an ID claimed
//     twice is a duplication the index reports as it happens.
//
// Position is a stable key, so blocks need no more than the first of those.
// Items do, because a slot number means nothing until something says which
// container it belongs to.

// ReconcileSink is told what the pass changed, so the change becomes a record
// rather than a log line that scrolls away.
//
// It is an interface of primitives rather than the server's Record type
// because a record is public and this package sits below the package that
// publishes it.
type ReconcileSink interface {
	Reconciled(minted, retired []world.ItemID, at world.Location, note string)
}

// Result is what one pass found. Every field is a count somebody should be
// able to read in a log line and decide whether to worry about.
type Result struct {
	// Chunks is how many columns the pass walked.
	Chunks int
	// Minted is items that were in the world without identity.
	Minted int
	// Retired is identity that was in the sidecar without an item or a block
	// under it.
	Retired int
	// Stale is chunks whose sidecar stamp disagreed with their column.
	Stale int
}

// Empty reports whether the pass found nothing, which is the case worth not
// logging.
func (r Result) Empty() bool { return r.Minted == 0 && r.Retired == 0 }

func (r Result) String() string {
	return fmt.Sprintf("%d chunks, %d minted, %d retired, %d stale sidecars",
		r.Chunks, r.Minted, r.Retired, r.Stale)
}

// reconcileActor is who the index and the records say did this. It is its own
// kind rather than the server, because "the server minted this at startup
// because it was unaccounted for" is a different event from "the server minted
// this because something happened", and a query has to tell them apart.
var reconcileActor = world.Actor{Kind: world.ActorReconcile}

// Reconciler squares stored identity with what is actually in a chunk.
//
// A nil Index makes every method a no-op, which is what a server running
// without item identity gets.
type Reconciler struct {
	Index  world.ItemIndex
	Blocks *BlockIdentity
	Sink   ReconcileSink
	Log    *slog.Logger

	// Dim and Air are what reading a block out of a column needs.
	Dim world.Dimension
	Air world.State
}

// Chunk reconciles one column and returns what it changed.
//
// stale says the sidecar's generation stamp disagreed with the column's, which
// is reported rather than acted on: the pass does the same work either way,
// because a stamp that agrees is not evidence that the contents do.
//
// It runs on a column the world has not published yet, which is why it may
// write into the chunk's containers directly: nothing else can be holding it,
// and reading the world for a chunk that is still being loaded would ask the
// world to load it again.
func (r *Reconciler) Chunk(pos world.ChunkPos, c *world.Chunk, stale bool) Result {
	out := Result{Chunks: 1}
	if r == nil || r.Index == nil || c == nil || c.Unreadable {
		return out
	}
	if stale {
		out.Stale = 1
	}

	out.Retired += r.orphanedBlocks(pos, c)
	for chest := range c.Chests {
		minted, retired := r.container(c, chest)
		out.Minted += minted
		out.Retired += retired
	}

	return out
}

// Inventory reconciles a player's own slots, which come off disk in a file of
// their own rather than in a chunk.
//
// get and set address the slots by index; a set is called only for a slot the
// pass changed. The location kind is the player's inventory, so an ID that was
// in a chest last run and is in a pocket this run is a duplication the index
// reports rather than a discrepancy nobody sees.
func (r *Reconciler) Inventory(uuid string, slots []int, get func(int) world.ItemStack, set func(int, world.ItemStack)) Result {
	var out Result
	if r == nil || r.Index == nil {
		return out
	}

	for _, slot := range slots {
		stack := get(slot)
		if stack.IsEmpty() {
			continue
		}

		loc := world.Location{Kind: world.LocationInventory, Player: uuid, Slot: slot}
		next, minted, retired := r.stack(stack, loc)
		out.Minted += minted
		out.Retired += retired
		if minted > 0 || retired > 0 {
			set(slot, next)
		}
	}

	return out
}

// orphanedBlocks retires identity spent on a position that no longer holds a
// block. It is the whole of block reconciliation: a position holds one block,
// so there is nothing else that can have gone wrong.
func (r *Reconciler) orphanedBlocks(pos world.ChunkPos, c *world.Chunk) int {
	retired := 0

	for at, id := range r.Blocks.Positions(pos) {
		if c.At(r.Dim, at.X, at.Y, at.Z, r.Air) != r.Air {
			continue
		}
		r.Blocks.Take(at)
		loc := world.Location{Kind: world.LocationWorld, Block: at}
		if err := r.Index.Retire([]world.ItemID{id}, loc, reconcileActor); err != nil {
			r.log().Warn("retire an unaccounted block identity", "at", at, "error", err)
		}
		r.report(nil, []world.ItemID{id}, loc, "the block this identity named was gone at load")
		retired++
	}

	return retired
}

// container reconciles one chest's slots.
func (r *Reconciler) container(c *world.Chunk, pos world.BlockPos) (minted, retired int) {
	contents := c.Chests[pos]
	changed := false

	for slot := range contents {
		stack := contents[slot]
		if stack.IsEmpty() {
			continue
		}

		loc := world.Location{Kind: world.LocationContainer, Block: pos, Slot: slot}
		next, m, d := r.stack(stack, loc)
		minted += m
		retired += d
		if m > 0 || d > 0 {
			contents[slot] = next
			changed = true
		}
	}

	if changed {
		c.Chests[pos] = contents
	}

	return minted, retired
}

// stack squares one stored stack's identity with the number of items in it and
// claims what survives in the index.
func (r *Reconciler) stack(stack world.ItemStack, loc world.Location) (world.ItemStack, int, int) {
	count, held := int(stack.ItemCount), len(stack.IDs)

	switch {
	case held > count:
		// More identity than items. The surplus named items that are not
		// there, so it stops naming anything.
		surplus := append([]world.ItemID(nil), stack.IDs[count:]...)
		stack.IDs = append([]world.ItemID(nil), stack.IDs[:count]...)
		r.claim(stack.IDs, loc)
		if err := r.Index.Retire(surplus, loc, reconcileActor); err != nil {
			r.log().Warn("retire surplus identity", "at", loc.String(), "error", err)
		}
		r.report(nil, surplus, loc, "more identity than items were in this slot at load")

		return stack, 0, len(surplus)

	case held < count:
		// Items with no identity: a stack written before identity was on, or
		// one an editor put there. Minting at the source is the honest
		// description — the items were already here — and a fresh ID cannot
		// collide with a live one, so this can never invent a duplication.
		r.claim(stack.IDs, loc)
		ids, err := r.Index.Mint(count-held, loc, reconcileActor)
		if err != nil {
			r.log().Error("mint identity at load", "at", loc.String(), "error", err)

			return stack, 0, 0
		}
		stack.IDs = append(append([]world.ItemID(nil), stack.IDs...), ids...)
		r.report(ids, nil, loc, "these items were in the world with no identity at load")

		return stack, len(ids), 0

	default:
		r.claim(stack.IDs, loc)

		return stack, 0, 0
	}
}

// claim tells the index where an ID that came off disk is.
//
// It is a move from nowhere, so an ID the index has never seen is simply
// recorded, and an ID it has already placed somewhere else is reported as the
// duplication it is. A restart empties the index, and this is what fills it.
func (r *Reconciler) claim(ids []world.ItemID, loc world.Location) {
	if len(ids) == 0 {
		return
	}
	if err := r.Index.Move(ids, world.Nowhere, loc, reconcileActor); err != nil {
		r.log().Error("identity claimed at load is live somewhere else", "at", loc.String(), "error", err)
	}
}

func (r *Reconciler) report(minted, retired []world.ItemID, loc world.Location, note string) {
	if r.Sink == nil {
		return
	}
	r.Sink.Reconciled(minted, retired, loc, note)
}

func (r *Reconciler) log() *slog.Logger {
	if r.Log == nil {
		return slog.New(slog.DiscardHandler)
	}

	return r.Log
}
