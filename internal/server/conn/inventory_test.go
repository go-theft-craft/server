package conn

import (
	"testing"

	"github.com/go-theft-craft/server/internal/server/player"
)

func stone(count int8) player.Slot {
	return player.Slot{BlockID: 1, ItemCount: count, ItemDamage: 0}
}

func dirt(count int8) player.Slot {
	return player.Slot{BlockID: 3, ItemCount: count, ItemDamage: 0}
}

func sword() player.Slot {
	return player.Slot{BlockID: 276, ItemCount: 1, ItemDamage: 0}
}

func ironHelmet() player.Slot {
	return player.Slot{BlockID: 306, ItemCount: 1, ItemDamage: 0}
}

// --- Protocol Slot Mapping Tests ---

func TestToProtocolSlots_Hotbar(t *testing.T) {
	inv := player.NewInventory()
	inv.SetSlot(0, sword()) // hotbar slot 0

	proto := inv.ToProtocolSlots()

	// Hotbar slot 0 maps to protocol slot 36.
	if proto[36] != sword() {
		t.Errorf("expected sword at proto 36, got %+v", proto[36])
	}
	// Protocol slot 0 (crafting output) should be empty.
	if !proto[0].IsEmpty() {
		t.Errorf("expected empty at proto 0, got %+v", proto[0])
	}
}

func TestToProtocolSlots_Armor(t *testing.T) {
	inv := player.NewInventory()
	inv.SetArmor(3, ironHelmet()) // helmet = Armor[3]

	proto := inv.ToProtocolSlots()

	// Helmet maps to protocol slot 5.
	if proto[5] != ironHelmet() {
		t.Errorf("expected iron helmet at proto 5, got %+v", proto[5])
	}
}

func TestSetGetProtocolSlot_RoundTrip(t *testing.T) {
	inv := player.NewInventory()
	s := stone(32)

	// Set via protocol index, read back.
	inv.SetProtocolSlot(36, s) // hotbar 0
	got := inv.GetProtocolSlot(36)
	if got != s {
		t.Errorf("expected %+v, got %+v", s, got)
	}

	// Verify internal: hotbar 0 = Slots[0].
	if inv.GetSlot(0) != s {
		t.Errorf("internal Slots[0] expected %+v, got %+v", s, inv.GetSlot(0))
	}
}

func TestSetGetProtocolSlot_Armor(t *testing.T) {
	inv := player.NewInventory()
	h := ironHelmet()

	inv.SetProtocolSlot(5, h) // helmet = proto 5 = Armor[3]
	got := inv.GetProtocolSlot(5)
	if got != h {
		t.Errorf("expected %+v, got %+v", h, got)
	}

	if inv.GetArmor(3) != h {
		t.Errorf("internal Armor[3] expected %+v, got %+v", h, inv.GetArmor(3))
	}
}

// --- Click Mode Tests ---

func newInventoryTestConn() *Connection {
	c, _, _ := newTestConn("Alice")
	// Clear default loadout for clean tests.
	for i := 0; i < 36; i++ {
		c.self.Inventory.SetSlot(i, player.EmptySlot)
	}
	for i := 0; i < 4; i++ {
		c.self.Inventory.SetArmor(i, player.EmptySlot)
	}
	return c
}

func TestNormalClick_PickupAndPlace(t *testing.T) {
	c := newInventoryTestConn()

	// Put stone in hotbar slot 0 (proto 36).
	c.setWindowSlot(36, stone(32))

	// Left-click to pick up.
	c.handleNormalClick(36, 0)
	if c.cursorSlot != stone(32) {
		t.Errorf("cursor should have stone(32), got %+v", c.cursorSlot)
	}
	if !c.getWindowSlot(36).IsEmpty() {
		t.Errorf("slot 36 should be empty after pickup")
	}

	// Left-click empty slot to place.
	c.handleNormalClick(37, 0)
	if !c.cursorSlot.IsEmpty() {
		t.Errorf("cursor should be empty after placing, got %+v", c.cursorSlot)
	}
	if c.getWindowSlot(37) != stone(32) {
		t.Errorf("slot 37 should have stone(32), got %+v", c.getWindowSlot(37))
	}
}

