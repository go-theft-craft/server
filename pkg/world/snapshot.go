package world

// Generation counts block writes. Every chunk a write publishes carries the
// generation it was stamped with, so a saver or an observer can tell whether
// what it holds is still current without comparing block data.
type Generation uint64

// Snapshot is a consistent view of the resident world. Taking one is a map
// copy of chunk pointers: the chunks themselves are immutable, so nothing a
// later write does can change what a snapshot already holds.
type Snapshot struct {
	Dimension Dimension
	Chunks    map[ChunkPos]*Chunk
	Gen       Generation
}
