package conn

import (
	"encoding/binary"
	"testing"

	"github.com/go-theft-craft/server/internal/server/player"
)

// clickPacket builds a 1.8.9 Window Click (0x0E) payload:
// windowId u8, slot i16, mouseButton i8, action i16, mode i8, item slot.
// The item slot is blockId i16, and (when not -1) count i8, damage i16,
// then a 0x00 optional-NBT marker.
func clickPacket(t *testing.T, slot int16, button int8, mode int8, item player.Slot) []byte {
	t.Helper()
	var b []byte
	b = append(b, 0) // window 0
	var s2 [2]byte
	binary.BigEndian.PutUint16(s2[:], uint16(slot))
	b = append(b, s2[:]...)
	b = append(b, byte(button))
	var a2 [2]byte
	binary.BigEndian.PutUint16(a2[:], 1)
	b = append(b, a2[:]...)
	b = append(b, byte(mode))
	// item
	var id2 [2]byte
	binary.BigEndian.PutUint16(id2[:], uint16(item.BlockID))
	b = append(b, id2[:]...)
	if item.BlockID != -1 {
		b = append(b, byte(item.ItemCount))
		var d2 [2]byte
		binary.BigEndian.PutUint16(d2[:], uint16(item.ItemDamage))
		b = append(b, d2[:]...)
		b = append(b, 0x00) // optional NBT = absent
	}
	return b
}

// TestClientFlow_PlaceAndCraft reproduces a real client session: pick a log
// out of the hotbar, drop it into the 2x2 grid, and take the result.
func TestClientFlow_PlaceAndCraft(t *testing.T) {
	c := newInventoryTestConn(t)

	log := craftItem(17, 0, 64)
	c.setWindowSlot(36, log) // hotbar slot 0

	// 1. Left-click the hotbar to pick the log onto the cursor.
	if err := c.handleWindowClick(clickPacket(t, 36, 0, 0, log)); err != nil {
		t.Fatalf("pickup click: %v", err)
	}
	if c.cursorSlot != log {
		t.Fatalf("after pickup cursor = %+v, want %+v", c.cursorSlot, log)
	}

	// 2. Left-click grid slot 1 (top-left) to place the log.
	if err := c.handleWindowClick(clickPacket(t, 1, 0, 0, player.EmptySlot)); err != nil {
		t.Fatalf("place click: %v", err)
	}
	if got := c.craftingGrid[0]; got != log {
		t.Fatalf("grid[0] = %+v, want %+v", got, log)
	}
	if got := c.craftingOutput; got.BlockID != 5 || got.ItemCount != 4 {
		t.Fatalf("craftingOutput = %+v, want 4 oak planks", got)
	}

	// 3. Left-click the output to take the planks.
	if err := c.handleWindowClick(clickPacket(t, 0, 0, 0, player.EmptySlot)); err != nil {
		t.Fatalf("output click: %v", err)
	}
	if c.cursorSlot.BlockID != 5 || c.cursorSlot.ItemCount != 4 {
		t.Fatalf("cursor after output click = %+v, want 4 oak planks", c.cursorSlot)
	}
}