func TestNormalClick_Swap(t *testing.T) {
	c := newInventoryTestConn()
	c.setWindowSlot(36, stone(10))
	c.cursorSlot = dirt(5)

	// Left-click on slot with different item: swap.
	c.handleNormalClick(36, 0)
	if c.cursorSlot != stone(10) {
		t.Errorf("cursor should have stone(10), got %+v", c.cursorSlot)
	}
	if c.getWindowSlot(36) != dirt(5) {
		t.Errorf("slot 36 should have dirt(5), got %+v", c.getWindowSlot(36))
	}
}

func TestNormalClick_Merge(t *testing.T) {
	c := newInventoryTestConn()
	c.setWindowSlot(36, stone(10))
	c.cursorSlot = stone(20)

	// Left-click on same item: merge.
	c.handleNormalClick(36, 0)
	if c.getWindowSlot(36) != stone(30) {
		t.Errorf("slot 36 should have stone(30), got %+v", c.getWindowSlot(36))
	}
	if !c.cursorSlot.IsEmpty() {
		t.Errorf("cursor should be empty, got %+v", c.cursorSlot)
	}
}

func TestNormalClick_RightHalfPickup(t *testing.T) {
	c := newInventoryTestConn()
	c.setWindowSlot(36, stone(10))

	// Right-click to pick up half.
	c.handleNormalClick(36, 1)
	if c.cursorSlot != stone(5) {
		t.Errorf("cursor should have stone(5), got %+v", c.cursorSlot)
	}
	if c.getWindowSlot(36) != stone(5) {
		t.Errorf("slot 36 should have stone(5), got %+v", c.getWindowSlot(36))
	}
}

func TestNormalClick_RightPlaceOne(t *testing.T) {
	c := newInventoryTestConn()
	c.cursorSlot = stone(10)

	// Right-click on empty slot: place one.
	c.handleNormalClick(36, 1)
	if c.cursorSlot != stone(9) {
		t.Errorf("cursor should have stone(9), got %+v", c.cursorSlot)
	}
	if c.getWindowSlot(36) != stone(1) {
		t.Errorf("slot 36 should have stone(1), got %+v", c.getWindowSlot(36))
	}
}

func TestShiftClick_HotbarToMain(t *testing.T) {
	c := newInventoryTestConn()
	c.setWindowSlot(36, stone(10)) // hotbar

	c.handleShiftClick(36, 0)
	if !c.getWindowSlot(36).IsEmpty() {
		t.Errorf("slot 36 should be empty, got %+v", c.getWindowSlot(36))
	}
	// Should be in main inventory (9-35).
	found := false
	for s := int16(9); s <= 35; s++ {
		if c.getWindowSlot(s) == stone(10) {
			found = true
			break
		}
	}
	if !found {
		t.Error("stone should have moved to main inventory")
	}
}

func TestShiftClick_MainToHotbar(t *testing.T) {
	c := newInventoryTestConn()
	c.setWindowSlot(9, stone(10)) // main

	c.handleShiftClick(9, 0)
	if !c.getWindowSlot(9).IsEmpty() {
		t.Errorf("slot 9 should be empty, got %+v", c.getWindowSlot(9))
	}
	// Should be in hotbar (36-44).
	found := false
	for s := int16(36); s <= 44; s++ {
		if c.getWindowSlot(s) == stone(10) {
			found = true
			break
		}
	}
	if !found {
		t.Error("stone should have moved to hotbar")
	}
}

func TestNumberKey_Swap(t *testing.T) {
	c := newInventoryTestConn()
	c.setWindowSlot(9, stone(10)) // main inv slot
	c.setWindowSlot(36, dirt(5))  // hotbar 0

	// Press number key 1 (button=0) while hovering slot 9.
	c.handleNumberKey(9, 0)
	if c.getWindowSlot(9) != dirt(5) {
		t.Errorf("slot 9 should have dirt(5), got %+v", c.getWindowSlot(9))
	}
	if c.getWindowSlot(36) != stone(10) {
		t.Errorf("slot 36 should have stone(10), got %+v", c.getWindowSlot(36))
	}
}

