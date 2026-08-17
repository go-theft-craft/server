package anvil

import (
	"bytes"
	"fmt"

	"github.com/go-theft-craft/server/pkg/world"
	"github.com/go-theft-craft/server/pkg/world/nbt"
)

// StateEncoder turns a state handle into the number a version's storage
// format writes. The Anvil format is Java 1.8's own, so the encoder is the
// same one that renders the wire.
type StateEncoder interface {
	EncodeState(world.State) (int32, error)
}

// EncodeChunkNBT encodes a chunk as MC 1.8 NBT format.
func EncodeChunkNBT(c *world.Chunk, enc StateEncoder) ([]byte, error) {
	if c == nil {
		return nil, fmt.Errorf("anvil: nil chunk")
	}

	var buf bytes.Buffer
	w := nbt.NewWriter(&buf)

	w.BeginCompound("")
	w.BeginCompound("Level")

	w.WriteInt("xPos", int32(c.Pos.X))
	w.WriteInt("zPos", int32(c.Pos.Z))
	w.WriteTagByte("TerrainPopulated", 1)
	w.WriteLong("LastUpdate", 0)

	var sectionCount int32
	for _, sec := range c.Sections {
		if sec != nil {
			sectionCount++
		}
	}

	w.BeginList("Sections", nbt.TagCompound, sectionCount)

	for secY, sec := range c.Sections {
		if sec == nil {
			continue
		}

		states := sec.States()
		blocks := make([]byte, len(states))
		data := make([]byte, len(states)/2)
		add := make([]byte, len(states)/2)
		hasAdd := false

		for i, s := range states {
			v, err := enc.EncodeState(s)
			if err != nil {
				return nil, fmt.Errorf("anvil: chunk %v section %d: %w", c.Pos, secY, err)
			}
			state := uint16(v)
			blockID := state >> 4
			blocks[i] = byte(blockID)
			setNibble(data, i, byte(state&0xF))
			if blockID > 255 {
				hasAdd = true
				setNibble(add, i, byte(state>>12))
			}
		}

		w.BeginCompound("")
		w.WriteTagByte("Y", byte(secY))
		w.WriteByteArray("Blocks", blocks)

		if hasAdd {
			w.WriteByteArray("Add", add)
		}

		w.WriteByteArray("Data", data)

		// Full brightness.
		light := make([]byte, len(states)/2)
		for i := range light {
			light[i] = 0xFF
		}
		w.WriteByteArray("BlockLight", light)

		skyLight := make([]byte, len(states)/2)
		for i := range skyLight {
			skyLight[i] = 0xFF
		}
		w.WriteByteArray("SkyLight", skyLight)

		w.EndCompound()
	}

	biomes := make([]byte, len(c.Biomes))
	for i, b := range c.Biomes {
		biomes[i] = byte(b)
	}
	w.WriteByteArray("Biomes", biomes)

	heightMap, err := computeHeightMap(c, enc)
	if err != nil {
		return nil, err
	}
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
func computeHeightMap(c *world.Chunk, enc StateEncoder) ([]int32, error) {
	hm := make([]int32, 256)

	for secY := len(c.Sections) - 1; secY >= 0; secY-- {
		sec := c.Sections[secY]
		if sec == nil {
			continue
		}
		states := sec.States()
		for localY := 15; localY >= 0; localY-- {
			for z := range 16 {
				for x := range 16 {
					idx := z*16 + x
					y := int32(secY*16 + localY)
					if hm[idx] > y {
						continue
					}
					v, err := enc.EncodeState(states[world.SectionBlockIndex(x, localY, z)])
					if err != nil {
						return nil, fmt.Errorf("anvil: chunk %v: %w", c.Pos, err)
					}
					if v != 0 && y+1 > hm[idx] {
						hm[idx] = y + 1
					}
				}
			}
		}
	}

	return hm, nil
}
