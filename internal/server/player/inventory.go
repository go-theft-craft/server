package player

import (
	"encoding/binary"
	"io"
	"sync"
)

// Slot represents a Minecraft inventory slot.
type Slot struct {
	BlockID    int16 // -1 = empty
	ItemCount  int8
	ItemDamage int16
}

// EmptySlot is a convenience value for an empty slot.
var EmptySlot = Slot{BlockID: -1}

// IsEmpty returns true if the slot contains no item.
func (s Slot) IsEmpty() bool {
	return s.BlockID == -1
}

// Inventory holds a player's hotbar, main inventory, and armor.
type Inventory struct {
	mu       sync.RWMutex
	Slots    [36]Slot // hotbar 0-8, main 9-35
	Armor    [4]Slot  // boots=0, leggings=1, chestplate=2, helmet=3
	HeldSlot int16    // 0-8
}

// NewInventory creates an empty inventory.
func NewInventory() *Inventory {
	inv := &Inventory{}
	for i := range inv.Slots {
		inv.Slots[i] = EmptySlot
	}
	for i := range inv.Armor {
		inv.Armor[i] = EmptySlot
	}
	return inv
}

// HeldItem returns the slot currently in the player's hand.
func (inv *Inventory) HeldItem() Slot {
	inv.mu.RLock()
	defer inv.mu.RUnlock()
	return inv.Slots[inv.HeldSlot]
}

// SetHeldSlot changes which hotbar slot is selected (0-8).
func (inv *Inventory) SetHeldSlot(slot int16) {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	inv.HeldSlot = slot
}

// GetHeldSlot returns the currently selected hotbar slot index.
func (inv *Inventory) GetHeldSlot() int16 {
	inv.mu.RLock()
	defer inv.mu.RUnlock()
	return inv.HeldSlot
}

// SetSlot sets the contents of a hotbar/main slot.
func (inv *Inventory) SetSlot(index int, slot Slot) {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	inv.Slots[index] = slot
}

// RemoveOne decrements the count of the given slot by 1 and returns the
// removed item (count=1). If the slot becomes empty, it is set to EmptySlot.
func (inv *Inventory) RemoveOne(index int) Slot {
	inv.mu.Lock()
	defer inv.mu.Unlock()

	s := inv.Slots[index]
	if s.IsEmpty() {
		return EmptySlot
	}

	removed := Slot{
		BlockID:    s.BlockID,
		ItemCount:  1,
		ItemDamage: s.ItemDamage,
	}

	s.ItemCount--
	if s.ItemCount <= 0 {
		inv.Slots[index] = EmptySlot
	} else {
		inv.Slots[index] = s
	}

	return removed
}

// DefaultLoadout fills the inventory with a creative-mode starter kit.
func (inv *Inventory) DefaultLoadout() {
	inv.mu.Lock()
	defer inv.mu.Unlock()

	// Diamond sword in slot 0
	inv.Slots[0] = Slot{BlockID: 276, ItemCount: 1, ItemDamage: 0}

	// Iron armor
	inv.Armor[0] = Slot{BlockID: 309, ItemCount: 1, ItemDamage: 0} // iron boots
	inv.Armor[1] = Slot{BlockID: 308, ItemCount: 1, ItemDamage: 0} // iron leggings
	inv.Armor[2] = Slot{BlockID: 307, ItemCount: 1, ItemDamage: 0} // iron chestplate
	inv.Armor[3] = Slot{BlockID: 306, ItemCount: 1, ItemDamage: 0} // iron helmet
}

// WriteSlot writes a slot in the Minecraft protocol format.
func WriteSlot(w io.Writer, s Slot) error {
	if err := binary.Write(w, binary.BigEndian, s.BlockID); err != nil {
		return err
	}
	if s.BlockID == -1 {
		return nil
	}
	if err := binary.Write(w, binary.BigEndian, s.ItemCount); err != nil {
		return err
	}
	if err := binary.Write(w, binary.BigEndian, s.ItemDamage); err != nil {
		return err
	}
	// No NBT data
	_, err := w.Write([]byte{0x00})
	return err
}
