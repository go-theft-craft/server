package conn

import (
	"testing"

	"github.com/go-theft-craft/server/internal/server/packet"
	"github.com/go-theft-craft/server/internal/server/player"
	"github.com/go-theft-craft/server/pkg/world"
)

// openChestAt puts a chest in the world and right-clicks it, which is how a
// player opens the container window.
func openChestAt(t *testing.T, c *Connection, x, y, z int) {
	t.Helper()

	c.world.SetBlock(x, y, z, int32(chestBlockID)<<4)
	click := placeOnTopOf(x, y, z, player.EmptySlot)
	if err := c.handleBlockPlace(click); err != nil {
		t.Fatalf("handleBlockPlace: %v", err)
	}
}

func TestChest_RightClickOpensTheWindow(t *testing.T) {
	c := newInventoryTestConn(t)

	openChestAt(t, c, 0, 4, 0)

	if c.windowID == 0 {
		t.Fatal("windowID = 0, want a chest window open")
	}
	if c.windowKind != windowChest {
		t.Errorf("window kind = %d, want windowChest", c.windowKind)
	}

	l := c.layout()
	if !l.hasContainer() {
		t.Fatal("chest layout reports no container")
	}
	if l.hasCrafting() {
		t.Error("chest layout reports a crafting area, which a chest does not have")
	}
	if got := l.total; got != chestSlotTotal {
		t.Errorf("total slots = %d, want %d", got, chestSlotTotal)
	}
}

// Slot 0 is a crafting output in the player's window and a container slot in a
// chest's. Reading it as the output is what would show the player a recipe
// result where their first chest slot belongs.
func TestChest_SlotZeroIsContainerNotCraftOutput(t *testing.T) {
	c := newInventoryTestConn(t)
	openChestAt(t, c, 0, 4, 0)

	c.craftingOutput = stone(1)
	c.setWindowSlot(0, dirt(5))

	if got := c.getWindowSlot(0); got != dirt(5) {
		t.Errorf("chest slot 0 = %+v, want dirt(5)", got)
	}
	if c.chestItems[0].ID != 3 || c.chestItems[0].Count != 5 {
		t.Errorf("stored slot 0 = %+v, want dirt(5)", c.chestItems[0])
	}
	if c.craftingOutput != stone(1) {
		t.Errorf("crafting output = %+v, want it untouched", c.craftingOutput)
	}
}

// The chest window's inventory section is the same inventory as the player's
// own window, one section lower.
func TestChest_InventorySlotsMapToThePlayerInventory(t *testing.T) {
	c := newInventoryTestConn(t)
	openChestAt(t, c, 0, 4, 0)

	c.self.Inventory.SetSlot(0, stone(7)) // hotbar 0 is player proto slot 36
	if got := c.getWindowSlot(chestHotbarStart); got != stone(7) {
		t.Errorf("chest slot %d = %+v, want stone(7)", chestHotbarStart, got)
	}

	c.setWindowSlot(chestMainStart, dirt(3)) // main 0 is player proto slot 9
	if got := c.self.Inventory.GetProtocolSlot(slotMainStart); got != dirt(3) {
		t.Errorf("player main slot = %+v, want dirt(3)", got)
	}
}

func TestChest_ContentsPersistToTheWorldOnClose(t *testing.T) {
	c := newInventoryTestConn(t)
	pos := world.BlockPos{X: 3, Y: 4, Z: 5}
	openChestAt(t, c, pos.X, pos.Y, pos.Z)

	c.setWindowSlot(0, dirt(12))
	if err := c.handleCloseWindow(); err != nil {
		t.Fatalf("handleCloseWindow: %v", err)
	}

	if c.windowKind != windowPlayer {
		t.Errorf("window kind = %d, want windowPlayer after close", c.windowKind)
	}

	stored := c.world.Chest(pos)
	if stored[0].ID != 3 || stored[0].Count != 12 {
		t.Errorf("stored slot 0 = %+v, want dirt(12)", stored[0])
	}
}

