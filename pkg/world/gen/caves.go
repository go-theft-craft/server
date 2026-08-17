package gen

// CaveGenerator carves caves using 3D simplex noise.
type CaveGenerator struct {
	noise1 *NoiseGenerator
	noise2 *NoiseGenerator
	params CaveParams
}

// NewCaveGenerator creates a CaveGenerator from a seed and its parameters.
func NewCaveGenerator(seed int64, params CaveParams) *CaveGenerator {
	return &CaveGenerator{
		noise1: NewNoiseGenerator(seed + 300),
		noise2: NewNoiseGenerator(seed + 400),
		params: params,
	}
}

// Carve removes blocks to form caves in the chunk.
func (cg *CaveGenerator) Carve(c setter, chunkX, chunkZ int, heights *[16][16]int) {
	p := cg.params

	for x := range 16 {
		for z := range 16 {
			bx := float64(chunkX*16 + x)
			bz := float64(chunkZ*16 + z)
			maxY := heights[x][z]
			if maxY < p.MinY+1 {
				continue
			}

			// Neither bedrock nor the surface is carved: MinY is the floor and
			// SurfaceMargin is how far below the top the carving stops, so a
			// cave never opens a hole in the ground.
			for y := p.MinY; y < maxY-p.SurfaceMargin; y++ {
				by := float64(y)

				// Two noise fields combined for more interesting cave shapes.
				n1 := cg.noise1.Noise3D(bx/p.ScaleAXZ, by/p.ScaleAY, bz/p.ScaleAXZ)
				n2 := cg.noise2.Noise3D(bx/p.ScaleBXZ, by/p.ScaleBY, bz/p.ScaleBXZ)

				density := (n1 + n2) / 2.0
				if density > p.Threshold {
					if y < p.LavaLevel {
						c.set(x, y, z, c.p.lava)
					} else {
						c.set(x, y, z, c.p.air)
					}
				}
			}
		}
	}
}
