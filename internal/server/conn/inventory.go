package conn

import (
	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/server/internal/server/player"
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

// craftingTableBlockID is the block a right-click opens the 3x3 window on.
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
		id:          0,
		gridSize:    2,
		gridStart:   slotCraftStart,
		gridEnd:     slotCraftEnd,
		armorStart:  slotArmorStart,
		armorEnd:    slotArmorEnd,
		mainStart:   slotMainStart,
		mainEnd:     slotMainEnd,
		hotbarStart: slotHotbarStart,
		hotbarEnd:   slotHotbarEnd,
		invShift:    0,
		total:       slotTotal,
	}
}

func tableWindowLayout(id int8) windowLayout {
	return windowLayout{
		id:          id,
		gridSize:    3,
		gridStart:   slotCraftStart,
		gridEnd:     tableGridEnd,
		armorStart:  -1,
		armorEnd:    -1,
		mainStart:   tableMainStart,
		mainEnd:     tableMainEnd,
		hotbarStart: tableHotbarStart,
		hotbarEnd:   tableHotbarEnd,
		invShift:    -1,
		total:       tableSlotTotal,
	}
}

// hasArmor reports whether the window shows the player's armor. A crafting
// table does not, so shift-clicking a helmet inside one stores it rather than
// wearing it.
func (l windowLayout) hasArmor() bool {
	return l.armorStart >= 0
}

// gridCells is how many slots the crafting grid has.
func (l windowLayout) gridCells() int {
	return l.gridSize * l.gridSize
}

