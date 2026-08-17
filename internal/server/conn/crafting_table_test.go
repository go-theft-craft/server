package conn

import (
	"testing"

	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/server/internal/server/packet"
	"github.com/go-theft-craft/server/internal/server/player"
)

const (
	cobblestoneID = 4
	furnaceID     = 61
)

// openTableAt puts a crafting table in the world and right-clicks it, which is
// how a player opens the 3x3 grid.
func openTableAt(t *testing.T, c *Connection, x, y, z int) {
	t.Helper()

	setBlockID(t, c, x, y, z, int32(craftingTableBlockID)<<4)
	click := placeOnTopOf(x, y, z, player.EmptySlot)
	if err := c.handleBlockPlace(click); err != nil {
		t.Fatalf("handleBlockPlace: %v", err)
	}
}

// The 1.8 client only draws the workbench GUI when OpenWindow reports no
// slots: `!packetIn.hasSlots()` is what picks the crafting branch, and any
// positive count falls through to the generic container path, which draws that
// many slots as a chest. Advertising nine drew one row of nine.
func TestCraftingTable_OpenWindowAdvertisesNoSlots(t *testing.T) {
	if tableAdvertisedSlots != 0 {
		t.Errorf("advertised slots = %d, want 0 so the client draws a workbench", tableAdvertisedSlots)
	}
}

func TestCraftingTable_RightClickOpensTheWindow(t *testing.T) {
	c := newInventoryTestConn(t)

	openTableAt(t, c, 0, 4, 0)

	if c.windowID == 0 {
		t.Fatal("windowID = 0, want a crafting table window open")
	}
	if got := c.layout().gridSize; got != 3 {
		t.Errorf("grid size = %d, want 3", got)
	}
	if got := c.layout().hotbarEnd; got != tableHotbarEnd {
		t.Errorf("hotbar ends at %d, want %d", got, tableHotbarEnd)
	}
}

// Sneaking is how a player builds against a table instead of opening it.
func TestCraftingTable_SneakingPlacesInstead(t *testing.T) {
	c := newInventoryTestConn(t)
	c.self.SetGameMode(packet.GameModeSurvival)
	c.self.Inventory.SetSlot(0, dirt(10))
	c.self.Inventory.SetHeldSlot(0)
	c.self.SetSneaking(true)

	setBlockID(t, c, 0, 4, 0, int32(craftingTableBlockID)<<4)
	if err := c.handleBlockPlace(placeOnTopOf(0, 4, 0, dirt(10))); err != nil {
		t.Fatalf("handleBlockPlace: %v", err)
	}

	if c.windowID != 0 {
		t.Errorf("windowID = %d, want 0: sneaking does not open the table", c.windowID)
	}
	if got := blockID(t, c, 0, 5, 0); got != int32(3)<<4 {
		t.Errorf("block above the table = %d, want dirt", got)
	}
}

// The 3x3 recipe a 2x2 grid cannot hold: eight cobblestone around an empty
// centre is a furnace.
func TestCraftingTable_CraftsA3x3Recipe(t *testing.T) {
	c := newInventoryTestConn(t)
	openTableAt(t, c, 0, 4, 0)

	for i := range 9 {
		if i == 4 {
			continue // the empty centre
		}
		c.craftingGrid[i] = player.Slot{BlockID: cobblestoneID, ItemCount: 1}
	}
	c.updateCraftingOutput()

	if got := c.craftingOutput; got.BlockID != furnaceID {
		t.Fatalf("output = %+v, want a furnace (%d)", got, furnaceID)
	}

	c.handleNormalClick(slotCraftOutput, 0)

	if c.cursorSlot.BlockID != furnaceID {
		t.Errorf("cursor = %+v, want the crafted furnace", c.cursorSlot)
	}
	for i := range 9 {
		if !c.craftingGrid[i].IsEmpty() {
			t.Errorf("grid[%d] = %+v, want empty after one craft consumed it", i, c.craftingGrid[i])
		}
	}
}

// The same eight cobblestone in the player's own 2x2 window craft nothing: the
// grid is too small to hold the shape.
func TestCraftingTable_2x2WindowCannotCraftA3x3Recipe(t *testing.T) {
	c := newInventoryTestConn(t)

	for i := range 4 {
		c.craftingGrid[i] = player.Slot{BlockID: cobblestoneID, ItemCount: 1}
	}
	c.updateCraftingOutput()

	if got := c.craftingOutput; got.BlockID == furnaceID {
		t.Errorf("output = %+v, want no furnace from a 2x2 grid", got)
	}
}

