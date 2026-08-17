package gen

import (
	"github.com/go-theft-craft/server/pkg/world"
)

// Generator produces chunk data deterministically from a seed.
//
// It writes into a world.Builder rather than returning a structure of its own,
// so a generated column is built in place once and published immutable.
type Generator interface {
	Generate(pos world.ChunkPos, into *world.Builder) error
	HeightAt(blockX, blockZ int) int
}

// bound is the palette bookkeeping every generator shares. A generator is
// bound once, by the world, before it writes anything.
type bound struct {
	p     palette
	ready bool
}

// Bind resolves the generator's palette. It satisfies world.Binder.
func (b *bound) Bind(reg world.StateRegistry) error {
	b.p = newPalette(reg)
	b.ready = true

	return nil
}

// setter is what the passes write through, so that a pass takes the builder
// and the palette without repeating both in every signature.
type setter struct {
	b *world.Builder
	p palette
}

// set writes a block at chunk-local x, z and world y, ignoring a position the
// dimension does not contain. The generators are written against 0..255 and
// clip at the top rather than reporting, which is what they did before.
func (s setter) set(x, y, z int, state world.State) {
	_ = s.b.Set(x, y, z, state)
}

// get reads back what the passes have written so far.
func (s setter) get(x, y, z int) world.State { return s.b.Get(x, y, z) }
