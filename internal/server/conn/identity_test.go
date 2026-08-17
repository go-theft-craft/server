package conn

import (
	"log/slog"
	"math/rand/v2"
	"testing"

	"github.com/go-theft-craft/server/internal/server/player"
	"github.com/go-theft-craft/server/pkg/world"
)

// The click paths with item identity on.
//
// The property test at the bottom is the one that matters: seven click types
// against a seeded inventory, and after every single click the two things that
// have to be true — every stack carries exactly one ID per item, and no ID is
// in two places at once. A hand-written test covers the combinations somebody
// thought of; this covers the ones they did not.

// duplicates collects what the index reported, so a test can say the detector
// stayed quiet as well as that the invariant held.
type duplicates struct {
	seen []*world.ErrDuplicate
}

func (d *duplicates) record(err *world.ErrDuplicate) { d.seen = append(d.seen, err) }

// newIdentityTestConn is an inventory test connection with item identity on.
func newIdentityTestConn(t *testing.T) (*Connection, world.ItemIndex, *duplicates) {
	t.Helper()

	c, _, players, _ := newTestConnWithCapture(t, "Alice")
	for i := range 36 {
		c.self.Inventory.SetSlot(i, player.EmptySlot)
	}
	for i := range 4 {
		c.self.Inventory.SetArmor(i, player.EmptySlot)
	}

	minter, err := world.NewMinter(1)
	if err != nil {
		t.Fatalf("new minter: %v", err)
	}

	found := &duplicates{}
	index := world.NewItemIndex(minter, world.DuplicateAllow, found.record)
	c.SetItemIndex(index)
	players.SetItemIndex(index, slog.New(slog.DiscardHandler))

	return c, index, found
}

// seed puts a stack in a window slot and gives it identity, which is what
// reconciliation at load will do for a stack restored from disk.
func seed(t *testing.T, c *Connection, slot int16, stack player.Slot) {
	t.Helper()

	c.setWindowSlot(slot, stack)
	c.mintInto(c.layout(), slot)
}

// located is one stack and the place the index should say it is.
type located struct {
	at    world.Location
	stack player.Slot
}

// everywhere enumerates every place this connection can be holding items. It
// reads the player inventory, the cursor, the crafting area, and the open
// chest directly rather than through a window layout, so a slot cannot escape
// the check by belonging to a window that is not open.
func everywhere(c *Connection) []located {
	uuid := c.self.UUID
	proto := c.self.Inventory.ToProtocolSlots()

	// Protocol slots 0-4 are the crafting area, which the inventory reports as
	// empty because the connection owns it; it is added below.
	all := make([]located, 0, len(proto)+len(c.chestItems)+11)
	for i := slotArmorStart; i <= slotHotbarEnd; i++ {
		all = append(all, located{
			at:    world.Location{Kind: world.LocationInventory, Player: uuid, Slot: int(i)},
			stack: proto[i],
		})
	}

	all = append(all, located{
		at:    world.Location{Kind: world.LocationCursor, Player: uuid, Slot: -1},
		stack: c.cursorSlot,
	})

	for i, cell := range c.craftingGrid {
		all = append(all, located{
			at:    world.Location{Kind: world.LocationCrafting, Player: uuid, Slot: i + 1},
			stack: cell,
		})
	}

	for i, stack := range c.chestItems {
		all = append(all, located{
			at: world.Location{
				Kind:  world.LocationContainer,
				Block: c.chestPositions[i/world.ChestSlots],
				Slot:  i % world.ChestSlots,
			},
			stack: stack,
		})
	}

	return all
}

// assertIdentityHolds checks the milestone's invariant everywhere at once.
//
// The crafting output is deliberately not among the places it checks: an
// untaken result is an offer rather than items, and it is minted at the moment
// somebody takes it. A result that carried identity while it sat in the output
// slot would be an item that exists twice — once as the offer and once as the
// ingredients still in the grid.
func assertIdentityHolds(t *testing.T, c *Connection, index world.ItemIndex, step string) {
	t.Helper()

	seen := map[world.ItemID]world.Location{}
	for _, held := range everywhere(c) {
		stack := held.stack
		if stack.IsEmpty() {
			if len(stack.IDs) != 0 {
				t.Fatalf("%s: %s is empty but carries %d IDs", step, held.at, len(stack.IDs))
			}

			continue
		}

		if len(stack.IDs) != int(stack.ItemCount) {
			t.Fatalf("%s: %s holds %d items with %d IDs", step, held.at, stack.ItemCount, len(stack.IDs))
		}

		for _, id := range stack.IDs {
			if other, ok := seen[id]; ok {
				t.Fatalf("%s: item %s is in %s and in %s", step, id, other, held.at)
			}
			seen[id] = held.at

			where, known := index.Where(id)
			if !known {
				t.Fatalf("%s: item %s is in %s, and the index has never heard of it", step, id, held.at)
			}
			if where != held.at {
				t.Fatalf("%s: item %s is in %s, but the index says %s", step, id, held.at, where)
			}
		}
	}
}

