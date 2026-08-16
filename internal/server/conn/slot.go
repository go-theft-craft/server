package conn

import (
	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"
)

// Slot represents a Minecraft inventory slot.
type Slot struct {
	BlockID    int16
	ItemCount  int8
	ItemDamage int16
}

// slotFromGenerated converts a decoded generated protocol 47 Slot into the
// connection's Slot. An empty slot (BlockID -1) carries no count or damage; the
// generated model leaves those switch fields zero, which this preserves.
func slotFromGenerated(s v1_8.Slot) Slot {
	if s.BlockID == -1 {
		return Slot{BlockID: -1}
	}
	return Slot{
		BlockID:    s.BlockID,
		ItemCount:  s.AnonymousSwitch1.Default.ItemCount,
		ItemDamage: s.AnonymousSwitch1.Default.ItemDamage,
	}
}
