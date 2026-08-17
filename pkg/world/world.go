package world

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

// BlockPos represents a block position in the world.
type BlockPos struct {
	X, Y, Z int
}

// Generator produces chunk data deterministically from a seed.
//
// It is declared here rather than taken from pkg/world/gen because generation
// writes into a world.Builder: gen imports world, so world cannot import gen.
// gen.Generator satisfies this structurally.
type Generator interface {
	Generate(pos ChunkPos, into *Builder) error
	HeightAt(blockX, blockZ int) int
}

// Loader is where a world looks for a column before generating one.
//
// It is declared here for the same reason Generator is: a store lives above
// this package, and this package must not import it.
type Loader interface {
	// LoadChunk returns the stored column, or nil for a position nothing has
	// been written to. An error means the store failed, and the world will
	// not generate over what it could not read.
	LoadChunk(pos ChunkPos) (*Chunk, error)
}

// Binder is the optional interface a Generator implements when it needs block
// state handles. The world binds a generator once, at construction, so that a
// palette is resolved before the first block is written rather than on the
// write path.
type Binder interface {
	Bind(reg StateRegistry) error
}

// World holds the resident chunks of one dimension.
//
// A chunk is immutable and lives behind an atomic pointer, so a read is a
// pointer load and a write is a compare-and-swap. Only time and containers
// still take a mutex; they are not on the block path.
type World struct {
	dim       Dimension
	reg       StateRegistry
	air       State
	generator Generator
	loader    Loader

	// adapter renders the world for the version its clients speak. The world
	// itself never calls it: it holds it so a connection and a saver can ask
	// one place which encoding this world is served in.
	adapter Adapter

	chunks     sync.Map // ChunkPos -> *atomic.Pointer[Chunk]
	generation atomic.Uint64
	genErr     atomic.Pointer[error]

	mu        sync.RWMutex
	chests    map[BlockPos]ChestContents
	age       int64 // total ticks since world creation
	timeOfDay int64 // 0-23999 cycle; negative = frozen
}

// NewWorld creates a World for one dimension, rendered by one adapter and
// filled by one generator.
func NewWorld(dim Dimension, adapter Adapter, generator Generator) (*World, error) {
	if adapter == nil {
		return nil, errors.New("world: nil adapter")
	}
	reg := adapter.Registry()
	if reg == nil {
		return nil, errors.New("world: adapter has no registry")
	}

	w := &World{
		dim:       dim,
		reg:       reg,
		air:       reg.Air(),
		generator: generator,
		adapter:   adapter,
		chests:    make(map[BlockPos]ChestContents),
	}

	if b, ok := generator.(Binder); ok {
		if err := b.Bind(reg); err != nil {
			return nil, fmt.Errorf("world: bind generator: %w", err)
		}
	}

	return w, nil
}

// SetLoader gives the world somewhere to look before it generates.
//
// It is construction-time only: the server calls it in New, before anything
// can read a chunk. There is no way to change a loader under a running world,
// because a chunk already generated would not be reloaded.
func (w *World) SetLoader(l Loader) { w.loader = l }

// Dimension is the world's vertical extent.
func (w *World) Dimension() Dimension { return w.dim }

// Registry is where the world's state handles come from.
func (w *World) Registry() StateRegistry { return w.reg }

// Adapter renders the world for the version its clients speak.
func (w *World) Adapter() Adapter { return w.adapter }

// Air is the handle the world treats as empty.
func (w *World) Air() State { return w.air }

// GenerationError is the first error a generator reported, if any. Generation
// happens under a read, which has nowhere to return an error to, so the world
// keeps the first one for whoever asks.
func (w *World) GenerationError() error {
	if p := w.genErr.Load(); p != nil {
		return *p
	}

	return nil
}

// Chunk returns the column at a position, generating it if this is the first
// time anyone asked.
func (w *World) Chunk(pos ChunkPos) *Chunk { return w.chunkSlot(pos).Load() }

// chunkSlot returns the atomic cell holding a column.
func (w *World) chunkSlot(pos ChunkPos) *atomic.Pointer[Chunk] {
	if slot, ok := w.chunks.Load(pos); ok {
		return slot.(*atomic.Pointer[Chunk])
	}

	// Generation runs outside the map, so two racers do the work twice rather
	// than one holding a lock across it. Only the first result is published,
	// and a generator is deterministic, so the loser's column is discarded
	// without anyone having seen it.
	slot := new(atomic.Pointer[Chunk])
	slot.Store(w.generate(pos))
	actual, _ := w.chunks.LoadOrStore(pos, slot)

	return actual.(*atomic.Pointer[Chunk])
}

