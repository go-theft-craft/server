package world

import (
	"encoding/binary"
	"testing"

	"github.com/OCharnyshevich/minecraft-server/internal/server/world/gen"
)

func TestEncodeChunkFlatGenerator(t *testing.T) {
	w := NewWorld(gen.NewFlatGenerator(0))
	chunk := w.EncodeChunk(0, 0)

	if chunk.X != 0 || chunk.Z != 0 {
		t.Errorf("chunk coords = (%d,%d), want (0,0)", chunk.X, chunk.Z)
	}
	if !chunk.GroundUp {
		t.Error("GroundUp should be true")
	}
	// Flat generator uses sections 0 only (bedrock + stone + dirt + grass all in y=0..4).
	if chunk.BitMap&0x0001 == 0 {
		t.Error("section 0 should be present in BitMap")
	}
}

func TestEncodeChunkBlockEncoding(t *testing.T) {
	w := NewWorld(gen.NewFlatGenerator(0))
	chunk := w.EncodeChunk(0, 0)

	// Check that block at y=0, x=0, z=0 is bedrock (7<<4 = 0x0070).
	idx := (0*256 + 0*16 + 0) * 2
	blockState := binary.LittleEndian.Uint16(chunk.ChunkData[idx:])
	expected := uint16(7 << 4) // bedrock
	if blockState != expected {
		t.Errorf("block at (0,0,0) = 0x%04X, want 0x%04X (bedrock)", blockState, expected)
	}

	// Check that block at y=4, x=0, z=0 is grass (2<<4 = 0x0020).
	idx = (4*256 + 0*16 + 0) * 2
	blockState = binary.LittleEndian.Uint16(chunk.ChunkData[idx:])
	expected = uint16(2 << 4) // grass
	if blockState != expected {
		t.Errorf("block at (0,4,0) = 0x%04X, want 0x%04X (grass)", blockState, expected)
	}

	// Check that block at y=5, x=0, z=0 is air (0x0000).
	idx = (5*256 + 0*16 + 0) * 2
	blockState = binary.LittleEndian.Uint16(chunk.ChunkData[idx:])
	if blockState != 0x0000 {
		t.Errorf("block at (0,5,0) = 0x%04X, want 0x0000 (air)", blockState)
	}
}

func TestEncodeChunkWithOverrides(t *testing.T) {
	w := NewWorld(gen.NewFlatGenerator(0))

	// Place a cobblestone block at (0, 10, 0) in chunk (0, 0).
	w.SetBlock(0, 10, 0, 4<<4) // cobblestone

	chunk := w.EncodeChunk(0, 0)

	// Section 0 should have the override (y=10 is in section 0).
	idx := (10*256 + 0*16 + 0) * 2
	blockState := binary.LittleEndian.Uint16(chunk.ChunkData[idx:])
	expected := uint16(4 << 4) // cobblestone
	if blockState != expected {
		t.Errorf("block at (0,10,0) = 0x%04X, want 0x%04X (cobblestone)", blockState, expected)
	}
}

func TestEncodeChunkCoords(t *testing.T) {
	w := NewWorld(gen.NewFlatGenerator(0))
	chunk := w.EncodeChunk(3, -2)
	if chunk.X != 3 || chunk.Z != -2 {
		t.Errorf("chunk coords = (%d,%d), want (3,-2)", chunk.X, chunk.Z)
	}
	if !chunk.GroundUp {
		t.Error("GroundUp should be true")
	}
}

func TestEncodeChunkDefaultGenerator(t *testing.T) {
	w := NewWorld(gen.NewDefaultGenerator(42))
	chunk := w.EncodeChunk(0, 0)

	// Should have at least section 0.
	if chunk.BitMap == 0 {
		t.Error("BitMap should have at least one section")
	}
	if len(chunk.ChunkData) == 0 {
		t.Error("chunk data should not be empty")
	}
}