// A table window has no armor slots, so its inventory sits one slot lower than
// the player window's. Getting that wrong moves the item next to the one the
// player clicked.
func TestCraftingTable_InventorySlotsSitOneLower(t *testing.T) {
	c := newInventoryTestConn(t)
	openTableAt(t, c, 0, 4, 0)

	c.self.Inventory.SetSlot(0, stone(5)) // hotbar 0 == player-window slot 36

	if got := c.getWindowSlot(tableHotbarStart); got != stone(5) {
		t.Errorf("table slot %d = %+v, want the hotbar's first stack", tableHotbarStart, got)
	}
	if got := c.getWindowSlot(tableMainStart); !got.IsEmpty() {
		t.Errorf("table slot %d = %+v, want the empty first main-inventory slot", tableMainStart, got)
	}
}

func TestCraftingTable_ShiftClickCraftsUntilTheGridRunsOut(t *testing.T) {
	c := newInventoryTestConn(t)
	openTableAt(t, c, 0, 4, 0)

	for i := range 9 {
		if i == 4 {
			continue
		}
		c.craftingGrid[i] = player.Slot{BlockID: cobblestoneID, ItemCount: 3}
	}
	c.updateCraftingOutput()

	c.handleShiftClick(slotCraftOutput, 0)

	for i := range 9 {
		if !c.craftingGrid[i].IsEmpty() {
			t.Errorf("grid[%d] = %+v, want empty after the grid was drained", i, c.craftingGrid[i])
		}
	}
	if got := countItem(c, furnaceID, 0); got != 3 {
		t.Errorf("furnaces in the inventory = %d, want 3", got)
	}
}

func TestCraftingTable_CloseReturnsTheGridAndClosesTheWindow(t *testing.T) {
	c := newInventoryTestConn(t)
	openTableAt(t, c, 0, 4, 0)

	c.craftingGrid[8] = stone(7)

	if err := c.handleCloseWindow(); err != nil {
		t.Fatalf("handleCloseWindow: %v", err)
	}

	if c.windowID != 0 {
		t.Errorf("windowID = %d, want 0 after the window closed", c.windowID)
	}
	if got := countItem(c, 1, 0); got != 7 {
		t.Errorf("stone in the inventory = %d, want the 7 returned from the grid", got)
	}
	for i := range 9 {
		if !c.craftingGrid[i].IsEmpty() {
			t.Errorf("grid[%d] = %+v, want empty after close", i, c.craftingGrid[i])
		}
	}
}

// Opening a window over a grid that still holds items returns them rather than
// leaving them in a grid the player can no longer reach.
func TestCraftingTable_OpeningReturnsWhatThe2x2Held(t *testing.T) {
	c := newInventoryTestConn(t)
	c.craftingGrid[0] = stone(4)

	openTableAt(t, c, 0, 4, 0)

	if got := countItem(c, 1, 0); got != 4 {
		t.Errorf("stone in the inventory = %d, want the 4 returned from the 2x2 grid", got)
	}
	if !c.craftingGrid[0].IsEmpty() {
		t.Errorf("grid[0] = %+v, want an empty grid in the new window", c.craftingGrid[0])
	}
}

// A click carrying a window ID the server does not have open is stale and must
// not move anything.
func TestCraftingTable_StaleWindowClickIsRefused(t *testing.T) {
	c := newInventoryTestConn(t)
	openTableAt(t, c, 0, 4, 0)
	c.craftingGrid[0] = stone(4)

	stale := &v1_8.PlayServerboundWindowClick{
		WindowID:    uint8(c.windowID + 1),
		Slot:        slotCraftStart,
		MouseButton: 0,
		Action:      1,
		Mode:        0,
		Item:        player.ToGeneratedSlot(player.EmptySlot),
	}
	if err := c.handleWindowClick(stale); err != nil {
		t.Fatalf("handleWindowClick: %v", err)
	}

	if got := c.craftingGrid[0]; got != stone(4) {
		t.Errorf("grid[0] = %+v, want the stack untouched by a stale click", got)
	}
	if !c.cursorSlot.IsEmpty() {
		t.Errorf("cursor = %+v, want empty after a refused click", c.cursorSlot)
	}
}
