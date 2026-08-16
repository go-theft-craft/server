package player

import (
	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"
)

// BuildEquipmentValues builds the 5 EntityEquipment (0x04) packet values for a
// player: slot 0 = held item, slots 1-4 = armor (boots, leggings, chestplate,
// helmet).
func BuildEquipmentValues(entityID int32, inv *Inventory) []v1_8.PlayClientboundEntityEquipment {
	inv.mu.RLock()
	defer inv.mu.RUnlock()

	values := make([]v1_8.PlayClientboundEntityEquipment, 5)

	// Slot 0: held item
	values[0] = equipmentValue(entityID, 0, inv.Slots[inv.HeldSlot])

	// Slots 1-4: armor (boots=1, leggings=2, chestplate=3, helmet=4)
	for i := 0; i < 4; i++ {
		values[i+1] = equipmentValue(entityID, int16(i+1), inv.Armor[i])
	}

	return values
}

func equipmentValue(entityID int32, equipSlot int16, slot Slot) v1_8.PlayClientboundEntityEquipment {
	return v1_8.PlayClientboundEntityEquipment{
		EntityID: entityID,
		Slot:     equipSlot,
		Item:     ToGeneratedSlot(slot),
	}
}

// BuildSingleEquipmentValue builds a single EntityEquipment (0x04) packet value
// for one equipment slot. It is the generated-type replacement for
// BuildSingleEquipment's raw-byte payload.
func BuildSingleEquipmentValue(entityID int32, equipSlot int16, slot Slot) v1_8.PlayClientboundEntityEquipment {
	return equipmentValue(entityID, equipSlot, slot)
}
