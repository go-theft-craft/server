package world

import "sync"

// BlockPos represents a block position in the world.
type BlockPos struct {
	X, Y, Z int
}

// World tracks block state overrides on top of the base flat-stone world.
type World struct {
	mu     sync.RWMutex
	blocks map[BlockPos]int32
}

// NewWorld creates a new World.
func NewWorld() *World {
	return &World{blocks: make(map[BlockPos]int32)}
}

// GetBlock returns the block state ID at the given position.
// Base world: y==0 → stone (1<<4 = 16), else air (0).
func (w *World) GetBlock(x, y, z int) int32 {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if s, ok := w.blocks[BlockPos{x, y, z}]; ok {
		return s
	}
	if y == 0 {
		return blockStone << 4
	}
	return 0
}

// SetBlock stores a block state override. If the value matches the base state,
// the override is removed to save memory.
func (w *World) SetBlock(x, y, z int, stateID int32) {
	w.mu.Lock()
	defer w.mu.Unlock()

	base := int32(0)
	if y == 0 {
		base = blockStone << 4
	}

	pos := BlockPos{x, y, z}
	if stateID == base {
		delete(w.blocks, pos)
	} else {
		w.blocks[pos] = stateID
	}
}

// ForEachOverride calls fn for every block override under a read lock.
func (w *World) ForEachOverride(fn func(pos BlockPos, stateID int32)) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	for pos, state := range w.blocks {
		fn(pos, state)
	}
}
