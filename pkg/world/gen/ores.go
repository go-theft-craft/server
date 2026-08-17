package gen

import (
	"github.com/go-theft-craft/server/pkg/world"
)

// OreGenerator places ore veins in stone using seeded per-chunk RNG.
type OreGenerator struct {
	seed int64
}

// NewOreGenerator creates an OreGenerator from a seed.
func NewOreGenerator(seed int64) *OreGenerator {
	return &OreGenerator{seed: seed}
}

type oreConfig struct {
	// block picks the ore out of the palette, since a palette is only known
	// once the generator is bound and this table is package state.
	block    func(palette) world.State
	minY     int
	maxY     int
	veinSize int // max blocks per vein
	attempts int // veins per chunk
}

var ores = []oreConfig{
	{func(p palette) world.State { return p.coalOre }, 0, 128, 12, 20},
	{func(p palette) world.State { return p.ironOre }, 0, 64, 8, 20},
	{func(p palette) world.State { return p.goldOre }, 0, 32, 8, 2},
	{func(p palette) world.State { return p.diamondOre }, 0, 16, 6, 1},
	{func(p palette) world.State { return p.redstoneOre }, 0, 16, 6, 8},
	{func(p palette) world.State { return p.lapisOre }, 0, 32, 6, 1},
}

// Place scatters ore veins within the chunk.
func (og *OreGenerator) Place(c setter, chunkX, chunkZ int, heights *[16][16]int) {
	// Seed RNG deterministically per chunk.
	rng := newChunkRNG(og.seed, chunkX, chunkZ, 500)

	for _, ore := range ores {
		for range ore.attempts {
			x := rng.nextN(16)
			y := ore.minY + rng.nextN(ore.maxY-ore.minY)
			z := rng.nextN(16)

			if y >= heights[x][z] {
				continue
			}

			og.placeVein(c, x, y, z, ore.block(c.p), ore.veinSize, heights, rng)
		}
	}
}

func (og *OreGenerator) placeVein(c setter, cx, cy, cz int, ore world.State, size int, heights *[16][16]int, rng *chunkRNG) {
	for range size {
		if cx >= 0 && cx < 16 && cz >= 0 && cz < 16 && cy >= 1 && cy < heights[cx][cz] {
			// Only replace stone.
			if c.get(cx, cy, cz) == c.p.stone {
				c.set(cx, cy, cz, ore)
			}
		}

		// Random walk.
		switch rng.nextN(6) {
		case 0:
			cx++
		case 1:
			cx--
		case 2:
			cy++
		case 3:
			cy--
		case 4:
			cz++
		case 5:
			cz--
		}
	}
}

// chunkRNG is a simple deterministic RNG for per-chunk generation.
type chunkRNG struct {
	state int64
}

func newChunkRNG(seed int64, cx, cz int, salt int64) *chunkRNG {
	s := seed ^ (int64(cx)*341873128712 + int64(cz)*132897987541 + salt)
	return &chunkRNG{state: s}
}

func (r *chunkRNG) next() int64 {
	r.state = r.state*6364136223846793005 + 1442695040888963407
	return r.state
}

func (r *chunkRNG) nextN(n int) int {
	v := int(r.next()>>33) % n
	if v < 0 {
		v = -v
	}
	return v
}
