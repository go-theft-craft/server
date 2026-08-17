package world

// Container storage.
//
// A chest's contents belong to the world, not to whoever opened it: two
// players standing at the same chest have to see one set of items, and the
// items have to outlive both sessions. The world already owns block state and
// its persistence, so it owns this beside it.

// ChestSlots is how many item slots a single chest shows.
const ChestSlots = 27

// ItemStack is one slot of a stored container or a saved inventory. It is
// deliberately not player.Slot: pkg/world sits below the player package and
// must not import it, and the wire type belongs to the protocol, not to
// storage.
//
// The JSON tags are the ones the pre-M11.3 server wrote for a player's
// inventory slot, so an existing players/<uuid>.json still loads. They are not
// the names this type would have chosen; compatibility outranks that.
type ItemStack struct {
	ID     int16 `json:"block_id"`
	Count  int8  `json:"item_count"`
	Damage int16 `json:"item_damage"`
}

// EmptyStack is the value of a slot holding nothing. The zero value will not
// do — ID 0 is stone, and only -1 means empty on the wire.
var EmptyStack = ItemStack{ID: -1}

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

// IsEmpty reports whether the stack holds nothing.
func (s ItemStack) IsEmpty() bool {
	return s.ID <= 0 || s.Count <= 0
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
