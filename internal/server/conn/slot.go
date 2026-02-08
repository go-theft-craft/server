package conn

import (
	"fmt"
	"io"

	mcnet "github.com/go-theft-craft/server/pkg/protocol"
)

// Slot represents a Minecraft inventory slot.
type Slot struct {
	BlockID    int16
	ItemCount  int8
	ItemDamage int16
}

// readSlot reads a slot from the given reader.
// If BlockID is -1, the slot is empty.
func readSlot(r io.Reader) (Slot, error) {
	blockID, err := mcnet.ReadI16(r)
	if err != nil {
		return Slot{}, fmt.Errorf("read slot block id: %w", err)
	}

	if blockID == -1 {
		return Slot{BlockID: -1}, nil
	}

	count, err := mcnet.ReadI8(r)
	if err != nil {
		return Slot{}, fmt.Errorf("read slot count: %w", err)
	}

	damage, err := mcnet.ReadI16(r)
	if err != nil {
		return Slot{}, fmt.Errorf("read slot damage: %w", err)
	}

	// NBT data: read tag type byte. If 0x00, no NBT follows.
	nbtTag, err := mcnet.ReadU8(r)
	if err != nil {
		return Slot{}, fmt.Errorf("read slot nbt tag: %w", err)
	}

	if nbtTag != 0x00 {
		// Skip remaining NBT data by reading until end.
		// For simplicity in a creative-mode server, we consume remaining bytes.
		_, _ = io.ReadAll(r)
	}

	return Slot{
		BlockID:    blockID,
		ItemCount:  count,
		ItemDamage: damage,
	}, nil
}
