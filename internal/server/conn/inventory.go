package conn

import (
	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/server/internal/server/player"
	"github.com/go-theft-craft/server/pkg/world"
)

// Player inventory window (window 0) slot layout.
// These are protocol-level indices, not internal Inventory array indices.
const (
	slotCraftOutput = 0
	slotCraftStart  = 1
	slotCraftEnd    = 4
	slotCraftCount  = slotCraftEnd - slotCraftStart + 1

	slotHelmet     = 5
	slotChestplate = 6
	slotLeggings   = 7
	slotBoots      = 8
	slotArmorStart = slotHelmet
	slotArmorEnd   = slotBoots

	slotMainStart = 9
	slotMainEnd   = 35

	slotHotbarStart = 36
	slotHotbarEnd   = 44

	slotTotal = 45

	slotOutside = -999 // click outside window
)

// Crafting table window (window type "minecraft:crafting_table") slot layout.
// It has no armor slots, so its inventory section sits one slot lower than the
// player window's: output 0, the 3x3 grid 1-9, main inventory 10-36, hotbar
// 37-45.
const (
	tableGridEnd     = 9
	tableMainStart   = 10
	tableMainEnd     = 36
	tableHotbarStart = 37
	tableHotbarEnd   = 45
	tableSlotTotal   = 46

	// tableAdvertisedSlots is what OpenWindow reports, and it has to be zero.
	// The 1.8 client picks the workbench GUI with `!packetIn.hasSlots()`; a
	// positive count sends it down the generic container path instead, which
	// draws that many slots as a chest. Vanilla reports no slots here for the
	// same reason — the count is for real containers, and a workbench is an
	// interface, not one.
	tableAdvertisedSlots = 0
)

// craftingTableBlockID is the inventory item ID of the block a right-click
// opens the 3x3 window on. It is a protocol 47 number because the inventory is
// protocol 47 throughout; the world names the block instead. See blockstate.go.
const craftingTableBlockID = 58

// windowLayout gives the slot ranges of whichever window is open. Every click
// handler works in these coordinates, so one implementation serves the player's
// own window and a crafting table's, which differ only in where each section
// starts and how big the grid is.
type windowLayout struct {
	id       int8
	gridSize int // side length: 2 for the player window, 3 for a table

	gridStart int16
	gridEnd   int16

	// armorStart is -1 when the window has no armor slots.
	armorStart int16
	armorEnd   int16

	// containerStart is -1 when the window shows no container of its own. A
	// chest sets it; the player window and a crafting table do not.
	containerStart int16
	containerEnd   int16

	mainStart   int16
	mainEnd     int16
	hotbarStart int16
	hotbarEnd   int16

	// invShift is added to a window slot in the main or hotbar range to reach
	// the same item's slot in the player inventory window.
	invShift int16

	total int
}

func playerWindowLayout() windowLayout {
	return windowLayout{
		id:             0,
		gridSize:       2,
		gridStart:      slotCraftStart,
		gridEnd:        slotCraftEnd,
		armorStart:     slotArmorStart,
		armorEnd:       slotArmorEnd,
		containerStart: -1,
		containerEnd:   -1,
		mainStart:      slotMainStart,
		mainEnd:        slotMainEnd,
		hotbarStart:    slotHotbarStart,
		hotbarEnd:      slotHotbarEnd,
		invShift:       0,
		total:          slotTotal,
	}
}

func tableWindowLayout(id int8) windowLayout {
	return windowLayout{
		id:             id,
		gridSize:       3,
		gridStart:      slotCraftStart,
		gridEnd:        tableGridEnd,
		armorStart:     -1,
		armorEnd:       -1,
		containerStart: -1,
		containerEnd:   -1,
		mainStart:      tableMainStart,
		mainEnd:        tableMainEnd,
		hotbarStart:    tableHotbarStart,
		hotbarEnd:      tableHotbarEnd,
		invShift:       -1,
		total:          tableSlotTotal,
	}
}

// hasArmor reports whether the window shows the player's armor. A crafting
// table does not, so shift-clicking a helmet inside one stores it rather than
// wearing it.
func (l windowLayout) hasArmor() bool {
	return l.armorStart >= 0
}

