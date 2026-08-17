package anvil

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"testing"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	"github.com/go-theft-craft/minecraft-protocol/wire/java"

	"github.com/go-theft-craft/server/pkg/world"
)

// assertUpstreamAccepts runs a chunk payload through minecraft-protocol's own
// NBT validator.
//
// It is a second opinion on this package's writer from code that has no stake
// in it: the validator is what the wire uses, it is strict, and it caught the
// malformed Sections list this writer emitted for as long as nothing read a
// region file back. A payload vanilla could not parse should fail here first.
func assertUpstreamAccepts(t *testing.T, payload []byte) {
	t.Helper()

	limits, err := protocol.NewLimits()
	if err != nil {
		t.Fatalf("limits: %v", err)
	}
	if _, err := java.NewNBT(payload, limits); err != nil {
		t.Fatalf("minecraft-protocol rejects what this writer produced: %v", err)
	}
}

// identityEncoder makes a handle its own wire value, so these tests can name a
// protocol 47 block state directly instead of building a whole registry.
type identityEncoder struct{}

func (identityEncoder) EncodeState(s world.State) (int32, error) { return int32(s), nil }

func (identityEncoder) DecodeState(v int32) (world.State, error) { return world.State(v), nil }

// newChunk returns an empty column of Java 1.8's overworld.
func newChunk(cx, cz int) *world.Chunk {
	dim := world.Overworld18()

	return &world.Chunk{
		Pos:      world.ChunkPos{X: cx, Z: cz},
		Sections: make([]*world.Section, dim.Sections()),
	}
}

// setBlock writes one block into a column under construction.
func setBlock(c *world.Chunk, x, y, z int, state world.State) {
	dim := world.Overworld18()
	index := dim.SectionIndex(y)
	c.Sections[index] = c.Sections[index].With(world.SectionBlockIndex(x, y&0xF, z), state)
}

func TestSetNibble(t *testing.T) {
	arr := make([]byte, 4)

	// Even index: low nibble.
	setNibble(arr, 0, 0x0A)
	if arr[0] != 0x0A {
		t.Fatalf("expected 0x0A, got 0x%02X", arr[0])
	}

	// Odd index: high nibble.
	setNibble(arr, 1, 0x0B)
	if arr[0] != 0xBA {
		t.Fatalf("expected 0xBA, got 0x%02X", arr[0])
	}

	// Another pair.
	setNibble(arr, 4, 0x03)
	setNibble(arr, 5, 0x07)
	if arr[2] != 0x73 {
		t.Fatalf("expected 0x73, got 0x%02X", arr[2])
	}
}

func TestEncodeChunkNBT(t *testing.T) {
	chunk := newChunk(0, 0)
	// Place a stone block (ID=1, meta=0 → state=0x10) at local (0, 0, 0).
	setBlock(chunk, 0, 0, 0, 0x10)
	// Place grass (ID=2, meta=0 → state=0x20) at local (1, 64, 1).
	setBlock(chunk, 1, 64, 1, 0x20)
	// Dirt (ID=3, meta=0) where a player put it.
	setBlock(chunk, 2, 10, 3, 0x30)

	data, err := EncodeChunkNBT(chunk, identityEncoder{})
	if err != nil {
		t.Fatalf("EncodeChunkNBT failed: %v", err)
	}

	assertUpstreamAccepts(t, data)

	// Basic structural checks: should start with compound tag (10).
	if len(data) == 0 {
		t.Fatal("empty NBT output")
	}
	if data[0] != 10 {
		t.Fatalf("expected root compound tag (10), got %d", data[0])
	}

	// Verify it ends with two End tags (inner Level compound + outer root compound).
	if data[len(data)-1] != 0 || data[len(data)-2] != 0 {
		t.Fatal("expected two End tags at end of NBT")
	}

	// Verify data is large enough to contain sections.
	if len(data) < 1000 {
		t.Fatalf("NBT data seems too small: %d bytes", len(data))
	}
}

func TestEncodeChunkNBTWithHighBlockID(t *testing.T) {
	chunk := newChunk(0, 0)
	// Block ID 300 (0x12C), meta 5 → state = 300<<4 | 5 = 0x12C5
	setBlock(chunk, 0, 0, 0, 0x12C5)

	data, err := EncodeChunkNBT(chunk, identityEncoder{})
	if err != nil {
		t.Fatalf("EncodeChunkNBT failed: %v", err)
	}

	assertUpstreamAccepts(t, data)

	// Should contain "Add" byte array for high block IDs.
	if !bytes.Contains(data, []byte("Add")) {
		t.Fatal("expected Add array for block ID > 255")
	}
}

