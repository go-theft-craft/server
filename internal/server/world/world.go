package world

import (
	"sync"

	"github.com/OCharnyshevich/minecraft-server/internal/server/world/gen"
)

// BlockPos represents a block position in the world.
type BlockPos struct {
	X, Y, Z int
}

// World tracks block state with a generator for base terrain and overrides for player modifications.
type World struct {
	mu        sync.RWMutex
	blocks    map[BlockPos]int32
	generator gen.Generator
	chunks    map[gen.ChunkPos]*gen.ChunkData
}

// NewWorld creates a new World with the given generator.
func NewWorld(generator gen.Generator) *World {
	return &World{
		blocks:    make(map[BlockPos]int32),
		generator: generator,
		chunks:    make(map[gen.ChunkPos]*gen.ChunkData),
	}
}

// GetOrGenerateChunk returns the ChunkData for the given chunk coordinates,
// generating and caching it if needed.
func (w *World) GetOrGenerateChunk(cx, cz int) *gen.ChunkData {
	pos := gen.ChunkPos{X: cx, Z: cz}

	w.mu.RLock()
	if c, ok := w.chunks[pos]; ok {
		w.mu.RUnlock()
		return c
	}
	w.mu.RUnlock()

	c := w.generator.Generate(cx, cz)

	w.mu.Lock()
	// Double-check after acquiring write lock.
	if existing, ok := w.chunks[pos]; ok {
		w.mu.Unlock()
		return existing
	}
	w.chunks[pos] = c
	w.mu.Unlock()
	return c
}

// GetBlock returns the block state ID at the given position.
// Checks overrides first, then falls back to the generated chunk.
func (w *World) GetBlock(x, y, z int) int32 {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if s, ok := w.blocks[BlockPos{x, y, z}]; ok {
		return s
	}

	cx, cz := x>>4, z>>4
	pos := gen.ChunkPos{X: cx, Z: cz}
	c, ok := w.chunks[pos]
	if !ok {
		// Chunk not generated yet — generate without lock.
		w.mu.RUnlock()
		c = w.GetOrGenerateChunk(cx, cz)
		w.mu.RLock()
	}

	lx, lz := x&0xF, z&0xF
	if y < 0 || y >= 256 {
		return 0
	}
	return int32(c.GetBlock(lx, y, lz))
}

// SetBlock stores a block state override.
func (w *World) SetBlock(x, y, z int, stateID int32) {
	// Ensure the chunk is generated so we know the base state.
	cx, cz := x>>4, z>>4
	c := w.GetOrGenerateChunk(cx, cz)

	w.mu.Lock()
	defer w.mu.Unlock()

	base := int32(0)
	lx, lz := x&0xF, z&0xF
	if y >= 0 && y < 256 {
		base = int32(c.GetBlock(lx, y, lz))
	}

	bpos := BlockPos{x, y, z}
	if stateID == base {
		delete(w.blocks, bpos)
	} else {
		w.blocks[bpos] = stateID
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

// SpawnHeight returns the terrain height at spawn (0, 0) + 1 for the player to stand on.
func (w *World) SpawnHeight() int {
	return w.generator.HeightAt(0, 0) + 1
}