// Reopening has to show what was stored, not an empty chest: the working copy
// is loaded from the world on open.
func TestChest_ReopenShowsStoredContents(t *testing.T) {
	c := newInventoryTestConn(t)
	openChestAt(t, c, 3, 4, 5)
	c.setWindowSlot(0, dirt(12))
	if err := c.handleCloseWindow(); err != nil {
		t.Fatalf("handleCloseWindow: %v", err)
	}

	openChestAt(t, c, 3, 4, 5)

	if got := c.getWindowSlot(0); got != dirt(12) {
		t.Errorf("reopened slot 0 = %+v, want dirt(12)", got)
	}
}

// Two chests are two containers. Opening the second must not carry the first
// one's items over, and must not lose them either.
func TestChest_SecondChestIsSeparateStorage(t *testing.T) {
	c := newInventoryTestConn(t)

	openChestAt(t, c, 3, 4, 5)
	c.setWindowSlot(0, dirt(12))

	openChestAt(t, c, 9, 4, 9)
	if got := c.getWindowSlot(0); !got.IsEmpty() {
		t.Errorf("second chest slot 0 = %+v, want empty", got)
	}

	first := c.world.Chest(world.BlockPos{X: 3, Y: 4, Z: 5})
	if first[0].ID != 3 || first[0].Count != 12 {
		t.Errorf("first chest slot 0 = %+v, want dirt(12) kept", first[0])
	}
}

func TestChest_ShiftClickMovesItemsBothWays(t *testing.T) {
	c := newInventoryTestConn(t)
	openChestAt(t, c, 0, 4, 0)

	// Inventory → chest.
	c.setWindowSlot(chestHotbarStart, stone(20))
	c.handleShiftClick(chestHotbarStart, 0)

	if got := c.getWindowSlot(chestHotbarStart); !got.IsEmpty() {
		t.Errorf("hotbar slot = %+v, want empty after shift-click into the chest", got)
	}
	if got := c.getWindowSlot(0); got != stone(20) {
		t.Errorf("chest slot 0 = %+v, want stone(20)", got)
	}

	// Chest → inventory.
	c.handleShiftClick(0, 0)

	if got := c.getWindowSlot(0); !got.IsEmpty() {
		t.Errorf("chest slot 0 = %+v, want empty after shift-click out", got)
	}
	if got := c.getWindowSlot(chestMainStart); got != stone(20) {
		t.Errorf("inventory slot = %+v, want stone(20)", got)
	}
}

func TestChest_BreakingSpillsTheContents(t *testing.T) {
	c := newInventoryTestConn(t)
	pos := world.BlockPos{X: 2, Y: 4, Z: 2}
	openChestAt(t, c, pos.X, pos.Y, pos.Z)
	c.setWindowSlot(0, dirt(9))
	if err := c.handleCloseWindow(); err != nil {
		t.Fatalf("handleCloseWindow: %v", err)
	}

	c.breakBlock(pos.X, pos.Y, pos.Z)

	stored := c.world.Chest(pos)
	for i, s := range stored {
		if !s.IsEmpty() {
			t.Fatalf("slot %d = %+v, want the broken chest's storage gone", i, s)
		}
	}
}

// A chest that was never opened stores nothing, and an emptied one goes back
// to storing nothing rather than 27 empty slots.
func TestChest_EmptyContentsAreNotStored(t *testing.T) {
	c := newInventoryTestConn(t)
	pos := world.BlockPos{X: 1, Y: 4, Z: 1}
	openChestAt(t, c, pos.X, pos.Y, pos.Z)

	c.setWindowSlot(0, dirt(4))
	c.flushChest()
	if len(c.world.GetChests()) != 1 {
		t.Fatalf("stored chests = %d, want 1", len(c.world.GetChests()))
	}

	c.setWindowSlot(0, player.EmptySlot)
	c.flushChest()
	if got := len(c.world.GetChests()); got != 0 {
		t.Errorf("stored chests = %d, want 0 once the chest is empty", got)
	}
}

// Sneaking is how a player builds against a chest instead of opening it.
func TestChest_SneakingPlacesInstead(t *testing.T) {
	c := newInventoryTestConn(t)
	c.self.SetSneaking(true)
	c.self.Inventory.SetSlot(0, dirt(10))
	c.self.Inventory.SetHeldSlot(0)

	c.world.SetBlock(0, 4, 0, int32(chestBlockID)<<4)
	if err := c.handleBlockPlace(placeOnTopOf(0, 4, 0, dirt(10))); err != nil {
		t.Fatalf("handleBlockPlace: %v", err)
	}

	if c.windowID != 0 {
		t.Errorf("windowID = %d, want no window opened while sneaking", c.windowID)
	}
	if got := c.world.GetBlock(0, 5, 0) >> 4; got != 3 {
		t.Errorf("block above the chest = %d, want dirt placed", got)
	}
}