// hasCrafting reports whether the window has a crafting area. A chest has
// none, and its slot 0 is a container slot rather than a crafting output.
func (l windowLayout) hasCrafting() bool {
	return l.gridStart >= 0
}

// hasContainer reports whether the window shows a container the world owns.
func (l windowLayout) hasContainer() bool {
	return l.containerStart >= 0
}

// isContainer reports whether a window slot addresses that container.
func (l windowLayout) isContainer(slot int16) bool {
	return l.hasContainer() && slot >= l.containerStart && slot <= l.containerEnd
}

// gridCells is how many slots the crafting grid has.
func (l windowLayout) gridCells() int {
	return l.gridSize * l.gridSize
}

// layout returns the layout of the window the player currently has open.
//
// The window ID alone no longer decides this: a chest and a crafting table
// both hold a non-zero ID and their slots mean entirely different things, so
// the kind is tracked beside the ID.
func (c *Connection) layout() windowLayout {
	if c.windowID == 0 {
		return playerWindowLayout()
	}

	if c.windowKind == windowChest {
		return chestWindowLayout(c.windowID, len(c.chestItems))
	}

	return tableWindowLayout(c.windowID)
}

// emptyCraftingGrid returns a grid with every cell empty. The zero value will
// not do: an empty slot is block ID -1, and a zeroed one reads as block 0.
func emptyCraftingGrid() [9]player.Slot {
	var grid [9]player.Slot
	for i := range grid {
		grid[i] = player.EmptySlot
	}

	return grid
}

// sendWindowItems sends every slot of the window the player has open.
func (c *Connection) sendWindowItems() error {
	l := c.layout()

	items := make([]v1_8.Slot, l.total)
	for i := range items {
		items[i] = player.ToGeneratedSlot(c.getWindowSlot(int16(i)))
	}

	return c.send(&v1_8.PlayClientboundWindowItems{WindowID: uint8(l.id), Items: items})
}

// sendSetSlot sends a single slot update to the client.
func (c *Connection) sendSetSlot(windowID int8, slotIndex int16, slot player.Slot) error {
	return c.send(&v1_8.PlayClientboundSetSlot{
		WindowID: windowID,
		Slot:     slotIndex,
		Item:     player.ToGeneratedSlot(slot),
	})
}

// handleWindowClick processes a WindowClick (0x0E) packet. The clicked item
// the client echoes back (value.Item) is not needed for validation.
func (c *Connection) handleWindowClick(value *v1_8.PlayServerboundWindowClick) error {
	c.counted(world.MeasureInventory, 1)

	windowID := int8(value.WindowID)
	slotIndex := value.Slot
	button := value.MouseButton
	actionID := value.Action
	mode := int(value.Mode)

	// A click in a window the player does not have open is stale — the window
	// closed while the click was in flight — and applying it would move items
	// through slot numbers that no longer mean what the client meant.
	if windowID != c.windowID {
		return c.sendTransaction(windowID, actionID, false)
	}

	c.log.Info("window click", "window", windowID, "slot", slotIndex, "button", button, "mode", mode, "craftOutput", c.craftingOutput, "cursor", c.cursorSlot)
	c.dispatchClick(slotIndex, button, mode)
	c.log.Info("after click", "craftOutput", c.craftingOutput, "cursor", c.cursorSlot)

	// An open chest is written back after every click rather than only on
	// close, so items are not lost if the session ends with the window open.
	c.flushChest()

	// Full inventory sync so client matches server state.
	_ = c.sendWindowItems()
	// Sync cursor slot (window -1, slot -1).
	_ = c.sendSetSlot(-1, -1, c.cursorSlot)

	// Always accept the transaction.
	return c.sendTransaction(windowID, actionID, true)
}

func (c *Connection) sendTransaction(windowID int8, actionID int16, accepted bool) error {
	return c.send(&v1_8.PlayClientboundTransaction{
		WindowID: windowID,
		Action:   actionID,
		Accepted: accepted,
	})
}

