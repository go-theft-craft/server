package player

import (
	"bytes"
	"encoding/binary"
	"math"
	"time"

	pkt "github.com/OCharnyshevich/minecraft-server/internal/gamedata/versions/pc_1_8"
	mcnet "github.com/OCharnyshevich/minecraft-server/internal/server/net"
)

// ItemEntity represents a dropped item in the world.
type ItemEntity struct {
	EntityID         int32
	Item             Slot
	X, Y, Z          float64
	VelX, VelY, VelZ int16
	SpawnTime        time.Time
}

// SpawnItemEntity creates and broadcasts a dropped item entity.
func (m *Manager) SpawnItemEntity(dropperEID int32, item Slot, x, y, z float64, yaw float32) {
	entityID := m.AllocateEntityID()

	// Calculate throw velocity based on player's yaw.
	yawRad := float64(yaw) * math.Pi / 180.0
	speed := 4000.0 // ~0.5 blocks/tick in protocol units (8000 = 1 block/tick)
	velX := int16(-math.Sin(yawRad) * speed)
	velY := int16(2000) // slight upward toss
	velZ := int16(math.Cos(yawRad) * speed)

	ie := &ItemEntity{
		EntityID:  entityID,
		Item:      item,
		X:         x,
		Y:         y,
		Z:         z,
		VelX:      velX,
		VelY:      velY,
		VelZ:      velZ,
		SpawnTime: time.Now(),
	}

	m.itemMu.Lock()
	m.itemEntities[entityID] = ie
	m.itemMu.Unlock()

	// Build SpawnEntity (0x0E) for item entity (type 2 = item stack).
	spawnData := buildSpawnEntityData(ie)
	metaData := buildItemMetadata(ie)

	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, pl := range m.players {
		_ = pl.WritePacket(&pkt.SpawnEntity{Data: spawnData})
		_ = pl.WritePacket(&pkt.EntityMetadata{Data: buildEntityMetadataData(entityID, metaData)})
	}
}

// cleanupItemEntities removes item entities older than 5 minutes.
func (m *Manager) cleanupItemEntities() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		m.itemMu.Lock()
		var expired []int32
		for id, ie := range m.itemEntities {
			if time.Since(ie.SpawnTime) > 5*time.Minute {
				expired = append(expired, id)
			}
		}
		for _, id := range expired {
			delete(m.itemEntities, id)
		}
		m.itemMu.Unlock()

		if len(expired) > 0 {
			destroyData := buildDestroyEntities(expired)
			m.mu.RLock()
			for _, pl := range m.players {
				_ = pl.WritePacket(&pkt.EntityDestroy{Data: destroyData})
			}
			m.mu.RUnlock()
		}
	}
}

// buildSpawnEntityData encodes the SpawnEntity (0x0E) data for an item entity.
// Object type 2 = item stack.
func buildSpawnEntityData(ie *ItemEntity) []byte {
	var buf bytes.Buffer

	_, _ = mcnet.WriteVarInt(&buf, ie.EntityID)
	_ = binary.Write(&buf, binary.BigEndian, int8(2)) // type: item stack
	_ = binary.Write(&buf, binary.BigEndian, FixedPoint(ie.X))
	_ = binary.Write(&buf, binary.BigEndian, FixedPoint(ie.Y))
	_ = binary.Write(&buf, binary.BigEndian, FixedPoint(ie.Z))
	_ = binary.Write(&buf, binary.BigEndian, int8(0))  // pitch
	_ = binary.Write(&buf, binary.BigEndian, int8(0))  // yaw
	_ = binary.Write(&buf, binary.BigEndian, int32(1)) // data field (non-zero → velocity follows)
	_ = binary.Write(&buf, binary.BigEndian, ie.VelX)
	_ = binary.Write(&buf, binary.BigEndian, ie.VelY)
	_ = binary.Write(&buf, binary.BigEndian, ie.VelZ)

	return buf.Bytes()
}

// buildItemMetadata builds entity metadata for an item entity.
// Index 10 (type 5 = slot) contains the item data.
func buildItemMetadata(ie *ItemEntity) []byte {
	var buf bytes.Buffer

	// Index 10, type 5 (slot)
	buf.WriteByte((10 & 0x1F) | (metaTypeSlot << 5))
	_ = WriteSlot(&buf, ie.Item)
	buf.WriteByte(pkt.MetadataEnd)

	return buf.Bytes()
}
