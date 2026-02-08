package gen

// ChunkPos identifies a chunk by its X and Z coordinates.
type ChunkPos struct{ X, Z int }

// Section holds block data for a 16×16×16 vertical slice of a chunk.
// Index = y*256 + z*16 + x, value = blockID<<4 | metadata.
type Section struct {
	Blocks [4096]uint16
}

// ChunkData holds the generated terrain for one chunk column.
type ChunkData struct {
	Sections [16]*Section // nil = all-air
	Biomes   [256]byte    // index = z*16 + x → biome ID
}

// Generator produces chunk data deterministically from a seed.
type Generator interface {
	Generate(chunkX, chunkZ int) *ChunkData
	HeightAt(blockX, blockZ int) int
}

// SetBlock sets a block state at the given local coordinates within the chunk.
// x, z must be in [0,16), y must be in [0,256).
func (c *ChunkData) SetBlock(x, y, z int, state uint16) {
	sec := y >> 4
	if c.Sections[sec] == nil {
		if state == 0 {
			return
		}
		c.Sections[sec] = &Section{}
	}
	c.Sections[sec].Blocks[(y&0xF)*256+z*16+x] = state
}

// GetBlock returns the block state at the given local coordinates.
func (c *ChunkData) GetBlock(x, y, z int) uint16 {
	sec := y >> 4
	if c.Sections[sec] == nil {
		return 0
	}
	return c.Sections[sec].Blocks[(y&0xF)*256+z*16+x]
}

// SetBiome sets the biome ID at the given local x, z coordinates.
func (c *ChunkData) SetBiome(x, z int, biome byte) {
	c.Biomes[z*16+x] = biome
}