// End to end for the report that a placed chest is gone after reconnecting:
// place it through the real handler, then read the block back out of the chunk
// packet a joining client is sent.
func TestChest_PlacedChestIsInTheChunkAClientRejoinsWith(t *testing.T) {
	c := newInventoryTestConn(t)
	c.self.SetGameMode(packet.GameModeSurvival)
	c.self.Inventory.SetSlot(0, player.Slot{BlockID: chestBlockID, ItemCount: 1})
	c.self.Inventory.SetHeldSlot(0)

	// Place on top of the block at (5, 4, 5), so the chest lands at (5, 5, 5).
	c.world.SetBlock(5, 4, 5, int32(1)<<4)
	if err := c.handleBlockPlace(placeOnTopOf(5, 4, 5, player.Slot{BlockID: chestBlockID, ItemCount: 1})); err != nil {
		t.Fatalf("handleBlockPlace: %v", err)
	}

	if got := c.world.GetBlock(5, 5, 5) >> 4; got != chestBlockID {
		t.Fatalf("world holds block %d at the placed position, want %d", got, chestBlockID)
	}

	chunk := c.world.EncodeChunk(0, 0)
	if chunk.BitMap&(1<<0) == 0 {
		t.Fatal("section 0 is missing from the chunk a rejoining client gets")
	}

	idx := (5*256 + 5*16 + 5) * 2
	got := int32(chunk.ChunkData[idx]) | int32(chunk.ChunkData[idx+1])<<8
	if got>>4 != chestBlockID {
		t.Errorf("chunk sent on rejoin has block %d at the chest position, want %d", got>>4, chestBlockID)
	}
}

// The bug behind every vanishing chest. A 1.8 client resolves each chunk
// section value against its registry of valid block states and draws air when
// there is no match, and a chest's facing is horizontal only — so the four
// chests are metadata 2 through 5 and metadata 0 is not a chest at all. A
// chest stored as 54<<4 was drawn as air on every chunk load, and only the
// client's own placement prediction ever made one appear.
func TestChest_PlacedChestHasAValidFacing(t *testing.T) {
	for _, yaw := range []float32{0, 45, 90, 135, 180, 225, 270, 315, -90, -233.99} {
		c := newInventoryTestConn(t)
		c.self.SetGameMode(packet.GameModeSurvival)
		c.self.SetPosition(0.5, 4, 0.5, yaw, 0, true)
		c.self.Inventory.SetSlot(0, player.Slot{BlockID: chestBlockID, ItemCount: 1})
		c.self.Inventory.SetHeldSlot(0)

		c.world.SetBlock(5, 4, 5, int32(1)<<4)
		if err := c.handleBlockPlace(placeOnTopOf(5, 4, 5, player.Slot{BlockID: chestBlockID, ItemCount: 1})); err != nil {
			t.Fatalf("handleBlockPlace: %v", err)
		}

		state := c.world.GetBlock(5, 5, 5)
		if got := state >> 4; got != chestBlockID {
			t.Fatalf("yaw %v: placed block %d, want a chest", yaw, got)
		}
		if meta := state & 0xF; meta < 2 || meta > 5 {
			t.Errorf("yaw %v: chest metadata %d, want a horizontal facing 2-5", yaw, meta)
		}
	}
}

// Vanilla faces a placed chest towards the player, so it opens facing them.
func TestChest_FacingIsOppositeThePlayer(t *testing.T) {
	// Yaw 0 looks south, so the chest faces north.
	for _, tc := range []struct {
		yaw  float32
		want int32
	}{
		{yaw: 0, want: 2},   // looking south → chest faces north
		{yaw: 90, want: 5},  // looking west → chest faces east
		{yaw: 180, want: 3}, // looking north → chest faces south
		{yaw: 270, want: 4}, // looking east → chest faces west
		{yaw: -90, want: 4}, // the same as 270
		{yaw: 360, want: 2}, // the same as 0
	} {
		if got := chestFacing(tc.yaw); got != tc.want {
			t.Errorf("chestFacing(%v) = %d, want %d", tc.yaw, got, tc.want)
		}
	}
}