// TestRandomClickSequencesNeverBreakTheInvariant is the only mechanism that
// covers the combinations of click paths a hand-written test would not think
// to make.
//
// Ten thousand clicks, drawn from all seven modes, spread over fresh
// inventories rather than one: a sequence that wedges the state into a corner
// should not be able to hide the next one. The connection is rebuilt every
// four hundred clicks rather than every click because building one loads the
// whole 1.8 registry, and the point is the number of clicks.
func TestRandomClickSequencesNeverBreakTheInvariant(t *testing.T) {
	const (
		rounds          = 25
		clicksPerRound  = 400
		differentItems  = 4
		startingStacks  = 10
		startingMaximum = 40
	)

	items := []int16{1, 3, 4, 17}

	for round := range rounds {
		random := rand.New(rand.NewPCG(uint64(round), 0x5eed))
		c, index, found := newIdentityTestConn(t)
		openChest := round%2 == 1
		if openChest {
			openChestAt(t, c, 2, 4, 2)
		}

		l := c.layout()
		for range startingStacks {
			slot := int16(random.IntN(int(l.hotbarEnd) + 1))
			if slot == slotCraftOutput && l.hasCrafting() {
				continue
			}
			seed(t, c, slot, player.Slot{
				BlockID:   items[random.IntN(differentItems)],
				ItemCount: int8(1 + random.IntN(startingMaximum)),
			})
		}
		assertIdentityHolds(t, c, index, "after seeding")

		for click := range clicksPerRound {
			randomClick(random, c, l)
			assertIdentityHolds(t, c, index, describeRound(round, click))
		}

		if len(found.seen) != 0 {
			t.Fatalf("round %d: the detector reported %d duplications on legitimate clicks, first %v",
				round, len(found.seen), found.seen[0])
		}
	}
}

func describeRound(round, click int) string {
	return "round " + itoa(round) + " click " + itoa(click)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}

	var digits []byte
	for ; n > 0; n /= 10 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
	}

	return string(digits)
}

// randomClick makes one of the seven clicks a client can send. A drag is three
// packets rather than one, so it is driven as the sequence the client sends.
func randomClick(random *rand.Rand, c *Connection, l windowLayout) {
	slot := func() int16 { return int16(random.IntN(int(l.hotbarEnd) + 1)) }

	switch random.IntN(8) {
	case 0: // Normal click, either button.
		c.dispatchClick(slot(), int8(random.IntN(2)), 0)
	case 1: // Normal click outside the window, which drops the cursor.
		c.dispatchClick(slotOutside, int8(random.IntN(2)), 0)
	case 2: // Shift click.
		c.dispatchClick(slot(), 0, 1)
	case 3: // Number key.
		c.dispatchClick(slot(), int8(random.IntN(9)), 2)
	case 4: // Middle click.
		c.dispatchClick(slot(), 0, 3)
	case 5: // Q and ctrl-Q.
		c.dispatchClick(slot(), int8(random.IntN(2)), 4)
	case 6: // Drag: start, paint some slots, end.
		start := int8(0)
		if random.IntN(2) == 1 {
			start = 4
		}
		c.dispatchClick(slotOutside, start, 5)
		for range 1 + random.IntN(4) {
			c.dispatchClick(slot(), start+1, 5)
		}
		c.dispatchClick(slotOutside, start+2, 5)
	case 7: // Double click.
		c.dispatchClick(slot(), 0, 6)
	}
}

// TestClickPathsWithIdentityOffCarryNoIDs is the other half of the invariant:
// a server that never asked for identity pays for none of it, and a stack it
// moves stays a stack of three numbers.
func TestClickPathsWithIdentityOffCarryNoIDs(t *testing.T) {
	c := newInventoryTestConn(t)
	if c.identityOn() {
		t.Fatal("a connection built without an index reports identity on")
	}

	c.setWindowSlot(36, stone(32))
	c.dispatchClick(36, 0, 0) // pick up
	c.dispatchClick(37, 1, 0) // place one
	c.dispatchClick(38, 0, 1) // shift click

	for _, held := range everywhere(c) {
		if len(held.stack.IDs) != 0 {
			t.Fatalf("%s carries %d IDs with identity off", held.at, len(held.stack.IDs))
		}
	}
}

