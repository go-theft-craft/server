package conn

import (
	"testing"

	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/server/internal/server/player"
)

// clickPacket builds a decoded Window Click (0x0E) value the way the generated
// session hands it to the play handler: window 0, action 1, and the echoed
// clicked item carried as a generated Slot.
func clickPacket(t *testing.T, slot int16, button int8, mode int8, item player.Slot) *v1_8.PlayServerboundWindowClick {
	t.Helper()
	return &v1_8.PlayServerboundWindowClick{
		WindowID:    0,
		Slot:        slot,
		MouseButton: button,
		Action:      1,
		Mode:        mode,
		Item:        player.ToGeneratedSlot(item),
	}
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
	if !c.cursorSlot.Equal(log) {
		t.Fatalf("after pickup cursor = %+v, want %+v", c.cursorSlot, log)
	}

	// 2. Left-click grid slot 1 (top-left) to place the log.
	if err := c.handleWindowClick(clickPacket(t, 1, 0, 0, player.EmptySlot)); err != nil {
		t.Fatalf("place click: %v", err)
	}
	if got := c.craftingGrid[0]; !got.Equal(log) {
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