func TestChest_TwoAdjacentChestsOpenOneDoubleWindow(t *testing.T) {
	c := newInventoryTestConn(t)

	c.world.SetBlock(4, 4, 4, int32(chestBlockID)<<4|2)
	c.world.SetBlock(5, 4, 4, int32(chestBlockID)<<4|2)

	if err := c.openChest(5, 4, 4); err != nil {
		t.Fatalf("openChest: %v", err)
	}

	if got := len(c.chestPositions); got != 2 {
		t.Fatalf("opened %d halves, want 2", got)
	}
	if got := len(c.chestItems); got != 2*world.ChestSlots {
		t.Fatalf("window holds %d container slots, want %d", got, 2*world.ChestSlots)
	}

	l := c.layout()
	if got := l.containerEnd; got != 2*world.ChestSlots-1 {
		t.Errorf("container ends at %d, want %d", got, 2*world.ChestSlots-1)
	}
	if got := l.total; got != 2*world.ChestSlots+36 {
		t.Errorf("total slots = %d, want %d", got, 2*world.ChestSlots+36)
	}

	// The half with the smaller coordinate holds the first slots, which is
	// the order vanilla builds a large chest in.
	if c.chestPositions[0].X != 4 {
		t.Errorf("first half is at x=%d, want the smaller coordinate 4", c.chestPositions[0].X)
	}
}

// Each half of a double chest keeps its own storage, so breaking one does not
// take the other's items with it.
func TestChest_DoubleChestHalvesStoreSeparately(t *testing.T) {
	c := newInventoryTestConn(t)
	c.world.SetBlock(4, 4, 4, int32(chestBlockID)<<4|2)
	c.world.SetBlock(5, 4, 4, int32(chestBlockID)<<4|2)

	if err := c.openChest(4, 4, 4); err != nil {
		t.Fatalf("openChest: %v", err)
	}
	c.setWindowSlot(0, dirt(5))                 // first half
	c.setWindowSlot(world.ChestSlots, stone(7)) // second half
	if err := c.handleCloseWindow(); err != nil {
		t.Fatalf("handleCloseWindow: %v", err)
	}

	first := c.world.Chest(world.BlockPos{X: 4, Y: 4, Z: 4})
	second := c.world.Chest(world.BlockPos{X: 5, Y: 4, Z: 4})
	if first[0].ID != 3 || first[0].Count != 5 {
		t.Errorf("first half slot 0 = %+v, want dirt(5)", first[0])
	}
	if second[0].ID != 1 || second[0].Count != 7 {
		t.Errorf("second half slot 0 = %+v, want stone(7)", second[0])
	}
}

// A trapped chest is a chest. It holds items the same way, takes the same
// window, and has the same horizontal facing — so it had the same invisibility
// bug, and it is what was actually being placed when the bug was reported.
func TestChest_TrappedChestIsAContainerToo(t *testing.T) {
	c := newInventoryTestConn(t)
	c.self.SetGameMode(packet.GameModeSurvival)
	c.self.Inventory.SetSlot(0, player.Slot{BlockID: trappedChestBlockID, ItemCount: 1})
	c.self.Inventory.SetHeldSlot(0)

	c.world.SetBlock(5, 4, 5, int32(1)<<4)
	if err := c.handleBlockPlace(placeOnTopOf(5, 4, 5, player.Slot{BlockID: trappedChestBlockID, ItemCount: 1})); err != nil {
		t.Fatalf("handleBlockPlace: %v", err)
	}

	state := c.world.GetBlock(5, 5, 5)
	if got := state >> 4; got != trappedChestBlockID {
		t.Fatalf("placed block %d, want a trapped chest", got)
	}
	if meta := state & 0xF; meta < 2 || meta > 5 {
		t.Errorf("trapped chest metadata %d, want a horizontal facing 2-5", meta)
	}

	// Right-clicking it opens a container window.
	if err := c.handleBlockPlace(placeOnTopOf(5, 5, 5, player.EmptySlot)); err != nil {
		t.Fatalf("handleBlockPlace: %v", err)
	}
	if c.windowKind != windowChest {
		t.Errorf("window kind = %d, want a chest window on a trapped chest", c.windowKind)
	}
}

