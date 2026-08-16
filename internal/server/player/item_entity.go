package player

import (
	"math"

	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"
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
	spawn := spawnItemEntityValue(ie, x, y, z, true)
	meta := itemMetadataPacket(ie)

	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, pl := range m.players {
		_ = pl.WritePacket(spawn)
		_ = pl.WritePacket(meta)
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
		destroy := &v1_8.PlayClientboundEntityDestroy{EntityIds: expired}
		m.mu.RLock()
		for _, pl := range m.players {
			_ = pl.WritePacket(destroy)
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
		collect := &v1_8.PlayClientboundCollect{
			CollectedEntityID: ci.collectedEID,
			CollectorEntityID: ci.collectorEID,
		}
		for _, pl := range m.players {
			_ = pl.WritePacket(collect)
		}
	}

	if len(toRemove) > 0 {
		destroy := &v1_8.PlayClientboundEntityDestroy{EntityIds: toRemove}
		for _, pl := range m.players {
			_ = pl.WritePacket(destroy)
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
	spawn := spawnItemEntityValue(ie, x, spawnY, z, true)
	meta := itemMetadataPacket(ie)

	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, pl := range m.players {
		_ = pl.WritePacket(spawn)
		_ = pl.WritePacket(meta)
	}
}

// spawnItemEntityValue builds the SpawnEntity (0x0E) packet for an item entity
// (object type 2 = item stack) at the given visual position. When withVelocity
// is true the object-data int is 1 and the velocity short triple follows; when
// false it is 0 and no velocity follows (used for late-joining players seeing an
// item at rest).
func spawnItemEntityValue(ie *ItemEntity, spawnX, spawnY, spawnZ float64, withVelocity bool) *v1_8.PlayClientboundSpawnEntity {
	spawn := &v1_8.PlayClientboundSpawnEntity{
		EntityID: ie.EntityID,
		Type:     2, // item stack
		X:        FixedPoint(spawnX),
		Y:        FixedPoint(spawnY),
		Z:        FixedPoint(spawnZ),
		Pitch:    0,
		Yaw:      0,
	}
	if withVelocity {
		spawn.IntField = 1
		spawn.ObjectData.Default.VelocityX = ie.VelX
		spawn.ObjectData.Default.VelocityY = ie.VelY
		spawn.ObjectData.Default.VelocityZ = ie.VelZ
	}
	return spawn
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

// itemMetadataPacket builds the EntityMetadata packet for an item entity.
func itemMetadataPacket(ie *ItemEntity) *v1_8.PlayClientboundEntityMetadata {
	return &v1_8.PlayClientboundEntityMetadata{
		EntityID: ie.EntityID,
		Metadata: itemMetadataValue(ie),
	}
}

// itemMetadataValue builds the entity metadata for an item entity.
// Index 10 (type 5 = slot) carries the item stack.
func itemMetadataValue(ie *ItemEntity) v1_8.EntityMetadata {
	return v1_8.EntityMetadata{{
		AnonymousBitField1: v1_8.EntityMetadataItemAnonymousBitField1Bits{
			Type: metaTypeSlot,
			Key:  10,
		},
		Value: v1_8.EntityMetadataItemValueSwitch{Case5: ToGeneratedSlot(ie.Item)},
	}}
}
