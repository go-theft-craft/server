package player

import (
	"bytes"
	"encoding/binary"

	mcnet "github.com/OCharnyshevich/minecraft-server/internal/server/net"
)

// BuildEquipmentPackets builds 5 EntityEquipment (0x04) raw data payloads:
// slot 0 = held item, slots 1-4 = armor (boots, leggings, chestplate, helmet).
func BuildEquipmentPackets(entityID int32, inv *Inventory) [][]byte {
	inv.mu.RLock()
	defer inv.mu.RUnlock()

	packets := make([][]byte, 5)

	// Slot 0: held item
	packets[0] = buildEquipmentData(entityID, 0, inv.Slots[inv.HeldSlot])

	// Slots 1-4: armor (boots=1, leggings=2, chestplate=3, helmet=4)
	for i := 0; i < 4; i++ {
		packets[i+1] = buildEquipmentData(entityID, int16(i+1), inv.Armor[i])
	}

	return packets
}

// BuildSingleEquipment builds a single EntityEquipment raw data payload.
func BuildSingleEquipment(entityID int32, equipSlot int16, slot Slot) []byte {
	return buildEquipmentData(entityID, equipSlot, slot)
}

func buildEquipmentData(entityID int32, equipSlot int16, slot Slot) []byte {
	var buf bytes.Buffer
	_, _ = mcnet.WriteVarInt(&buf, entityID)
	_ = binary.Write(&buf, binary.BigEndian, equipSlot)
	_ = WriteSlot(&buf, slot)
	return buf.Bytes()
}
