package world

import "slices"

// Container storage.
//
// A chest's contents belong to the world, not to whoever opened it: two
// players standing at the same chest have to see one set of items, and the
// items have to outlive both sessions. The world already owns block state and
// its persistence, so it owns this beside it.

// ChestSlots is how many item slots a single chest shows.
const ChestSlots = 27

// MaxStackSize is how many of one item a slot holds.
//
// Vanilla stacks tools to one and ender pearls to sixteen; this server stacks
// everything to sixty-four, which every path that fills a slot already
// assumed. It is named here because the identity invariant is stated in terms
// of it: a split that overflows a slot loses IDs.
const MaxStackSize = 64

// ItemStack is one slot: of a stored container, of a saved inventory, or of a
// player's own. It is the *only* item type — internal/server/player.Slot is an
// alias for it — because attaching identity to two types would guarantee they
// diverge.
//
// The JSON tags are the ones the pre-M11.3 server wrote for a player's
// inventory slot, so an existing players/<uuid>.json still loads.
type ItemStack struct {
	BlockID    int16 `json:"block_id"`
	ItemCount  int8  `json:"item_count"`
	ItemDamage int16 `json:"item_damage"`

	// IDs is one entry per item when item identity is enabled, and nil when it
	// is not. The wire never carries it: the protocol has no field for it, and
	// ToGeneratedSlot drops it.
	//
	// The invariant, when identity is on, is len(IDs) == int(ItemCount) on
	// every stack, after any sequence of splits and merges.
	IDs []ItemID `json:"ids,omitempty"`
}

// EmptyStack is the value of a slot holding nothing. The zero value will not
// do — ID 0 is stone, and only -1 means empty on the wire.
var EmptyStack = ItemStack{BlockID: -1}

// ChestContents is the full contents of one chest.
type ChestContents [ChestSlots]ItemStack

// EmptyChest returns contents with every slot empty.
func EmptyChest() ChestContents {
	var c ChestContents
	for i := range c {
		c[i] = EmptyStack
	}

	return c
}

// Equal compares two chests, including item identity.
func (c ChestContents) Equal(other ChestContents) bool {
	for i := range c {
		if !c[i].Equal(other[i]) {
			return false
		}
	}

	return true
}

// IsEmpty reports whether the stack holds nothing.
func (s ItemStack) IsEmpty() bool {
	return s.BlockID <= 0 || s.ItemCount <= 0
}

// SameItem reports whether two stacks hold the same thing, ignoring how many
// and ignoring identity. It is what decides whether two stacks may merge.
func (s ItemStack) SameItem(other ItemStack) bool {
	return s.BlockID == other.BlockID && s.ItemDamage == other.ItemDamage
}

// Equal compares two stacks including their identity.
//
// It exists because ItemStack stopped being comparable with == the moment it
// carried a slice.
func (s ItemStack) Equal(other ItemStack) bool {
	if s.BlockID != other.BlockID || s.ItemCount != other.ItemCount || s.ItemDamage != other.ItemDamage {
		return false
	}
	if len(s.IDs) != len(other.IDs) {
		return false
	}
	for i := range s.IDs {
		if s.IDs[i] != other.IDs[i] {
			return false
		}
	}

	return true
}

// Clone returns a stack whose IDs do not alias the receiver's.
func (s ItemStack) Clone() ItemStack {
	s.IDs = slices.Clone(s.IDs)

	return s
}

// Split takes n items off the stack and returns the two halves.
//
// The first n IDs go with the taken half, deterministically, because a replay
// of the same click sequence has to produce the same assignment.
func (s ItemStack) Split(n int) (taken, rest ItemStack) {
	switch {
	case n <= 0:
		return EmptyStack, s.Clone()
	case n >= int(s.ItemCount):
		return s.Clone(), EmptyStack
	}

	taken = ItemStack{BlockID: s.BlockID, ItemCount: int8(n), ItemDamage: s.ItemDamage}
	rest = ItemStack{BlockID: s.BlockID, ItemCount: s.ItemCount - int8(n), ItemDamage: s.ItemDamage}

	if s.IDs != nil {
		taken.IDs = slices.Clone(s.IDs[:min(n, len(s.IDs))])
		rest.IDs = slices.Clone(s.IDs[min(n, len(s.IDs)):])
	}

	return taken, rest
}

// Merge moves up to n items from other onto the stack and returns both.
//
// It refuses to merge stacks of different items, which the caller is expected
// to have checked with SameItem; the check is repeated here because a merge
// that silently did nothing would look like item loss.
func (s ItemStack) Merge(other ItemStack, n int) (merged, remainder ItemStack) {
	if other.IsEmpty() || n <= 0 {
		return s.Clone(), other.Clone()
	}
	if !s.IsEmpty() && !s.SameItem(other) {
		return s.Clone(), other.Clone()
	}

	moving := min(n, int(other.ItemCount))
	taken, rest := other.Split(moving)

	merged = s.Clone()
	if merged.IsEmpty() {
		merged = ItemStack{BlockID: other.BlockID, ItemDamage: other.ItemDamage}
	}
	merged.ItemCount += taken.ItemCount
	if taken.IDs != nil || merged.IDs != nil {
		merged.IDs = append(merged.IDs, taken.IDs...)
	}

	return merged, rest
}

// isEmptyChest reports whether every slot holds nothing.
func isEmptyChest(contents ChestContents) bool {
	for _, s := range contents {
		if !s.IsEmpty() {
			return false
		}
	}

	return true
}

// Chest returns the contents stored at a position. A chest that has never been
// opened has no entry, and an empty one is returned rather than a zero value
// that would read as 27 stone blocks.
func (w *World) Chest(pos BlockPos) ChestContents {
	if !w.dim.Contains(pos.Y) {
		return EmptyChest()
	}
	if c, ok := w.Chunk(pos.ChunkPos()).Chests[pos]; ok {
		return c
	}

	return EmptyChest()
}

// SetChest replaces the contents stored at a position. Contents that are
// entirely empty are dropped rather than stored, so an untouched chest costs
// nothing to save.
func (w *World) SetChest(pos BlockPos, contents ChestContents) {
	w.writeChest(pos, contents, isEmptyChest(contents))
}

// RemoveChest deletes the contents stored at a position and returns what was
// there, which is what a broken chest has to spill.
func (w *World) RemoveChest(pos BlockPos) ChestContents {
	if !w.dim.Contains(pos.Y) {
		return EmptyChest()
	}

	contents := w.Chest(pos)
	w.writeChest(pos, ChestContents{}, true)

	return contents
}

// writeChest publishes a container change the same way a block write is
// published: by swapping the column, so a chunk save carries its containers
// and a snapshot cannot see half of one.
func (w *World) writeChest(pos BlockPos, contents ChestContents, empty bool) {
	if !w.dim.Contains(pos.Y) {
		return
	}

	slot := w.chunkSlot(pos.ChunkPos())
	for {
		old := slot.Load()
		next, changed := old.withChest(pos, contents, empty, Generation(w.generation.Add(1)))
		if !changed {
			return
		}
		if slot.CompareAndSwap(old, next) {
			return
		}
		// Lost the race: reload and rebuild from the winner's chunk.
	}
}

// Chests returns every container in the resident world. It exists for the
// one-way migration off chests.json and for tests; a saver reads them from the
// chunks a snapshot holds.
func (w *World) Chests() map[BlockPos]ChestContents {
	result := map[BlockPos]ChestContents{}
	w.ForEachChunk(func(_ ChunkPos, c *Chunk) {
		for pos, contents := range c.Chests {
			result[pos] = contents
		}
	})

	return result
}
