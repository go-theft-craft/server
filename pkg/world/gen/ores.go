package gen

import (
	"fmt"

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

// resolvedOre is one OreParams entry with its block turned into a handle.
type resolvedOre struct {
	block  world.State
	params OreParams
}

func resolveOres(reg world.StateRegistry, params []OreParams) ([]resolvedOre, error) {
	out := make([]resolvedOre, 0, len(params))
	for i, p := range params {
		if p.MaxY <= p.MinY {
			return nil, fmt.Errorf("gen: ore %d (%s) has max_y %d at or below min_y %d",
				i, p.Block, p.MaxY, p.MinY)
		}
		state, err := resolveBlock(reg, p.Block)
		if err != nil {
			return nil, fmt.Errorf("gen: ore %d: %w", i, err)
		}
		out = append(out, resolvedOre{block: state, params: p})
	}

	return out, nil
}

// Place scatters ore veins within the chunk.
func (og *OreGenerator) Place(c setter, chunkX, chunkZ int, heights *[16][16]int, ores []resolvedOre) {
	// Seed RNG deterministically per chunk.
	rng := newChunkRNG(og.seed, chunkX, chunkZ, 500)

	for _, ore := range ores {
		for range ore.params.Attempts {
			x := rng.nextN(16)
			y := ore.params.MinY + rng.nextN(ore.params.MaxY-ore.params.MinY)
			z := rng.nextN(16)

			if y >= heights[x][z] {
				continue
			}

			og.placeVein(c, x, y, z, ore.block, ore.params.VeinSize, heights, rng)
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
