package anvil

import (
	"bytes"
	"fmt"
	"slices"

	"github.com/go-theft-craft/server/pkg/world"
	"github.com/go-theft-craft/server/pkg/world/nbt"
)

// StateEncoder turns a state handle into the number a version's storage
// format writes. The Anvil format is Java 1.8's own, so the encoder is the
// same one that renders the wire.
type StateEncoder interface {
	EncodeState(world.State) (int32, error)
}

// ItemNamer resolves the name a stored item stack carries.
//
// Since Java 1.8 an item inside a tile entity is named by string — "minecraft:
// stone" — rather than by the numeric ID older versions wrote. The name is
// resolved through the version's item registry rather than assumed, because
// that is the one thing an external tool reading these files depends on.
type ItemNamer interface {
	ItemName(id int16) (string, bool)
	ItemID(name string) (int16, bool)
}

// Codec is everything the Anvil format needs that belongs to a version rather
// than to the format.
type Codec interface {
	StateEncoder
	StateDecoder
	ItemNamer
}

// chestTileEntityID is what vanilla calls a chest's tile entity.
const chestTileEntityID = "Chest"

// EncodeChunkNBT encodes a chunk as MC 1.8 NBT format.
//
// enc may be a plain StateEncoder, in which case containers are not written:
// naming an item needs the version's item registry, and a caller that has only
// a block-state encoder has no chests to write either.
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

		w.BeginListCompound()
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

	if err := writeTileEntities(w, c, enc); err != nil {
		return nil, err
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

// writeTileEntities writes the chunk's containers where vanilla keeps them.
//
// A chest is {id: "Chest", x, y, z, Items: [{Slot, id, Count, Damage}]}, in
// world coordinates, and only the slots holding something are listed — which
// is what vanilla does and what keeps an untouched chest cheap.
func writeTileEntities(w *nbt.Writer, c *world.Chunk, enc StateEncoder) error {
	namer, ok := enc.(ItemNamer)
	if !ok || len(c.Chests) == 0 {
		w.BeginList("TileEntities", nbt.TagCompound, 0)

		return nil
	}

	positions := make([]world.BlockPos, 0, len(c.Chests))
	for pos := range c.Chests {
		positions = append(positions, pos)
	}
	// Sorted, so two saves of the same chunk produce the same bytes.
	slices.SortFunc(positions, func(a, b world.BlockPos) int {
		if a.Y != b.Y {
			return a.Y - b.Y
		}
		if a.Z != b.Z {
			return a.Z - b.Z
		}

		return a.X - b.X
	})

	w.BeginList("TileEntities", nbt.TagCompound, int32(len(positions)))
	for _, pos := range positions {
		w.BeginListCompound()
		w.WriteString("id", chestTileEntityID)
		w.WriteInt("x", int32(pos.X))
		w.WriteInt("y", int32(pos.Y))
		w.WriteInt("z", int32(pos.Z))

		contents := c.Chests[pos]
		filled := make([]int, 0, len(contents))
		for slot, stack := range contents {
			if !stack.IsEmpty() {
				filled = append(filled, slot)
			}
		}

		w.BeginList("Items", nbt.TagCompound, int32(len(filled)))
		for _, slot := range filled {
			stack := contents[slot]
			name, ok := namer.ItemName(stack.BlockID)
			if !ok {
				return fmt.Errorf("anvil: chunk %v chest %v slot %d: no item is numbered %d",
					c.Pos, pos, slot, stack.BlockID)
			}
			w.BeginListCompound()
			w.WriteTagByte("Slot", byte(slot))
			w.WriteString("id", name)
			w.WriteTagByte("Count", byte(stack.ItemCount))
			w.WriteShort("Damage", stack.ItemDamage)
			w.EndCompound()
		}

		w.EndCompound()
	}

	return nil
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
