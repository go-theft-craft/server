package conn

import (
	"bytes"
	"encoding/binary"
	"fmt"

	pkt "github.com/go-theft-craft/server/pkg/gamedata/versions/pc_1_8"
	mcnet "github.com/go-theft-craft/server/pkg/protocol"
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

// sendWindowItems sends the full Window 0 (player inventory) to the client.
func (c *Connection) sendWindowItems() error {
	proto := c.self.Inventory.ToProtocolSlots()

	// Overlay crafting slots from connection state.
	proto[slotCraftOutput] = c.craftingOutput
	for i := 0; i < slotCraftCount; i++ {
		proto[slotCraftStart+i] = c.craftingGrid[i]
	}

	var buf bytes.Buffer
	buf.WriteByte(0) // window ID = 0
	_ = binary.Write(&buf, binary.BigEndian, int16(slotTotal))
	for _, s := range proto {
		_ = player.WriteSlot(&buf, s)
	}
	return c.writePacket(&pkt.WindowItems{Data: buf.Bytes()})
}

// sendSetSlot sends a single slot update to the client.
func (c *Connection) sendSetSlot(windowID int8, slotIndex int16, slot player.Slot) error {
	var buf bytes.Buffer
	buf.WriteByte(byte(windowID))
	_ = binary.Write(&buf, binary.BigEndian, slotIndex)
	_ = player.WriteSlot(&buf, slot)
	return c.writePacket(&pkt.SetSlot{Data: buf.Bytes()})
}

// handleWindowClick processes a WindowClick (0x0E) packet.
func (c *Connection) handleWindowClick(data []byte) error {
	r := bytes.NewReader(data)

	windowID, err := mcnet.ReadU8(r)
	if err != nil {
		return fmt.Errorf("read window id: %w", err)
	}

	slotIndex, err := mcnet.ReadI16(r)
	if err != nil {
		return fmt.Errorf("read slot: %w", err)
	}

	button, err := mcnet.ReadI8(r)
	if err != nil {
		return fmt.Errorf("read button: %w", err)
	}

	actionID, err := mcnet.ReadI16(r)
	if err != nil {
		return fmt.Errorf("read action id: %w", err)
	}

	mode, _, err := mcnet.ReadVarInt(r)
	if err != nil {
		return fmt.Errorf("read mode: %w", err)
	}

	// Read clicked item (we don't use it for validation, but must consume it).
	if _, err := readSlot(r); err != nil {
		return fmt.Errorf("read clicked item: %w", err)
	}

	// Only handle player inventory (window 0) for now.
	if windowID != 0 {
		return c.sendTransaction(0, actionID, false)
	}

	c.log.Info("window click", "slot", slotIndex, "button", button, "mode", mode, "craftOutput", c.craftingOutput, "cursor", c.cursorSlot)
	c.dispatchClick(slotIndex, button, int(mode))
	c.log.Info("after click", "craftOutput", c.craftingOutput, "cursor", c.cursorSlot)

	// Full inventory sync so client matches server state.
	_ = c.sendWindowItems()
	// Sync cursor slot (window -1, slot -1).
	_ = c.sendSetSlot(-1, -1, c.cursorSlot)

	// Always accept the transaction.
	return c.sendTransaction(0, actionID, true)
}

func (c *Connection) sendTransaction(windowID int8, actionID int16, accepted bool) error {
	return c.writePacket(&pkt.TransactionCB{
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

// getWindowSlot reads a slot from the player inventory window (0-44).
func (c *Connection) getWindowSlot(slot int16) player.Slot {
	switch {
	case slot == slotCraftOutput:
		return c.craftingOutput
	case slot >= slotCraftStart && slot <= slotCraftEnd:
		return c.craftingGrid[slot-slotCraftStart]
	case slot >= slotArmorStart && slot <= slotHotbarEnd:
		return c.self.Inventory.GetProtocolSlot(int(slot))
	default:
		return player.EmptySlot
	}
}

// setWindowSlot writes a slot to the player inventory window (0-44)
// and broadcasts equipment changes to trackers if needed.
func (c *Connection) setWindowSlot(slot int16, item player.Slot) {
	switch {
	case slot == slotCraftOutput:
		c.craftingOutput = item
	case slot >= slotCraftStart && slot <= slotCraftEnd:
		c.craftingGrid[slot-slotCraftStart] = item
	case slot >= slotArmorStart && slot <= slotHotbarEnd:
		c.self.Inventory.SetProtocolSlot(int(slot), item)
		c.broadcastEquipmentIfNeeded(slot)
	}
}

// broadcastEquipmentIfNeeded sends equipment updates to trackers when
// armor or held item slots change.
func (c *Connection) broadcastEquipmentIfNeeded(protoSlot int16) {
	eid := c.self.EntityID
	switch {
	case protoSlot == slotHelmet:
		slot := c.self.Inventory.GetArmor(3)
		c.players.BroadcastToTrackers(&pkt.EntityEquipment{Data: player.BuildSingleEquipment(eid, 4, slot)}, eid)
	case protoSlot == slotChestplate:
		slot := c.self.Inventory.GetArmor(2)
		c.players.BroadcastToTrackers(&pkt.EntityEquipment{Data: player.BuildSingleEquipment(eid, 3, slot)}, eid)
	case protoSlot == slotLeggings:
		slot := c.self.Inventory.GetArmor(1)
		c.players.BroadcastToTrackers(&pkt.EntityEquipment{Data: player.BuildSingleEquipment(eid, 2, slot)}, eid)
	case protoSlot == slotBoots:
		slot := c.self.Inventory.GetArmor(0)
		c.players.BroadcastToTrackers(&pkt.EntityEquipment{Data: player.BuildSingleEquipment(eid, 1, slot)}, eid)
	case protoSlot >= slotHotbarStart && protoSlot <= slotHotbarEnd:
		// Check if this is the active hotbar slot.
		hotbarIdx := protoSlot - slotHotbarStart
		if hotbarIdx == int16(c.self.Inventory.GetHeldSlot()) {
			heldItem := c.self.Inventory.HeldItem()
			c.players.BroadcastToTrackers(&pkt.EntityEquipment{Data: player.BuildSingleEquipment(eid, 0, heldItem)}, eid)
		}
	}
}

// handleNormalClick handles mode 0: left-click (pickup/place/swap) and right-click (half-pickup/place-one).
func (c *Connection) handleNormalClick(slot int16, button int8) {
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

	if slot < 0 || slot > slotHotbarEnd {
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
		if c.cursorSlot.IsEmpty() {
			// Pick up entire stack.
			c.cursorSlot = current
			c.setWindowSlot(slot, player.EmptySlot)
		} else if current.IsEmpty() {
			// Place entire cursor stack.
			c.setWindowSlot(slot, c.cursorSlot)
			c.cursorSlot = player.EmptySlot
		} else if canStack(c.cursorSlot, current) {
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
		} else {
			// Swap cursor and slot.
			c.setWindowSlot(slot, c.cursorSlot)
			c.cursorSlot = current
		}
	} else { // Right click
		if c.cursorSlot.IsEmpty() && !current.IsEmpty() {
			// Pick up half.
			half := (current.ItemCount + 1) / 2
			c.cursorSlot = player.Slot{BlockID: current.BlockID, ItemCount: half, ItemDamage: current.ItemDamage}
			current.ItemCount -= half
			if current.ItemCount <= 0 {
				c.setWindowSlot(slot, player.EmptySlot)
			} else {
				c.setWindowSlot(slot, current)
			}
		} else if !c.cursorSlot.IsEmpty() && current.IsEmpty() {
			// Place one from cursor.
			placed := player.Slot{BlockID: c.cursorSlot.BlockID, ItemCount: 1, ItemDamage: c.cursorSlot.ItemDamage}
			c.setWindowSlot(slot, placed)
			c.cursorSlot.ItemCount--
			if c.cursorSlot.ItemCount <= 0 {
				c.cursorSlot = player.EmptySlot
			}
		} else if !c.cursorSlot.IsEmpty() && canStack(c.cursorSlot, current) && current.ItemCount < 64 {
			// Place one from cursor onto existing stack.
			current.ItemCount++
			c.setWindowSlot(slot, current)
			c.cursorSlot.ItemCount--
			if c.cursorSlot.ItemCount <= 0 {
				c.cursorSlot = player.EmptySlot
			}
		} else if !c.cursorSlot.IsEmpty() && !current.IsEmpty() {
			// Swap.
			c.setWindowSlot(slot, c.cursorSlot)
			c.cursorSlot = current
		}
	}

	// Update crafting output if a crafting slot was modified.
	if slot >= slotCraftStart && slot <= slotCraftEnd {
		c.updateCraftingOutput()
	}
}

// handleShiftClick handles mode 1: shift-click to move items between sections.
func (c *Connection) handleShiftClick(slot int16, _ int8) {
	if slot < 0 || slot > slotHotbarEnd || slot == slotCraftOutput {
		// Shift-click crafting output: take result and auto-move.
		if slot == slotCraftOutput && !c.craftingOutput.IsEmpty() {
			result := c.craftingOutput
			if c.tryAddToSection(result, slotMainStart, slotHotbarEnd) {
				c.consumeCraftingIngredients()
				c.updateCraftingOutput()
			}
		}
		return
	}

	item := c.getWindowSlot(slot)
	if item.IsEmpty() {
		return
	}

	moved := false
	switch {
	case slot >= slotArmorStart && slot <= slotArmorEnd:
		// Armor → main inventory or hotbar.
		moved = c.tryAddToSection(item, slotMainStart, slotHotbarEnd)
	case slot >= slotMainStart && slot <= slotMainEnd:
		// Main inventory → try armor first if applicable, then hotbar.
		if armorSlot := armorSlotForItem(item.BlockID); armorSlot >= 0 {
			existing := c.getWindowSlot(armorSlot)
			if existing.IsEmpty() {
				c.setWindowSlot(armorSlot, item)
				moved = true
			}
		}
		if !moved {
			moved = c.tryAddToSection(item, slotHotbarStart, slotHotbarEnd)
		}
	case slot >= slotHotbarStart && slot <= slotHotbarEnd:
		// Hotbar → try armor first if applicable, then main inventory.
		if armorSlot := armorSlotForItem(item.BlockID); armorSlot >= 0 {
			existing := c.getWindowSlot(armorSlot)
			if existing.IsEmpty() {
				c.setWindowSlot(armorSlot, item)
				moved = true
			}
		}
		if !moved {
			moved = c.tryAddToSection(item, slotMainStart, slotMainEnd)
		}
	case slot >= slotCraftStart && slot <= slotCraftEnd:
		// Crafting grid → main or hotbar.
		moved = c.tryAddToSection(item, slotMainStart, slotHotbarEnd)
	}

	if moved {
		c.setWindowSlot(slot, player.EmptySlot)
		if slot >= slotCraftStart && slot <= slotCraftEnd {
			c.updateCraftingOutput()
		}
	}
}

// tryAddToSection tries to add an item into slots [lo, hi]. Returns true if fully placed.
func (c *Connection) tryAddToSection(item player.Slot, lo, hi int16) bool {
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

	return remaining == 0
}

// handleNumberKey handles mode 2: pressing number keys 1-9 to swap with hotbar.
func (c *Connection) handleNumberKey(slot int16, button int8) {
	if slot < 0 || slot > slotHotbarEnd {
		return
	}
	hotbarSlot := int16(slotHotbarStart) + int16(button)
	if hotbarSlot < slotHotbarStart || hotbarSlot > slotHotbarEnd {
		return
	}

	slotItem := c.getWindowSlot(slot)
	hotbarItem := c.getWindowSlot(hotbarSlot)
	c.setWindowSlot(slot, hotbarItem)
	c.setWindowSlot(hotbarSlot, slotItem)

	if slot >= slotCraftStart && slot <= slotCraftEnd {
		c.updateCraftingOutput()
	}
}

// handleMiddleClick handles mode 3: middle-click in creative mode (clone to cursor).
func (c *Connection) handleMiddleClick(slot int16) {
	if slot < 0 || slot > slotHotbarEnd {
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
	if slot == slotOutside {
		// Drop cursor (already handled by normal click path when mode=4 slot=-999).
		// In practice this shouldn't happen, but handle gracefully.
		return
	}
	if slot < 0 || slot > slotHotbarEnd {
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

	if slot >= slotCraftStart && slot <= slotCraftEnd {
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
		if c.dragActive && slot >= 0 && slot <= slotHotbarEnd {
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

	needed := 64 - int(c.cursorSlot.ItemCount)
	// Scan all inventory slots (skip crafting output).
	for s := int16(slotCraftStart); s <= slotHotbarEnd && needed > 0; s++ {
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
func (c *Connection) handleCreativeSlot(data []byte) error {
	r := bytes.NewReader(data)

	slotIndex, err := mcnet.ReadI16(r)
	if err != nil {
		return fmt.Errorf("read creative slot index: %w", err)
	}

	item, err := readSlot(r)
	if err != nil {
		return fmt.Errorf("read creative slot item: %w", err)
	}

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
	c.setWindowSlot(slotIndex, pSlot)
	return nil
}

// handleCloseWindow processes a CloseWindow (0x0D) packet.
func (c *Connection) handleCloseWindow(data []byte) error {
	r := bytes.NewReader(data)
	if _, err := mcnet.ReadU8(r); err != nil {
		return fmt.Errorf("read close window id: %w", err)
	}

	// Return crafting grid items to inventory or drop them.
	pos := c.self.GetPosition()
	groundAt := c.groundAtFunc()
	for i := 0; i < slotCraftCount; i++ {
		if c.craftingGrid[i].IsEmpty() {
			continue
		}
		if !c.tryAddToSection(c.craftingGrid[i], slotMainStart, slotHotbarEnd) {
			// Inventory full, drop the item.
			c.players.SpawnItemEntity(c.self.EntityID, c.craftingGrid[i], pos.X, pos.Y+1.3, pos.Z, pos.Yaw, groundAt)
		}
		c.craftingGrid[i] = player.EmptySlot
	}
	c.craftingOutput = player.EmptySlot

	// Drop cursor item.
	if !c.cursorSlot.IsEmpty() {
		c.players.SpawnItemEntity(c.self.EntityID, c.cursorSlot, pos.X, pos.Y+1.3, pos.Z, pos.Yaw, groundAt)
		c.cursorSlot = player.EmptySlot
	}

	return nil
}

// handleTransaction processes a Transaction (0x0F) packet. No-op for now.
func (c *Connection) handleTransaction(data []byte) error {
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
	for i := 0; i < slotCraftCount; i++ {
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
	_ = c.sendSetSlot(0, slotCraftOutput, result)
}

// matchCraftingRecipe tries to match the 2x2 crafting grid against known recipes.
// This is a placeholder that will be replaced by the full crafting system.
func (c *Connection) matchCraftingRecipe() player.Slot {
	// Check if crafting grid is empty.
	allEmpty := true
	for i := 0; i < slotCraftCount; i++ {
		if !c.craftingGrid[i].IsEmpty() {
			allEmpty = false
			break
		}
	}
	if allEmpty {
		return player.EmptySlot
	}

	if c.gameData == nil || c.gameData.Recipes == nil {
		return player.EmptySlot
	}

	return matchRecipe2x2(c.craftingGrid, c.gameData.Recipes)
}