// TestAStackRestoredWithoutIdentityGetsItOnFirstUse covers the stopgap that
// stands in for load-time reconciliation: a stack that came off disk has no
// IDs, and the first click that moves it is where it gets them.
func TestAStackRestoredWithoutIdentityGetsItOnFirstUse(t *testing.T) {
	c, index, _ := newIdentityTestConn(t)

	// Written straight into the slot, the way a load does: no minting.
	c.self.Inventory.SetSlot(0, stone(5))

	c.dispatchClick(36, 0, 0) // pick the whole stack up

	if got := len(c.cursorSlot.IDs); got != 5 {
		t.Fatalf("cursor carries %d IDs for %d items", got, c.cursorSlot.ItemCount)
	}
	assertIdentityHolds(t, c, index, "after picking up a restored stack")
}

// TestSplittingAStackSplitsItsIdentity is the shape every click path is built
// out of: half the items go, and exactly half the IDs go with them.
func TestSplittingAStackSplitsItsIdentity(t *testing.T) {
	c, index, _ := newIdentityTestConn(t)
	seed(t, c, 36, stone(10))

	before := append([]world.ItemID(nil), c.getWindowSlot(36).IDs...)
	c.dispatchClick(36, 1, 0) // right click takes half, rounded up

	cursor := c.cursorSlot
	rest := c.getWindowSlot(36)
	if cursor.ItemCount != 5 || rest.ItemCount != 5 {
		t.Fatalf("split %d/%d, want 5/5", cursor.ItemCount, rest.ItemCount)
	}
	for i, id := range before[:5] {
		if cursor.IDs[i] != id {
			t.Errorf("cursor ID %d = %s, want %s: a split is deterministic", i, cursor.IDs[i], id)
		}
	}
	for i, id := range before[5:] {
		if rest.IDs[i] != id {
			t.Errorf("slot ID %d = %s, want %s: a split is deterministic", i, rest.IDs[i], id)
		}
	}
	assertIdentityHolds(t, c, index, "after a split")
}

// TestCraftingMintsTheResultAndRetiresTheIngredients is what makes a crafted
// item a new item rather than the ingredients wearing a different name.
func TestCraftingMintsTheResultAndRetiresTheIngredients(t *testing.T) {
	c, index, _ := newIdentityTestConn(t)

	// Four planks in the 2x2 grid make a crafting table.
	for slot := int16(slotCraftStart); slot <= slotCraftEnd; slot++ {
		seed(t, c, slot, player.Slot{BlockID: 5, ItemCount: 1})
	}
	c.updateCraftingOutput()
	if c.craftingOutput.IsEmpty() {
		t.Fatal("four planks offered no recipe")
	}
	if len(c.craftingOutput.IDs) != 0 {
		t.Fatal("an untaken crafting offer carries identity, so the result exists before anybody makes it")
	}

	ingredients := map[world.ItemID]bool{}
	for slot := int16(slotCraftStart); slot <= slotCraftEnd; slot++ {
		for _, id := range c.getWindowSlot(slot).IDs {
			ingredients[id] = true
		}
	}

	c.dispatchClick(slotCraftOutput, 0, 0)

	if len(c.cursorSlot.IDs) != int(c.cursorSlot.ItemCount) {
		t.Fatalf("the crafted result carries %d IDs for %d items", len(c.cursorSlot.IDs), c.cursorSlot.ItemCount)
	}
	for _, id := range c.cursorSlot.IDs {
		if ingredients[id] {
			t.Errorf("crafted item %s is one of the ingredients, not a new item", id)
		}
	}
	for id := range ingredients {
		if _, live := index.Where(id); live {
			t.Errorf("ingredient %s is still live after being crafted away", id)
		}
	}
	assertIdentityHolds(t, c, index, "after crafting")
}