func (c *Connection) dispatchClick(slot int16, button int8, mode int) {
	switch mode {
	case 0:
		c.handleNormalClick(slot, button)
	case 1:
		c.handleShiftClick(slot, button)
	case 2:
		c.handleNumberKey(slot, button)
	case 3:
		c.handleMiddleClick(slot)
	case 4:
		c.handleDropClick(slot, button)
	case 5:
		c.handleDragClick(slot, button)
	case 6:
		c.handleDoubleClick(slot)
	}
}

// inventorySlot maps a window slot onto the player inventory's own protocol
// slot, reporting false for slots that do not address the inventory — the
// crafting area, and anything out of range.
func (l windowLayout) inventorySlot(slot int16) (int16, bool) {
	if l.hasArmor() && slot >= l.armorStart && slot <= l.armorEnd {
		return slot + l.invShift, true
	}
	if slot >= l.mainStart && slot <= l.hotbarEnd {
		return slot + l.invShift, true
	}

	return 0, false
}

// getWindowSlot reads a slot of the open window.
func (c *Connection) getWindowSlot(slot int16) player.Slot {
	return c.getSlotIn(c.layout(), slot)
}

// setWindowSlot writes a slot of the open window.
func (c *Connection) setWindowSlot(slot int16, item player.Slot) {
	c.setSlotIn(c.layout(), slot, item)
}

// getSlotIn reads a slot in the coordinates of the given window.
func (c *Connection) getSlotIn(l windowLayout, slot int16) player.Slot {
	switch {
	case slot == slotCursor:
		return c.cursorSlot
	case l.isContainer(slot):
		return stackToSlot(c.chestItems[slot-l.containerStart])
	case l.hasCrafting() && slot == slotCraftOutput:
		return c.craftingOutput
	case l.hasCrafting() && slot >= l.gridStart && slot <= l.gridEnd:
		return c.craftingGrid[slot-l.gridStart]
	default:
		if proto, ok := l.inventorySlot(slot); ok {
			return c.self.Inventory.GetProtocolSlot(int(proto))
		}

		return player.EmptySlot
	}
}

// setSlotIn writes a slot in the coordinates of the given window and
// broadcasts equipment changes to trackers if needed.
func (c *Connection) setSlotIn(l windowLayout, slot int16, item player.Slot) {
	switch {
	case slot == slotCursor:
		c.cursorSlot = item
	case l.isContainer(slot):
		c.chestItems[slot-l.containerStart] = slotToStack(item)
	case l.hasCrafting() && slot == slotCraftOutput:
		c.craftingOutput = item
	case l.hasCrafting() && slot >= l.gridStart && slot <= l.gridEnd:
		c.craftingGrid[slot-l.gridStart] = item
	default:
		if proto, ok := l.inventorySlot(slot); ok {
			c.self.Inventory.SetProtocolSlot(int(proto), item)
			c.broadcastEquipmentIfNeeded(proto)
		}
	}
}

// broadcastSingleEquipment sends one EntityEquipment (0x04) update to the
// player's trackers.
func (c *Connection) broadcastSingleEquipment(entityID int32, equipSlot int16, slot player.Slot) {
	value := player.BuildSingleEquipmentValue(entityID, equipSlot, slot)
	c.players.BroadcastToTrackers(&value, entityID)
}

// broadcastEquipmentIfNeeded sends equipment updates to trackers when
// armor or held item slots change.
func (c *Connection) broadcastEquipmentIfNeeded(protoSlot int16) {
	eid := c.self.EntityID
	switch {
	case protoSlot == slotHelmet:
		slot := c.self.Inventory.GetArmor(3)
		c.broadcastSingleEquipment(eid, 4, slot)
	case protoSlot == slotChestplate:
		slot := c.self.Inventory.GetArmor(2)
		c.broadcastSingleEquipment(eid, 3, slot)
	case protoSlot == slotLeggings:
		slot := c.self.Inventory.GetArmor(1)
		c.broadcastSingleEquipment(eid, 2, slot)
	case protoSlot == slotBoots:
		slot := c.self.Inventory.GetArmor(0)
		c.broadcastSingleEquipment(eid, 1, slot)
	case protoSlot >= slotHotbarStart && protoSlot <= slotHotbarEnd:
		// Check if this is the active hotbar slot.
		hotbarIdx := protoSlot - slotHotbarStart
		if hotbarIdx == int16(c.self.Inventory.GetHeldSlot()) {
			heldItem := c.self.Inventory.HeldItem()
			c.broadcastSingleEquipment(eid, 0, heldItem)
		}
	}
}

