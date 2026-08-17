package conn

import (
	"testing"

	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/server/internal/server/player"
)

func craftItem(id, metadata int16, count int8) player.Slot {
	return player.Slot{BlockID: id, ItemCount: count, ItemDamage: metadata}
}

// TestMatchRecipe2x2_RealRegistry runs the matcher against the shared
// minecraft-protocol registry rather than hand-built recipe structs. Tests that
// build their own recipes cannot see the data underneath the matcher change,
// which is how a registry swap could alter crafting without failing anything.
func TestMatchRecipe2x2_RealRegistry(t *testing.T) {
	gameData, err := v1_8.Data()
	if err != nil {
		t.Fatalf("game data: %v", err)
	}
	recipes := gameData.Recipes()

	empty := player.EmptySlot
	cases := []struct {
		name      string
		grid      [4]player.Slot
		wantID    int16
		wantMeta  int16
		wantCount int8
	}{
		// Exact-metadata ingredients: one recipe per wood variant.
		{"oak log to oak planks", [4]player.Slot{craftItem(17, 0, 1), empty, empty, empty}, 5, 0, 4},
		{"spruce log to spruce planks", [4]player.Slot{craftItem(17, 1, 1), empty, empty, empty}, 5, 1, 4},
		{"acacia log to acacia planks", [4]player.Slot{craftItem(162, 0, 1), empty, empty, empty}, 5, 4, 4},

		// Wildcard ingredients: any plank variant, in any grid position.
		{"planks to sticks, left column", [4]player.Slot{craftItem(5, 0, 1), empty, craftItem(5, 0, 1), empty}, 280, 0, 4},
		{"planks to sticks, right column", [4]player.Slot{empty, craftItem(5, 0, 1), empty, craftItem(5, 0, 1)}, 280, 0, 4},
		{"spruce planks to sticks", [4]player.Slot{craftItem(5, 1, 1), empty, craftItem(5, 1, 1), empty}, 280, 0, 4},
		{"mixed planks to crafting table", [4]player.Slot{craftItem(5, 0, 1), craftItem(5, 1, 1), craftItem(5, 2, 1), craftItem(5, 3, 1)}, 58, 0, 1},

		// Both coal variants make torches; the recipe accepts either.
		{"coal and stick to torches", [4]player.Slot{craftItem(263, 0, 1), empty, craftItem(280, 0, 1), empty}, 50, 0, 4},
		{"charcoal and stick to torches", [4]player.Slot{craftItem(263, 1, 1), empty, craftItem(280, 0, 1), empty}, 50, 0, 4},

		// Full-grid and shapeless recipes.
		{"four snowballs to a snow block", [4]player.Slot{craftItem(332, 0, 1), craftItem(332, 0, 1), craftItem(332, 0, 1), craftItem(332, 0, 1)}, 80, 0, 1},
		{"four bricks to a brick block", [4]player.Slot{craftItem(336, 0, 1), craftItem(336, 0, 1), craftItem(336, 0, 1), craftItem(336, 0, 1)}, 45, 0, 1},
		{"sugar cane to sugar", [4]player.Slot{craftItem(338, 0, 1), empty, empty, empty}, 353, 0, 1},

		// Non-recipes must not produce anything.
		{"dirt alone is not a recipe", [4]player.Slot{craftItem(3, 0, 1), empty, empty, empty}, -1, 0, 0},
		{"sand and gravel is not a recipe", [4]player.Slot{craftItem(12, 0, 1), craftItem(13, 0, 1), empty, empty}, -1, 0, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := matchRecipe2x2(tc.grid, recipes)
			if got.BlockID != tc.wantID || got.ItemDamage != tc.wantMeta || got.ItemCount != tc.wantCount {
				t.Errorf("got id=%d metadata=%d count=%d, want id=%d metadata=%d count=%d",
					got.BlockID, got.ItemDamage, got.ItemCount, tc.wantID, tc.wantMeta, tc.wantCount)
			}
		})
	}
}

// TestMatchRecipe2x2_Deterministic guards against the registry being a map:
// if two recipes ever match one grid, the result would vary between calls.
func TestMatchRecipe2x2_Deterministic(t *testing.T) {
	gameData, err := v1_8.Data()
	if err != nil {
		t.Fatalf("game data: %v", err)
	}
	recipes := gameData.Recipes()

	empty := player.EmptySlot
	grids := map[string][4]player.Slot{
		"four planks": {craftItem(5, 0, 1), craftItem(5, 0, 1), craftItem(5, 0, 1), craftItem(5, 0, 1)},
		"one oak log": {craftItem(17, 0, 1), empty, empty, empty},
		"two planks":  {craftItem(5, 0, 1), empty, craftItem(5, 0, 1), empty},
	}

	for name, grid := range grids {
		t.Run(name, func(t *testing.T) {
			want := matchRecipe2x2(grid, recipes)
			for range 200 {
				if got := matchRecipe2x2(grid, recipes); !got.Equal(want) {
					t.Fatalf("got %+v, want %+v", got, want)
				}
			}
		})
	}
}

