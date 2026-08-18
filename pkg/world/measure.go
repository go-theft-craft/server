package world

// Measurement, from below.
//
// A world cannot import the package that publishes the feature list — that
// package imports this one — so the seam is a function and the feature is a
// string. The names are declared here because this is where the work happens;
// the server has a typed constant for each, and a test there asserts the two
// spellings agree, which is the only thing that could drift.
//
// A nil Measure is a world nobody is watching, and every call site checks for
// it rather than calling through a no-op: the check is one predictable branch
// and the alternative is an indirect call in a loop that runs 625 times when a
// player joins.

// Measure starts a span and returns the function that ends it.
type Measure func(feature string, pos ChunkPos) func()

// The features a world reports.
const (
	MeasureChunkGenerate = "chunk_generate"
	MeasureChunkLoad     = "chunk_load"
	MeasureChunkEncode   = "chunk_encode"
	MeasureChunkSend     = "chunk_send"
	MeasureChunkSave     = "chunk_save"
	// MeasureBlockWrite and MeasureInventory are counted rather than timed:
	// they happen thousands of times a second, and a sample each would make
	// the measurement the load.
	MeasureBlockWrite = "block_write"
	MeasureInventory  = "inventory"
	MeasureEntitySync = "entity_sync"
)

// SetMeasure gives the world somewhere to report how long its work took.
//
// It is set before the world serves anyone, the same way the loader is, and
// read without a lock.
func (w *World) SetMeasure(m Measure) { w.measure = m }

// span starts a measurement, or does nothing.
func (w *World) span(feature string, pos ChunkPos) func() {
	if w.measure == nil {
		return nil
	}

	return w.measure(feature, pos)
}

// end closes a span that may be nil.
func end(span func()) {
	if span != nil {
		span()
	}
}

// EncodeChunk renders a column for the wire, measured.
//
// It is on the world rather than reached through Adapter() so that the
// measurement is in one place and every caller is inside it. The span covers
// the *cache lookup as well as the encode*, which means a cache hit reads as a
// fast encode rather than as no encode at all. That is deliberate: a hit that
// recorded nothing would make the section cache look like it removed the work
// rather than made it cheap, and the two are different claims.
func (w *World) EncodeChunk(pos ChunkPos) (Packet, error) {
	defer end(w.span(MeasureChunkEncode, pos))

	return w.adapter.EncodeChunk(w.Chunk(pos))
}
