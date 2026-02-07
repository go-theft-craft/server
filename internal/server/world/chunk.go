package world

import (
	"encoding/binary"

	pkt "github.com/OCharnyshevich/minecraft-server/internal/gamedata/versions/pc_1_8"
	mcnet "github.com/OCharnyshevich/minecraft-server/internal/server/net"
)

const (
	sectionBlockBytes = 16 * 16 * 16 * 2 // 8192 bytes: 4096 blocks × 2 bytes each
	sectionLightBytes = 16 * 16 * 16 / 2 // 2048 bytes: 4096 nibbles
	biomeBytes        = 256              // 16×16 biome IDs
)

// EncodeChunk encodes a ChunkData into a MapChunk packet, applying any block overrides.
func (w *World) EncodeChunk(cx, cz int) pkt.MapChunk {
	chunk := w.GetOrGenerateChunk(cx, cz)

	// Determine which sections are non-nil.
	var bitMap uint16
	for i, sec := range chunk.Sections {
		if sec != nil {
			bitMap |= 1 << uint(i)
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
		w.applyOverrides(blocks, cx, cz, i)
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

	return pkt.MapChunk{
		X:         int32(cx),
		Z:         int32(cz),
		GroundUp:  true,
		BitMap:    bitMap,
		ChunkData: data,
	}
}

// applyOverrides writes block overrides into the section's block data.
func (w *World) applyOverrides(blocks []byte, cx, cz, sectionIdx int) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	baseY := sectionIdx * 16
	for pos, stateID := range w.blocks {
		pcx, pcz := pos.X>>4, pos.Z>>4
		if pcx != cx || pcz != cz {
			continue
		}
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

// WriteChunkGrid writes a radius-based grid of chunks centered on (0,0).
func (w *World) WriteChunkGrid(writer interface{ Write([]byte) (int, error) }, radius int) error {
	for cx := -radius; cx <= radius; cx++ {
		for cz := -radius; cz <= radius; cz++ {
			chunk := w.EncodeChunk(cx, cz)
			if err := mcnet.WritePacket(writer, &chunk); err != nil {
				return err
			}
		}
	}
	return nil
}
