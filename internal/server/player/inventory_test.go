package player

import (
	"bytes"
	"testing"
)

func TestDefaultLoadout(t *testing.T) {
	inv := NewInventory()
	inv.DefaultLoadout()

	// Slot 0 should be diamond sword (276).
	if inv.Slots[0].BlockID != 276 {
		t.Errorf("expected diamond sword (276) in slot 0, got %d", inv.Slots[0].BlockID)
	}
	if inv.Slots[0].ItemCount != 1 {
		t.Errorf("expected count 1, got %d", inv.Slots[0].ItemCount)
	}

	// Remaining hotbar slots should be empty.
	for i := 1; i <= 8; i++ {
		if !inv.Slots[i].IsEmpty() {
			t.Errorf("expected slot %d to be empty, got %d", i, inv.Slots[i].BlockID)
		}
	}

	// Armor should be iron set.
	expectedArmor := []int16{309, 308, 307, 306} // boots, leggings, chestplate, helmet
	for i, expected := range expectedArmor {
		if inv.Armor[i].BlockID != expected {
			t.Errorf("armor[%d]: expected %d, got %d", i, expected, inv.Armor[i].BlockID)
		}
	}
}

func TestHeldItemAfterSetHeldSlot(t *testing.T) {
	inv := NewInventory()
	inv.DefaultLoadout()

	// Default held slot is 0 → diamond sword.
	held := inv.HeldItem()
	if held.BlockID != 276 {
		t.Errorf("expected held item 276, got %d", held.BlockID)
	}

	// Switch to empty slot 1.
	inv.SetHeldSlot(1)
	held = inv.HeldItem()
	if !held.IsEmpty() {
		t.Errorf("expected empty held item, got %d", held.BlockID)
	}

	// Switch back to slot 0.
	inv.SetHeldSlot(0)
	held = inv.HeldItem()
	if held.BlockID != 276 {
		t.Errorf("expected held item 276, got %d", held.BlockID)
	}
}

func TestRemoveOne(t *testing.T) {
	inv := NewInventory()
	inv.SetSlot(0, Slot{BlockID: 4, ItemCount: 3, ItemDamage: 0}) // 3 cobblestone

	removed := inv.RemoveOne(0)
	if removed.BlockID != 4 || removed.ItemCount != 1 {
		t.Errorf("expected removed item {4, 1, 0}, got {%d, %d, %d}", removed.BlockID, removed.ItemCount, removed.ItemDamage)
	}

	remaining := inv.Slots[0]
	if remaining.ItemCount != 2 {
		t.Errorf("expected 2 remaining, got %d", remaining.ItemCount)
	}

	// Remove until empty.
	inv.RemoveOne(0)
	last := inv.RemoveOne(0)
	if last.BlockID != 4 {
		t.Errorf("expected last removed item 4, got %d", last.BlockID)
	}

	if !inv.Slots[0].IsEmpty() {
		t.Errorf("expected slot 0 to be empty after removing all items")
	}

	// Removing from empty slot returns EmptySlot.
	empty := inv.RemoveOne(0)
	if !empty.IsEmpty() {
		t.Errorf("expected empty slot from RemoveOne on empty slot")
	}
}

func TestGetHeldSlot(t *testing.T) {
	inv := NewInventory()

	if inv.GetHeldSlot() != 0 {
		t.Errorf("expected initial held slot 0, got %d", inv.GetHeldSlot())
	}

	inv.SetHeldSlot(5)
	if inv.GetHeldSlot() != 5 {
		t.Errorf("expected held slot 5, got %d", inv.GetHeldSlot())
	}
}

func TestWriteSlotEmpty(t *testing.T) {
	var buf bytes.Buffer
	err := WriteSlot(&buf, EmptySlot)
	if err != nil {
		t.Fatalf("WriteSlot error: %v", err)
	}

	data := buf.Bytes()
	// Empty slot: just i16(-1) = 0xFF 0xFF
	if len(data) != 2 {
		t.Fatalf("expected 2 bytes for empty slot, got %d", len(data))
	}
	if data[0] != 0xFF || data[1] != 0xFF {
		t.Errorf("expected 0xFFFF for empty slot, got %02X%02X", data[0], data[1])
	}
}

func TestWriteSlotWithItem(t *testing.T) {
	var buf bytes.Buffer
	slot := Slot{BlockID: 276, ItemCount: 1, ItemDamage: 0}
	err := WriteSlot(&buf, slot)
	if err != nil {
		t.Fatalf("WriteSlot error: %v", err)
	}

	data := buf.Bytes()
	// i16 blockID (2) + i8 count (1) + i16 damage (2) + u8 nbt tag (1) = 6 bytes
	if len(data) != 6 {
		t.Fatalf("expected 6 bytes for item slot, got %d", len(data))
	}

	// blockID = 276 = 0x0114
	if data[0] != 0x01 || data[1] != 0x14 {
		t.Errorf("expected blockID 0x0114, got %02X%02X", data[0], data[1])
	}
	// count = 1
	if data[2] != 0x01 {
		t.Errorf("expected count 0x01, got %02X", data[2])
	}
	// damage = 0
	if data[3] != 0x00 || data[4] != 0x00 {
		t.Errorf("expected damage 0x0000, got %02X%02X", data[3], data[4])
	}
	// no NBT
	if data[5] != 0x00 {
		t.Errorf("expected NBT tag 0x00, got %02X", data[5])
	}
}
