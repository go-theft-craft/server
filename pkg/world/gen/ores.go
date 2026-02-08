package gen

// OreGenerator places ore veins in stone using seeded per-chunk RNG.
type OreGenerator struct {
	seed int64
}

// NewOreGenerator creates an OreGenerator from a seed.
func NewOreGenerator(seed int64) *OreGenerator {
	return &OreGenerator{seed: seed}
}

type oreConfig struct {
	block    uint16 // blockID
	minY     int
	maxY     int
	veinSize int // max blocks per vein
	attempts int // veins per chunk
}

var ores = []oreConfig{
	{blockCoalOre, 0, 128, 12, 20},
	{blockIronOre, 0, 64, 8, 20},
	{blockGoldOre, 0, 32, 8, 2},
	{blockDiamondOre, 0, 16, 6, 1},
	{blockRedstoneOre, 0, 16, 6, 8},
	{blockLapisOre, 0, 32, 6, 1},
}

// Place scatters ore veins within the chunk.
func (og *OreGenerator) Place(c *ChunkData, chunkX, chunkZ int, heights *[16][16]int) {
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

			og.placeVein(c, x, y, z, ore.block, ore.veinSize, heights, rng)
		}
	}
}

func (og *OreGenerator) placeVein(c *ChunkData, cx, cy, cz int, blockID uint16, size int, heights *[16][16]int, rng *chunkRNG) {
	for range size {
		if cx >= 0 && cx < 16 && cz >= 0 && cz < 16 && cy >= 1 && cy < heights[cx][cz] {
			// Only replace stone.
			if c.GetBlock(cx, cy, cz) == blockStone<<4 {
				c.SetBlock(cx, cy, cz, blockID<<4)
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