func (w *World) generate(pos ChunkPos) *Chunk {
	if c := w.load(pos); c != nil {
		c.Gen = Generation(w.generation.Add(1))

		return c
	}

	b := NewBuilder(w.dim, pos, w.air)
	if w.generator != nil {
		if err := w.generator.Generate(pos, b); err != nil {
			wrapped := fmt.Errorf("world: generate chunk %v: %w", pos, err)
			w.genErr.CompareAndSwap(nil, &wrapped)
		}
	}
	c := b.Build()
	c.Gen = Generation(w.generation.Add(1))

	return c
}

// load asks the loader for a column. It returns nil when there is no loader or
// the loader has nothing, which is the signal to generate.
//
// A loader that *fails* is different: the column comes back empty and marked
// Unreadable, so nothing generates over data that is there but could not be
// read, and nothing saves the empty column back over it either. A world that
// quietly regenerates on a disk fault looks like a world that was deleted.
func (w *World) load(pos ChunkPos) *Chunk {
	if w.loader == nil {
		return nil
	}

	c, err := w.loader.LoadChunk(pos)
	if err != nil {
		wrapped := fmt.Errorf("world: load chunk %v: %w", pos, err)
		w.genErr.CompareAndSwap(nil, &wrapped)

		return &Chunk{Pos: pos, Sections: make([]*Section, w.dim.Sections()), Unreadable: true}
	}

	return c
}

// Regenerate produces the column the generator would make for a position,
// without publishing it and without disturbing the resident one.
//
// It exists so a saver can tell which blocks a player changed by diffing
// against pristine terrain. M11.3's chunk-granular save removes the need.
func (w *World) Regenerate(pos ChunkPos) *Chunk { return w.generate(pos) }

// Block returns the state at a position. A position outside the dimension
// reads as air.
func (w *World) Block(pos BlockPos) State {
	if !w.dim.Contains(pos.Y) {
		return w.air
	}

	return w.Chunk(pos.ChunkPos()).At(w.dim, pos.X&0xF, pos.Y, pos.Z&0xF, w.air)
}

// SetBlock writes a state and reports whether anything changed.
func (w *World) SetBlock(pos BlockPos, state State) bool {
	if !w.dim.Contains(pos.Y) {
		return false
	}

	slot := w.chunkSlot(pos.ChunkPos())
	for {
		old := slot.Load()
		next, changed := old.with(w.dim, pos, state, w.air, Generation(w.generation.Add(1)))
		if !changed {
			return false
		}
		if slot.CompareAndSwap(old, next) {
			return true
		}
		// Lost the race: loop, reload, and rebuild from the winner's chunk.
		// Rebuilding from `old` here is how writes get lost.
	}
}

// Snapshot is a consistent view of every resident chunk. Chunks are immutable,
// so this is a map copy and nothing a later write does can change it.
func (w *World) Snapshot() Snapshot {
	s := Snapshot{
		Dimension: w.dim,
		Chunks:    make(map[ChunkPos]*Chunk),
		Gen:       Generation(w.generation.Load()),
	}
	w.chunks.Range(func(key, value any) bool {
		s.Chunks[key.(ChunkPos)] = value.(*atomic.Pointer[Chunk]).Load()

		return true
	})

	return s
}

// ForEachChunk calls fn for each resident chunk.
func (w *World) ForEachChunk(fn func(pos ChunkPos, chunk *Chunk)) {
	w.chunks.Range(func(key, value any) bool {
		fn(key.(ChunkPos), value.(*atomic.Pointer[Chunk]).Load())

		return true
	})
}

// PreGenerateRadius generates all chunks within the given radius centered on (0,0).
func (w *World) PreGenerateRadius(radius int) int {
	count := 0
	for cx := -radius; cx <= radius; cx++ {
		for cz := -radius; cz <= radius; cz++ {
			w.Chunk(ChunkPos{X: cx, Z: cz})
			count++
		}
	}

	return count
}

// SpawnHeight returns the terrain height at spawn (0, 0) + 1 for the player to stand on.
func (w *World) SpawnHeight() int {
	if w.generator == nil {
		return w.dim.MinY
	}

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
