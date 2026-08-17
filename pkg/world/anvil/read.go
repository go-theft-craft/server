package anvil

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/go-theft-craft/server/pkg/world"
	"github.com/go-theft-craft/server/pkg/world/nbt"
)

// compressionGzip is the other compression vanilla writes. It is rare in
// practice and cheap to accept, and a reader that refused it would reject a
// world it could have read.
const compressionGzip = 1

// maxChunkPayload bounds a decompressed chunk. A 1.8 chunk is about 160 KB of
// section data before compression; 8 MB is far past anything real and stops a
// crafted region file from asking for gigabytes.
const maxChunkPayload = 8 << 20

// ErrCorrupt reports a region file whose structure does not hold together.
var ErrCorrupt = errors.New("anvil: corrupt region file")

// StateDecoder turns the block state number an Anvil file stores into a handle.
//
// The Anvil format's numbering is Java 1.8's — id<<4|metadata — whatever
// version the server serves, so this is a protocol 47 decoder and not, in
// general, the running server's adapter.
type StateDecoder interface {
	DecodeState(int32) (world.State, error)
}

// Region is one opened .mca file.
type Region struct {
	dim  world.Dimension
	dec  StateDecoder
	air  world.State
	rx   int
	rz   int
	data []byte
}

// OpenRegion reads the region file covering region coordinates rx, rz.
//
// It reports os.ErrNotExist through the returned error when the file is
// absent, which the caller must distinguish from a chunk that is absent
// inside a region that exists.
func OpenRegion(dir string, rx, rz int, dim world.Dimension, dec StateDecoder, air world.State) (*Region, error) {
	path := filepath.Join(dir, fmt.Sprintf("r.%d.%d.mca", rx, rz))

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) < headerSectors*sectorSize {
		return nil, fmt.Errorf("%w: %s is %d bytes, shorter than its header", ErrCorrupt, path, len(data))
	}

	return &Region{dim: dim, dec: dec, air: air, rx: rx, rz: rz, data: data}, nil
}

// RegionOf is the region a chunk belongs to.
func RegionOf(pos world.ChunkPos) (rx, rz int) { return pos.X >> 5, pos.Z >> 5 }

// Chunk decodes one column.
//
// The second result distinguishes absent from empty, which is the distinction
// a store depends on: absent means generate, empty means a column of air, and
// confusing them regenerates terrain over a player's excavation.
func (r *Region) Chunk(pos world.ChunkPos) (*world.Chunk, bool, error) {
	if rx, rz := RegionOf(pos); rx != r.rx || rz != r.rz {
		return nil, false, fmt.Errorf("anvil: chunk %v is not in region %d,%d", pos, r.rx, r.rz)
	}

	payload, present, err := r.rawChunk(pos)
	if err != nil || !present {
		return nil, false, err
	}

	root, err := nbt.Decode(payload)
	if err != nil {
		return nil, false, fmt.Errorf("anvil: chunk %v: %w", pos, err)
	}
	level, ok := root.Compound("Level")
	if !ok {
		return nil, false, fmt.Errorf("%w: chunk %v has no Level compound", ErrCorrupt, pos)
	}

	c, err := r.decodeLevel(pos, level)
	if err != nil {
		return nil, false, err
	}

	return c, true, nil
}

// Payloads returns every chunk present in the region, still compressed.
//
// A store rewriting one region needs it: a region holds 1,024 columns and a
// snapshot holds only the resident ones, so saving without carrying the rest
// forward would delete the world outside the players. They come back
// compressed because they are going straight back in — decompressing and
// re-compressing 1,023 untouched columns to change one is most of what a save
// used to cost.
func (r *Region) Payloads() (map[world.ChunkPos]Payload, error) {
	out := make(map[world.ChunkPos]Payload)
	for i := range 1024 {
		pos := world.ChunkPos{X: r.rx*32 + i%32, Z: r.rz*32 + i/32}
		compressed, present, err := r.compressedChunk(pos)
		if err != nil {
			return nil, err
		}
		if present {
			out[pos] = Payload{Compressed: compressed}
		}
	}

	return out, nil
}

// compressedChunk locates one chunk's stored bytes without decompressing them.
func (r *Region) compressedChunk(pos world.ChunkPos) ([]byte, bool, error) {
	start, length, present, err := r.locate(pos)
	if err != nil || !present {
		return nil, false, err
	}
	if r.data[start+4] != compressionZlib {
		// A gzip payload cannot be written through as-is, because SaveRegion
		// writes zlib. Decompressing it here is the price of accepting a
		// scheme this server does not produce.
		return nil, false, errPassthroughUnavailable
	}

	return r.data[start+5 : start+4+length], true, nil
}

