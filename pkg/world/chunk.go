package world

// Chunk is one column of the world. It is immutable: a block write produces a
// new Chunk that shares every section it did not touch, and the world publishes
// it by swapping a pointer.
type Chunk struct {
	Pos      ChunkPos
	Sections []*Section // len == dim.Sections(); a nil entry is all air
	Biomes   [256]Biome
	Gen      Generation

	// Chests is the container contents inside this column, keyed by the block
	// they live in. They belong to the chunk so that a chunk save carries
	// them, which is what makes them incremental with everything else and is
	// where the vanilla format keeps them.
	Chests map[BlockPos]ChestContents

	// Unreadable marks a column the store failed to read. It is empty, and it
	// must never be written back: doing so would replace data that is there
	// with the nothing that could not be loaded.
	Unreadable bool
}

// withChest returns a chunk whose container at pos holds contents, or holds
// nothing when contents is empty. It reports false when nothing changed.
func (c *Chunk) withChest(pos BlockPos, contents ChestContents, empty bool, gen Generation) (*Chunk, bool) {
	existing, had := c.Chests[pos]
	switch {
	case empty && !had:
		return c, false
	case !empty && had && existing == contents:
		return c, false
	}

	next := c.clone(gen)
	if empty {
		delete(next.Chests, pos)
	} else {
		next.Chests[pos] = contents
	}

	return next, true
}

// clone copies a chunk's mutable parts, sharing every section.
func (c *Chunk) clone(gen Generation) *Chunk {
	next := &Chunk{
		Pos:        c.Pos,
		Sections:   make([]*Section, len(c.Sections)),
		Biomes:     c.Biomes,
		Gen:        gen,
		Chests:     make(map[BlockPos]ChestContents, len(c.Chests)),
		Unreadable: c.Unreadable,
	}
	copy(next.Sections, c.Sections)
	for k, v := range c.Chests {
		next.Chests[k] = v
	}

	return next
}

// At returns the state at chunk-local x, z and world y. A y outside the
// dimension, or a column the chunk left empty, reads as the given air state.
func (c *Chunk) At(dim Dimension, x, y, z int, air State) State {
	if c == nil || !dim.Contains(y) {
		return air
	}
	sec := c.Sections[dim.SectionIndex(y)]
	if sec == nil {
		return air
	}

	return sec.At(SectionBlockIndex(x, y&0xF, z))
}

// with returns a chunk with one block changed, sharing every section it did not
// touch. It reports false when the write changes nothing, which is what keeps a
// no-op place from allocating a column and invalidating an encode cache entry.
func (c *Chunk) with(dim Dimension, pos BlockPos, state, air State, gen Generation) (*Chunk, bool) {
	sectionIdx := dim.SectionIndex(pos.Y)
	blockIdx := SectionBlockIndex(pos.X&0xF, pos.Y&0xF, pos.Z&0xF)

	sec := c.Sections[sectionIdx]
	switch {
	case sec == nil && state == air:
		return c, false
	case sec != nil && sec.At(blockIdx) == state:
		return c, false
	}

	next := c.clone(gen)
	if sec == nil {
		sec = newAirSection(air)
	}
	next.Sections[sectionIdx] = sec.With(blockIdx, state)

	return next, true
}

// newAirSection allocates a section filled with the dimension's empty block.
func newAirSection(air State) *Section {
	s := new(Section)
	if air != 0 {
		for i := range s.states {
			s.states[i] = air
		}
	}

	return s
}
