package anvil

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/OCharnyshevich/minecraft-server/internal/server/world"
	"github.com/OCharnyshevich/minecraft-server/internal/server/world/gen"
)

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
	chunk := &gen.ChunkData{}
	// Place a stone block (ID=1, meta=0 → state=0x10) at local (0, 0, 0).
	chunk.SetBlock(0, 0, 0, 0x10)
	// Place grass (ID=2, meta=0 → state=0x20) at local (1, 64, 1).
	chunk.SetBlock(1, 64, 1, 0x20)

	overrides := map[world.BlockPos]int32{
		{X: 2, Y: 10, Z: 3}: 0x30, // dirt (ID=3, meta=0)
	}

	data, err := EncodeChunkNBT(0, 0, chunk, overrides)
	if err != nil {
		t.Fatalf("EncodeChunkNBT failed: %v", err)
	}

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
	chunk := &gen.ChunkData{}
	// Block ID 300 (0x12C), meta 5 → state = 300<<4 | 5 = 0x12C5
	chunk.SetBlock(0, 0, 0, 0x12C5)

	data, err := EncodeChunkNBT(0, 0, chunk, nil)
	if err != nil {
		t.Fatalf("EncodeChunkNBT failed: %v", err)
	}

	// Should contain "Add" byte array for high block IDs.
	if !bytes.Contains(data, []byte("Add")) {
		t.Fatal("expected Add array for block ID > 255")
	}
}

func TestComputeHeightMap(t *testing.T) {
	chunk := &gen.ChunkData{}
	// Place block at y=64.
	chunk.SetBlock(0, 64, 0, 0x10)
	// Place block at y=100.
	chunk.SetBlock(5, 100, 5, 0x20)

	hm := computeHeightMap(chunk, nil)

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

func TestComputeHeightMapWithOverrides(t *testing.T) {
	chunk := &gen.ChunkData{}
	chunk.SetBlock(0, 64, 0, 0x10)

	overrides := map[world.BlockPos]int32{
		{X: 0, Y: 200, Z: 0}: 0x10, // override higher than base
	}

	hm := computeHeightMap(chunk, overrides)
	if hm[0] != 201 {
		t.Fatalf("expected heightmap[0]=201, got %d", hm[0])
	}
}

func TestSaveRegion(t *testing.T) {
	dir := t.TempDir()

	chunk := &gen.ChunkData{}
	chunk.SetBlock(0, 0, 0, 0x10) // stone

	nbtData, err := EncodeChunkNBT(0, 0, chunk, nil)
	if err != nil {
		t.Fatalf("encode chunk: %v", err)
	}

	chunks := map[gen.ChunkPos][]byte{
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

	chunks := make(map[gen.ChunkPos][]byte)
	for i := 0; i < 3; i++ {
		chunk := &gen.ChunkData{}
		chunk.SetBlock(0, 0, 0, 0x10)
		nbtData, err := EncodeChunkNBT(i, 0, chunk, nil)
		if err != nil {
			t.Fatalf("encode chunk %d: %v", i, err)
		}
		chunks[gen.ChunkPos{X: i, Z: 0}] = nbtData
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
