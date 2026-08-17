package conn

import (
	"math"
	"sort"

	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/server/internal/server/player"
	"github.com/go-theft-craft/server/pkg/world"
)

// The chest window.
//
// A chest is the first window whose slots are not the player's own: a crafting
// table shows a grid backed by connection state, and the player window shows
// the inventory, but a chest shows storage the world owns and every session
// shares. The layout below therefore describes a container section, and
// getSlotIn and setSlotIn read and write it through the world.

// The blocks a right-click opens a container window on. A trapped chest holds
// items exactly as a plain one does — the difference is a redstone signal this
// server does not model — and vanilla gives it the same window.
const (
	chestBlockID        = 54
	trappedChestBlockID = 146
)

// isChestBlock reports whether a block ID is a chest of either kind.
func isChestBlock(blockID int32) bool {
	return blockID == chestBlockID || blockID == trappedChestBlockID
}

// Chest window slot layout for a single chest: the container first, then the
// player's inventory and hotbar. It has no crafting area and no armor slots.
// A double chest has the same shape with 54 container slots.
const (
	chestMainStart   = world.ChestSlots
	chestMainEnd     = chestMainStart + 26
	chestHotbarStart = chestMainEnd + 1
	chestHotbarEnd   = chestHotbarStart + 8
	chestSlotTotal   = chestHotbarEnd + 1
)

// chestWindowLayout describes a window with containerSlots container slots
// followed by the player's own inventory.
func chestWindowLayout(id int8, containerSlots int) windowLayout {
	slots := int16(containerSlots)

	return windowLayout{
		id:       id,
		gridSize: 0,
		// A chest has no crafting area at all, and slot 0 is a container slot
		// rather than a crafting output. Both ranges are empty so no slot can
		// fall into them.
		gridStart:      -1,
		gridEnd:        -1,
		armorStart:     -1,
		armorEnd:       -1,
		containerStart: 0,
		containerEnd:   slots - 1,
		mainStart:      slots,
		mainEnd:        slots + 26,
		hotbarStart:    slots + 27,
		hotbarEnd:      slots + 35,
		// Chest slot containerSlots is the same item as player slot 9.
		invShift: slotMainStart - slots,
		total:    containerSlots + 36,
	}
}

// chestFacing returns the metadata a chest placed by a player looking along
// yaw takes.
//
// This is not decoration, and getting it wrong is why a placed chest used to
// disappear on the next chunk load. A 1.8 client reads a chunk section value
// by looking it up in the registry of valid block states and falls back to air
// when it finds none, and a chest's facing property is horizontal only — so
// the four valid chests are metadata 2 through 5, and the metadata 0 this
// server used to store is not a chest at all. The client drew air, exactly as
// it was told to, and only the client's own placement prediction ever made one
// visible.
//
// The rule is vanilla's, from BlockChest.onBlockPlacedBy: take the player's
// horizontal facing and face the chest the other way, so it opens towards
// them.
func chestFacing(yaw float32) int32 {
	// Vanilla's horizontal index: south 0, west 1, north 2, east 3.
	index := int(math.Floor(float64(yaw)*4.0/360.0+0.5)) & 3

	// The metadata of each horizontal index, then its opposite: north 2 and
	// south 3 are opposites, west 4 and east 5 are opposites.
	switch index {
	case 0: // south
		return 2 // north
	case 1: // west
		return 5 // east
	case 2: // north
		return 3 // south
	default: // east
		return 4 // west
	}
}

// horizontalNeighbours are the four positions a chest can pair with. Vanilla
// walks the horizontal plane in the order west, east, north, south, and the
// order matters nowhere here — a chest has at most one partner.
func horizontalNeighbours(pos world.BlockPos) [4]world.BlockPos {
	return [4]world.BlockPos{
		{X: pos.X - 1, Y: pos.Y, Z: pos.Z},
		{X: pos.X + 1, Y: pos.Y, Z: pos.Z},
		{X: pos.X, Y: pos.Y, Z: pos.Z - 1},
		{X: pos.X, Y: pos.Y, Z: pos.Z + 1},
	}
}

// canPlaceChestAt reports whether a chest of the given kind may be placed at a
// position. This is BlockChest.canPlaceBlockAt: count the neighbouring chests
// of the same kind, refuse if any of them is already half of a double chest,
// and allow at most one.
//
// Two things follow that are easy to get wrong. A chest may not join a pair,
// so no three chests ever meet — a chest with two chest neighbours would have
// two partners and vanilla has no such thing. And the kind is the exact block:
// a trapped chest neither pairs with nor blocks a plain one, because vanilla
// compares against the block instance and they are two different blocks.
func (c *Connection) canPlaceChestAt(kind int32, pos world.BlockPos) bool {
	adjacent := 0
	for _, neighbour := range horizontalNeighbours(pos) {
		if c.world.GetBlock(neighbour.X, neighbour.Y, neighbour.Z)>>4 != kind {
			continue
		}

		if c.isDoubleChestAt(kind, neighbour) {
			return false
		}

		adjacent++
	}

	return adjacent <= 1
}

