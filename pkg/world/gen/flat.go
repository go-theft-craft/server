package gen

import (
	"fmt"

	"github.com/go-theft-craft/server/pkg/world"
)

// FlatGenerator generates a superflat world from a list of layers, bottom up.
// The default list is the classic one: bedrock, two stone, dirt, grass.
type FlatGenerator struct {
	params FlatParams

	// bands is the layer list flattened to one state per y, resolved once.
	bands []world.State
	biome byte
	ready bool
}

// NewFlatGenerator creates a FlatGenerator with the default layers. Its blocks
// are resolved when the world binds it.
func NewFlatGenerator(_ int64) *FlatGenerator {
	return &FlatGenerator{params: FlatDefaults()}
}

// NewFlatGeneratorWith creates one from explicit parameters and resolves every
// block they name straight away.
func NewFlatGeneratorWith(_ int64, params FlatParams, reg world.StateRegistry) (*FlatGenerator, error) {
	g := &FlatGenerator{params: params}
	if reg == nil {
		return g, nil
	}
	if err := g.Bind(reg); err != nil {
		return nil, err
	}

	return g, nil
}

// Params is what this generator was built from, for the world's metadata.
func (g *FlatGenerator) Params() Params { return g.params }

// Bind resolves the layer list into one state per height.
func (g *FlatGenerator) Bind(reg world.StateRegistry) error {
	bands := make([]world.State, 0, g.params.Height()+1)
	for i, layer := range g.params.Layers {
		state, err := resolveBlock(reg, layer.Block)
		if err != nil {
			return fmt.Errorf("gen: flat layer %d: %w", i, err)
		}
		for range layer.Thickness {
			bands = append(bands, state)
		}
	}

	g.bands = bands
	g.biome = flatBiomeID(g.params.Biome)
	g.ready = true

	return nil
}

// flatBiomeID resolves a biome name to the ID this version numbers it with,
// falling back to plains for a name the generator does not know.
func flatBiomeID(name string) byte {
	for id, known := range biomeNames {
		if known == name {
			return id
		}
	}

	return biomePlains
}

// Generate fills one column with the configured layers.
func (g *FlatGenerator) Generate(_ world.ChunkPos, into *world.Builder) error {
	if !g.ready {
		return fmt.Errorf("gen: flat generator was not bound to a registry")
	}

	for x := range 16 {
		for z := range 16 {
			for y, state := range g.bands {
				if err := into.Set(x, y, z, state); err != nil {
					return err
				}
			}
			into.SetBiome(x, z, world.Biome(g.biome))
		}
	}

	return nil
}

// HeightAt is the top block, which is the sum of the layer thicknesses minus
// one because the first layer sits at y=0.
func (g *FlatGenerator) HeightAt(_, _ int) int { return g.params.Height() }