// handleNormalClick handles mode 0: left-click (pickup/place/swap) and right-click (half-pickup/place-one).
func (c *Connection) handleNormalClick(slot int16, button int8) {
	l := c.layout()

	if slot == slotOutside {
		// Click outside window: drop cursor item.
		if !c.cursorSlot.IsEmpty() {
			dropped := 1
			if button == 0 {
				dropped = int(c.cursorSlot.ItemCount)
			}
			c.dropFromSlot(l, slotCursor, dropped)
		}
		return
	}

	if slot < 0 || slot > l.hotbarEnd {
		return
	}

	// Clicking crafting output.
	if l.hasCrafting() && slot == slotCraftOutput {
		if c.craftingOutput.IsEmpty() {
			return
		}
		if !c.cursorSlot.IsEmpty() {
			// Can only pick up crafting output if cursor matches and has room.
			// Vanilla refuses a craft it cannot deposit whole rather than
			// splitting the result, so the whole result has to fit.
			if !canStack(c.cursorSlot, c.craftingOutput) {
				return
			}
			if int(c.cursorSlot.ItemCount)+int(c.craftingOutput.ItemCount) > world.MaxStackSize {
				return
			}
		}
		// The result comes into existence here, not when the grid started
		// matching a recipe: an offer nobody took mints nothing.
		c.mintInto(l, slotCraftOutput)
		c.transfer(l, slotCraftOutput, slotCursor, int(c.craftingOutput.ItemCount))
		c.consumeCraftingIngredients()
		c.updateCraftingOutput()
		return
	}

	current := c.getWindowSlot(slot)

	if button == 0 { // Left click
		if c.cursorSlot.IsEmpty() && current.IsEmpty() {
			return
		}
		switch {
		case c.cursorSlot.IsEmpty():
			// Pick up entire stack.
			c.transfer(l, slot, slotCursor, int(current.ItemCount))

		case current.IsEmpty():
			// Place entire cursor stack.
			c.transfer(l, slotCursor, slot, int(c.cursorSlot.ItemCount))

		case canStack(c.cursorSlot, current):
			// Merge cursor into slot, or swap when the slot is already full.
			if int(current.ItemCount) >= world.MaxStackSize {
				c.swapSlots(l, slotCursor, slot)
			} else {
				c.transfer(l, slotCursor, slot, int(c.cursorSlot.ItemCount))
			}

		default:
			// Swap cursor and slot.
			c.swapSlots(l, slotCursor, slot)
		}
	} else { // Right click
		switch {
		case c.cursorSlot.IsEmpty() && !current.IsEmpty():
			// Pick up half, rounded up, which is what vanilla hands you.
			c.transfer(l, slot, slotCursor, int((current.ItemCount+1)/2))

		case c.cursorSlot.IsEmpty():
			// Nothing in either place.

		case current.IsEmpty(), canStack(c.cursorSlot, current) && int(current.ItemCount) < world.MaxStackSize:
			// Place one from cursor, onto an empty slot or an existing stack.
			c.transfer(l, slotCursor, slot, 1)

		case !current.IsEmpty():
			// Swap.
			c.swapSlots(l, slotCursor, slot)
		}
	}

	// Update crafting output if a crafting slot was modified.
	if slot >= l.gridStart && slot <= l.gridEnd {
		c.updateCraftingOutput()
	}
}

