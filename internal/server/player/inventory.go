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

// ReadSlots calls fn with copies of the current slots and armor under a read lock.
func (inv *Inventory) ReadSlots(fn func(slots [36]Slot, armor [4]Slot)) {
	inv.mu.RLock()
	defer inv.mu.RUnlock()
	fn(inv.Slots, inv.Armor)
}

// ApplyState replaces the entire inventory state. Must be called under the player's lock.
func (inv *Inventory) ApplyState(slots [36]Slot, armor [4]Slot, heldSlot int16) {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	inv.Slots = slots
	inv.Armor = armor
	inv.HeldSlot = heldSlot
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

// ToProtocolSlots maps the internal inventory layout to the 45-slot protocol
// Window 0 format. Crafting slots (0-4) are returned as empty since they live
// on the Connection, not the Inventory.
//
// Protocol layout:
//
//	0      = crafting output (not stored here)
//	1-4    = crafting grid   (not stored here)
//	5      = helmet   (Armor[3])
//	6      = chestplate (Armor[2])
//	7      = leggings (Armor[1])
//	8      = boots   (Armor[0])
//	9-35   = main inventory (Slots[9-35])
//	36-44  = hotbar (Slots[0-8])
func (inv *Inventory) ToProtocolSlots() [45]Slot {
	inv.mu.RLock()
	defer inv.mu.RUnlock()

	var proto [45]Slot
	// 0-4: crafting (empty, managed externally)
	for i := 0; i < 5; i++ {
		proto[i] = EmptySlot
	}
	// 5-8: armor (reverse order: helmet=5, chest=6, legs=7, boots=8)
	proto[5] = inv.Armor[3] // helmet
	proto[6] = inv.Armor[2] // chestplate
	proto[7] = inv.Armor[1] // leggings
	proto[8] = inv.Armor[0] // boots
	// 9-35: main inventory
	for i := 9; i <= 35; i++ {
		proto[i] = inv.Slots[i]
	}
	// 36-44: hotbar
	for i := 0; i < 9; i++ {
		proto[36+i] = inv.Slots[i]
	}
	return proto
}

// SetProtocolSlot writes a slot value using the protocol index (5-44).
// Indices 0-4 (crafting) are ignored since they are managed on Connection.
func (inv *Inventory) SetProtocolSlot(protoIndex int, slot Slot) {
	inv.mu.Lock()
	defer inv.mu.Unlock()

	switch {
	case protoIndex >= 36 && protoIndex <= 44:
		inv.Slots[protoIndex-36] = slot // hotbar
	case protoIndex >= 9 && protoIndex <= 35:
		inv.Slots[protoIndex] = slot // main
	case protoIndex == 5:
		inv.Armor[3] = slot // helmet
	case protoIndex == 6:
		inv.Armor[2] = slot // chestplate
	case protoIndex == 7:
		inv.Armor[1] = slot // leggings
	case protoIndex == 8:
		inv.Armor[0] = slot // boots
	}
}

// GetProtocolSlot reads a slot value using the protocol index (5-44).
// Indices 0-4 (crafting) return EmptySlot since they are managed on Connection.
func (inv *Inventory) GetProtocolSlot(protoIndex int) Slot {
	inv.mu.RLock()
	defer inv.mu.RUnlock()

	switch {
	case protoIndex >= 36 && protoIndex <= 44:
		return inv.Slots[protoIndex-36]
	case protoIndex >= 9 && protoIndex <= 35:
		return inv.Slots[protoIndex]
	case protoIndex == 5:
		return inv.Armor[3]
	case protoIndex == 6:
		return inv.Armor[2]
	case protoIndex == 7:
		return inv.Armor[1]
	case protoIndex == 8:
		return inv.Armor[0]
	default:
		return EmptySlot
	}
}

// GetSlot returns the slot at the given internal index (0-35).
func (inv *Inventory) GetSlot(index int) Slot {
	inv.mu.RLock()
	defer inv.mu.RUnlock()
	return inv.Slots[index]
}

// SetArmor sets an armor slot (0=boots, 1=leggings, 2=chestplate, 3=helmet).
func (inv *Inventory) SetArmor(index int, slot Slot) {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	inv.Armor[index] = slot
}

// GetArmor returns the armor slot at the given index.
func (inv *Inventory) GetArmor(index int) Slot {
	inv.mu.RLock()
	defer inv.mu.RUnlock()
	return inv.Armor[index]
}

// AddItem tries to insert an item into the inventory by merging into existing
// stacks first, then placing in empty slots. Scans hotbar (0-8) then main (9-35).
// Returns the leftover that didn't fit (or EmptySlot if fully absorbed).
func (inv *Inventory) AddItem(item Slot) Slot {
	if item.IsEmpty() {
		return EmptySlot
	}

	inv.mu.Lock()
	defer inv.mu.Unlock()

	remaining := int(item.ItemCount)

	// First pass: merge into existing stacks in hotbar, then main.
	for _, i := range inv.addItemOrder() {
		s := inv.Slots[i]
		if s.IsEmpty() || s.BlockID != item.BlockID || s.ItemDamage != item.ItemDamage {
			continue
		}
		space := 64 - int(s.ItemCount)
		if space <= 0 {
			continue
		}
		transfer := remaining
		if transfer > space {
			transfer = space
		}
		inv.Slots[i].ItemCount += int8(transfer)
		remaining -= transfer
		if remaining == 0 {
			return EmptySlot
		}
	}

	// Second pass: place in empty slots.
	for _, i := range inv.addItemOrder() {
		if !inv.Slots[i].IsEmpty() {
			continue
		}
		place := remaining
		if place > 64 {
			place = 64
		}
		inv.Slots[i] = Slot{BlockID: item.BlockID, ItemCount: int8(place), ItemDamage: item.ItemDamage}
		remaining -= place
		if remaining == 0 {
			return EmptySlot
		}
	}

	if remaining <= 0 {
		return EmptySlot
	}
	return Slot{BlockID: item.BlockID, ItemCount: int8(remaining), ItemDamage: item.ItemDamage}
}

// addItemOrder returns slot indices in the order: hotbar 0-8, then main 9-35.
func (inv *Inventory) addItemOrder() []int {
	order := make([]int, 36)
	for i := 0; i < 9; i++ {
		order[i] = i
	}
	for i := 9; i < 36; i++ {
		order[i] = i
	}
	return order
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
