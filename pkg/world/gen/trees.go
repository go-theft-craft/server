package gen

import (
	"github.com/go-theft-craft/server/pkg/world"
)

// TreeGenerator places trees and vegetation per biome.
type TreeGenerator struct {
	seed     int64
	params   TreeParams
	seaLevel int
}

// NewTreeGenerator creates a TreeGenerator from a seed and its parameters.
func NewTreeGenerator(seed int64, params TreeParams, seaLevel int) *TreeGenerator {
	return &TreeGenerator{seed: seed, params: params, seaLevel: seaLevel}
}

// Decorate places trees and vegetation in the chunk.
func (tg *TreeGenerator) Decorate(c setter, chunkX, chunkZ int, heights *[16][16]int) {
	rng := newChunkRNG(tg.seed, chunkX, chunkZ, 600)

	// Determine biome from center of chunk for tree density.
	centerBiome := byte(c.b.Biome(8, 8))

	// Place trees.
	treeCount := tg.treesForBiome(centerBiome)
	for range treeCount {
		x := rng.nextN(16)
		z := rng.nextN(16)
		y := heights[x][z]

		if y <= tg.seaLevel || y >= 250 {
			continue
		}

		// Check that the top block is grass.
		if c.get(x, y, z) != c.p.grass {
			continue
		}

		localBiome := byte(c.b.Biome(x, z))
		tg.placeTree(c, x, y+1, z, localBiome, rng, heights)
	}

	// Place vegetation (tall grass, flowers, cacti, dead bushes).
	tg.placeVegetation(c, chunkX, chunkZ, heights, rng)
}

// treesForBiome is how many trees a biome gets per chunk, from the parameters
// rather than from a switch.
func (tg *TreeGenerator) treesForBiome(biome byte) int {
	if count, ok := tg.params.Density[biomeName(biome)]; ok {
		return count
	}

	return tg.params.DefaultDensity
}

// placeTree places a single tree at the given position. Constrained to chunk bounds.
func (tg *TreeGenerator) placeTree(c setter, x, baseY, z int, biome byte, rng *chunkRNG, heights *[16][16]int) {
	switch biome {
	case biomeTaiga, biomeSnowyTaiga:
		tg.placeSpruce(c, x, baseY, z, rng)
	case biomeForest, biomeDarkForest:
		if rng.nextN(3) == 0 {
			tg.placeBirch(c, x, baseY, z, rng)
		} else {
			tg.placeOak(c, x, baseY, z, rng)
		}
	default:
		tg.placeOak(c, x, baseY, z, rng)
	}
}

// placeOak places a standard oak tree (trunk + leaf canopy).
func (tg *TreeGenerator) placeOak(c setter, x, baseY, z int, rng *chunkRNG) {
	trunkHeight := 4 + rng.nextN(3) // 4-6

	// Check bounds: trunk must fit in chunk and in world height.
	if baseY+trunkHeight+2 > 255 {
		return
	}

	// Place trunk.
	for y := baseY; y < baseY+trunkHeight; y++ {
		setIfInBounds(c, x, y, z, c.p.logOak)
	}

	// Place leaves.
	leafBase := baseY + trunkHeight - 2
	for dy := 0; dy < 4; dy++ {
		y := leafBase + dy
		radius := 2
		if dy >= 2 {
			radius = 1
		}
		for dx := -radius; dx <= radius; dx++ {
			for dz := -radius; dz <= radius; dz++ {
				lx, lz := x+dx, z+dz
				if lx < 0 || lx >= 16 || lz < 0 || lz >= 16 {
					continue
				}
				// Don't replace trunk.
				if dx == 0 && dz == 0 && dy < trunkHeight-(leafBase-baseY) {
					continue
				}
				// Skip corners for round shape on wider layers.
				if radius == 2 && abs(dx) == 2 && abs(dz) == 2 && rng.nextN(2) == 0 {
					continue
				}
				if c.get(lx, y, lz) == c.p.air {
					c.set(lx, y, lz, c.p.leavesOak)
				}
			}
		}
	}
}