// errPassthroughUnavailable reports a chunk whose stored bytes cannot go
// straight back out.
var errPassthroughUnavailable = errors.New("anvil: chunk cannot be written through uncompressed")

// locate finds a chunk's stored bytes: the offset of its length field and how
// many bytes follow it.
func (r *Region) locate(pos world.ChunkPos) (start, length int, present bool, err error) {
	entry := binary.BigEndian.Uint32(r.data[((pos.X&31)+(pos.Z&31)*32)*4:])
	offset, sectors := entry>>8, entry&0xFF
	if offset == 0 && sectors == 0 {
		return 0, 0, false, nil
	}
	if offset < headerSectors {
		return 0, 0, false, fmt.Errorf("%w: chunk %v claims sector %d, inside the header", ErrCorrupt, pos, offset)
	}

	start = int(offset) * sectorSize
	if start+5 > len(r.data) {
		return 0, 0, false, fmt.Errorf("%w: chunk %v starts past the end of the file", ErrCorrupt, pos)
	}

	length = int(binary.BigEndian.Uint32(r.data[start:]))
	if length < 1 || start+4+length > len(r.data) {
		return 0, 0, false, fmt.Errorf("%w: chunk %v claims %d payload bytes with %d left",
			ErrCorrupt, pos, length, len(r.data)-start-4)
	}

	return start, length, true, nil
}

// rawChunk locates and decompresses one chunk's NBT payload.
func (r *Region) rawChunk(pos world.ChunkPos) ([]byte, bool, error) {
	start, length, present, err := r.locate(pos)
	if err != nil || !present {
		return nil, false, err
	}

	payload, err := decompress(r.data[start+4], r.data[start+5:start+4+length])
	if err != nil {
		return nil, false, fmt.Errorf("anvil: chunk %v: %w", pos, err)
	}

	return payload, true, nil
}

func decompress(scheme byte, payload []byte) ([]byte, error) {
	var (
		reader io.ReadCloser
		err    error
	)
	switch scheme {
	case compressionZlib:
		reader, err = zlib.NewReader(bytes.NewReader(payload))
	case compressionGzip:
		reader, err = gzip.NewReader(bytes.NewReader(payload))
	default:
		return nil, fmt.Errorf("%w: compression scheme %d", ErrCorrupt, scheme)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCorrupt, err)
	}
	defer reader.Close()

	out, err := io.ReadAll(io.LimitReader(reader, maxChunkPayload+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCorrupt, err)
	}
	if len(out) > maxChunkPayload {
		return nil, fmt.Errorf("%w: payload past %d bytes", ErrCorrupt, maxChunkPayload)
	}

	return out, nil
}

func (r *Region) decodeLevel(pos world.ChunkPos, level nbt.Compound) (*world.Chunk, error) {
	// The stored coordinates are the authority on which column this is; a file
	// whose index and contents disagree is corrupt rather than relocatable.
	if x, ok := level.Int("xPos"); ok && int(x) != pos.X {
		return nil, fmt.Errorf("%w: chunk at index %v says xPos %d", ErrCorrupt, pos, x)
	}
	if z, ok := level.Int("zPos"); ok && int(z) != pos.Z {
		return nil, fmt.Errorf("%w: chunk at index %v says zPos %d", ErrCorrupt, pos, z)
	}

	c := &world.Chunk{Pos: pos, Sections: make([]*world.Section, r.dim.Sections())}

	sections, err := level.Compounds("Sections")
	if err != nil {
		return nil, fmt.Errorf("anvil: chunk %v: %w", pos, err)
	}
	for _, section := range sections {
		if err := r.decodeSection(c, pos, section); err != nil {
			return nil, err
		}
	}

	if biomes, ok := level.ByteArray("Biomes"); ok && len(biomes) == len(c.Biomes) {
		for i, b := range biomes {
			c.Biomes[i] = world.Biome(b)
		}
	}

	if err := r.decodeTileEntities(c, pos, level); err != nil {
		return nil, err
	}

	return c, nil
}