// isDoubleChestAt reports whether the chest at pos already has a partner.
//
// The position being placed into is still empty while this runs, so a chest
// does not count itself as its neighbour's partner — which is what makes the
// rule allow a second chest beside a lone one and refuse a third.
func (c *Connection) isDoubleChestAt(kind int32, pos world.BlockPos) bool {
	if c.world.GetBlock(pos.X, pos.Y, pos.Z)>>4 != kind {
		return false
	}

	for _, neighbour := range horizontalNeighbours(pos) {
		if c.world.GetBlock(neighbour.X, neighbour.Y, neighbour.Z)>>4 == kind {
			return true
		}
	}

	return false
}

// The horizontal facings a chest can take, as protocol 47 metadata.
const (
	facingNorth int32 = 2
	facingSouth int32 = 3
	facingWest  int32 = 4
	facingEast  int32 = 5
)

// refreshChestCluster recomputes the facing of a chest and of every chest
// touching it, and tells the clients what changed.
//
// This is BlockChest.onBlockAdded, which runs checkForSurroundingChests on the
// block that changed and on each neighbouring chest. Without it a double chest
// can end up with two halves facing different ways, which is not a shape the
// model has: breaking one half of a pair and placing a chest nearby left the
// survivor still oriented for a partner it no longer had.
func (c *Connection) refreshChestCluster(kind int32, pos world.BlockPos) {
	c.refreshChestFacing(kind, pos)
	for _, neighbour := range horizontalNeighbours(pos) {
		c.refreshChestFacing(kind, neighbour)
	}
}

// refreshChestFacing writes the facing a chest should have, if it is not
// already the one it has.
func (c *Connection) refreshChestFacing(kind int32, pos world.BlockPos) {
	state := c.world.GetBlock(pos.X, pos.Y, pos.Z)
	if state>>4 != kind {
		return
	}

	next := kind<<4 | c.surroundingChestFacing(kind, pos, state&0xF)
	if next == state {
		return
	}

	c.world.SetBlock(pos.X, pos.Y, pos.Z, next)

	change := &v1_8.PlayClientboundBlockChange{
		Location: blockPos(pos.X, pos.Y, pos.Z),
		Type:     next,
	}
	c.players.BroadcastExcept(change, c.self.EntityID)
	_ = c.send(change)
}

// surroundingChestFacing is BlockChest.checkForSurroundingChests: the facing a
// chest takes given its partner, if it has one.
//
// A pair faces across its own axis — two chests side by side along x face
// north or south, two along z face east or west — because the double model is
// twice as wide as it is deep. Which of the two it picks is decided first by
// the partner's current facing and then by what is solid around the pair: a
// chest turns away from a wall, so a double chest against a north wall opens
// south.
func (c *Connection) surroundingChestFacing(kind int32, pos world.BlockPos, current int32) int32 {
	north := world.BlockPos{X: pos.X, Y: pos.Y, Z: pos.Z - 1}
	south := world.BlockPos{X: pos.X, Y: pos.Y, Z: pos.Z + 1}
	west := world.BlockPos{X: pos.X - 1, Y: pos.Y, Z: pos.Z}
	east := world.BlockPos{X: pos.X + 1, Y: pos.Y, Z: pos.Z}

	switch {
	case c.isChestAt(kind, north) || c.isChestAt(kind, south):
		// Paired along z, so the pair faces west or east.
		partner := north
		if !c.isChestAt(kind, north) {
			partner = south
		}

		facing := facingEast
		if c.world.GetBlock(partner.X, partner.Y, partner.Z)&0xF == facingWest {
			facing = facingWest
		}

		partnerWest := world.BlockPos{X: partner.X - 1, Y: partner.Y, Z: partner.Z}
		partnerEast := world.BlockPos{X: partner.X + 1, Y: partner.Y, Z: partner.Z}

		blockedWest := c.isFullBlock(west) || c.isFullBlock(partnerWest)
		blockedEast := c.isFullBlock(east) || c.isFullBlock(partnerEast)
		if blockedWest && !blockedEast {
			facing = facingEast
		}
		if blockedEast && !blockedWest {
			facing = facingWest
		}

		return facing

	case c.isChestAt(kind, west) || c.isChestAt(kind, east):
		// Paired along x, so the pair faces north or south.
		partner := west
		if !c.isChestAt(kind, west) {
			partner = east
		}

		facing := facingSouth
		if c.world.GetBlock(partner.X, partner.Y, partner.Z)&0xF == facingNorth {
			facing = facingNorth
		}

		partnerNorth := world.BlockPos{X: partner.X, Y: partner.Y, Z: partner.Z - 1}
		partnerSouth := world.BlockPos{X: partner.X, Y: partner.Y, Z: partner.Z + 1}

		blockedNorth := c.isFullBlock(north) || c.isFullBlock(partnerNorth)
		blockedSouth := c.isFullBlock(south) || c.isFullBlock(partnerSouth)
		if blockedNorth && !blockedSouth {
			facing = facingSouth
		}
		if blockedSouth && !blockedNorth {
			facing = facingNorth
		}

		return facing

	default:
		// A lone chest keeps the facing it was placed with.
		return current
	}
}

