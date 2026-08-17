package gen

import (
	"github.com/go-theft-craft/server/pkg/world"
)

// FlatGenerator generates a classic superflat world:
// bedrock at y=0, stone y=1..2, dirt y=3, grass y=4.
type FlatGenerator struct {
	bound
}

// NewFlatGenerator creates a FlatGenerator.
func NewFlatGenerator(_ int64) *FlatGenerator {
	return &FlatGenerator{}
}

// Generate fills one column with the four flat layers.
func (g *FlatGenerator) Generate(_ world.ChunkPos, into *world.Builder) error {
	s := setter{b: into, p: g.p}

	for x := range 16 {
		for z := range 16 {
			s.set(x, 0, z, s.p.bedrock)
			s.set(x, 1, z, s.p.stone)
			s.set(x, 2, z, s.p.stone)
			s.set(x, 3, z, s.p.dirt)
			s.set(x, 4, z, s.p.grass)
			into.SetBiome(x, z, world.Biome(biomePlains))
		}
	}

	return nil
}

// HeightAt is the top solid block, which is the grass at y=4.
func (g *FlatGenerator) HeightAt(_, _ int) int { return 4 }
