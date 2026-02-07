package world

import (
	"encoding/binary"

	"github.com/OCharnyshevich/minecraft-server/internal/server/net"
	"github.com/OCharnyshevich/minecraft-server/internal/server/packet"
)

const (
	sectionBlockBytes = 16 * 16 * 16 * 2 // 8192 bytes: 4096 blocks × 2 bytes each
	sectionLightBytes = 16 * 16 * 16 / 2 // 2048 bytes: 4096 nibbles
	biomeBytes        = 256              // 16×16 biome IDs

	blockStone = 1
	biomePlain = 1
)

// FlatStoneChunk generates a ChunkData packet for a flat stone world.
// The chunk has stone at y=0 and air everywhere else.
func FlatStoneChunk(chunkX, chunkZ int32) packet.ChunkData {
	// Section 0 only (y=0..15)
	// Block data: 4096 blocks × 2 bytes = 8192 bytes (LE u16: blockId<<4 | meta)
	blocks := make([]byte, sectionBlockBytes)
	for x := 0; x < 16; x++ {
		for z := 0; z < 16; z++ {
			// y=0: stone (id=1, meta=0) → (1<<4)|0 = 0x10
			idx := (0*256 + z*16 + x) * 2
			binary.LittleEndian.PutUint16(blocks[idx:], blockStone<<4)
			// y>0: air (0x0000) — already zero
		}
	}

	// Block light: all 0xFF (full light)
	blockLight := make([]byte, sectionLightBytes)
	for i := range blockLight {
		blockLight[i] = 0xFF
	}

	// Sky light: all 0xFF
	skyLight := make([]byte, sectionLightBytes)
	for i := range skyLight {
		skyLight[i] = 0xFF
	}

	// Biome data: all plains
	biomes := make([]byte, biomeBytes)
	for i := range biomes {
		biomes[i] = biomePlain
	}

	// Combine: blocks + blockLight + skyLight + biomes
	dataLen := len(blocks) + len(blockLight) + len(skyLight) + len(biomes)
	data := make([]byte, 0, dataLen)
	data = append(data, blocks...)
	data = append(data, blockLight...)
	data = append(data, skyLight...)
	data = append(data, biomes...)

	return packet.ChunkData{
		ChunkX:         chunkX,
		ChunkZ:         chunkZ,
		GroundUp:       true,
		PrimaryBitMask: 0x0001, // only section 0
		Data:           data,
	}
}

// WriteChunkGrid writes a 7×7 grid of flat stone chunks centered on (0,0).
func WriteChunkGrid(w interface{ Write([]byte) (int, error) }) error {
	for cx := int32(-3); cx <= 3; cx++ {
		for cz := int32(-3); cz <= 3; cz++ {
			chunk := FlatStoneChunk(cx, cz)
			if err := net.WritePacket(w, &chunk); err != nil {
				return err
			}
		}
	}
	return nil
}
