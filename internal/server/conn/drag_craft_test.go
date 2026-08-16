package conn

import (
	"testing"

	"github.com/go-theft-craft/server/internal/server/player"
)

const (
	oakPlanksID     = 5
	craftingTableID = craftingTableBlockID
)

// dragInto runs the three phases a client sends for one drag: start, a slot per
// cell, then end. The buttons are 0/1/2 for a left drag and 4/5/6 for a right.
func dragInto(c *Connection, start, add, end int8, slots ...int16) {
	c.handleDragClick(slotOutside, start)
	for _, s := range slots {
		c.handleDragClick(s, add)
	}
	c.handleDragClick(slotOutside, end)
}

// Dragging ingredients across the grid fills it exactly as clicking them in one
// at a time does, so it has to offer the same result. It offered nothing, which
// is what a player sees as "crafting does not work" — and with no crafting
// table craftable, nothing that needs a 3x3 can be reached at all.
func TestDragClick_OffersTheResultItJustFilled(t *testing.T) {
	c := newInventoryTestConn(t)
	c.cursorSlot = player.Slot{BlockID: oakPlanksID, ItemCount: 5}

	// A right drag places one plank in each of the four 2x2 grid cells.
	dragInto(c, 4, 5, 6, 1, 2, 3, 4)

	for i := range 4 {
		if got := c.craftingGrid[i]; got.BlockID != oakPlanksID || got.ItemCount != 1 {
			t.Fatalf("grid[%d] = %+v, want one oak plank", i, got)
		}
	}
	if got := c.craftingOutput; got.BlockID != craftingTableID {
		t.Errorf("output = %+v, want a crafting table (%d) offered", got, craftingTableID)
	}
}

// The same drag in a crafting table fills its 3x3.
func TestDragClick_OffersTheResultInATableWindow(t *testing.T) {
	c := newInventoryTestConn(t)
	openTableAt(t, c, 0, 4, 0)
	c.cursorSlot = player.Slot{BlockID: cobblestoneID, ItemCount: 8}

	// Every cell but the centre: a furnace.
	dragInto(c, 4, 5, 6, 1, 2, 3, 4, 6, 7, 8, 9)

	if got := c.craftingOutput; got.BlockID != furnaceID {
		t.Errorf("output = %+v, want a furnace (%d) offered", got, furnaceID)
	}
}

// A drag that never touches the grid leaves the offer alone.
func TestDragClick_LeavesTheOutputAloneOutsideTheGrid(t *testing.T) {
	c := newInventoryTestConn(t)
	for i := range 4 {
		c.craftingGrid[i] = player.Slot{BlockID: oakPlanksID, ItemCount: 1}
	}
	c.updateCraftingOutput()

	c.cursorSlot = stone(4)
	dragInto(c, 0, 1, 2, slotMainStart, slotMainStart+1)

	if got := c.craftingOutput; got.BlockID != craftingTableID {
		t.Errorf("output = %+v, want the crafting table still offered", got)
	}
}
