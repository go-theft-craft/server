package conn

import (
	"slices"

	"github.com/go-theft-craft/server/internal/server/player"
	"github.com/go-theft-craft/server/pkg/world"
)

// Item identity on the click paths.
//
// Every path that moves items between two places goes through transfer or
// swapSlots; every path that ends items goes through consume; everything that
// leaves a window for the ground goes through dropFromSlot. Those four are the
// only places a click changes how many items a slot holds, which is what makes
// the index the write path rather than an observer of it: a move that claims an
// item came from somewhere it is not is reported at the moment it happens.
//
// With identity off — the default — the index is nil, stacks carry no IDs, and
// these helpers are the same arithmetic the handlers used to do inline.
//
// The invariant they keep is the one the design states: when identity is on,
// len(IDs) == int(ItemCount) on every stack, after any sequence of clicks. The
// property test over random click sequences is what says it holds.

// slotCursor addresses the stack the player is dragging with the mouse.
//
// The cursor is not part of any window, but every click that touches it moves
// items to or from a window slot, so giving it a slot number of its own is what
// lets one transfer serve every path. It is negative, like the client's own
// out-of-window -999, and cannot collide with a real slot.
const slotCursor int16 = -2

// identityOn reports whether this connection tracks item identity.
func (c *Connection) identityOn() bool { return c.index != nil }

// actor is who the index is told did this.
func (c *Connection) actor() world.Actor {
	if c.self == nil {
		return world.Actor{Kind: world.ActorServer}
	}

	return world.Actor{Kind: world.ActorPlayer, UUID: c.self.UUID, Name: c.self.Username}
}

// playerID is the UUID a location names, or empty before login.
func (c *Connection) playerID() string {
	if c.self == nil {
		return ""
	}

	return c.self.UUID
}

// locationOf is where a window slot is, in the index's own coordinates.
//
// A window slot is a number that means different things in different windows,
// and the index has to name a place that outlives the window: a player's
// protocol slot, a cell of the grid they have open, or a slot of a chest at a
// position in the world.
func (c *Connection) locationOf(l windowLayout, slot int16) world.Location {
	switch {
	case slot == slotCursor:
		return world.Location{Kind: world.LocationCursor, Player: c.playerID(), Slot: -1}

	case l.isContainer(slot):
		index := int(slot - l.containerStart)
		half := index / world.ChestSlots
		var pos world.BlockPos
		if half < len(c.chestPositions) {
			pos = c.chestPositions[half]
		}

		return world.Location{
			Kind:  world.LocationContainer,
			Block: pos,
			Slot:  index % world.ChestSlots,
		}

	case l.hasCrafting() && slot >= slotCraftOutput && slot <= l.gridEnd:
		// The output is cell 0 and the grid runs from 1, in both the player's
		// own window and a crafting table. A window closing empties the grid, so
		// no cell number outlives the window that gave it meaning.
		return world.Location{Kind: world.LocationCrafting, Player: c.playerID(), Slot: int(slot)}

	default:
		if proto, ok := l.inventorySlot(slot); ok {
			return world.Location{Kind: world.LocationInventory, Player: c.playerID(), Slot: int(proto)}
		}

		return world.Nowhere
	}
}

// ensureIdentity gives a stack the IDs it is missing, minted where it already
// is, and returns it.
//
// Reconciliation at load is what gives a stack from disk its identity, so this
// is the second line rather than the first: what reaches it is a stack that
// appeared without going through a path that mints — a creative slot set, an
// extension writing into an inventory — and something has to give it identity
// before it moves or the invariant is false from the click that moves it.
// Minting at the source is the honest description — the items were already
// there — and a freshly minted ID cannot collide with a live one, so this can
// never invent a duplication.
func (c *Connection) ensureIdentity(l windowLayout, slot int16, s player.Slot) player.Slot {
	if !c.identityOn() || s.IsEmpty() {
		return s
	}

	missing := int(s.ItemCount) - len(s.IDs)
	if missing <= 0 {
		return s
	}

	ids, err := c.index.Mint(missing, c.locationOf(l, slot), c.actor())
	if err != nil {
		c.log.Error("mint item identity", "error", err, "slot", slot)

		return s
	}

	s.IDs = append(slices.Clone(s.IDs), ids...)
	c.setSlotIn(l, slot, s)

	return s
}

// moveIDs tells the index that items went from one place to another.
//
// A detected duplication is logged here as well as recorded, because the index
// tells the recorder and an operator reading the log should not have to open
// the audit trail to see that the detector fired. Under the refusing policy the
// index keeps the location it believes and the window keeps the one the click
// produced: the click layer has no rollback, and the disagreement is what the
// operator asked to be told about.
func (c *Connection) moveIDs(ids []world.ItemID, from, to world.Location) {
	if !c.identityOn() || len(ids) == 0 {
		return
	}

	if err := c.index.Move(ids, from, to, c.actor()); err != nil {
		c.log.Error("item movement", "error", err, "from", from.String(), "to", to.String())
	}
}

