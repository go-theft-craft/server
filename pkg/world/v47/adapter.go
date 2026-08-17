// Package v47 encodes the version-neutral world model as Java Edition 1.8's
// protocol 47 sees it: a block state is an ID in the high twelve bits and a
// metadata nibble in the low four, and a chunk column is sixteen sections of
// little-endian uint16 followed by light and biomes.
package v47

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/go-theft-craft/minecraft-protocol/data"
	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/server/pkg/world"
)

const (
	sections          = 16
	sectionBlockBytes = 16 * 16 * 16 * 2 // 8192: 4096 blocks × 2 bytes
	sectionLightBytes = 16 * 16 * 16 / 2 // 2048: 4096 nibbles
	biomeBytes        = 256              // 16×16 biome IDs
)

// maxCachedSections bounds the encode cache. 4096 entries × 8 KB per encoded
// section is 32 MB, which covers a full 16-section column for 256 resident
// chunks — a little over a 15-chunk-radius view.
const maxCachedSections = 4096

// Adapter renders the world model for protocol 47.
type Adapter struct {
	reg world.StateRegistry
	dim world.Dimension

	// encode is indexed by handle, so the hot loop is an array index rather
	// than a map lookup.
	encode []uint16
	decode map[uint16]world.State

	// airSection is what a nil section renders as: one section of the empty
	// block, built once rather than zeroed per encode.
	airSection []byte

	cache *cache
}

// New builds an adapter for a registry and the data set that registry was
// built from. The two must agree: a handle whose block the set does not name
// is a programming error at construction, not a wrong block at render time.
func New(reg world.StateRegistry, set *data.Set) (*Adapter, error) {
	if reg == nil {
		return nil, fmt.Errorf("v47: nil registry")
	}
	if set == nil {
		return nil, fmt.Errorf("v47: nil data set")
	}

	a := &Adapter{
		reg:    reg,
		dim:    world.Overworld18(),
		encode: make([]uint16, reg.Len()),
		decode: make(map[uint16]world.State, reg.Len()),
		cache:  newCache(),
	}

	blocks := set.Blocks()
	for s := world.State(0); int(s) < reg.Len(); s++ {
		name, props, ok := reg.Lookup(s)
		if !ok {
			return nil, fmt.Errorf("v47: registry reported %d states but %d is unknown", reg.Len(), s)
		}
		block, ok := blocks.ByName(strings.TrimPrefix(name, "minecraft:"))
		if !ok {
			return nil, fmt.Errorf("v47: the data set does not know %s", name)
		}
		if block.ID < 0 || block.ID > 0xFFF {
			return nil, fmt.Errorf("v47: %s has ID %d, which does not fit the twelve-bit field", name, block.ID)
		}
		meta, err := metadataOf(props)
		if err != nil {
			return nil, fmt.Errorf("v47: %s: %w", name, err)
		}
		value := uint16(block.ID)<<4 | uint16(meta)
		a.encode[s] = value
		if _, clash := a.decode[value]; !clash {
			a.decode[value] = s
		}
	}

	a.airSection = make([]byte, sectionBlockBytes)
	air := a.encode[reg.Air()]
	for i := 0; i < sectionBlockBytes; i += 2 {
		binary.LittleEndian.PutUint16(a.airSection[i:], air)
	}

	return a, nil
}

// metadataOf reads the single property protocol 47 has. A state with no
// properties is metadata 0; anything else is a state from a flattened version,
// which this adapter cannot represent.
func metadataOf(props world.Properties) (int, error) {
	switch len(props) {
	case 0:
		return 0, nil
	case 1:
		if props[0].Key != "metadata" {
			return 0, fmt.Errorf("unexpected property %q", props[0].Key)
		}
		meta, err := strconv.Atoi(props[0].Value)
		if err != nil {
			return 0, fmt.Errorf("metadata %q: %w", props[0].Value, err)
		}
		if meta < 0 || meta > 15 {
			return 0, fmt.Errorf("metadata %d outside 0..15", meta)
		}

		return meta, nil
	default:
		return 0, fmt.Errorf("%d properties, which protocol 47 cannot encode", len(props))
	}
}

// Registry implements world.Adapter.
func (a *Adapter) Registry() world.StateRegistry { return a.reg }

// Dimension implements world.Adapter.
func (a *Adapter) Dimension() world.Dimension { return a.dim }

// EncodeState implements world.Adapter.
func (a *Adapter) EncodeState(s world.State) (int32, error) {
	if int(s) >= len(a.encode) {
		return 0, fmt.Errorf("v47: state %d is not from this adapter's registry", s)
	}

	return int32(a.encode[s]), nil
}

