package world

import (
	"encoding/binary"
	"testing"
)

func TestFlatStoneChunkDataSize(t *testing.T) {
	chunk := FlatStoneChunk(0, 0)

	// Expected: blocks(8192) + blockLight(2048) + skyLight(2048) + biomes(256) = 12544
	expected := sectionBlockBytes + sectionLightBytes + sectionLightBytes + biomeBytes
	if len(chunk.ChunkData) != expected {
		t.Errorf("chunk data length = %d, want %d", len(chunk.ChunkData), expected)
	}
}

func TestFlatStoneChunkBlockEncoding(t *testing.T) {
	chunk := FlatStoneChunk(0, 0)

	// Check that block at y=0, x=0, z=0 is stone (1<<4 = 0x0010).
	idx := (0*256 + 0*16 + 0) * 2 // y=0, z=0, x=0
	blockState := binary.LittleEndian.Uint16(chunk.ChunkData[idx:])
	if blockState != 0x0010 {
		t.Errorf("block at (0,0,0) = 0x%04X, want 0x0010 (stone)", blockState)
	}

	// Check that block at y=1, x=0, z=0 is air (0x0000).
	idx = (1*256 + 0*16 + 0) * 2 // y=1, z=0, x=0
	blockState = binary.LittleEndian.Uint16(chunk.ChunkData[idx:])
	if blockState != 0x0000 {
		t.Errorf("block at (0,1,0) = 0x%04X, want 0x0000 (air)", blockState)
	}
}

func TestFlatStoneChunkBitmask(t *testing.T) {
	chunk := FlatStoneChunk(3, -2)
	if chunk.BitMap != 0x0001 {
		t.Errorf("BitMap = 0x%04X, want 0x0001", chunk.BitMap)
	}
	if chunk.X != 3 || chunk.Z != -2 {
		t.Errorf("chunk coords = (%d,%d), want (3,-2)", chunk.X, chunk.Z)
	}
	if !chunk.GroundUp {
		t.Error("GroundUp should be true")
	}
}
