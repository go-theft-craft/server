package conn

import (
	"testing"

	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/server/internal/server/packet"
	"github.com/go-theft-craft/server/internal/server/player"
)

// placeOnTopOf builds the BlockPlace a client sends when it right-clicks the
// upward face of the block at (x, y, z), which puts the new block at y+1.
func placeOnTopOf(x, y, z int, held player.Slot) *v1_8.PlayServerboundBlockPlace {
	return &v1_8.PlayServerboundBlockPlace{
		Location:  blockPos(x, y, z),
		Direction: 1, // +Y
		HeldItem:  player.ToGeneratedSlot(held),
		CursorX:   8,
		CursorY:   8,
		CursorZ:   8,
	}
}

func TestBlockPlace_ConsumesHeldItemInSurvival(t *testing.T) {
	c := newInventoryTestConn(t)
	c.self.SetGameMode(packet.GameModeSurvival)
	c.self.Inventory.SetSlot(0, dirt(10))
	c.self.Inventory.SetHeldSlot(0)

	if err := c.handleBlockPlace(placeOnTopOf(0, 3, 0, dirt(10))); err != nil {
		t.Fatalf("handleBlockPlace: %v", err)
	}

	if got := c.world.GetBlockID(0, 4, 0); got != int32(3)<<4 {
		t.Errorf("block at (0,4,0) = %d, want dirt state %d", got, int32(3)<<4)
	}
	if got := countItem(c, 3, 0); got != 9 {
		t.Errorf("dirt after placing one = %d, want 9", got)
	}
}

func TestBlockPlace_EmptiesTheSlotOnTheLastItem(t *testing.T) {
	c := newInventoryTestConn(t)
	c.self.SetGameMode(packet.GameModeSurvival)
	c.self.Inventory.SetSlot(0, dirt(1))
	c.self.Inventory.SetHeldSlot(0)

	if err := c.handleBlockPlace(placeOnTopOf(0, 3, 0, dirt(1))); err != nil {
		t.Fatalf("handleBlockPlace: %v", err)
	}

	if held := c.self.Inventory.HeldItem(); !held.IsEmpty() {
		t.Errorf("held slot = %+v, want empty after placing the last block", held)
	}
}

func TestBlockPlace_CreativeDoesNotConsume(t *testing.T) {
	c := newInventoryTestConn(t)
	c.self.SetGameMode(packet.GameModeCreative)
	c.self.Inventory.SetSlot(0, dirt(10))
	c.self.Inventory.SetHeldSlot(0)

	if err := c.handleBlockPlace(placeOnTopOf(0, 3, 0, dirt(10))); err != nil {
		t.Fatalf("handleBlockPlace: %v", err)
	}

	if got := c.world.GetBlockID(0, 4, 0); got != int32(3)<<4 {
		t.Errorf("block at (0,4,0) = %d, want dirt state %d", got, int32(3)<<4)
	}
	if got := countItem(c, 3, 0); got != 10 {
		t.Errorf("dirt after a creative place = %d, want 10", got)
	}
}

// A block's item damage is its variant — red wool is wool with metadata 14 —
// so the placed state has to carry it or every variant becomes the default.
func TestBlockPlace_KeepsTheVariantMetadata(t *testing.T) {
	c := newInventoryTestConn(t)
	c.self.SetGameMode(packet.GameModeSurvival)
	redWool := player.Slot{BlockID: 35, ItemCount: 4, ItemDamage: 14}
	c.self.Inventory.SetSlot(0, redWool)
	c.self.Inventory.SetHeldSlot(0)

	if err := c.handleBlockPlace(placeOnTopOf(0, 3, 0, redWool)); err != nil {
		t.Fatalf("handleBlockPlace: %v", err)
	}

	want := int32(35)<<4 | 14
	if got := c.world.GetBlockID(0, 4, 0); got != want {
		t.Errorf("block at (0,4,0) = %d, want red wool state %d", got, want)
	}
}

