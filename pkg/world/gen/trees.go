package gen

// TreeGenerator places trees and vegetation per biome.
type TreeGenerator struct {
	seed int64
}

// NewTreeGenerator creates a TreeGenerator from a seed.
func NewTreeGenerator(seed int64) *TreeGenerator {
	return &TreeGenerator{seed: seed}
}

// Decorate places trees and vegetation in the chunk.
func (tg *TreeGenerator) Decorate(c *ChunkData, chunkX, chunkZ int, heights *[16][16]int) {
	rng := newChunkRNG(tg.seed, chunkX, chunkZ, 600)

	// Determine biome from center of chunk for tree density.
	centerBiome := c.Biomes[8*16+8]

	// Place trees.
	treeCount := treesForBiome(centerBiome)
	for range treeCount {
		x := rng.nextN(16)
		z := rng.nextN(16)
		y := heights[x][z]

		if y <= seaLevel || y >= 250 {
			continue
		}

		// Check that the top block is grass.
		if c.GetBlock(x, y, z) != blockGrass<<4 {
			continue
		}

		localBiome := c.Biomes[z*16+x]
		tg.placeTree(c, x, y+1, z, localBiome, rng, heights)
	}

	// Place vegetation (tall grass, flowers, cacti, dead bushes).
	tg.placeVegetation(c, chunkX, chunkZ, heights, rng)
}

func treesForBiome(biome byte) int {
	switch biome {
	case biomeDesert:
		return 0
	case biomeOcean, biomeBeach:
		return 0
	case biomePlains, biomeSavanna:
		return 1
	case biomeTundra, biomeSnowyTaiga:
		return 4
	case biomeTaiga:
		return 6
	case biomeForest:
		return 8
	case biomeDarkForest:
		return 10
	case biomeJungle:
		return 12
	default:
		return 2
	}
}

// placeTree places a single tree at the given position. Constrained to chunk bounds.
func (tg *TreeGenerator) placeTree(c *ChunkData, x, baseY, z int, biome byte, rng *chunkRNG, heights *[16][16]int) {
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
func (tg *TreeGenerator) placeOak(c *ChunkData, x, baseY, z int, rng *chunkRNG) {
	trunkHeight := 4 + rng.nextN(3) // 4-6

	// Check bounds: trunk must fit in chunk and in world height.
	if baseY+trunkHeight+2 > 255 {
		return
	}

	// Place trunk.
	for y := baseY; y < baseY+trunkHeight; y++ {
		setIfInBounds(c, x, y, z, blockLog<<4|logOak)
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
				if c.GetBlock(lx, y, lz) == 0 {
					c.SetBlock(lx, y, lz, blockLeaves<<4|leavesOak)
				}
			}
		}
	}
}

// placeBirch places a birch tree (similar to oak but with birch log/leaves).
func (tg *TreeGenerator) placeBirch(c *ChunkData, x, baseY, z int, rng *chunkRNG) {
	trunkHeight := 5 + rng.nextN(2) // 5-6

	if baseY+trunkHeight+2 > 255 {
		return
	}

	for y := baseY; y < baseY+trunkHeight; y++ {
		setIfInBounds(c, x, y, z, blockLog<<4|logBirch)
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
				if c.GetBlock(lx, y, lz) == 0 {
					c.SetBlock(lx, y, lz, blockLeaves<<4|leavesBirch)
				}
			}
		}
	}
}

// placeSpruce places a spruce/taiga tree (conical shape).
func (tg *TreeGenerator) placeSpruce(c *ChunkData, x, baseY, z int, rng *chunkRNG) {
	trunkHeight := 6 + rng.nextN(4) // 6-9

	if baseY+trunkHeight+1 > 255 {
		return
	}

	// Trunk.
	for y := baseY; y < baseY+trunkHeight; y++ {
		setIfInBounds(c, x, y, z, blockLog<<4|logSpruce)
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
				if c.GetBlock(lx, y, lz) == 0 {
					c.SetBlock(lx, y, lz, blockLeaves<<4|leavesSpruce)
				}
			}
		}
	}
	// Top leaf.
	topY := baseY + trunkHeight
	if topY < 256 {
		c.SetBlock(x, topY, z, blockLeaves<<4|leavesSpruce)
	}
}

// placeVegetation scatters grass, flowers, cacti, and dead bushes.
func (tg *TreeGenerator) placeVegetation(c *ChunkData, _, _ int, heights *[16][16]int, rng *chunkRNG) {
	for range 20 {
		x := rng.nextN(16)
		z := rng.nextN(16)
		y := heights[x][z]
		if y <= seaLevel || y >= 255 {
			continue
		}
		biome := c.Biomes[z*16+x]
		topBlock := c.GetBlock(x, y, z)

		switch biome {
		case biomeDesert:
			if topBlock != blockSand<<4 {
				continue
			}
			if rng.nextN(8) == 0 {
				// Cactus (1-3 blocks tall).
				h := 1 + rng.nextN(3)
				for dy := 1; dy <= h && y+dy < 256; dy++ {
					c.SetBlock(x, y+dy, z, blockCactus<<4)
				}
			} else if rng.nextN(4) == 0 {
				c.SetBlock(x, y+1, z, blockDeadBush<<4)
			}

		case biomePlains, biomeForest, biomeDarkForest, biomeSavanna, biomeJungle:
			if topBlock != blockGrass<<4 {
				continue
			}
			if rng.nextN(3) == 0 {
				// Tall grass (metadata 1 = tall grass, not dead shrub).
				c.SetBlock(x, y+1, z, blockTallGrass<<4|1)
			} else if rng.nextN(8) == 0 {
				// Flower.
				c.SetBlock(x, y+1, z, blockFlower<<4)
			}

		case biomeTaiga, biomeSnowyTaiga, biomeTundra:
			if topBlock != blockGrass<<4 {
				continue
			}
			if rng.nextN(6) == 0 {
				c.SetBlock(x, y+1, z, blockTallGrass<<4|1)
			}
		}
	}
}

func setIfInBounds(c *ChunkData, x, y, z int, state uint16) {
	if x >= 0 && x < 16 && z >= 0 && z < 16 && y >= 0 && y < 256 {
		c.SetBlock(x, y, z, state)
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