// isChestAt reports whether a position holds a chest of the given kind.
func (c *Connection) isChestAt(kind int32, pos world.BlockPos) bool {
	return c.world.GetBlock(pos.X, pos.Y, pos.Z)>>4 == kind
}

// isFullBlock reports whether a position holds a solid opaque cube, which is
// what a chest turns away from.
func (c *Connection) isFullBlock(pos world.BlockPos) bool {
	state := c.world.GetBlock(pos.X, pos.Y, pos.Z)
	if state>>4 == 0 {
		return false
	}

	block, ok := c.lookupBlock(state)
	if !ok {
		return false
	}

	return block.BoundingBox == "block" && !block.Transparent
}

// chestPair returns the positions of the chest at pos and the one it forms a
// double chest with, in window order.
//
// Vanilla pairs a chest with a single horizontal neighbour, and the half with
// the smaller coordinate holds the first slots — BlockChest builds the large
// inventory with the north or west half first. Sorting the pair says the same
// thing without naming a direction.
func (c *Connection) chestPair(pos world.BlockPos) []world.BlockPos {
	// A chest pairs only with its own kind: vanilla never joins a plain chest
	// to a trapped one.
	kind := c.world.GetBlock(pos.X, pos.Y, pos.Z) >> 4

	for _, neighbour := range horizontalNeighbours(pos) {
		if c.world.GetBlock(neighbour.X, neighbour.Y, neighbour.Z)>>4 != kind {
			continue
		}

		pair := []world.BlockPos{pos, neighbour}
		sort.Slice(pair, func(i, j int) bool {
			if pair[i].X != pair[j].X {
				return pair[i].X < pair[j].X
			}

			return pair[i].Z < pair[j].Z
		})

		return pair
	}

	return []world.BlockPos{pos}
}

// openChest shows the chest at (x, y, z) — or the double chest it is half of —
// and loads the stored contents.
func (c *Connection) openChest(x, y, z int) error {
	// Whatever the window being replaced held belongs where it came from: the
	// crafting area goes back to the player, and a chest opened without an
	// intervening close is written back to the world.
	c.emptyCraftingArea()
	c.closeChest()

	c.chestPositions = c.chestPair(world.BlockPos{X: x, Y: y, Z: z})
	c.chestItems = nil
	for _, pos := range c.chestPositions {
		contents := c.world.Chest(pos)
		c.chestItems = append(c.chestItems, contents[:]...)
	}

	// Vanilla's getNextWindowId, which cycles 1-100 and never yields 0.
	c.nextWindowID = c.nextWindowID%100 + 1
	c.windowID = c.nextWindowID
	c.windowKind = windowChest

	// BlockChest's display name, a translation key rather than text, so each
	// client shows it in its own language.
	title := `{"translate":"container.chest"}`
	if len(c.chestPositions) > 1 {
		title = `{"translate":"container.chestDouble"}`
	}

	if err := c.send(&v1_8.PlayClientboundOpenWindow{
		WindowID:      uint8(c.windowID),
		InventoryType: "minecraft:chest",
		WindowTitle:   title,
		SlotCount:     uint8(len(c.chestItems)),
	}); err != nil {
		return err
	}

	return c.sendWindowItems()
}

// closeChest writes the open chest back to the world and forgets it.
//
// The contents are flushed on every click as well as here, so a second player
// opening the same chest sees the items rather than a snapshot taken when the
// first player opened it. Two players holding the same chest open still race —
// the last writer wins — which is worth fixing when the server grows a real
// shared-container lock.
func (c *Connection) closeChest() {
	if c.windowKind != windowChest {
		return
	}

	c.flushChest()
	c.chestItems = nil
	c.chestPositions = nil
}

// flushChest stores the open chest's contents in the world, one stored chest
// per half.
func (c *Connection) flushChest() {
	if c.windowKind != windowChest {
		return
	}

	for i, pos := range c.chestPositions {
		var contents world.ChestContents
		copy(contents[:], c.chestItems[i*world.ChestSlots:])
		c.world.SetChest(pos, contents)
	}
}

// spillChest drops everything a broken chest held and deletes its storage.
func (c *Connection) spillChest(x, y, z int) {
	contents := c.world.RemoveChest(world.BlockPos{X: x, Y: y, Z: z})

	pos := c.self.GetPosition()
	groundAt := c.groundAtFunc()
	for _, stack := range contents {
		if stack.IsEmpty() {
			continue
		}
		c.players.SpawnItemEntity(
			c.self.EntityID, stackToSlot(stack),
			float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, pos.Yaw, groundAt,
		)
	}
}

// stackToSlot converts stored contents into the runtime slot type.
func stackToSlot(s world.ItemStack) player.Slot {
	if s.IsEmpty() {
		return player.EmptySlot
	}

	return player.Slot{BlockID: s.ID, ItemCount: s.Count, ItemDamage: s.Damage}
}

// slotToStack converts a runtime slot into stored contents.
func slotToStack(s player.Slot) world.ItemStack {
	if s.IsEmpty() {
		return world.EmptyStack
	}

	return world.ItemStack{ID: s.BlockID, Count: s.ItemCount, Damage: s.ItemDamage}
}