// decodeTileEntities reads back the containers writeTileEntities wrote.
//
// A tile entity this server does not model is skipped rather than rejected: a
// world may hold furnaces and signs this server has no idea about, and
// refusing to load it would be worse than not showing them.
func (r *Region) decodeTileEntities(c *world.Chunk, pos world.ChunkPos, level nbt.Compound) error {
	entities, err := level.Compounds("TileEntities")
	if err != nil {
		return fmt.Errorf("anvil: chunk %v: %w", pos, err)
	}

	namer, ok := r.dec.(ItemNamer)

	for _, entity := range entities {
		if id, _ := entity.String("id"); id != chestTileEntityID {
			continue
		}
		if !ok {
			continue
		}

		x, okX := entity.Int("x")
		y, okY := entity.Int("y")
		z, okZ := entity.Int("z")
		if !okX || !okY || !okZ {
			return fmt.Errorf("%w: chunk %v has a chest with no position", ErrCorrupt, pos)
		}

		items, err := entity.Compounds("Items")
		if err != nil {
			return fmt.Errorf("anvil: chunk %v chest at %d,%d,%d: %w", pos, x, y, z, err)
		}

		contents := world.EmptyChest()
		for _, item := range items {
			slot, okSlot := item.Byte("Slot")
			name, okName := item.String("id")
			if !okSlot || !okName {
				return fmt.Errorf("%w: chunk %v has a chest item with no slot or name", ErrCorrupt, pos)
			}
			if int(slot) >= len(contents) {
				return fmt.Errorf("%w: chunk %v has a chest item in slot %d", ErrCorrupt, pos, slot)
			}
			id, okID := namer.ItemID(name)
			if !okID {
				// An item this version does not name is dropped rather than
				// guessed at. Recorded here as a skipped slot; M11.5 gives
				// that a durable destination.
				continue
			}
			count, _ := item.Byte("Count")
			damage, _ := item.Short("Damage")
			contents[slot] = world.ItemStack{BlockID: id, ItemCount: int8(count), ItemDamage: damage}
		}

		if c.Chests == nil {
			c.Chests = map[world.BlockPos]world.ChestContents{}
		}
		c.Chests[world.BlockPos{X: int(x), Y: int(y), Z: int(z)}] = contents
	}

	return nil
}

func (r *Region) decodeSection(c *world.Chunk, pos world.ChunkPos, section nbt.Compound) error {
	y, ok := section.Byte("Y")
	if !ok {
		return fmt.Errorf("%w: chunk %v has a section with no Y", ErrCorrupt, pos)
	}
	index := int(y)
	if index < 0 || index >= len(c.Sections) {
		return fmt.Errorf("%w: chunk %v has section Y=%d outside %s", ErrCorrupt, pos, y, r.dim.Name)
	}

	blocks, ok := section.ByteArray("Blocks")
	if !ok || len(blocks) != world.BlocksPerSection {
		return fmt.Errorf("%w: chunk %v section %d has %d Blocks, want %d",
			ErrCorrupt, pos, index, len(blocks), world.BlocksPerSection)
	}
	data, ok := section.ByteArray("Data")
	if !ok || len(data) != world.BlocksPerSection/2 {
		return fmt.Errorf("%w: chunk %v section %d has %d Data bytes, want %d",
			ErrCorrupt, pos, index, len(data), world.BlocksPerSection/2)
	}
	// Add carries the high four bits of a block ID past 255. Vanilla omits it
	// unless a mod put such a block in the world.
	add, hasAdd := section.ByteArray("Add")
	if hasAdd && len(add) != world.BlocksPerSection/2 {
		return fmt.Errorf("%w: chunk %v section %d has %d Add bytes, want %d",
			ErrCorrupt, pos, index, len(add), world.BlocksPerSection/2)
	}

	changes := make([]world.Change, world.BlocksPerSection)
	for i := range changes {
		id := int32(blocks[i])
		if hasAdd {
			id |= int32(nibble(add, i)) << 8
		}
		state, err := r.dec.DecodeState(id<<4 | int32(nibble(data, i)))
		if err != nil {
			return fmt.Errorf("anvil: chunk %v section %d block %d: %w", pos, index, i, err)
		}
		changes[i] = world.Change{Index: i, State: state}
	}

	var empty *world.Section
	c.Sections[index] = empty.WithMany(changes)

	return nil
}

// nibble reads the 4-bit value at index from a packed array, low nibble first,
// which is the order setNibble writes.
func nibble(arr []byte, index int) byte {
	b := arr[index/2]
	if index%2 == 0 {
		return b & 0x0F
	}

	return b >> 4
}
