package player

import (
	"bytes"
	"encoding/binary"
	"math"

	pkt "github.com/go-theft-craft/server/pkg/gamedata/versions/pc_1_8"
	mcnet "github.com/go-theft-craft/server/pkg/protocol"
)

// ItemEntity represents a dropped item in the world.
type ItemEntity struct {
	EntityID         int32
	Item             Slot
	X, Y, Z          float64
	VelX, VelY, VelZ int16
	SpawnTick        int64
}

// SpawnItemEntity creates and broadcasts a dropped item entity.
// groundAt returns the ground-level Y below a given block position (x, y, z),
// used to estimate where the item will land for pickup distance checks.
func (m *Manager) SpawnItemEntity(dropperEID int32, item Slot, x, y, z float64, yaw float32, groundAt func(x, y, z int) float64) {
	entityID := m.AllocateEntityID()

	// Calculate throw velocity based on player's yaw (vanilla: 0.3 blocks/tick horizontal, 0.1 up).
	yawRad := float64(yaw) * math.Pi / 180.0
	speed := 2400.0 // 0.3 blocks/tick in protocol units (8000 = 1 block/tick)
	velX := int16(-math.Sin(yawRad) * speed)
	velY := int16(800) // 0.1 blocks/tick upward toss
	velZ := int16(math.Cos(yawRad) * speed)

	// Estimate where the item will land so the server-side position
	// (used for pickup distance) matches the client visual after the arc.
	landX, landY, landZ := estimateLanding(x, y, z, velX, velY, velZ, groundAt)

	ie := &ItemEntity{
		EntityID:  entityID,
		Item:      item,
		X:         landX,
		Y:         landY,
		Z:         landZ,
		VelX:      velX,
		VelY:      velY,
		VelZ:      velZ,
		SpawnTick: m.currentTick.Load(),
	}

	m.itemMu.Lock()
	m.itemEntities[entityID] = ie
	m.itemMu.Unlock()

	// Build SpawnEntity using the original throw position so the client
	// animates the arc from the player's hand. The stored X/Y/Z (landing)
	// is only used server-side for pickup distance checks.
	spawnData := buildSpawnEntityDataAt(ie, x, y, z)
	metaData := buildItemMetadata(ie)

	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, pl := range m.players {
		_ = pl.WritePacket(&pkt.SpawnEntity{Data: spawnData})
		_ = pl.WritePacket(&pkt.EntityMetadata{Data: buildEntityMetadataData(entityID, metaData)})
	}
}