// TestCraftingOutput_TakenOneStackPerClick covers the normal-click path: each
// click yields one result stack and consumes one of every ingredient.
func TestCraftingOutput_TakenOneStackPerClick(t *testing.T) {
	c := newInventoryTestConn(t)
	c.craftingGrid[0] = craftItem(17, 0, 8)
	c.updateCraftingOutput()

	if !c.craftingOutput.Equal(craftItem(5, 0, 4)) {
		t.Fatalf("output = %+v, want 4 oak planks", c.craftingOutput)
	}

	c.handleNormalClick(slotCraftOutput, 0)

	if !c.cursorSlot.Equal(craftItem(5, 0, 4)) {
		t.Errorf("cursor = %+v, want 4 oak planks", c.cursorSlot)
	}
	if c.craftingGrid[0].ItemCount != 7 {
		t.Errorf("grid kept %d logs, want 7", c.craftingGrid[0].ItemCount)
	}

	// A second click stacks onto the cursor.
	c.handleNormalClick(slotCraftOutput, 0)
	if c.cursorSlot.ItemCount != 8 {
		t.Errorf("cursor = %d planks, want 8", c.cursorSlot.ItemCount)
	}
}

// TestShiftClickOutput_CraftsUntilGridEmpties is the behavior a real client
// session found missing: shift-clicking the output crafted once instead of
// draining the grid, which reads as crafting working only some of the time.
func TestShiftClickOutput_CraftsUntilGridEmpties(t *testing.T) {
	c := newInventoryTestConn(t)
	c.craftingGrid[0] = craftItem(17, 0, 8)
	c.updateCraftingOutput()

	c.handleShiftClick(slotCraftOutput, 0)

	for i := range slotCraftCount {
		if !c.craftingGrid[i].IsEmpty() {
			t.Errorf("grid[%d] = %+v, want empty", i, c.craftingGrid[i])
		}
	}
	if !c.craftingOutput.IsEmpty() {
		t.Errorf("output = %+v, want empty", c.craftingOutput)
	}

	// Eight logs at four planks each.
	if got := countItem(c, 5, 0); got != 32 {
		t.Errorf("inventory holds %d oak planks, want 32", got)
	}
}

// TestShiftClickOutput_StopsWhenResultNoLongerFits proves the loop respects a
// full inventory, and that a refused craft consumes nothing.
func TestShiftClickOutput_StopsWhenResultNoLongerFits(t *testing.T) {
	c := newInventoryTestConn(t)
	c.craftingGrid[0] = craftItem(17, 0, 8)
	c.updateCraftingOutput()

	// Room for exactly two crafts: every slot is full except one holding 56
	// oak planks, which leaves space for eight more.
	for s := int16(slotMainStart); s <= slotHotbarEnd; s++ {
		c.setWindowSlot(s, craftItem(1, 0, 64))
	}
	c.setWindowSlot(slotMainStart, craftItem(5, 0, 56))

	c.handleShiftClick(slotCraftOutput, 0)

	if got := countItem(c, 5, 0); got != 64 {
		t.Errorf("inventory holds %d oak planks, want 64", got)
	}
	if c.craftingGrid[0].ItemCount != 6 {
		t.Errorf("grid kept %d logs, want 6", c.craftingGrid[0].ItemCount)
	}
	if !c.craftingOutput.Equal(craftItem(5, 0, 4)) {
		t.Errorf("output = %+v, want 4 planks still offered", c.craftingOutput)
	}
}

// TestShiftClickOutput_RefusedCraftConsumesNothing pins the failure mode: when
// no result stack fits, the grid must be untouched.
func TestShiftClickOutput_RefusedCraftConsumesNothing(t *testing.T) {
	c := newInventoryTestConn(t)
	c.craftingGrid[0] = craftItem(17, 0, 8)
	c.updateCraftingOutput()

	for s := int16(slotMainStart); s <= slotHotbarEnd; s++ {
		c.setWindowSlot(s, craftItem(1, 0, 64))
	}

	c.handleShiftClick(slotCraftOutput, 0)

	if c.craftingGrid[0].ItemCount != 8 {
		t.Errorf("grid kept %d logs, want 8 untouched", c.craftingGrid[0].ItemCount)
	}
	if got := countItem(c, 5, 0); got != 0 {
		t.Errorf("inventory holds %d planks, want none", got)
	}
}

// TestShiftClick_PartialMoveKeepsRemainder covers the duplication the crafting
// fix depends on: a section add moves what fits even when it cannot take the
// whole stack, so the caller that treated that as "nothing moved" left the
// source stack intact and created items out of nothing.
func TestShiftClick_PartialMoveKeepsRemainder(t *testing.T) {
	c := newInventoryTestConn(t)

	// One hotbar stack to move, and a main inventory with room for 10 of them.
	c.setWindowSlot(slotHotbarStart, craftItem(1, 0, 64))
	for s := int16(slotMainStart); s <= slotMainEnd; s++ {
		c.setWindowSlot(s, craftItem(1, 0, 64))
	}
	c.setWindowSlot(slotMainEnd, craftItem(1, 0, 54))

	c.handleShiftClick(slotHotbarStart, 0)

	// 10 moved into the partial stack; 54 stay behind. Nothing is created.
	if got := countItem(c, 1, 0); got != 64*27+54 {
		t.Errorf("stone total = %d, want %d", got, 64*27+54)
	}
	if got := c.getWindowSlot(slotHotbarStart); got.ItemCount != 54 {
		t.Errorf("source slot = %+v, want 54 stone", got)
	}
}

// countItem totals an item across the main inventory and hotbar only. The
// crafting output slot is excluded: it shows the next result on offer, which
// has not been crafted yet.
func countItem(c *Connection, id, metadata int16) int {
	total := 0
	for s := int16(slotMainStart); s <= slotHotbarEnd; s++ {
		item := c.getWindowSlot(s)
		if item.BlockID == id && item.ItemDamage == metadata {
			total += int(item.ItemCount)
		}
	}

	return total
}
