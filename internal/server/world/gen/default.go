package gen

// DefaultGenerator produces vanilla-like terrain with biomes, caves, ores, and trees.
type DefaultGenerator struct {
	terrain  *NoiseGenerator
	detail   *NoiseGenerator
	biomeGen *BiomeGenerator
	caveGen  *CaveGenerator
	oreGen   *OreGenerator
	treeGen  *TreeGenerator
}

// NewDefaultGenerator creates a DefaultGenerator from a seed.
func NewDefaultGenerator(seed int64) *DefaultGenerator {
	return &DefaultGenerator{
		terrain:  NewNoiseGenerator(seed),
		detail:   NewNoiseGenerator(seed + 1),
		biomeGen: NewBiomeGenerator(seed),
		caveGen:  NewCaveGenerator(seed),
		oreGen:   NewOreGenerator(seed),
		treeGen:  NewTreeGenerator(seed),
	}
}

func (g *DefaultGenerator) Generate(chunkX, chunkZ int) *ChunkData {
	c := &ChunkData{}

	// Pass 1: compute heightmap and fill terrain + biomes.
	var heights [16][16]int
	for x := 0; x < 16; x++ {
		for z := 0; z < 16; z++ {
			bx := chunkX*16 + x
			bz := chunkZ*16 + z

			biome := g.biomeGen.BiomeAt(bx, bz)
			c.SetBiome(x, z, biome)

			height := g.terrainHeight(bx, bz, biome)
			heights[x][z] = height

			g.fillColumn(c, x, z, height, biome)
		}
	}

	// Pass 2: carve caves.
	g.caveGen.Carve(c, chunkX, chunkZ, &heights)

	// Pass 3: place ores.
	g.oreGen.Place(c, chunkX, chunkZ, &heights)

	// Pass 4: place trees and vegetation.
	g.treeGen.Decorate(c, chunkX, chunkZ, &heights)

	return c
}

func (g *DefaultGenerator) HeightAt(blockX, blockZ int) int {
	biome := g.biomeGen.BiomeAt(blockX, blockZ)
	return g.terrainHeight(blockX, blockZ, biome)
}

// terrainHeight computes the terrain height at a world block coordinate.
// Different biomes scale noise amplitude differently.
func (g *DefaultGenerator) terrainHeight(bx, bz int, biome byte) int {
	// Base terrain noise.
	nx := float64(bx) / 128.0
	nz := float64(bz) / 128.0
	base := g.terrain.OctaveNoise2D(nx, nz, 6, 0.5)

	// Detail noise for small-scale variation.
	dx := float64(bx) / 32.0
	dz := float64(bz) / 32.0
	detail := g.detail.OctaveNoise2D(dx, dz, 3, 0.5)

	amplitude, baseHeight := biomeTerrainParams(biome)

	height := baseHeight + base*amplitude + detail*4.0
	h := int(height)
	if h < 1 {
		h = 1
	}
	if h > 250 {
		h = 250
	}
	return h
}

// biomeTerrainParams returns (amplitude, baseHeight) for terrain noise scaling.
func biomeTerrainParams(biome byte) (amplitude, baseHeight float64) {
	switch biome {
	case biomeOcean:
		return 8.0, 40.0
	case biomePlains, biomeSavanna:
		return 12.0, float64(seaLevel)
	case biomeForest, biomeDarkForest:
		return 16.0, float64(seaLevel) + 2
	case biomeTaiga, biomeSnowyTaiga:
		return 18.0, float64(seaLevel) + 4
	case biomeDesert:
		return 10.0, float64(seaLevel) + 2
	case biomeJungle:
		return 18.0, float64(seaLevel) + 4
	case biomeMountains:
		return 40.0, float64(seaLevel) + 10
	case biomeBeach:
		return 3.0, float64(seaLevel)
	case biomeTundra:
		return 10.0, float64(seaLevel)
	default:
		return 14.0, float64(seaLevel)
	}
}

// fillColumn fills a single block column with terrain blocks.
func (g *DefaultGenerator) fillColumn(c *ChunkData, x, z, height int, biome byte) {
	// Bedrock layers: y=0 always, y=1..3 randomized.
	c.SetBlock(x, 0, z, blockBedrock<<4)
	for y := 1; y <= 3; y++ {
		bx := x + y*7 // cheap variation
		if g.terrain.Noise2D(float64(bx)*0.5, float64(z)*0.5) > 0.0 {
			c.SetBlock(x, y, z, blockBedrock<<4)
		} else {
			c.SetBlock(x, y, z, blockStone<<4)
		}
	}

	// Stone fill from y=4 up to surface-4 (or surface if below sea level).
	surfaceDepth := surfaceLayerDepth(biome)
	stoneTop := height - surfaceDepth
	if stoneTop < 4 {
		stoneTop = 4
	}
	for y := 4; y <= stoneTop && y <= height; y++ {
		c.SetBlock(x, y, z, blockStone<<4)
	}

	// Surface layers.
	applySurface(c, x, z, height, biome)

	// Water fill from surface+1 to sea level where terrain is below sea level.
	if height < seaLevel {
		for y := height + 1; y <= seaLevel; y++ {
			c.SetBlock(x, y, z, blockWater<<4)
		}
	}
}

// surfaceLayerDepth returns how many blocks of surface material go below the top block.
func surfaceLayerDepth(biome byte) int {
	switch biome {
	case biomeDesert:
		return 5 // deep sand
	default:
		return 4
	}
}