// cleanupExpiredItems removes item entities older than 5 minutes (6000 ticks).
func (m *Manager) cleanupExpiredItems(currentTick int64) {
	m.itemMu.Lock()
	var expired []int32
	for id, ie := range m.itemEntities {
		if currentTick-ie.SpawnTick > itemExpiryTicks {
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

const (
	// pickupDelayTicks is the minimum ticks after spawn before an item can be picked up (10 ticks = 500ms at 20 TPS).
	pickupDelayTicks int64 = 10

	// itemExpiryTicks is the lifetime of a dropped item in ticks (6000 ticks = 5 minutes at 20 TPS).
	itemExpiryTicks int64 = 6000

	// pickupRadius is the distance (in blocks) within which a player can pick up items.
	// Larger than vanilla (1.0) to compensate for estimated landing positions
	// (server doesn't tick item physics, so positions may differ from client).
	pickupRadius = 2.5
)

// TryPickupItems checks for item entities near the player, attempts to add them
// to the player's inventory, and broadcasts collect/destroy packets.
// Returns the number of items collected.
func (m *Manager) TryPickupItems(p *Player) int {
	pos := p.GetPosition()
	collected := 0

	m.itemMu.Lock()
	var toRemove []int32
	var collectPackets []collectInfo

	currentTick := m.currentTick.Load()
	for id, ie := range m.itemEntities {
		if currentTick-ie.SpawnTick < pickupDelayTicks {
			continue
		}
		dx := pos.X - ie.X
		dy := (pos.Y + 0.5) - ie.Y // check from player center height
		dz := pos.Z - ie.Z
		dist := math.Sqrt(dx*dx + dy*dy + dz*dz)
		if dist > pickupRadius {
			continue
		}

		leftover := p.Inventory.AddItem(ie.Item)
		if leftover.IsEmpty() {
			// Fully absorbed.
			toRemove = append(toRemove, id)
			collectPackets = append(collectPackets, collectInfo{
				collectedEID: ie.EntityID,
				collectorEID: p.EntityID,
			})
			collected++
		} else if leftover.ItemCount < ie.Item.ItemCount {
			// Partially absorbed — update the remaining item.
			ie.Item = leftover
			collectPackets = append(collectPackets, collectInfo{
				collectedEID: ie.EntityID,
				collectorEID: p.EntityID,
			})
			collected++
		}
	}

	for _, id := range toRemove {
		delete(m.itemEntities, id)
	}
	m.itemMu.Unlock()

	if len(collectPackets) == 0 {
		return 0
	}

	// Broadcast collect and destroy packets.
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, ci := range collectPackets {
		for _, pl := range m.players {
			_ = pl.WritePacket(&pkt.Collect{
				CollectedEntityID: ci.collectedEID,
				CollectorEntityID: ci.collectorEID,
			})
		}
	}

	if len(toRemove) > 0 {
		destroyData := buildDestroyEntities(toRemove)
		for _, pl := range m.players {
			_ = pl.WritePacket(&pkt.EntityDestroy{Data: destroyData})
		}
	}

	return collected
}

type collectInfo struct {
	collectedEID int32
	collectorEID int32
}

// SpawnBlockDrop creates and broadcasts a dropped item from a broken block.
// spawnY is the visual spawn height (block center), while (x, y, z) is the
// ground-level resting position stored for pickup distance checks.
func (m *Manager) SpawnBlockDrop(item Slot, x, y, z, spawnY float64) {
	entityID := m.AllocateEntityID()

	ie := &ItemEntity{
		EntityID:  entityID,
		Item:      item,
		X:         x,
		Y:         y,
		Z:         z,
		VelX:      0,
		VelY:      800, // small upward pop for visual effect
		VelZ:      0,
		SpawnTick: m.currentTick.Load(),
	}

	m.itemMu.Lock()
	m.itemEntities[entityID] = ie
	m.itemMu.Unlock()

	// Visual spawn at block height; stored X/Y/Z at ground level for pickup.
	spawnData := buildSpawnEntityDataAt(ie, x, spawnY, z)
	metaData := buildItemMetadata(ie)

	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, pl := range m.players {
		_ = pl.WritePacket(&pkt.SpawnEntity{Data: spawnData})
		_ = pl.WritePacket(&pkt.EntityMetadata{Data: buildEntityMetadataData(entityID, metaData)})
	}
}

// buildSpawnEntityDataAtRest encodes a SpawnEntity packet for an item entity
// at its stored resting position with data field = 0 (no velocity follows).
// Used for sending existing items to late-joining players.
func buildSpawnEntityDataAtRest(ie *ItemEntity) []byte {
	var buf bytes.Buffer

	_, _ = mcnet.WriteVarInt(&buf, ie.EntityID)
	_ = binary.Write(&buf, binary.BigEndian, int8(2)) // type: item stack
	_ = binary.Write(&buf, binary.BigEndian, FixedPoint(ie.X))
	_ = binary.Write(&buf, binary.BigEndian, FixedPoint(ie.Y))
	_ = binary.Write(&buf, binary.BigEndian, FixedPoint(ie.Z))
	_ = binary.Write(&buf, binary.BigEndian, int8(0))  // pitch
	_ = binary.Write(&buf, binary.BigEndian, int8(0))  // yaw
	_ = binary.Write(&buf, binary.BigEndian, int32(0)) // data field 0 → no velocity follows

	return buf.Bytes()
}

// buildSpawnEntityDataAt encodes the SpawnEntity (0x0E) data for an item entity
// at the given visual spawn position. Object type 2 = item stack.
func buildSpawnEntityDataAt(ie *ItemEntity, spawnX, spawnY, spawnZ float64) []byte {
	var buf bytes.Buffer

	_, _ = mcnet.WriteVarInt(&buf, ie.EntityID)
	_ = binary.Write(&buf, binary.BigEndian, int8(2)) // type: item stack
	_ = binary.Write(&buf, binary.BigEndian, FixedPoint(spawnX))
	_ = binary.Write(&buf, binary.BigEndian, FixedPoint(spawnY))
	_ = binary.Write(&buf, binary.BigEndian, FixedPoint(spawnZ))
	_ = binary.Write(&buf, binary.BigEndian, int8(0))  // pitch
	_ = binary.Write(&buf, binary.BigEndian, int8(0))  // yaw
	_ = binary.Write(&buf, binary.BigEndian, int32(1)) // data field (non-zero → velocity follows)
	_ = binary.Write(&buf, binary.BigEndian, ie.VelX)
	_ = binary.Write(&buf, binary.BigEndian, ie.VelY)
	_ = binary.Write(&buf, binary.BigEndian, ie.VelZ)

	return buf.Bytes()
}

// estimateLanding approximates where an item entity will land by simulating
// vanilla entity physics (gravity before move, drag after) for up to 4 seconds.
// groundAt returns the ground-level Y below a given block (x, y, z) position so
// the simulation finds the correct floor even inside caves.
func estimateLanding(x, y, z float64, velX, velY, velZ int16, groundAt func(x, y, z int) float64) (float64, float64, float64) {
	const (
		gravity  = 0.04 // blocks/tick² downward
		drag     = 0.98 // velocity multiplier per tick
		maxTicks = 80   // 4 seconds at 20 tps
	)

	vx := float64(velX) / 8000.0
	vy := float64(velY) / 8000.0
	vz := float64(velZ) / 8000.0

	px, py, pz := x, y, z
	for range maxTicks {
		// Save pre-move Y so we scan ground from above the surface
		// even if the item falls past it in one tick.
		prevPY := py

		// Vanilla order: gravity → move → drag.
		vy -= gravity
		px += vx
		py += vy
		pz += vz
		vx *= drag
		vy *= drag
		vz *= drag
		// Check ground level from the pre-move Y to avoid missing
		// the surface when the item falls through it in a single tick.
		groundY := groundAt(int(math.Floor(px)), int(math.Floor(prevPY))+1, int(math.Floor(pz)))
		if vy < 0 && py <= groundY {
			py = groundY
			break
		}
	}
	return px, py, pz
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