// handleShiftClick handles mode 1: shift-click to move items between sections.
func (c *Connection) handleShiftClick(slot int16, _ int8) {
	l := c.layout()

	if l.hasCrafting() && slot == slotCraftOutput {
		c.shiftCraftAll()
		return
	}

	if slot < 0 || slot > l.hotbarEnd {
		return
	}

	item := c.getWindowSlot(slot)
	if item.IsEmpty() {
		return
	}

	// Whatever the destination could not take stays in the source slot: the
	// section fills from the source rather than from a copy of it, so a
	// partial move cannot leave the moved items in both places.
	switch {
	// A container splits the window in two, and shift-clicking crosses the
	// divide: out of the chest into the inventory, or out of the inventory
	// into the chest. Armor is not equipped from a chest window, the same way
	// a crafting table does not equip it.
	case l.isContainer(slot):
		c.addToSection(l, slot, l.mainStart, l.hotbarEnd)
	case l.hasContainer() && slot >= l.mainStart && slot <= l.hotbarEnd:
		c.addToSection(l, slot, l.containerStart, l.containerEnd)

	case l.hasArmor() && slot >= l.armorStart && slot <= l.armorEnd:
		// Armor → main inventory or hotbar.
		c.addToSection(l, slot, l.mainStart, l.hotbarEnd)
	case slot >= l.mainStart && slot <= l.mainEnd:
		// Main inventory → try armor first if applicable, then hotbar.
		if !c.equipArmor(l, slot) {
			c.addToSection(l, slot, l.hotbarStart, l.hotbarEnd)
		}
	case slot >= l.hotbarStart && slot <= l.hotbarEnd:
		// Hotbar → try armor first if applicable, then main inventory.
		if !c.equipArmor(l, slot) {
			c.addToSection(l, slot, l.mainStart, l.mainEnd)
		}
	case slot >= l.gridStart && slot <= l.gridEnd:
		// Crafting grid → main or hotbar.
		c.addToSection(l, slot, l.mainStart, l.hotbarEnd)
	}

	if slot >= l.gridStart && slot <= l.gridEnd {
		c.updateCraftingOutput()
	}
}

// equipArmor moves a slot's contents into their armor slot when the item is
// armor and that slot is empty. A window without armor slots — a crafting
// table — never equips: vanilla stores the piece instead.
func (c *Connection) equipArmor(l windowLayout, from int16) bool {
	if !l.hasArmor() {
		return false
	}

	item := c.getSlotIn(l, from)
	armorSlot := armorSlotForItem(item.BlockID)
	if armorSlot < 0 || !c.getSlotIn(l, armorSlot).IsEmpty() {
		return false
	}

	return c.transfer(l, from, armorSlot, int(item.ItemCount)) > 0
}

// shiftCraftAll crafts repeatedly while a whole result stack still fits, which
// is what vanilla does when the crafting output is shift-clicked. Taking the
// output with a normal click crafts once; shift-click drains the grid.
func (c *Connection) shiftCraftAll() {
	l := c.layout()
	crafted := false

	// A grid slot holds at most 64 items, so no grid can support more crafts
	// than that. The bound keeps a matcher that fails to consume its
	// ingredients from spinning here.
	for range world.MaxStackSize {
		result := c.craftingOutput
		if result.IsEmpty() {
			break
		}
		// Vanilla refuses a craft it cannot deposit whole rather than
		// splitting the result stack.
		if c.spaceForItem(result, l.mainStart, l.hotbarEnd) < int(result.ItemCount) {
			break
		}

		// One craft, one mint: the result becomes real as it leaves the output
		// slot, and the ingredients stop being items in the same breath.
		c.mintInto(l, slotCraftOutput)
		c.addToSection(l, slotCraftOutput, l.mainStart, l.hotbarEnd)
		c.consumeCraftingIngredients()
		c.craftingOutput = c.matchCraftingRecipe()
		crafted = true
	}

	if crafted {
		c.updateCraftingOutput()
	}
}

// addToSection moves as much of a slot's contents as fits into slots [lo, hi]
// and returns the count that did not fit, which stays where it was.
//
// It reports the leftover rather than a bool because a partial add still moves
// what fits: a caller that read failure as "nothing happened" and cleared the
// source duplicated the placed items.
func (c *Connection) addToSection(l windowLayout, from, lo, hi int16) int {
	remaining := int(c.getSlotIn(l, from).ItemCount)

	// First pass: merge into existing stacks. Second pass: fill empty ones.
	// transfer refuses a slot holding something else and caps at what the
	// destination has room for, so both passes are the same loop.
	for _, wantEmpty := range [2]bool{false, true} {
		for s := lo; s <= hi && remaining > 0; s++ {
			if s == from || c.getSlotIn(l, s).IsEmpty() != wantEmpty {
				continue
			}
			remaining -= c.transfer(l, from, s, remaining)
		}
	}

	return remaining
}