// A trapped chest pairs with a trapped chest, never with a plain one.
func TestChest_TrappedChestDoesNotPairWithAPlainChest(t *testing.T) {
	c := newInventoryTestConn(t)
	c.world.SetBlock(4, 4, 4, int32(trappedChestBlockID)<<4|2)
	c.world.SetBlock(5, 4, 4, int32(chestBlockID)<<4|2)

	if err := c.openChest(4, 4, 4); err != nil {
		t.Fatalf("openChest: %v", err)
	}

	if got := len(c.chestPositions); got != 1 {
		t.Errorf("opened %d halves, want 1 — the neighbour is a different kind", got)
	}
}

// placeChest tries to place a chest of the given kind at (x, y+1, z) by
// right-clicking the top of the block below it.
func placeChest(t *testing.T, c *Connection, kind int16, x, y, z int) {
	t.Helper()

	c.self.SetGameMode(packet.GameModeSurvival)
	c.self.Inventory.SetSlot(0, player.Slot{BlockID: kind, ItemCount: 1})
	c.self.Inventory.SetHeldSlot(0)
	c.world.SetBlock(x, y, z, int32(1)<<4)

	if err := c.handleBlockPlace(placeOnTopOf(x, y, z, player.Slot{BlockID: kind, ItemCount: 1})); err != nil {
		t.Fatalf("handleBlockPlace: %v", err)
	}
}

// BlockChest.canPlaceBlockAt: a chest may join a lone chest, but never a pair,
// and never two at once. Three chests in a row would leave the middle one with
// two partners, which vanilla has no way to represent.
func TestChest_CannotPlaceAThirdChestAgainstAPair(t *testing.T) {
	c := newInventoryTestConn(t)

	// A pair along x: (4,5,4) and (5,5,4).
	c.world.SetBlock(4, 5, 4, int32(chestBlockID)<<4|2)
	c.world.SetBlock(5, 5, 4, int32(chestBlockID)<<4|2)

	// Extending the row is refused: (6,5,4) touches a chest that is already
	// half of a double.
	placeChest(t, c, chestBlockID, 6, 4, 4)

	if got := c.world.GetBlock(6, 5, 4); got != 0 {
		t.Errorf("block at the refused position = %d, want nothing placed", got)
	}
}

// A chest with two chest neighbours would have two partners, so the position
// between two lone chests is refused even though neither is yet a pair.
func TestChest_CannotPlaceBetweenTwoLoneChests(t *testing.T) {
	c := newInventoryTestConn(t)

	c.world.SetBlock(3, 5, 4, int32(chestBlockID)<<4|2)
	c.world.SetBlock(5, 5, 4, int32(chestBlockID)<<4|2)

	placeChest(t, c, chestBlockID, 4, 4, 4)

	if got := c.world.GetBlock(4, 5, 4); got != 0 {
		t.Errorf("block between two chests = %d, want nothing placed", got)
	}
}

// The rule allows a pair: one neighbour, not already paired.
func TestChest_CanPlaceBesideALoneChest(t *testing.T) {
	c := newInventoryTestConn(t)

	c.world.SetBlock(4, 5, 4, int32(chestBlockID)<<4|2)

	placeChest(t, c, chestBlockID, 5, 4, 4)

	if got := c.world.GetBlock(5, 5, 4) >> 4; got != chestBlockID {
		t.Errorf("block beside a lone chest = %d, want a chest placed", got)
	}
}

// The kind is the exact block. A trapped chest neither pairs with a plain one
// nor is blocked by it, because vanilla compares block instances.
func TestChest_TrappedChestIgnoresPlainChestsWhenPlacing(t *testing.T) {
	c := newInventoryTestConn(t)

	c.world.SetBlock(3, 5, 4, int32(chestBlockID)<<4|2)
	c.world.SetBlock(5, 5, 4, int32(chestBlockID)<<4|2)

	placeChest(t, c, trappedChestBlockID, 4, 4, 4)

	if got := c.world.GetBlock(4, 5, 4) >> 4; got != trappedChestBlockID {
		t.Errorf("block between two plain chests = %d, want a trapped chest placed", got)
	}
}