// TestDroppingAndPickingUpKeepsOneIdentity follows a stack out of the
// inventory, onto the ground, and back, which is the path the index would
// otherwise lose sight of at the window's edge.
func TestDroppingAndPickingUpKeepsOneIdentity(t *testing.T) {
	c, index, found := newIdentityTestConn(t)
	seed(t, c, 36, stone(4))

	dropped := append([]world.ItemID(nil), c.getWindowSlot(36).IDs...)
	c.dispatchClick(36, 1, 4) // ctrl-Q drops the whole stack

	if !c.getWindowSlot(36).IsEmpty() {
		t.Fatal("the slot still holds the stack that was dropped")
	}
	for _, id := range dropped {
		where, known := index.Where(id)
		if !known || where.Kind != world.LocationEntity {
			t.Fatalf("dropped item %s is at %s, want an entity", id, where)
		}
	}

	// The pickup delay is in ticks since the spawn, so the manager has to be
	// told time passed before it will collect anything.
	for range 20 {
		c.players.Tick()
	}
	if got := c.players.TryPickupItems(c.self); got == 0 {
		t.Fatal("the player picked nothing up")
	}

	for _, id := range dropped {
		where, known := index.Where(id)
		if !known || where.Kind != world.LocationInventory {
			t.Fatalf("picked-up item %s is at %s, want an inventory slot", id, where)
		}
	}
	if len(found.seen) != 0 {
		t.Fatalf("the detector fired on a drop and a pickup: %v", found.seen[0])
	}
	assertIdentityHolds(t, c, index, "after a drop and a pickup")
}

// TestShiftClickingIntoAFullSectionLeavesNothingInTwoPlaces is the M3
// duplication's shape at the click layer: a deposit that only partly fits.
// What moved has to be gone from the source, and what did not has to have
// stayed there.
func TestShiftClickingIntoAFullSectionLeavesNothingInTwoPlaces(t *testing.T) {
	c, index, found := newIdentityTestConn(t)

	// Fill the whole main inventory with something that cannot merge with what
	// is about to be shift-clicked, leaving one slot with room for 10.
	for slot := int16(slotMainStart); slot <= slotMainEnd; slot++ {
		seed(t, c, slot, dirt(64))
	}
	c.setWindowSlot(slotMainStart, player.EmptySlot)
	seed(t, c, slotMainStart, stone(54))

	seed(t, c, slotHotbarStart, stone(30))
	c.dispatchClick(slotHotbarStart, 0, 1)

	if got := c.getWindowSlot(slotHotbarStart); got.ItemCount != 20 {
		t.Fatalf("the source slot kept %d items, want the 20 that did not fit", got.ItemCount)
	}
	if got := c.getWindowSlot(slotMainStart); got.ItemCount != 64 {
		t.Fatalf("the destination took %d, want a full stack", got.ItemCount)
	}
	if len(found.seen) != 0 {
		t.Fatalf("the detector fired on a legitimate partial deposit: %v", found.seen[0])
	}
	assertIdentityHolds(t, c, index, "after a partial shift click")
}

// TestChestTransfersMoveIdentityIntoTheWorld covers the one window whose slots
// nobody owns: a chest outlives the session that filled it, so the location
// the index records has to be the block rather than the player.
func TestChestTransfersMoveIdentityIntoTheWorld(t *testing.T) {
	c, index, _ := newIdentityTestConn(t)
	openChestAt(t, c, 2, 4, 2)

	l := c.layout()
	seed(t, c, l.hotbarStart, stone(16))
	moved := append([]world.ItemID(nil), c.getWindowSlot(l.hotbarStart).IDs...)

	c.dispatchClick(l.hotbarStart, 0, 1) // shift click into the chest

	if !c.getWindowSlot(l.hotbarStart).IsEmpty() {
		t.Fatal("the hotbar slot still holds what went into the chest")
	}
	for _, id := range moved {
		where, known := index.Where(id)
		if !known || where.Kind != world.LocationContainer {
			t.Fatalf("item %s is at %s, want a container slot", id, where)
		}
		if where.Block != (world.BlockPos{X: 2, Y: 4, Z: 2}) {
			t.Fatalf("item %s is in the chest at %v, want the one at 2,4,2", id, where.Block)
		}
	}
	assertIdentityHolds(t, c, index, "after a chest transfer")

	// Closing writes the working copy into the world and reopening builds a
	// new one from it. Identity has to survive that round trip, or every reopen
	// would mint a second set of IDs for items the index already knows about.
	if err := c.handleCloseWindow(); err != nil {
		t.Fatalf("handleCloseWindow: %v", err)
	}
	openChestAt(t, c, 2, 4, 2)

	reopened := c.getWindowSlot(0)
	if len(reopened.IDs) != int(reopened.ItemCount) {
		t.Fatalf("the reopened chest holds %d items with %d IDs", reopened.ItemCount, len(reopened.IDs))
	}
	for i, id := range moved {
		if reopened.IDs[i] != id {
			t.Errorf("reopened ID %d = %s, want %s: the same items came back", i, reopened.IDs[i], id)
		}
	}
	assertIdentityHolds(t, c, index, "after closing and reopening the chest")
}