// spaceForItem reports how many of item slots [lo, hi] can still accept,
// without moving anything.
func (c *Connection) spaceForItem(item player.Slot, lo, hi int16) int {
	space := 0
	for s := lo; s <= hi; s++ {
		existing := c.getWindowSlot(s)
		switch {
		case existing.IsEmpty():
			space += world.MaxStackSize
		case canStack(existing, item) && int(existing.ItemCount) < world.MaxStackSize:
			space += world.MaxStackSize - int(existing.ItemCount)
		}
	}

	return space
}

// handleNumberKey handles mode 2: pressing number keys 1-9 to swap with hotbar.
func (c *Connection) handleNumberKey(slot int16, button int8) {
	l := c.layout()

	if slot < 0 || slot > l.hotbarEnd {
		return
	}
	hotbarSlot := l.hotbarStart + int16(button)
	if hotbarSlot < l.hotbarStart || hotbarSlot > l.hotbarEnd {
		return
	}

	c.swapSlots(l, slot, hotbarSlot)

	if slot >= l.gridStart && slot <= l.gridEnd {
		c.updateCraftingOutput()
	}
}

// handleMiddleClick handles mode 3: middle-click in creative mode (clone to cursor).
func (c *Connection) handleMiddleClick(slot int16) {
	l := c.layout()
	if slot < 0 || slot > l.hotbarEnd {
		return
	}
	item := c.getWindowSlot(slot)
	if item.IsEmpty() {
		return
	}

	// A clone is a full stack of new items, and whatever the cursor held stops
	// existing: creative mode conjures and destroys, and saying so is the only
	// way the index does not report the clone as the original turning up twice.
	c.retireIDs(c.cursorSlot.IDs, c.locationOf(l, slotCursor))
	c.cursorSlot = player.Slot{BlockID: item.BlockID, ItemCount: world.MaxStackSize, ItemDamage: item.ItemDamage}
	c.mintInto(l, slotCursor)
}

// handleDropClick handles mode 4: Q key drop.
func (c *Connection) handleDropClick(slot int16, button int8) {
	l := c.layout()

	if slot == slotOutside {
		// Drop cursor (already handled by normal click path when mode=4 slot=-999).
		// In practice this shouldn't happen, but handle gracefully.
		return
	}
	if slot < 0 || slot > l.hotbarEnd {
		return
	}

	item := c.getWindowSlot(slot)
	if item.IsEmpty() {
		return
	}

	// Button 0 drops one; Ctrl+Q — button 1 — drops the whole stack.
	dropped := 1
	if button != 0 {
		dropped = int(item.ItemCount)
	}
	c.dropFromSlot(l, slot, dropped)

	if slot >= l.gridStart && slot <= l.gridEnd {
		c.updateCraftingOutput()
	}
}

// handleDragClick handles mode 5: drag (paint) click. This is a 3-phase operation:
// Phase 1: start drag (slot=-999, button=0/4/8 for left/right/middle)
// Phase 2: add slot (button=1/5/9)
// Phase 3: end drag (slot=-999, button=2/6/10)
func (c *Connection) handleDragClick(slot int16, button int8) {
	switch button {
	case 0: // Start left drag
		c.dragActive = true
		c.dragMode = 0
		c.dragSlots = nil
	case 4: // Start right drag
		c.dragActive = true
		c.dragMode = 1
		c.dragSlots = nil
	case 1, 5: // Add slot
		if c.dragActive && slot >= 0 && slot <= c.layout().hotbarEnd {
			c.dragSlots = append(c.dragSlots, slot)
		}
	case 2: // End left drag
		if c.dragActive && c.dragMode == 0 {
			c.finishDrag()
		}
		c.dragActive = false
	case 6: // End right drag
		if c.dragActive && c.dragMode == 1 {
			c.finishDrag()
		}
		c.dragActive = false
	default:
		c.dragActive = false
	}
}