// placeBirch places a birch tree (similar to oak but with birch log/leaves).
func (tg *TreeGenerator) placeBirch(c setter, x, baseY, z int, rng *chunkRNG) {
	trunkHeight := 5 + rng.nextN(2) // 5-6

	if baseY+trunkHeight+2 > 255 {
		return
	}

	for y := baseY; y < baseY+trunkHeight; y++ {
		setIfInBounds(c, x, y, z, c.p.logBirch)
	}

	leafBase := baseY + trunkHeight - 2
	for dy := 0; dy < 4; dy++ {
		y := leafBase + dy
		radius := 2
		if dy >= 2 {
			radius = 1
		}
		for dx := -radius; dx <= radius; dx++ {
			for dz := -radius; dz <= radius; dz++ {
				lx, lz := x+dx, z+dz
				if lx < 0 || lx >= 16 || lz < 0 || lz >= 16 {
					continue
				}
				if dx == 0 && dz == 0 && dy < trunkHeight-(leafBase-baseY) {
					continue
				}
				if radius == 2 && abs(dx) == 2 && abs(dz) == 2 && rng.nextN(2) == 0 {
					continue
				}
				if c.get(lx, y, lz) == c.p.air {
					c.set(lx, y, lz, c.p.leavesBirch)
				}
			}
		}
	}
}

// placeSpruce places a spruce/taiga tree (conical shape).
func (tg *TreeGenerator) placeSpruce(c setter, x, baseY, z int, rng *chunkRNG) {
	trunkHeight := 6 + rng.nextN(4) // 6-9

	if baseY+trunkHeight+1 > 255 {
		return
	}

	// Trunk.
	for y := baseY; y < baseY+trunkHeight; y++ {
		setIfInBounds(c, x, y, z, c.p.logSpruce)
	}

	// Conical leaves: widest at bottom, narrowing to top.
	for dy := 1; dy <= trunkHeight; dy++ {
		y := baseY + dy
		// Radius narrows as we go up.
		radius := (trunkHeight - dy) / 2
		if radius > 3 {
			radius = 3
		}
		if radius <= 0 && dy < trunkHeight {
			continue
		}
		// Only place every other row for the wider sections.
		if radius >= 2 && dy%2 == 0 {
			continue
		}
		for dx := -radius; dx <= radius; dx++ {
			for dz := -radius; dz <= radius; dz++ {
				lx, lz := x+dx, z+dz
				if lx < 0 || lx >= 16 || lz < 0 || lz >= 16 {
					continue
				}
				if dx == 0 && dz == 0 {
					continue
				}
				if c.get(lx, y, lz) == c.p.air {
					c.set(lx, y, lz, c.p.leavesSpruce)
				}
			}
		}
	}
	// Top leaf.
	topY := baseY + trunkHeight
	if topY < 256 {
		c.set(x, topY, z, c.p.leavesSpruce)
	}
}

// placeVegetation scatters grass, flowers, cacti, and dead bushes.
func (tg *TreeGenerator) placeVegetation(c setter, _, _ int, heights *[16][16]int, rng *chunkRNG) {
	for range tg.params.VegetationAttempts {
		x := rng.nextN(16)
		z := rng.nextN(16)
		y := heights[x][z]
		if y <= tg.seaLevel || y >= 255 {
			continue
		}
		biome := byte(c.b.Biome(x, z))
		topBlock := c.get(x, y, z)

		switch biome {
		case biomeDesert:
			if topBlock != c.p.sand {
				continue
			}
			if rng.nextN(8) == 0 {
				// Cactus (1-3 blocks tall).
				h := 1 + rng.nextN(3)
				for dy := 1; dy <= h && y+dy < 256; dy++ {
					c.set(x, y+dy, z, c.p.cactus)
				}
			} else if rng.nextN(4) == 0 {
				c.set(x, y+1, z, c.p.deadBush)
			}

		case biomePlains, biomeForest, biomeDarkForest, biomeSavanna, biomeJungle:
			if topBlock != c.p.grass {
				continue
			}
			if rng.nextN(3) == 0 {
				// Tall grass (metadata 1 = tall grass, not dead shrub).
				c.set(x, y+1, z, c.p.tallGrass)
			} else if rng.nextN(8) == 0 {
				// Flower.
				c.set(x, y+1, z, c.p.flower)
			}

		case biomeTaiga, biomeSnowyTaiga, biomeTundra:
			if topBlock != c.p.grass {
				continue
			}
			if rng.nextN(6) == 0 {
				c.set(x, y+1, z, c.p.tallGrass)
			}
		}
	}
}

func setIfInBounds(c setter, x, y, z int, state world.State) {
	if x >= 0 && x < 16 && z >= 0 && z < 16 && y >= 0 && y < 256 {
		c.set(x, y, z, state)
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