// The block placed is the one the server has in hand, not the one the packet
// claims: a client that claims a block it does not hold would otherwise place
// it for free.
func TestBlockPlace_RefusesWhatTheServerDoesNotHold(t *testing.T) {
	c := newInventoryTestConn(t)
	c.self.SetGameMode(packet.GameModeSurvival)
	c.self.Inventory.SetHeldSlot(0) // empty hand

	before := c.world.GetBlockID(0, 4, 0)
	if err := c.handleBlockPlace(placeOnTopOf(0, 3, 0, dirt(64))); err != nil {
		t.Fatalf("handleBlockPlace: %v", err)
	}

	if got := c.world.GetBlockID(0, 4, 0); got != before {
		t.Errorf("block at (0,4,0) = %d, want %d unchanged: an empty hand places nothing", got, before)
	}
}

// An item is not a block: right-clicking with a sword must not build with it.
func TestBlockPlace_RefusesANonBlockItem(t *testing.T) {
	c := newInventoryTestConn(t)
	c.self.SetGameMode(packet.GameModeSurvival)
	c.self.Inventory.SetSlot(0, sword())
	c.self.Inventory.SetHeldSlot(0)

	before := c.world.GetBlockID(0, 4, 0)
	if err := c.handleBlockPlace(placeOnTopOf(0, 3, 0, sword())); err != nil {
		t.Fatalf("handleBlockPlace: %v", err)
	}

	if got := c.world.GetBlockID(0, 4, 0); got != before {
		t.Errorf("block at (0,4,0) = %d, want %d unchanged: a sword is not placeable", got, before)
	}
	if got := countItem(c, 276, 0); got != 1 {
		t.Errorf("swords after a refused place = %d, want 1", got)
	}
}

// The M3 finding: place a block in survival and break it again, and the
// inventory must hold exactly what it started with. It held one more, because
// the place never consumed anything while the break credited a drop.
func TestPlaceThenBreak_ReturnsExactlyWhatWasPlaced(t *testing.T) {
	c := newInventoryTestConn(t)
	c.self.SetGameMode(packet.GameModeSurvival)
	c.self.Inventory.SetSlot(0, dirt(10))
	c.self.Inventory.SetHeldSlot(0)

	if err := c.handleBlockPlace(placeOnTopOf(0, 3, 0, dirt(10))); err != nil {
		t.Fatalf("handleBlockPlace: %v", err)
	}
	if got := countItem(c, 3, 0); got != 9 {
		t.Fatalf("dirt after placing = %d, want 9", got)
	}

	c.breakBlock(0, 4, 0)

	// The drop is only collectable after the pickup delay.
	for range pickupDelayTicksForTest {
		c.players.Tick()
	}
	if collected := c.players.TryPickupItems(c.self); collected != 1 {
		t.Fatalf("items collected = %d, want 1", collected)
	}

	if got := countItem(c, 3, 0); got != 10 {
		t.Errorf("dirt after place and break = %d, want 10", got)
	}
}

// The pickup delay is unexported in player; this mirrors it with a margin so
// the test does not depend on its exact value.
const pickupDelayTicksForTest = 16

// Items are not blocks. Right-clicking with an apple used to store block 260
// in the world, which no client can draw: protocol 47 numbers blocks 0-255 and
// items above that, so the state resolves to nothing and the client shows air.
func TestBlockPlace_ItemsAreNotPlaced(t *testing.T) {
	for _, item := range []int16{260, 295, 278, 276} { // apple, seeds, pickaxe, sword
		c := newInventoryTestConn(t)
		c.self.SetGameMode(packet.GameModeSurvival)
		c.self.Inventory.SetSlot(0, player.Slot{BlockID: item, ItemCount: 1})
		c.self.Inventory.SetHeldSlot(0)

		c.world.SetBlockID(5, 4, 5, int32(1)<<4)
		if err := c.handleBlockPlace(placeOnTopOf(5, 4, 5, player.Slot{BlockID: item, ItemCount: 1})); err != nil {
			t.Fatalf("handleBlockPlace: %v", err)
		}

		if got := c.world.GetBlockID(5, 5, 5); got != 0 {
			t.Errorf("item %d placed block state %d, want nothing placed", item, got)
		}
	}
}