func TestComputeHeightMap(t *testing.T) {
	chunk := newChunk(0, 0)
	// Place block at y=64.
	setBlock(chunk, 0, 64, 0, 0x10)
	// Place block at y=100.
	setBlock(chunk, 5, 100, 5, 0x20)

	hm, err := computeHeightMap(chunk, identityEncoder{})
	if err != nil {
		t.Fatalf("computeHeightMap: %v", err)
	}

	if hm[0] != 65 { // y=64 → heightmap = 65
		t.Fatalf("expected heightmap[0]=65, got %d", hm[0])
	}
	if hm[5*16+5] != 101 { // y=100 → heightmap = 101
		t.Fatalf("expected heightmap[85]=101, got %d", hm[5*16+5])
	}
	if hm[1] != 0 { // no blocks at (1,_,0)
		t.Fatalf("expected heightmap[1]=0, got %d", hm[1])
	}
}

func TestComputeHeightMapTakesTheHighestBlock(t *testing.T) {
	chunk := newChunk(0, 0)
	setBlock(chunk, 0, 64, 0, 0x10)
	setBlock(chunk, 0, 200, 0, 0x10)

	hm, err := computeHeightMap(chunk, identityEncoder{})
	if err != nil {
		t.Fatalf("computeHeightMap: %v", err)
	}
	if hm[0] != 201 {
		t.Fatalf("expected heightmap[0]=201, got %d", hm[0])
	}
}

func TestSaveRegion(t *testing.T) {
	dir := t.TempDir()

	chunk := newChunk(0, 0)
	setBlock(chunk, 0, 0, 0, 0x10) // stone

	nbtData, err := EncodeChunkNBT(chunk, identityEncoder{})
	if err != nil {
		t.Fatalf("encode chunk: %v", err)
	}

	chunks := map[world.ChunkPos][]byte{
		{X: 0, Z: 0}: nbtData,
	}

	if err := SaveRegion(dir, 0, 0, chunks); err != nil {
		t.Fatalf("SaveRegion failed: %v", err)
	}

	// Verify the file exists.
	path := filepath.Join(dir, "r.0.0.mca")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open region file: %v", err)
	}
	defer f.Close()

	// Read location table.
	var locations [4096]byte
	if _, err := io.ReadFull(f, locations[:]); err != nil {
		t.Fatalf("read locations: %v", err)
	}

	// Chunk (0,0) should be at index 0.
	entry := binary.BigEndian.Uint32(locations[0:4])
	offset := entry >> 8
	sectorCount := entry & 0xFF

	if offset != 2 { // first data sector starts at 2 (after location + timestamp)
		t.Fatalf("expected offset 2, got %d", offset)
	}
	if sectorCount == 0 {
		t.Fatal("expected non-zero sector count")
	}

	// Skip timestamp table.
	if _, err := f.Seek(int64(offset)*sectorSize, io.SeekStart); err != nil {
		t.Fatalf("seek to chunk data: %v", err)
	}

	// Read chunk header.
	var chunkHeader [5]byte
	if _, err := io.ReadFull(f, chunkHeader[:]); err != nil {
		t.Fatalf("read chunk header: %v", err)
	}

	payloadLen := binary.BigEndian.Uint32(chunkHeader[0:4])
	compression := chunkHeader[4]

	if compression != 2 {
		t.Fatalf("expected zlib compression (2), got %d", compression)
	}
	if payloadLen < 2 {
		t.Fatalf("payload too small: %d", payloadLen)
	}

	// Read and decompress chunk data.
	compressed := make([]byte, payloadLen-1) // -1 for compression byte
	if _, err := io.ReadFull(f, compressed); err != nil {
		t.Fatalf("read compressed data: %v", err)
	}

	zr, err := zlib.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("create zlib reader: %v", err)
	}
	defer zr.Close()

	decompressed, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("decompress: %v", err)
	}

	// Decompressed data should be valid NBT starting with compound tag.
	if len(decompressed) == 0 {
		t.Fatal("decompressed data is empty")
	}
	if decompressed[0] != 10 {
		t.Fatalf("expected compound tag (10), got %d", decompressed[0])
	}
}

func TestSaveRegionMultipleChunks(t *testing.T) {
	dir := t.TempDir()

	chunks := make(map[world.ChunkPos][]byte)
	for i := range 3 {
		chunk := newChunk(i, 0)
		setBlock(chunk, 0, 0, 0, 0x10)
		nbtData, err := EncodeChunkNBT(chunk, identityEncoder{})
		if err != nil {
			t.Fatalf("encode chunk %d: %v", i, err)
		}
		chunks[world.ChunkPos{X: i, Z: 0}] = nbtData
	}

	if err := SaveRegion(dir, 0, 0, chunks); err != nil {
		t.Fatalf("SaveRegion failed: %v", err)
	}

	// Verify file exists and is larger than just the header.
	path := filepath.Join(dir, "r.0.0.mca")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat region file: %v", err)
	}

	// At minimum: 2 header sectors + at least 3 data sectors.
	minSize := int64(sectorSize * 5)
	if info.Size() < minSize {
		t.Fatalf("region file too small: %d bytes (expected at least %d)", info.Size(), minSize)
	}
}