// layout returns the layout of the window the player currently has open.
func (c *Connection) layout() windowLayout {
	if c.windowID == 0 {
		return playerWindowLayout()
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
	case slot == slotCraftOutput:
		return c.craftingOutput
	case slot >= l.gridStart && slot <= l.gridEnd:
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
	case slot == slotCraftOutput:
		c.craftingOutput = item
	case slot >= l.gridStart && slot <= l.gridEnd:
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
			c.dropItem(c.cursorSlot, button == 0)
			if button == 0 {
				c.cursorSlot = player.EmptySlot
			} else {
				c.cursorSlot.ItemCount--
				if c.cursorSlot.ItemCount <= 0 {
					c.cursorSlot = player.EmptySlot
				}
			}
		}
		return
	}

	if slot < 0 || slot > l.hotbarEnd {
		return
	}

	// Clicking crafting output.
	if slot == slotCraftOutput {
		if c.craftingOutput.IsEmpty() {
			return
		}
		if !c.cursorSlot.IsEmpty() {
			// Can only pick up crafting output if cursor matches and has room.
			if !canStack(c.cursorSlot, c.craftingOutput) {
				return
			}
			newCount := int(c.cursorSlot.ItemCount) + int(c.craftingOutput.ItemCount)
			if newCount > 64 {
				return
			}
			c.cursorSlot.ItemCount = int8(newCount)
		} else {
			c.cursorSlot = c.craftingOutput
		}
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
			c.cursorSlot = current
			c.setWindowSlot(slot, player.EmptySlot)

		case current.IsEmpty():
			// Place entire cursor stack.
			c.setWindowSlot(slot, c.cursorSlot)
			c.cursorSlot = player.EmptySlot

		case canStack(c.cursorSlot, current):
			// Merge cursor into slot.
			space := 64 - int(current.ItemCount)
			if space <= 0 {
				// Swap.
				c.cursorSlot, current = current, c.cursorSlot
				c.setWindowSlot(slot, current)
			} else {
				transfer := int(c.cursorSlot.ItemCount)
				if transfer > space {
					transfer = space
				}
				current.ItemCount += int8(transfer)
				c.cursorSlot.ItemCount -= int8(transfer)
				if c.cursorSlot.ItemCount <= 0 {
					c.cursorSlot = player.EmptySlot
				}
				c.setWindowSlot(slot, current)
			}

		default:
			// Swap cursor and slot.
			c.setWindowSlot(slot, c.cursorSlot)
			c.cursorSlot = current
		}
	} else { // Right click
		switch {
		case c.cursorSlot.IsEmpty() && !current.IsEmpty():
			// Pick up half.
			half := (current.ItemCount + 1) / 2
			c.cursorSlot = player.Slot{BlockID: current.BlockID, ItemCount: half, ItemDamage: current.ItemDamage}
			current.ItemCount -= half
			if current.ItemCount <= 0 {
				c.setWindowSlot(slot, player.EmptySlot)
			} else {
				c.setWindowSlot(slot, current)
			}

		case !c.cursorSlot.IsEmpty() && current.IsEmpty():
			// Place one from cursor.
			placed := player.Slot{BlockID: c.cursorSlot.BlockID, ItemCount: 1, ItemDamage: c.cursorSlot.ItemDamage}
			c.setWindowSlot(slot, placed)
			c.cursorSlot.ItemCount--
			if c.cursorSlot.ItemCount <= 0 {
				c.cursorSlot = player.EmptySlot
			}

		case !c.cursorSlot.IsEmpty() && canStack(c.cursorSlot, current) && current.ItemCount < 64:
			// Place one from cursor onto existing stack.
			current.ItemCount++
			c.setWindowSlot(slot, current)
			c.cursorSlot.ItemCount--
			if c.cursorSlot.ItemCount <= 0 {
				c.cursorSlot = player.EmptySlot
			}

		case !c.cursorSlot.IsEmpty() && !current.IsEmpty():
			// Swap.
			c.setWindowSlot(slot, c.cursorSlot)
			c.cursorSlot = current
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

	if slot == slotCraftOutput {
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

	// leftover is what the destination could not take. The items that did fit
	// have already been placed, so the source slot keeps only the remainder.
	leftover := int(item.ItemCount)
	switch {
	case l.hasArmor() && slot >= l.armorStart && slot <= l.armorEnd:
		// Armor → main inventory or hotbar.
		leftover = c.addToSection(item, l.mainStart, l.hotbarEnd)
	case slot >= l.mainStart && slot <= l.mainEnd:
		// Main inventory → try armor first if applicable, then hotbar.
		if c.equipArmor(item) {
			leftover = 0
		} else {
			leftover = c.addToSection(item, l.hotbarStart, l.hotbarEnd)
		}
	case slot >= l.hotbarStart && slot <= l.hotbarEnd:
		// Hotbar → try armor first if applicable, then main inventory.
		if c.equipArmor(item) {
			leftover = 0
		} else {
			leftover = c.addToSection(item, l.mainStart, l.mainEnd)
		}
	case slot >= l.gridStart && slot <= l.gridEnd:
		// Crafting grid → main or hotbar.
		leftover = c.addToSection(item, l.mainStart, l.hotbarEnd)
	}

	if leftover == int(item.ItemCount) {
		return
	}

	if leftover == 0 {
		c.setWindowSlot(slot, player.EmptySlot)
	} else {
		remainder := item
		remainder.ItemCount = int8(leftover)
		c.setWindowSlot(slot, remainder)
	}

	if slot >= l.gridStart && slot <= l.gridEnd {
		c.updateCraftingOutput()
	}
}

// equipArmor moves item into its armor slot when the item is armor and that
// slot is empty. A window without armor slots — a crafting table — never
// equips: vanilla stores the piece instead.
func (c *Connection) equipArmor(item player.Slot) bool {
	if !c.layout().hasArmor() {
		return false
	}

	armorSlot := armorSlotForItem(item.BlockID)
	if armorSlot < 0 || !c.getWindowSlot(armorSlot).IsEmpty() {
		return false
	}
	c.setWindowSlot(armorSlot, item)

	return true
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
	for range 64 {
		result := c.craftingOutput
		if result.IsEmpty() {
			break
		}
		// Vanilla refuses a craft it cannot deposit whole rather than
		// splitting the result stack.
		if c.spaceForItem(result, l.mainStart, l.hotbarEnd) < int(result.ItemCount) {
			break
		}

		c.addToSection(result, l.mainStart, l.hotbarEnd)
		c.consumeCraftingIngredients()
		c.craftingOutput = c.matchCraftingRecipe()
		crafted = true
	}

	if crafted {
		c.updateCraftingOutput()
	}
}

// addToSection places as much of item as fits into slots [lo, hi] and returns
// the count that did not fit. It reports the leftover rather than a bool
// because a partial add still moves what fits: a caller that read failure as
// "nothing happened" and kept the source stack duplicated the placed items.
func (c *Connection) addToSection(item player.Slot, lo, hi int16) int {
	remaining := int(item.ItemCount)

	// First pass: try to merge into existing stacks.
	for s := lo; s <= hi && remaining > 0; s++ {
		existing := c.getWindowSlot(s)
		if !existing.IsEmpty() && canStack(existing, item) && existing.ItemCount < 64 {
			space := 64 - int(existing.ItemCount)
			transfer := remaining
			if transfer > space {
				transfer = space
			}
			existing.ItemCount += int8(transfer)
			c.setWindowSlot(s, existing)
			remaining -= transfer
		}
	}

	// Second pass: place into empty slots.
	for s := lo; s <= hi && remaining > 0; s++ {
		existing := c.getWindowSlot(s)
		if existing.IsEmpty() {
			place := remaining
			if place > 64 {
				place = 64
			}
			c.setWindowSlot(s, player.Slot{BlockID: item.BlockID, ItemCount: int8(place), ItemDamage: item.ItemDamage})
			remaining -= place
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
			space += 64
		case canStack(existing, item) && existing.ItemCount < 64:
			space += 64 - int(existing.ItemCount)
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

	slotItem := c.getWindowSlot(slot)
	hotbarItem := c.getWindowSlot(hotbarSlot)
	c.setWindowSlot(slot, hotbarItem)
	c.setWindowSlot(hotbarSlot, slotItem)

	if slot >= l.gridStart && slot <= l.gridEnd {
		c.updateCraftingOutput()
	}
}

// handleMiddleClick handles mode 3: middle-click in creative mode (clone to cursor).
func (c *Connection) handleMiddleClick(slot int16) {
	if slot < 0 || slot > c.layout().hotbarEnd {
		return
	}
	item := c.getWindowSlot(slot)
	if item.IsEmpty() {
		return
	}
	c.cursorSlot = player.Slot{BlockID: item.BlockID, ItemCount: 64, ItemDamage: item.ItemDamage}
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

	if button == 0 {
		// Drop one.
		dropped := player.Slot{BlockID: item.BlockID, ItemCount: 1, ItemDamage: item.ItemDamage}
		item.ItemCount--
		if item.ItemCount <= 0 {
			c.setWindowSlot(slot, player.EmptySlot)
		} else {
			c.setWindowSlot(slot, item)
		}
		pos := c.self.GetPosition()
		c.players.SpawnItemEntity(c.self.EntityID, dropped, pos.X, pos.Y+1.3, pos.Z, pos.Yaw, c.groundAtFunc())
	} else {
		// Ctrl+Q: drop entire stack.
		c.setWindowSlot(slot, player.EmptySlot)
		pos := c.self.GetPosition()
		c.players.SpawnItemEntity(c.self.EntityID, item, pos.X, pos.Y+1.3, pos.Z, pos.Yaw, c.groundAtFunc())
	}

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

	if c.dragMode == 0 {
		// Left drag: distribute evenly.
		perSlot := int(c.cursorSlot.ItemCount) / len(c.dragSlots)
		if perSlot == 0 {
			perSlot = 1
		}
		remaining := int(c.cursorSlot.ItemCount)
		for _, s := range c.dragSlots {
			existing := c.getWindowSlot(s)
			if !existing.IsEmpty() && !canStack(existing, c.cursorSlot) {
				continue
			}
			current := int8(0)
			if !existing.IsEmpty() {
				current = existing.ItemCount
			}
			space := 64 - int(current)
			give := perSlot
			if give > remaining {
				give = remaining
			}
			if give > space {
				give = space
			}
			if give <= 0 {
				continue
			}
			c.setWindowSlot(s, player.Slot{
				BlockID:    c.cursorSlot.BlockID,
				ItemCount:  current + int8(give),
				ItemDamage: c.cursorSlot.ItemDamage,
			})
			remaining -= give
		}
		if remaining <= 0 {
			c.cursorSlot = player.EmptySlot
		} else {
			c.cursorSlot.ItemCount = int8(remaining)
		}
	} else {
		// Right drag: place one in each slot.
		remaining := int(c.cursorSlot.ItemCount)
		for _, s := range c.dragSlots {
			if remaining <= 0 {
				break
			}
			existing := c.getWindowSlot(s)
			if !existing.IsEmpty() && !canStack(existing, c.cursorSlot) {
				continue
			}
			current := int8(0)
			if !existing.IsEmpty() {
				current = existing.ItemCount
			}
			if current >= 64 {
				continue
			}
			c.setWindowSlot(s, player.Slot{
				BlockID:    c.cursorSlot.BlockID,
				ItemCount:  current + 1,
				ItemDamage: c.cursorSlot.ItemDamage,
			})
			remaining--
		}
		if remaining <= 0 {
			c.cursorSlot = player.EmptySlot
		} else {
			c.cursorSlot.ItemCount = int8(remaining)
		}
	}
}

// handleDoubleClick handles mode 6: double-click to collect matching items to cursor.
func (c *Connection) handleDoubleClick(_ int16) {
	if c.cursorSlot.IsEmpty() {
		return
	}

	l := c.layout()

	needed := 64 - int(c.cursorSlot.ItemCount)
	// Scan all inventory slots (skip crafting output).
	for s := l.gridStart; s <= l.hotbarEnd && needed > 0; s++ {
		item := c.getWindowSlot(s)
		if item.IsEmpty() || !canStack(item, c.cursorSlot) {
			continue
		}
		take := int(item.ItemCount)
		if take > needed {
			take = needed
		}
		item.ItemCount -= int8(take)
		if item.ItemCount <= 0 {
			c.setWindowSlot(s, player.EmptySlot)
		} else {
			c.setWindowSlot(s, item)
		}
		c.cursorSlot.ItemCount += int8(take)
		needed -= take
	}
}

// handleCreativeSlot processes a SetCreativeSlot (0x10) packet.
func (c *Connection) handleCreativeSlot(value *v1_8.PlayServerboundSetCreativeSlot) error {
	slotIndex := value.Slot
	item := slotFromGenerated(value.Item)

	// Slot -1: drop item.
	if slotIndex == -1 {
		if item.BlockID > 0 {
			pos := c.self.GetPosition()
			dropped := player.Slot{BlockID: item.BlockID, ItemCount: item.ItemCount, ItemDamage: item.ItemDamage}
			c.players.SpawnItemEntity(c.self.EntityID, dropped, pos.X, pos.Y+1.3, pos.Z, pos.Yaw, c.groundAtFunc())
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
	c.setSlotIn(playerWindowLayout(), slotIndex, pSlot)

	return nil
}

// openCraftingTable opens the 3x3 window on the table at (x, y, z) and tells
// the client to show it. The grid it opens onto is always empty: whatever the
// player left in the last one was returned when that window closed.
func (c *Connection) openCraftingTable() error {
	c.emptyCraftingArea()

	// Vanilla's getNextWindowId, which cycles 1-100 and never yields 0.
	c.nextWindowID = c.nextWindowID%100 + 1
	c.windowID = c.nextWindowID

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
	pos := c.self.GetPosition()
	groundAt := c.groundAtFunc()

	for i := range l.gridCells() {
		if c.craftingGrid[i].IsEmpty() {
			continue
		}
		// Only what did not fit is dropped. Dropping the whole stack after
		// part of it was already stored would duplicate the stored part.
		if leftover := c.addToSection(c.craftingGrid[i], l.mainStart, l.hotbarEnd); leftover > 0 {
			dropped := c.craftingGrid[i]
			dropped.ItemCount = int8(leftover)
			c.players.SpawnItemEntity(c.self.EntityID, dropped, pos.X, pos.Y+1.3, pos.Z, pos.Yaw, groundAt)
		}
		c.craftingGrid[i] = player.EmptySlot
	}

	c.craftingGrid = emptyCraftingGrid()
	c.craftingOutput = player.EmptySlot
}

// handleCloseWindow processes a CloseWindow (0x0D) packet. The window ID it
// carries is not checked: a client closing a window the server no longer has
// open still means the player is looking at their own inventory again.
func (c *Connection) handleCloseWindow() error {
	c.emptyCraftingArea()
	c.windowID = 0

	// Drop cursor item.
	if !c.cursorSlot.IsEmpty() {
		pos := c.self.GetPosition()
		c.players.SpawnItemEntity(c.self.EntityID, c.cursorSlot, pos.X, pos.Y+1.3, pos.Z, pos.Yaw, c.groundAtFunc())
		c.cursorSlot = player.EmptySlot
	}

	return nil
}

// handleTransaction processes a Transaction (0x0F) packet. No-op for now.
func (c *Connection) handleTransaction() error {
	// The client sends this to confirm/deny server-initiated transactions.
	// We don't initiate any, so just ignore.
	return nil
}

// dropItem spawns a dropped item entity. If fullStack is true, drops the entire item;
// otherwise drops one.
func (c *Connection) dropItem(item player.Slot, fullStack bool) {
	pos := c.self.GetPosition()
	groundAt := c.groundAtFunc()
	if fullStack {
		c.players.SpawnItemEntity(c.self.EntityID, item, pos.X, pos.Y+1.3, pos.Z, pos.Yaw, groundAt)
	} else {
		dropped := player.Slot{BlockID: item.BlockID, ItemCount: 1, ItemDamage: item.ItemDamage}
		c.players.SpawnItemEntity(c.self.EntityID, dropped, pos.X, pos.Y+1.3, pos.Z, pos.Yaw, groundAt)
	}
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

// consumeCraftingIngredients removes one item from each occupied crafting grid slot.
func (c *Connection) consumeCraftingIngredients() {
	for i := range c.layout().gridCells() {
		if c.craftingGrid[i].IsEmpty() {
			continue
		}
		c.craftingGrid[i].ItemCount--
		if c.craftingGrid[i].ItemCount <= 0 {
			c.craftingGrid[i] = player.EmptySlot
		}
	}
}

// updateCraftingOutput checks the crafting grid against recipes and updates the output slot.
func (c *Connection) updateCraftingOutput() {
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