func TestMiddleClick_CreativeClone(t *testing.T) {
	c := newInventoryTestConn()
	c.setWindowSlot(36, stone(1))

	c.handleMiddleClick(36)
	if c.cursorSlot.BlockID != 1 || c.cursorSlot.ItemCount != 64 {
		t.Errorf("expected stone(64) on cursor, got %+v", c.cursorSlot)
	}
}

func TestDropClick_DropOne(t *testing.T) {
	c := newInventoryTestConn()
	c.setWindowSlot(36, stone(10))

	c.handleDropClick(36, 0) // Q key, drop one
	if c.getWindowSlot(36) != stone(9) {
		t.Errorf("slot should have 9 stone, got %+v", c.getWindowSlot(36))
	}
}

func TestDropClick_DropStack(t *testing.T) {
	c := newInventoryTestConn()
	c.setWindowSlot(36, stone(10))

	c.handleDropClick(36, 1) // Ctrl+Q, drop stack
	if !c.getWindowSlot(36).IsEmpty() {
		t.Errorf("slot should be empty, got %+v", c.getWindowSlot(36))
	}
}

func TestDoubleClick_Collect(t *testing.T) {
	c := newInventoryTestConn()
	c.setWindowSlot(36, stone(10))
	c.setWindowSlot(37, stone(20))
	c.setWindowSlot(9, stone(5))
	c.cursorSlot = stone(1)

	c.handleDoubleClick(36)
	// Should collect stone from all slots up to 64.
	if c.cursorSlot.ItemCount != 36 { // 1 + 10 + 20 + 5 = 36
		t.Errorf("expected 36 stone on cursor, got %d", c.cursorSlot.ItemCount)
	}
}

func TestWindowItems_NonZero(t *testing.T) {
	c := newInventoryTestConn()
	c.setWindowSlot(36, sword())
	rec := c.rw.(*packetRecorder)
	rec.buf.Reset()

	err := c.sendWindowItems()
	if err != nil {
		t.Fatalf("sendWindowItems error: %v", err)
	}
	if rec.buf.Len() == 0 {
		t.Error("expected WindowItems packet, got nothing")
	}
}

func TestCloseWindow_ReturnsCraftingItems(t *testing.T) {
	c := newInventoryTestConn()
	c.craftingGrid[0] = stone(5)
	c.craftingGrid[1] = dirt(3)

	_ = c.handleCloseWindow([]byte{0}) // window ID 0

	// Crafting grid should be empty.
	for i := 0; i < 4; i++ {
		if !c.craftingGrid[i].IsEmpty() {
			t.Errorf("crafting grid[%d] should be empty, got %+v", i, c.craftingGrid[i])
		}
	}

	// Items should have been placed in inventory.
	foundStone, foundDirt := false, false
	for s := int16(9); s <= 44; s++ {
		item := c.getWindowSlot(s)
		if item.BlockID == 1 && item.ItemCount == 5 {
			foundStone = true
		}
		if item.BlockID == 3 && item.ItemCount == 3 {
			foundDirt = true
		}
	}
	if !foundStone {
		t.Error("stone(5) should be in inventory after closing window")
	}
	if !foundDirt {
		t.Error("dirt(3) should be in inventory after closing window")
	}
}

func TestArmorSlotForItem(t *testing.T) {
	cases := []struct {
		blockID int16
		want    int16
	}{
		{306, 5},  // iron helmet
		{307, 6},  // iron chestplate
		{308, 7},  // iron leggings
		{309, 8},  // iron boots
		{310, 5},  // diamond helmet
		{1, -1},   // stone (not armor)
		{276, -1}, // diamond sword (not armor)
	}
	for _, tc := range cases {
		got := armorSlotForItem(tc.blockID)
		if got != tc.want {
			t.Errorf("armorSlotForItem(%d) = %d, want %d", tc.blockID, got, tc.want)
		}
	}
}
