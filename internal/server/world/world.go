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

	// Time tracking (protected by mu).
	age       int64 // total ticks since world creation
	timeOfDay int64 // 0-23999 cycle; negative = frozen
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

// ForEachChunk calls fn for each generated chunk under a read lock.
func (w *World) ForEachChunk(fn func(pos gen.ChunkPos, chunk *gen.ChunkData)) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	for pos, chunk := range w.chunks {
		fn(pos, chunk)
	}
}

// OverridesForChunk returns block overrides that belong to the given chunk.
func (w *World) OverridesForChunk(cx, cz int) map[BlockPos]int32 {
	w.mu.RLock()
	defer w.mu.RUnlock()

	result := make(map[BlockPos]int32)
	for pos, stateID := range w.blocks {
		if pos.X>>4 == cx && pos.Z>>4 == cz {
			result[pos] = stateID
		}
	}
	return result
}

// PreGenerateRadius generates all chunks within the given radius centered on (0,0).
func (w *World) PreGenerateRadius(radius int) int {
	count := 0
	for cx := -radius; cx <= radius; cx++ {
		for cz := -radius; cz <= radius; cz++ {
			w.GetOrGenerateChunk(cx, cz)
			count++
		}
	}
	return count
}

// SpawnHeight returns the terrain height at spawn (0, 0) + 1 for the player to stand on.
func (w *World) SpawnHeight() int {
	return w.generator.HeightAt(0, 0) + 1
}

// Tick advances the world age by one tick and, if timeOfDay is non-negative,
// advances it within the 0-23999 range. Returns the new age and timeOfDay.
func (w *World) Tick() (age, timeOfDay int64) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.age++
	if w.timeOfDay >= 0 {
		w.timeOfDay = (w.timeOfDay + 1) % 24000
	}
	return w.age, w.timeOfDay
}

// GetTime returns the current world age and time of day.
func (w *World) GetTime() (age, timeOfDay int64) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.age, w.timeOfDay
}

// SetTimeOfDay sets the time of day (0-23999, or negative to freeze).
func (w *World) SetTimeOfDay(t int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.timeOfDay = t
}

// SetTime sets both the world age and time of day (used when loading from storage).
func (w *World) SetTime(age, timeOfDay int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.age = age
	w.timeOfDay = timeOfDay
}

// GetBlockOverrides returns a copy of all block overrides (used for persistence).
func (w *World) GetBlockOverrides() map[BlockPos]int32 {
	w.mu.RLock()
	defer w.mu.RUnlock()

	result := make(map[BlockPos]int32, len(w.blocks))
	for k, v := range w.blocks {
		result[k] = v
	}
	return result
}

// SetBlockOverrides replaces all block overrides (used when loading from storage).
func (w *World) SetBlockOverrides(overrides map[BlockPos]int32) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.blocks = overrides
}