func (c *Connection) finishDrag() {
	if c.cursorSlot.IsEmpty() || len(c.dragSlots) == 0 {
		return
	}

	// A drag fills grid cells the same way clicking them one at a time does, so
	// it has to leave the same offer behind. Every other click path refreshes
	// the output; this one did not, and a grid filled by dragging silently
	// crafted nothing.
	defer func() {
		l := c.layout()
		for _, s := range c.dragSlots {
			if s >= l.gridStart && s <= l.gridEnd {
				c.updateCraftingOutput()

				return
			}
		}
	}()

	l := c.layout()

	// A left drag spreads the cursor evenly over the painted slots; a right
	// drag leaves one in each. Both take from the cursor a slot at a time, and
	// what none of them could hold stays on the cursor.
	perSlot := 1
	if c.dragMode == 0 {
		perSlot = max(int(c.cursorSlot.ItemCount)/len(c.dragSlots), 1)
	}

	for _, s := range c.dragSlots {
		if c.cursorSlot.IsEmpty() {
			break
		}
		c.transfer(l, slotCursor, s, perSlot)
	}
}

// handleDoubleClick handles mode 6: double-click to collect matching items to cursor.
func (c *Connection) handleDoubleClick(_ int16) {
	if c.cursorSlot.IsEmpty() {
		return
	}

	l := c.layout()

	needed := world.MaxStackSize - int(c.cursorSlot.ItemCount)
	// Scan all inventory slots (skip crafting output).
	for s := l.gridStart; s <= l.hotbarEnd && needed > 0; s++ {
		item := c.getWindowSlot(s)
		if item.IsEmpty() || !canStack(item, c.cursorSlot) {
			continue
		}
		needed -= c.transfer(l, s, slotCursor, min(int(item.ItemCount), needed))
	}
}

// handleCreativeSlot processes a SetCreativeSlot (0x10) packet.
func (c *Connection) handleCreativeSlot(value *v1_8.PlayServerboundSetCreativeSlot) error {
	slotIndex := value.Slot
	item := slotFromGenerated(value.Item)

	// Slot -1: drop item.
	if slotIndex == -1 {
		if item.BlockID > 0 {
			// Straight out of the creative menu, so it comes from nowhere and
			// its identity is minted where it lands.
			dropped := player.Slot{BlockID: item.BlockID, ItemCount: item.ItemCount, ItemDamage: item.ItemDamage}
			c.dropStack(dropped, world.Nowhere)
		}
		return nil
	}

	if slotIndex < 0 || slotIndex > slotHotbarEnd {
		return nil
	}

	// Convert conn.Slot to player.Slot.
	pSlot := player.EmptySlot
	if item.BlockID != -1 {
		pSlot = player.Slot{BlockID: item.BlockID, ItemCount: item.ItemCount, ItemDamage: item.ItemDamage}
	}

	// A creative slot is always addressed in the player window's coordinates,
	// whatever window happens to be open on top of it.
	//
	// The client sends the state it wants rather than the movement that got
	// there, so the honest description is that whatever was in the slot stopped
	// existing and whatever is there now came into being. Creative mode is
	// where items are conjured; a server that called this a move would report
	// every creative click as a duplication.
	l := playerWindowLayout()
	replaced := c.getSlotIn(l, slotIndex)
	c.setSlotIn(l, slotIndex, pSlot)
	c.retireIDs(replaced.IDs, c.locationOf(l, slotIndex))
	c.mintInto(l, slotIndex)

	return nil
}

// openCraftingTable opens the 3x3 window on the table at (x, y, z) and tells
// the client to show it. The grid it opens onto is always empty: whatever the
// player left in the last one was returned when that window closed.
func (c *Connection) openCraftingTable() error {
	c.emptyCraftingArea()

	// Vanilla's getNextWindowId, which cycles 1-100 and never yields 0.
	c.closeChest()

	c.nextWindowID = c.nextWindowID%100 + 1
	c.windowID = c.nextWindowID
	c.windowKind = windowTable

	if err := c.send(&v1_8.PlayClientboundOpenWindow{
		WindowID:      uint8(c.windowID),
		InventoryType: "minecraft:crafting_table",
		// The title vanilla sends: BlockWorkbench's display name, which is a
		// translation key rather than text, so a client shows it in its own
		// language.
		WindowTitle: `{"translate":"tile.workbench.name"}`,
		SlotCount:   tableAdvertisedSlots,
	}); err != nil {
		return err
	}

	return c.sendWindowItems()
}

