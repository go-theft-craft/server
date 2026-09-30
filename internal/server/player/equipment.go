package player

import "github.com/go-theft-craft/server/pkg/world"

// BuildEquipmentValues builds the 5 EntityEquipment (0x04) packets for a
// player: slot 0 = held item, slots 1-4 = armor (boots, leggings, chestplate,
// helmet).
func BuildEquipmentValues(packets Packets, entityID int32, inv *Inventory) []world.Packet {
	inv.mu.RLock()
	defer inv.mu.RUnlock()

	values := make([]world.Packet, 5)

	// Slot 0: held item
	values[0] = packets.EntityEquipment(entityID, 0, inv.Slots[inv.HeldSlot])

	// Slots 1-4: armor (boots=1, leggings=2, chestplate=3, helmet=4)
	for i := 0; i < 4; i++ {
		values[i+1] = packets.EntityEquipment(entityID, int16(i+1), inv.Armor[i])
	}

	return values
}
