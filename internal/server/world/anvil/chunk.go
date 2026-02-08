package anvil

import (
	"bytes"

	"github.com/go-theft-craft/server/internal/server/world"
	"github.com/go-theft-craft/server/internal/server/world/gen"
	"github.com/go-theft-craft/server/internal/server/world/nbt"
)

// EncodeChunkNBT encodes a chunk as MC 1.8 NBT format.
// overrides contains block overrides for this chunk only (pre-filtered by caller).
func EncodeChunkNBT(cx, cz int, chunk *gen.ChunkData, overrides map[world.BlockPos]int32) ([]byte, error) {
	var buf bytes.Buffer
	w := nbt.NewWriter(&buf)

	w.BeginCompound("")
	w.BeginCompound("Level")

	w.WriteInt("xPos", int32(cx))
	w.WriteInt("zPos", int32(cz))
	w.WriteTagByte("TerrainPopulated", 1)
	w.WriteLong("LastUpdate", 0)

	// Count non-nil sections.
	var sectionCount int32
	for i := 0; i < 16; i++ {
		if chunk.Sections[i] != nil {
			sectionCount++
		}
	}

	// Also count sections that have overrides but no base section.
	overrideSections := make(map[int]bool)
	for pos := range overrides {
		sec := pos.Y >> 4
		if sec >= 0 && sec < 16 && chunk.Sections[sec] == nil {
			if !overrideSections[sec] {
				overrideSections[sec] = true
				sectionCount++
			}
		}
	}

	w.BeginList("Sections", nbt.TagCompound, sectionCount)

	for secY := 0; secY < 16; secY++ {
		sec := chunk.Sections[secY]
		if sec == nil && !overrideSections[secY] {
			continue
		}

		blocks := make([]byte, 4096)
		data := make([]byte, 2048)
		hasAdd := false

		// Fill from base section data.
		if sec != nil {
			for i := 0; i < 4096; i++ {
				state := sec.Blocks[i]
				blockID := state >> 4
				meta := state & 0xF

				blocks[i] = byte(blockID)
				if blockID > 255 {
					hasAdd = true
				}
				setNibble(data, i, byte(meta))
			}
		}

		// Apply overrides for this section.
		baseY := secY << 4
		for pos, stateID := range overrides {
			if pos.Y>>4 != secY {
				continue
			}
			lx := pos.X & 0xF
			ly := pos.Y - baseY
			lz := pos.Z & 0xF
			i := ly*256 + lz*16 + lx

			blockID := uint16(stateID) >> 4
			meta := byte(stateID) & 0xF

			blocks[i] = byte(blockID)
			if blockID > 255 {
				hasAdd = true
			}
			setNibble(data, i, meta)
		}

		w.BeginCompound("")
		w.WriteTagByte("Y", byte(secY))
		w.WriteByteArray("Blocks", blocks)

		if hasAdd {
			add := make([]byte, 2048)
			if sec != nil {
				for i := 0; i < 4096; i++ {
					state := sec.Blocks[i]
					setNibble(add, i, byte(state>>12))
				}
			}
			// Re-apply overrides for Add nibbles.
			for pos, stateID := range overrides {
				if pos.Y>>4 != secY {
					continue
				}
				lx := pos.X & 0xF
				ly := pos.Y - baseY
				lz := pos.Z & 0xF
				i := ly*256 + lz*16 + lx
				setNibble(add, i, byte(uint16(stateID)>>12))
			}
			w.WriteByteArray("Add", add)
		}

		w.WriteByteArray("Data", data)

		// Full brightness.
		light := make([]byte, 2048)
		for i := range light {
			light[i] = 0xFF
		}
		w.WriteByteArray("BlockLight", light)

		skyLight := make([]byte, 2048)
		for i := range skyLight {
			skyLight[i] = 0xFF
		}
		w.WriteByteArray("SkyLight", skyLight)

		w.EndCompound()
	}

	// Biomes.
	w.WriteByteArray("Biomes", chunk.Biomes[:])

	// HeightMap.
	heightMap := computeHeightMap(chunk, overrides)
	w.WriteIntArray("HeightMap", heightMap)

	w.EndCompound() // Level
	w.EndCompound() // root

	if w.Err() != nil {
		return nil, w.Err()
	}

	return buf.Bytes(), nil
}

// setNibble sets a 4-bit value at the given block index in a nibble array.
func setNibble(arr []byte, index int, val byte) {
	byteIdx := index / 2
	if index%2 == 0 {
		arr[byteIdx] = (arr[byteIdx] & 0xF0) | (val & 0x0F)
	} else {
		arr[byteIdx] = (arr[byteIdx] & 0x0F) | ((val & 0x0F) << 4)
	}
}

// computeHeightMap calculates the highest non-air block for each x,z column.
func computeHeightMap(chunk *gen.ChunkData, overrides map[world.BlockPos]int32) []int32 {
	hm := make([]int32, 256)

	for z := 0; z < 16; z++ {
		for x := 0; x < 16; x++ {
			highest := int32(0)
			for y := 255; y >= 0; y-- {
				state := chunk.GetBlock(x, y, z)
				if state != 0 {
					highest = int32(y + 1)
					break
				}
			}
			hm[z*16+x] = highest
		}
	}

	// Adjust for overrides.
	for pos, stateID := range overrides {
		lx := pos.X & 0xF
		lz := pos.Z & 0xF
		idx := lz*16 + lx
		if stateID != 0 && int32(pos.Y+1) > hm[idx] {
			hm[idx] = int32(pos.Y + 1)
		}
	}

	return hm
}