// DecodeState implements world.Adapter.
func (a *Adapter) DecodeState(v int32) (world.State, error) {
	if v < 0 || v > 0xFFFF {
		return 0, fmt.Errorf("v47: %d is not a protocol 47 block state", v)
	}
	s, ok := a.decode[uint16(v)]
	if !ok {
		return 0, fmt.Errorf("v47: no block state encodes to %d (id %d, metadata %d)", v, v>>4, v&0xF)
	}

	return s, nil
}

// EncodeUnload implements world.Adapter. Protocol 47 unloads a column with a
// ground-up MapChunk whose bitmap is zero and whose data is empty.
func (a *Adapter) EncodeUnload(pos world.ChunkPos) (world.Packet, error) {
	return &v1_8.PlayClientboundMapChunk{
		X:         int32(pos.X),
		Z:         int32(pos.Z),
		GroundUp:  true,
		BitMap:    0,
		ChunkData: []byte{},
	}, nil
}

// EncodeChunk implements world.Adapter.
func (a *Adapter) EncodeChunk(c *world.Chunk) (world.Packet, error) {
	if c == nil {
		return nil, fmt.Errorf("v47: nil chunk")
	}
	if len(c.Sections) != sections {
		return nil, fmt.Errorf("v47: chunk has %d sections, want %d", len(c.Sections), sections)
	}

	// A section the generator left empty still has to be sent once a player
	// has built in it, which is why presence is "the section exists" and not
	// "the section has a non-air block": a write allocates the section.
	var bitMap uint16
	for i, sec := range c.Sections {
		if sec != nil {
			bitMap |= 1 << uint(i)
		}
	}
	// If no sections exist at all, send section 0 so the client has something.
	if bitMap == 0 {
		bitMap = 0x0001
	}

	present := 0
	for i := range sections {
		if bitMap&(1<<uint(i)) != 0 {
			present++
		}
	}

	dataLen := present*(sectionBlockBytes+2*sectionLightBytes) + biomeBytes
	out := make([]byte, 0, dataLen)

	for i := range sections {
		if bitMap&(1<<uint(i)) == 0 {
			continue
		}
		blocks, err := a.encodeSection(c.Sections[i])
		if err != nil {
			return nil, fmt.Errorf("v47: chunk %v section %d: %w", c.Pos, i, err)
		}
		out = append(out, blocks...)
	}

	// Block light then sky light, both full for every present section.
	for range 2 * present {
		out = append(out, fullLight[:]...)
	}

	for _, b := range c.Biomes {
		out = append(out, byte(b))
	}

	return &v1_8.PlayClientboundMapChunk{
		X:         int32(c.Pos.X),
		Z:         int32(c.Pos.Z),
		GroundUp:  true,
		BitMap:    bitMap,
		ChunkData: out,
	}, nil
}

// fullLight is one section's worth of maximum light. The server does no
// lighting, so every nibble is 15.
var fullLight = func() [sectionLightBytes]byte {
	var b [sectionLightBytes]byte
	for i := range b {
		b[i] = 0xFF
	}

	return b
}()

// encodeSection returns the 8192 block bytes of one section, from the cache
// where it can.
func (a *Adapter) encodeSection(sec *world.Section) ([]byte, error) {
	if sec == nil {
		return a.airSection, nil
	}
	if b, ok := a.cache.get(sec); ok {
		return b, nil
	}

	states := sec.States()
	b := make([]byte, sectionBlockBytes)
	for i, s := range states {
		if int(s) >= len(a.encode) {
			return nil, fmt.Errorf("state %d at index %d is not from this adapter's registry", s, i)
		}
		binary.LittleEndian.PutUint16(b[i*2:], a.encode[s])
	}
	a.cache.put(sec, b)

	return b, nil
}

// cache maps a section pointer to its encoded block bytes. A Section is
// immutable, so an entry can never be stale: a write produces a new pointer
// and the old entry becomes unreachable.
type cache struct {
	mu      sync.Mutex
	entries map[*world.Section][]byte
	order   []*world.Section // bounded FIFO; see maxCachedSections
}

func newCache() *cache {
	return &cache{entries: make(map[*world.Section][]byte)}
}

func (c *cache) get(sec *world.Section) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	b, ok := c.entries[sec]

	return b, ok
}

func (c *cache) put(sec *world.Section, b []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.entries[sec]; ok {
		return
	}
	if len(c.order) >= maxCachedSections {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.entries, oldest)
	}
	c.entries[sec] = b
	c.order = append(c.order, sec)
}

// len reports how many sections the cache holds, for tests.
func (c *cache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return len(c.entries)
}
