package world

import "fmt"

// Builder is the mutable staging buffer generation writes into. It is the one
// place a section is written in place, and that is correct only because nothing
// else can see the chunk yet.
type Builder struct {
	dim      Dimension
	pos      ChunkPos
	air      State
	sections []*Section
	biomes   [256]Biome
}

// NewBuilder returns a Builder for one chunk column of the dimension.
func NewBuilder(dim Dimension, pos ChunkPos, air State) *Builder {
	return &Builder{
		dim:      dim,
		pos:      pos,
		air:      air,
		sections: make([]*Section, dim.Sections()),
	}
}

// Dimension is the extent the builder was made for.
func (b *Builder) Dimension() Dimension { return b.dim }

// Pos is the column the builder is filling.
func (b *Builder) Pos() ChunkPos { return b.pos }

// Air is the state the builder treats as empty.
func (b *Builder) Air() State { return b.air }

// Set writes a block at chunk-local x, z and world y.
func (b *Builder) Set(x, y, z int, state State) error {
	if b.sections == nil {
		return fmt.Errorf("world: builder for %v was already built", b.pos)
	}
	if x < 0 || x >= 16 || z < 0 || z >= 16 {
		return fmt.Errorf("world: local coordinates (%d,%d) outside the chunk", x, z)
	}
	if !b.dim.Contains(y) {
		return fmt.Errorf("world: y=%d outside %s (%d to %d)",
			y, b.dim.Name, b.dim.MinY, b.dim.MinY+b.dim.Height-1)
	}

	index := b.dim.SectionIndex(y)
	sec := b.sections[index]
	if sec == nil {
		if state == b.air {
			return nil
		}
		sec = new(Section)
		if b.air != 0 {
			for i := range sec.states {
				sec.states[i] = b.air
			}
		}
		b.sections[index] = sec
	}
	sec.states[SectionBlockIndex(x, y&0xF, z)] = state

	return nil
}

// Get reads back what the builder holds so far.
func (b *Builder) Get(x, y, z int) State {
	if b.sections == nil || !b.dim.Contains(y) {
		return b.air
	}
	sec := b.sections[b.dim.SectionIndex(y)]
	if sec == nil {
		return b.air
	}

	return sec.states[SectionBlockIndex(x, y&0xF, z)]
}

// Biome reads back the biome at chunk-local x, z.
func (b *Builder) Biome(x, z int) Biome {
	if x < 0 || x >= 16 || z < 0 || z >= 16 {
		return 0
	}

	return b.biomes[z*16+x]
}

// SetBiome sets the biome at chunk-local x, z.
func (b *Builder) SetBiome(x, z int, biome Biome) {
	if x < 0 || x >= 16 || z < 0 || z >= 16 {
		return
	}
	b.biomes[z*16+x] = biome
}

// Build hands the builder's sections to a chunk and nils its own, so a builder
// reused after Build fails rather than mutating a published chunk.
func (b *Builder) Build() *Chunk {
	c := &Chunk{Pos: b.pos, Sections: b.sections, Biomes: b.biomes}
	b.sections = nil

	return c
}
