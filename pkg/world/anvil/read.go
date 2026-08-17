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

	entry := binary.BigEndian.Uint32(r.data[((pos.X&31)+(pos.Z&31)*32)*4:])
	offset, sectors := entry>>8, entry&0xFF
	if offset == 0 && sectors == 0 {
		return nil, false, nil
	}
	if offset < headerSectors {
		return nil, false, fmt.Errorf("%w: chunk %v claims sector %d, inside the header", ErrCorrupt, pos, offset)
	}

	start := int(offset) * sectorSize
	if start+5 > len(r.data) {
		return nil, false, fmt.Errorf("%w: chunk %v starts past the end of the file", ErrCorrupt, pos)
	}

	length := int(binary.BigEndian.Uint32(r.data[start:]))
	if length < 1 || start+4+length > len(r.data) {
		return nil, false, fmt.Errorf("%w: chunk %v claims %d payload bytes with %d left",
			ErrCorrupt, pos, length, len(r.data)-start-4)
	}

	payload, err := decompress(r.data[start+4], r.data[start+5:start+4+length])
	if err != nil {
		return nil, false, fmt.Errorf("anvil: chunk %v: %w", pos, err)
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

	return c, nil
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
