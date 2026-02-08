package gen

// CaveGenerator carves caves using 3D simplex noise.
type CaveGenerator struct {
	noise1 *NoiseGenerator
	noise2 *NoiseGenerator
}

// NewCaveGenerator creates a CaveGenerator from a seed.
func NewCaveGenerator(seed int64) *CaveGenerator {
	return &CaveGenerator{
		noise1: NewNoiseGenerator(seed + 300),
		noise2: NewNoiseGenerator(seed + 400),
	}
}

// Carve removes blocks to form caves in the chunk.
func (cg *CaveGenerator) Carve(c *ChunkData, chunkX, chunkZ int, heights *[16][16]int) {
	const (
		threshold = 0.55
		lavaLevel = 10
	)

	for x := 0; x < 16; x++ {
		for z := 0; z < 16; z++ {
			bx := float64(chunkX*16 + x)
			bz := float64(chunkZ*16 + z)
			maxY := heights[x][z]
			if maxY < 5 {
				continue
			}

			for y := 4; y < maxY-4; y++ { // Don't carve bedrock or surface
				by := float64(y)

				// Two noise fields combined for more interesting cave shapes.
				n1 := cg.noise1.Noise3D(bx/32.0, by/24.0, bz/32.0)
				n2 := cg.noise2.Noise3D(bx/48.0, by/32.0, bz/48.0)

				density := (n1 + n2) / 2.0
				if density > threshold {
					if y < lavaLevel {
						c.SetBlock(x, y, z, blockLava<<4)
					} else {
						c.SetBlock(x, y, z, blockAir<<4)
					}
				}
			}
		}
	}
}