// emptyCraftingArea returns the crafting grid to the inventory, dropping what
// does not fit, and clears the output. It runs whenever the crafting area stops
// being reachable: a window opening over it, or the open window closing.
func (c *Connection) emptyCraftingArea() {
	l := c.layout()

	for i := range l.gridCells() {
		cell := l.gridStart + int16(i)
		if c.getSlotIn(l, cell).IsEmpty() {
			continue
		}
		// Only what did not fit is dropped, and it is dropped out of the cell
		// it is still in. Dropping the whole stack after part of it was already
		// stored would duplicate the stored part.
		if leftover := c.addToSection(l, cell, l.mainStart, l.hotbarEnd); leftover > 0 {
			c.dropFromSlot(l, cell, leftover)
		}
	}

	// The offer is not an item and never was: nothing is retired here, because
	// a result only comes into existence when somebody takes it.
	c.craftingGrid = emptyCraftingGrid()
	c.craftingOutput = player.EmptySlot
}

// handleCloseWindow processes a CloseWindow (0x0D) packet. The window ID it
// carries is not checked: a client closing a window the server no longer has
// open still means the player is looking at their own inventory again.
func (c *Connection) handleCloseWindow() error {
	c.emptyCraftingArea()
	c.closeChest()
	c.windowID = 0
	c.windowKind = windowPlayer

	// Drop cursor item.
	if !c.cursorSlot.IsEmpty() {
		c.dropFromSlot(c.layout(), slotCursor, int(c.cursorSlot.ItemCount))
	}

	return nil
}

// handleTransaction processes a Transaction (0x0F) packet. No-op for now.
func (c *Connection) handleTransaction() error {
	// The client sends this to confirm/deny server-initiated transactions.
	// We don't initiate any, so just ignore.
	return nil
}

// canStack returns true if two slots can be merged (same block ID and damage).
func canStack(a, b player.Slot) bool {
	return a.BlockID == b.BlockID && a.ItemDamage == b.ItemDamage
}

// armorSlotForItem returns the protocol armor slot (5-8) for the given item ID,
// or -1 if the item is not armor.
func armorSlotForItem(blockID int16) int16 {
	switch blockID {
	// Helmets
	case 298, 302, 306, 310, 314:
		return slotHelmet
	// Chestplates
	case 299, 303, 307, 311, 315:
		return slotChestplate
	// Leggings
	case 300, 304, 308, 312, 316:
		return slotLeggings
	// Boots
	case 301, 305, 309, 313, 317:
		return slotBoots
	default:
		return -1
	}
}

// consumeCraftingIngredients removes one item from each occupied crafting grid
// slot. Those items stop existing rather than going anywhere: the result the
// player took is a new item, not the ingredients rearranged.
func (c *Connection) consumeCraftingIngredients() {
	l := c.layout()
	for i := range l.gridCells() {
		c.consume(l, l.gridStart+int16(i), 1)
	}
}

// updateCraftingOutput checks the crafting grid against recipes and updates the output slot.
func (c *Connection) updateCraftingOutput() {
	// A window with no crafting area has no output slot, and slot 0 there
	// belongs to a container: writing a recipe result into it would overwrite
	// the first chest slot on the client.
	if !c.layout().hasCrafting() {
		return
	}

	result := c.matchCraftingRecipe()
	c.craftingOutput = result
	_ = c.sendSetSlot(c.windowID, slotCraftOutput, result)
}

// matchCraftingRecipe matches the open window's crafting grid against the
// recipe registry. The grid is 2x2 in the player's own window and 3x3 in a
// crafting table, which is the whole difference between them.
func (c *Connection) matchCraftingRecipe() player.Slot {
	cells := c.layout().gridCells()

	allEmpty := true
	for i := range cells {
		if !c.craftingGrid[i].IsEmpty() {
			allEmpty = false

			break
		}
	}
	if allEmpty {
		return player.EmptySlot
	}

	if c.gameData == nil || c.gameData.Recipes() == nil {
		return player.EmptySlot
	}

	return matchRecipe(c.craftingGrid[:], c.layout().gridSize, c.gameData.Recipes())
}
