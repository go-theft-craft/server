package world

import (
	"encoding/binary"

	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"
)

const (
	sectionBlockBytes = 16 * 16 * 16 * 2 // 8192 bytes: 4096 blocks × 2 bytes each
	sectionLightBytes = 16 * 16 * 16 / 2 // 2048 bytes: 4096 nibbles
	biomeBytes        = 256              // 16×16 biome IDs
)

// Chunk is one column of the world. It is immutable: a block write produces a
// new Chunk that shares every section it did not touch, and the world publishes
// it by swapping a pointer.
type Chunk struct {
	Pos      ChunkPos
	Sections []*Section // len == dim.Sections(); a nil entry is all air
	Biomes   [256]Biome
	Gen      Generation
}

// At returns the state at chunk-local x, z and world y. A y outside the
// dimension, or a column the chunk left empty, reads as the given air state.
func (c *Chunk) At(dim Dimension, x, y, z int, air State) State {
	if c == nil || !dim.Contains(y) {
		return air
	}
	sec := c.Sections[dim.SectionIndex(y)]
	if sec == nil {
		return air
	}

	return sec.At(SectionBlockIndex(x, y&0xF, z))
}

// EncodeChunk encodes a ChunkData into a MapChunk packet, applying any block overrides.
func (w *World) EncodeChunk(cx, cz int) v1_8.PlayClientboundMapChunk {
	chunk := w.GetOrGenerateChunk(cx, cz)

	// The overrides are read once. Reading them per section would rescan the
	// whole world's overrides sixteen times per chunk sent.
	overrides := w.OverridesForChunk(cx, cz)

	// Determine which sections are non-nil.
	var bitMap uint16
	for i, sec := range chunk.Sections {
		if sec != nil {
			bitMap |= 1 << uint(i)
		}
	}

	// A section the generator left empty still has to be sent when a player
	// built in it. Without this, a block placed above the terrain — on a hill,
	// on a roof, anywhere the column's top section is nil — is stored, is
	// broadcast as a block change so it appears immediately, and then vanishes
	// from every later chunk send: on reconnect, on respawn, or as soon as the
	// chunk is reloaded.
	for pos := range overrides {
		if section := pos.Y >> 4; section >= 0 && section < 16 {
			bitMap |= 1 << uint(section)
		}
	}

	// If no sections exist at all, send at least section 0 so the client has something.
	if bitMap == 0 {
		bitMap = 0x0001
	}

	sectionCount := 0
	for i := 0; i < 16; i++ {
		if bitMap&(1<<uint(i)) != 0 {
			sectionCount++
		}
	}

	// Allocate data: per section (blocks + blockLight + skyLight) + biomes.
	dataLen := sectionCount*(sectionBlockBytes+sectionLightBytes+sectionLightBytes) + biomeBytes
	data := make([]byte, 0, dataLen)

	// Block data for each active section.
	for i := 0; i < 16; i++ {
		if bitMap&(1<<uint(i)) == 0 {
			continue
		}
		blocks := make([]byte, sectionBlockBytes)
		sec := chunk.Sections[i]
		if sec != nil {
			for idx := 0; idx < 4096; idx++ {
				binary.LittleEndian.PutUint16(blocks[idx*2:], sec.Blocks[idx])
			}
		}
		// Apply overrides for this section.
		applyOverrides(blocks, overrides, i)
		data = append(data, blocks...)
	}

	// Block light: all 0xFF (full light) for each section.
	fullLight := make([]byte, sectionLightBytes)
	for i := range fullLight {
		fullLight[i] = 0xFF
	}
	for i := 0; i < 16; i++ {
		if bitMap&(1<<uint(i)) == 0 {
			continue
		}
		data = append(data, fullLight...)
	}

	// Sky light: all 0xFF for each section.
	for i := 0; i < 16; i++ {
		if bitMap&(1<<uint(i)) == 0 {
			continue
		}
		data = append(data, fullLight...)
	}

	// Biome data.
	data = append(data, chunk.Biomes[:]...)

	return v1_8.PlayClientboundMapChunk{
		X:         int32(cx),
		Z:         int32(cz),
		GroundUp:  true,
		BitMap:    bitMap,
		ChunkData: data,
	}
}

// applyOverrides writes one chunk's block overrides into a section's block
// data. The overrides are already filtered to the chunk, so the caller holds
// no lock here.
func applyOverrides(blocks []byte, overrides map[BlockPos]int32, sectionIdx int) {
	baseY := sectionIdx * 16
	for pos, stateID := range overrides {
		if pos.Y < baseY || pos.Y >= baseY+16 {
			continue
		}
		lx := pos.X & 0xF
		ly := pos.Y & 0xF
		lz := pos.Z & 0xF
		idx := (ly*256 + lz*16 + lx) * 2
		binary.LittleEndian.PutUint16(blocks[idx:], uint16(stateID))
	}
}
