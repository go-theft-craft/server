package world

// Packet is what an Adapter returns. The world names only the interface,
// because the concrete type belongs to the version the adapter speaks and the
// connection hands it straight to its stream.
type Packet interface{ PacketID() int32 }

// Adapter turns the version-neutral world model into one version's wire form.
// It is the only place a State handle becomes a number a client understands.
type Adapter interface {
	// Registry is the registry the adapter's handles come from. Mixing
	// handles from another registry is a programming error.
	Registry() StateRegistry
	// Dimension is the vertical extent the adapter encodes for.
	Dimension() Dimension
	// EncodeChunk renders a whole column.
	EncodeChunk(c *Chunk) (Packet, error)
	// EncodeUnload tells a client to drop a column.
	EncodeUnload(pos ChunkPos) (Packet, error)
	// EncodeState is the wire value for one handle, for BlockChange and
	// MultiBlockChange.
	EncodeState(s State) (int32, error)
	// DecodeState is the reverse. It exists for the paths that still read a
	// version's own encoding back in — the Anvil region files and the
	// overrides file — which M11.3 moves onto canonical names.
	DecodeState(v int32) (State, error)
}
