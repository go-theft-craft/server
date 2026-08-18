package server

import "strconv"

// The label set.
//
// A closed set is what makes a sample mappable onto a metrics backend's label
// schema without the sink inventing one. An open map would let any call site
// coin a key, and the first one to add a UUID alongside the username doubles
// every series in the system. It is a struct rather than a map for a second
// reason as well: a map allocates, and some of these are built per frame.
//
// The player label is the username, not the UUID. A UUID is the stable
// identity and a username is the readable one, and a metrics label is read by
// a person. Carrying both would double cardinality for a query nobody runs; a
// sink that needs the UUID can ask the server.

// The label keys, for a sink turning a Sample into whatever its backend wants.
const (
	LabelPlayer    = "player"    // username, not UUID
	LabelFeature   = "feature"   // one of the Feature constants
	LabelRegion    = "region"    // "r.2.-3", 32×32 chunks
	LabelChunk     = "chunk"     // "12,-40", only under WithChunkDetail
	LabelWorld     = "world"     //
	LabelPacket    = "packet"    // generated packet name, e.g. "map_chunk"
	LabelDirection = "direction" // "in" or "out"
)

// The two directions a byte can travel, as the direction label spells them.
const (
	DirectionIn  = "in"
	DirectionOut = "out"
)

// Feature is a named unit of server work.
//
// The list below is the API. A free-form string would let a call site coin a
// feature per block type, and per-feature attribution is only useful if the
// features are comparable across servers and across releases — so adding one
// means editing this file, where the next person can see the whole set.
//
// The names are chosen for where the work is rather than where the code is:
// chunk_encode and chunk_send are separate because one is CPU in pkg/world and
// the other is bytes through a stream, and those are two different problems.
type Feature string

// The thirteen features this server measures.
const (
	FeatureChunkGenerate Feature = "chunk_generate"
	FeatureChunkEncode   Feature = "chunk_encode"
	FeatureChunkSend     Feature = "chunk_send"
	FeatureChunkLoad     Feature = "chunk_load" // from the store
	FeatureChunkSave     Feature = "chunk_save"
	FeatureTick          Feature = "tick"
	FeatureEntitySync    Feature = "entity_sync"
	FeatureInventory     Feature = "inventory"
	FeatureCrafting      Feature = "crafting"
	FeatureCombat        Feature = "combat"
	FeatureCommand       Feature = "command"
	FeatureLogin         Feature = "login"
	FeatureProvenance    Feature = "provenance"
)

// Features is every feature this server measures, in the order they are
// declared. A sink pre-registering one series per feature reads it here rather
// than discovering the list one sample at a time.
func Features() []Feature {
	return []Feature{
		FeatureChunkGenerate, FeatureChunkEncode, FeatureChunkSend,
		FeatureChunkLoad, FeatureChunkSave, FeatureTick,
		FeatureEntitySync, FeatureInventory, FeatureCrafting,
		FeatureCombat, FeatureCommand, FeatureLogin, FeatureProvenance,
	}
}

// RegionPos is a 32×32 block of chunks: the same grouping the Anvil files use.
//
// It is the default chunk attribution because a chunk label is unbounded — a
// world with 10,000 resident chunks is 10,000 label values — and a region
// divides that by 1,024 while still answering "which part of the world is
// expensive". Matching the storage granularity is not incidental either: a
// slow region in the metrics and a slow region file on disk are then the same
// region.
type RegionPos struct {
	X, Z int
	// Set distinguishes region 0,0 from no region at all, which is what a
	// sample about something that is not in the world has.
	Set bool
}

// RegionOf is the region a chunk falls in.
//
// The arithmetic is a shift, not a division. They disagree for negatives —
// -1/32 is 0 and -1>>5 is -1 — and the Anvil layout uses the shift, so a
// metrics label built with division would name a region file that does not
// hold the chunk it claims to be about. That is worse than no label.
func RegionOf(cx, cz int) RegionPos { return RegionPos{X: cx >> 5, Z: cz >> 5, Set: true} }

// String renders a region the way its file is named: "r.2.-3".
func (r RegionPos) String() string {
	if !r.Set {
		return ""
	}

	return "r." + strconv.Itoa(r.X) + "." + strconv.Itoa(r.Z)
}

// ChunkPosLabel renders exact chunk coordinates, which is what WithChunkDetail
// puts in the chunk label.
func ChunkPosLabel(cx, cz int) string {
	return strconv.Itoa(cx) + "," + strconv.Itoa(cz)
}

// Labels is everything a sample can be attributed to.
//
// Every field is optional and the zero value carries no attribution, which is
// what a process-wide sample such as memory has.
type Labels struct {
	Player  string
	Feature Feature
	Region  RegionPos
	World   string
	// Chunk is set only under WithChunkDetail. See its cardinality note.
	Chunk string
	// Packet and Direction are set by the network sink only.
	Packet    string
	Direction string
}
