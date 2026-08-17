package world

// ChunkPos identifies a chunk column by its X and Z coordinates.
type ChunkPos struct{ X, Z int }

// ChunkPos returns the column a block position falls in.
func (p BlockPos) ChunkPos() ChunkPos { return ChunkPos{X: p.X >> 4, Z: p.Z >> 4} }

// Biome is a biome ID as the version's data set numbers it.
type Biome byte

// Dimension is a world's vertical extent and its name. Java 1.8's overworld is
// 0 to 255; Java 26.1's is -64 to 319.
type Dimension struct {
	Name   string
	MinY   int
	Height int
}

// Sections is how many 16-block-tall sections a column holds.
func (d Dimension) Sections() int { return d.Height / 16 }

// SectionIndex is the section a Y coordinate falls in.
func (d Dimension) SectionIndex(y int) int { return (y - d.MinY) >> 4 }

// Contains reports whether a Y coordinate is inside the dimension.
func (d Dimension) Contains(y int) bool { return y >= d.MinY && y < d.MinY+d.Height }

// Overworld18 is Java 1.8's overworld.
func Overworld18() Dimension {
	return Dimension{Name: "minecraft:overworld", MinY: 0, Height: 256}
}