// retireIDs tells the index that items stopped existing.
func (c *Connection) retireIDs(ids []world.ItemID, at world.Location) {
	if !c.identityOn() || len(ids) == 0 {
		return
	}

	if err := c.index.Retire(ids, at, c.actor()); err != nil {
		c.log.Error("item retirement", "error", err, "at", at.String())
	}
}

// transfer moves up to n items from one window slot to another and reports how
// many moved.
//
// It is the single place a movement between two slots is both applied to the
// window and told to the index. What the destination cannot take stays where it
// was: a caller that read a partial transfer as "nothing happened" and cleared
// the source would duplicate the items that did move, which is the shape of the
// bug this milestone's detector was built for.
func (c *Connection) transfer(l windowLayout, from, to int16, n int) int {
	if n <= 0 || from == to {
		return 0
	}

	src := c.getSlotIn(l, from)
	if src.IsEmpty() {
		return 0
	}

	dst := c.getSlotIn(l, to)
	if !dst.IsEmpty() && !canStack(dst, src) {
		return 0
	}

	space := world.MaxStackSize
	if !dst.IsEmpty() {
		space -= int(dst.ItemCount)
	}

	moving := min(n, int(src.ItemCount), space)
	if moving <= 0 {
		return 0
	}

	src = c.ensureIdentity(l, from, src)
	taken, rest := src.Split(moving)
	merged, _ := dst.Merge(taken, moving)

	c.setSlotIn(l, from, rest)
	c.setSlotIn(l, to, merged)
	c.moveIDs(taken.IDs, c.locationOf(l, from), c.locationOf(l, to))

	return moving
}

// swapSlots exchanges two window slots, which is what a click that cannot merge
// does.
func (c *Connection) swapSlots(l windowLayout, a, b int16) {
	if a == b {
		return
	}

	first := c.ensureIdentity(l, a, c.getSlotIn(l, a))
	second := c.ensureIdentity(l, b, c.getSlotIn(l, b))

	c.setSlotIn(l, a, second)
	c.setSlotIn(l, b, first)

	atA, atB := c.locationOf(l, a), c.locationOf(l, b)
	c.moveIDs(first.IDs, atA, atB)
	c.moveIDs(second.IDs, atB, atA)
}

// take removes n items from a window slot and returns them, carrying their
// identity. The caller says where they went: they are still live as far as the
// index is concerned until it is told otherwise.
func (c *Connection) take(l windowLayout, slot int16, n int) player.Slot {
	if n <= 0 {
		return player.EmptySlot
	}

	s := c.ensureIdentity(l, slot, c.getSlotIn(l, slot))
	if s.IsEmpty() {
		return player.EmptySlot
	}

	taken, rest := s.Split(min(n, int(s.ItemCount)))
	c.setSlotIn(l, slot, rest)

	return taken
}

// consume removes n items from a window slot and ends their identity: they
// stopped being items rather than moving anywhere. A crafting ingredient and a
// block that becomes part of the world are both this.
func (c *Connection) consume(l windowLayout, slot int16, n int) player.Slot {
	taken := c.take(l, slot, n)
	if taken.IsEmpty() {
		return taken
	}

	c.retireIDs(taken.IDs, c.locationOf(l, slot))

	return taken
}

// mintInto gives a slot's whole contents fresh identity, which is what items
// coming into existence get: a creative slot set, a middle-click clone.
func (c *Connection) mintInto(l windowLayout, slot int16) {
	c.ensureIdentity(l, slot, c.getSlotIn(l, slot))
}

// dropFromSlot takes n items out of a window slot and drops them at the
// player's feet.
func (c *Connection) dropFromSlot(l windowLayout, slot int16, n int) {
	taken := c.take(l, slot, n)
	if taken.IsEmpty() {
		return
	}

	c.dropStack(taken, c.locationOf(l, slot))
}

// dropStack spawns a dropped item entity holding a stack that has already left
// wherever it was.
func (c *Connection) dropStack(stack player.Slot, from world.Location) {
	if stack.IsEmpty() {
		return
	}

	pos := c.self.GetPosition()
	c.players.SpawnItemEntity(
		c.self.EntityID, stack,
		pos.X, pos.Y+1.3, pos.Z, pos.Yaw, c.groundAtFunc(),
		player.ItemOrigin{From: from, By: c.actor()},
	)
}