// A refused placement gives the item back: the client decremented its own
// stack when it predicted the placement.
func TestChest_RefusedPlacementKeepsTheItem(t *testing.T) {
	c := newInventoryTestConn(t)
	c.world.SetBlock(4, 5, 4, int32(chestBlockID)<<4|2)
	c.world.SetBlock(5, 5, 4, int32(chestBlockID)<<4|2)

	c.self.SetGameMode(packet.GameModeSurvival)
	c.self.Inventory.SetSlot(0, player.Slot{BlockID: chestBlockID, ItemCount: 3})
	c.self.Inventory.SetHeldSlot(0)
	c.world.SetBlock(6, 4, 4, int32(1)<<4)

	if err := c.handleBlockPlace(placeOnTopOf(6, 4, 4, player.Slot{BlockID: chestBlockID, ItemCount: 3})); err != nil {
		t.Fatalf("handleBlockPlace: %v", err)
	}

	if got := c.self.Inventory.GetSlot(0).ItemCount; got != 3 {
		t.Errorf("held stack = %d, want 3 kept when the placement is refused", got)
	}
}

// A double chest is twice as wide as it is deep, so both halves must face
// across the pair's axis and must agree. Vanilla enforces this in
// checkForSurroundingChests, run on the new chest and on every neighbouring
// chest.
func TestChest_PairFacesAcrossItsAxisAndAgrees(t *testing.T) {
	c := newInventoryTestConn(t)

	// A lone chest facing east, then a partner placed to its east: the pair
	// runs along x, so both have to face north or south.
	c.world.SetBlock(4, 5, 4, int32(chestBlockID)<<4|facingEast)
	placeChest(t, c, chestBlockID, 5, 4, 4)

	first := c.world.GetBlock(4, 5, 4)
	second := c.world.GetBlock(5, 5, 4)

	if first&0xF != second&0xF {
		t.Errorf("halves face %d and %d, want one shared facing", first&0xF, second&0xF)
	}
	if f := first & 0xF; f != facingNorth && f != facingSouth {
		t.Errorf("pair along x faces %d, want north or south", f)
	}
}

// The same, for a pair along z: it has to face west or east.
func TestChest_PairAlongZFacesWestOrEast(t *testing.T) {
	c := newInventoryTestConn(t)

	c.world.SetBlock(4, 5, 4, int32(chestBlockID)<<4|facingNorth)
	placeChest(t, c, chestBlockID, 4, 4, 5)

	first := c.world.GetBlock(4, 5, 4)
	second := c.world.GetBlock(4, 5, 5)

	if first&0xF != second&0xF {
		t.Errorf("halves face %d and %d, want one shared facing", first&0xF, second&0xF)
	}
	if f := first & 0xF; f != facingWest && f != facingEast {
		t.Errorf("pair along z faces %d, want west or east", f)
	}
}

// Breaking half of a pair leaves a lone chest, and a lone chest must not keep
// an orientation it only had because it had a partner. This is the report that
// removing one half and placing another nearby rebuilt the old shape.
func TestChest_BreakingHalfReorientsTheSurvivor(t *testing.T) {
	c := newInventoryTestConn(t)

	// Build a pair along x, which faces north or south.
	c.world.SetBlock(4, 5, 4, int32(chestBlockID)<<4|facingEast)
	placeChest(t, c, chestBlockID, 5, 4, 4)

	paired := c.world.GetBlock(4, 5, 4) & 0xF

	// Break the second half, then pair the survivor along z instead.
	c.breakBlock(5, 5, 4)
	placeChest(t, c, chestBlockID, 4, 4, 5)

	survivor := c.world.GetBlock(4, 5, 4) & 0xF
	partner := c.world.GetBlock(4, 5, 5) & 0xF

	if survivor != partner {
		t.Errorf("halves face %d and %d, want one shared facing", survivor, partner)
	}
	if survivor != facingWest && survivor != facingEast {
		t.Errorf("new pair along z faces %d, want west or east — not the old %d", survivor, paired)
	}
}
