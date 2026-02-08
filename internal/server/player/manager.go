package player

import (
	"bytes"
	"encoding/binary"
	"strings"
	"sync"
	"sync/atomic"

	pkt "github.com/go-theft-craft/server/pkg/gamedata/versions/pc_1_8"
	mcnet "github.com/go-theft-craft/server/pkg/protocol"
)

// Manager tracks all connected players and handles entity visibility.
type Manager struct {
	mu           sync.RWMutex
	players      map[int32]*Player // entityID → Player
	byUUID       map[string]int32  // UUID → entityID
	nextEntityID atomic.Int32
	currentTick  atomic.Int64
	viewDistance int

	itemMu       sync.Mutex
	itemEntities map[int32]*ItemEntity
}

// NewManager creates a new player manager with the given view distance (in chunks).
func NewManager(viewDistance int) *Manager {
	mgr := &Manager{
		players:      make(map[int32]*Player),
		byUUID:       make(map[string]int32),
		viewDistance: viewDistance,
		itemEntities: make(map[int32]*ItemEntity),
	}
	return mgr
}

// AllocateEntityID returns the next unique entity ID.
func (m *Manager) AllocateEntityID() int32 {
	return m.nextEntityID.Add(1)
}

// Tick advances the manager by one tick and runs periodic cleanup.
func (m *Manager) Tick() {
	tick := m.currentTick.Add(1)

	// Run item expiry cleanup every 600 ticks (~30 seconds).
	if tick%600 == 0 {
		m.cleanupExpiredItems(tick)
	}

	// Resync absolute entity positions every 400 ticks (~20 seconds)
	// to prevent client-side hitbox drift from accumulated relative moves.
	if tick%400 == 0 {
		m.resyncPositions()
	}
}

// resyncPositions broadcasts absolute EntityTeleport packets for all players
// to correct any client-side position drift from relative movement packets.
func (m *Manager) resyncPositions() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, p := range m.players {
		pos := p.GetPosition()
		tp := &pkt.EntityTeleport{
			EntityID: p.EntityID,
			X:        FixedPoint(pos.X),
			Y:        FixedPoint(pos.Y),
			Z:        FixedPoint(pos.Z),
			Yaw:      DegreesToAngle(pos.Yaw),
			Pitch:    DegreesToAngle(pos.Pitch),
			OnGround: pos.OnGround,
		}
		for _, other := range m.players {
			if other.EntityID != p.EntityID && other.IsTracking(p.EntityID) {
				_ = other.WritePacket(tp)
			}
		}
	}
}

// Add registers a player and sends cross-wise PlayerInfo + spawn packets.
// It also sends existing item entities to the new player.
func (m *Manager) Add(p *Player) {
	m.mu.Lock()

	m.players[p.EntityID] = p
	m.byUUID[p.UUID] = p.EntityID

	newPlayerInfo := buildPlayerInfoAdd(p)
	cx, cz := p.ChunkX(), p.ChunkZ()

	// Send the player their own PlayerInfo so the client knows its skin for the inventory.
	_ = p.WritePacket(&pkt.PlayerInfo{Data: newPlayerInfo})

	for _, other := range m.players {
		if other.EntityID == p.EntityID {
			continue
		}

		// Send existing player's info to the new player.
		_ = p.WritePacket(&pkt.PlayerInfo{Data: buildPlayerInfoAdd(other)})

		// Send new player's info to existing players.
		_ = other.WritePacket(&pkt.PlayerInfo{Data: newPlayerInfo})

		// Check view distance for entity spawning.
		ocx, ocz := other.ChunkX(), other.ChunkZ()
		if InViewDistance(cx, cz, ocx, ocz, m.viewDistance) {
			m.spawnPlayerFor(other, p) // existing sees new
			m.spawnPlayerFor(p, other) // new sees existing
		}
	}

	// Release mu before acquiring itemMu to avoid deadlock
	// (SpawnItemEntity acquires itemMu then mu).
	m.mu.Unlock()

	// Send existing item entities to the new player.
	type itemSnapshot struct {
		spawnData []byte
		metaData  []byte
		entityID  int32
	}

	m.itemMu.Lock()
	items := make([]itemSnapshot, 0, len(m.itemEntities))
	for _, ie := range m.itemEntities {
		items = append(items, itemSnapshot{
			spawnData: buildSpawnEntityDataAtRest(ie),
			metaData:  buildItemMetadata(ie),
			entityID:  ie.EntityID,
		})
	}
	m.itemMu.Unlock()

	for _, it := range items {
		_ = p.WritePacket(&pkt.SpawnEntity{Data: it.spawnData})
		_ = p.WritePacket(&pkt.EntityMetadata{Data: buildEntityMetadataData(it.entityID, it.metaData)})
	}
}

// Remove unregisters a player and cleans up tracking/tab list for all others.
func (m *Manager) Remove(p *Player) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.players, p.EntityID)
	delete(m.byUUID, p.UUID)

	removeInfo := buildPlayerInfoRemove(p)
	destroyData := buildDestroyEntities([]int32{p.EntityID})

	for _, other := range m.players {
		_ = other.WritePacket(&pkt.PlayerInfo{Data: removeInfo})

		if other.IsTracking(p.EntityID) {
			_ = other.WritePacket(&pkt.EntityDestroy{Data: destroyData})
			other.Untrack(p.EntityID)
		}
	}
}

// Broadcast sends a packet to all connected players.
func (m *Manager) Broadcast(p mcnet.Packet) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, pl := range m.players {
		_ = pl.WritePacket(p)
	}
}

// BroadcastExcept sends a packet to all players except the one with excludeEntityID.
func (m *Manager) BroadcastExcept(p mcnet.Packet, excludeEntityID int32) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, pl := range m.players {
		if pl.EntityID != excludeEntityID {
			_ = pl.WritePacket(p)
		}
	}
}

// BroadcastToTrackers sends a packet to all players tracking the given entity.
func (m *Manager) BroadcastToTrackers(p mcnet.Packet, entityID int32) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, pl := range m.players {
		if pl.EntityID != entityID && pl.IsTracking(entityID) {
			_ = pl.WritePacket(p)
		}
	}
}

// UpdateTracking checks all player pairs for enter/leave range events
// after a player has moved.
func (m *Manager) UpdateTracking(moved *Player) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cx, cz := moved.ChunkX(), moved.ChunkZ()

	for _, other := range m.players {
		if other.EntityID == moved.EntityID {
			continue
		}

		ocx, ocz := other.ChunkX(), other.ChunkZ()
		inRange := InViewDistance(cx, cz, ocx, ocz, m.viewDistance)

		otherTracksMoved := other.IsTracking(moved.EntityID)
		movedTracksOther := moved.IsTracking(other.EntityID)

		if inRange && !otherTracksMoved {
			// Enter range: spawn for each other.
			m.spawnPlayerFor(other, moved)
			if !movedTracksOther {
				m.spawnPlayerFor(moved, other)
			}
		} else if !inRange && otherTracksMoved {
			// Leave range: destroy for each other.
			destroyMoved := buildDestroyEntities([]int32{moved.EntityID})
			_ = other.WritePacket(&pkt.EntityDestroy{Data: destroyMoved})
			other.Untrack(moved.EntityID)

			if movedTracksOther {
				destroyOther := buildDestroyEntities([]int32{other.EntityID})
				_ = moved.WritePacket(&pkt.EntityDestroy{Data: destroyOther})
				moved.Untrack(other.EntityID)
			}
		}
	}
}

// BroadcastEntityMetadata sends an EntityMetadata packet to all trackers of the given player.
func (m *Manager) BroadcastEntityMetadata(p *Player) {
	metaData := BuildEntityMetadata(p)
	m.BroadcastToTrackers(&pkt.EntityMetadata{
		Data: buildEntityMetadataData(p.EntityID, metaData),
	}, p.EntityID)
}

// PlayerCount returns the number of connected players.
func (m *Manager) PlayerCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.players)
}

// GetByEntityID returns the player with the given entity ID, or nil.
func (m *Manager) GetByEntityID(entityID int32) *Player {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.players[entityID]
}

// GetByUUID returns the player with the given UUID string, or nil.
func (m *Manager) GetByUUID(uuid string) *Player {
	m.mu.RLock()
	defer m.mu.RUnlock()
	eid, ok := m.byUUID[uuid]
	if !ok {
		return nil
	}
	return m.players[eid]
}

// GetByName returns the player with the given username (case-insensitive), or nil.
func (m *Manager) GetByName(name string) *Player {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, p := range m.players {
		if strings.EqualFold(p.Username, name) {
			return p
		}
	}
	return nil
}

// ForEach calls fn for every connected player under a read lock.
func (m *Manager) ForEach(fn func(*Player)) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, p := range m.players {
		fn(p)
	}
}

// spawnPlayerFor sends the SpawnNamedEntity + EntityHeadLook + EntityTeleport
// + EntityMetadata + EntityEquipment packets so that viewer can see target.
func (m *Manager) spawnPlayerFor(viewer, target *Player) {
	pos := target.GetPosition()

	spawnData := buildSpawnNamedEntity(target, pos)
	_ = viewer.WritePacket(&pkt.NamedEntitySpawn{Data: spawnData})

	_ = viewer.WritePacket(&pkt.EntityHeadRotation{
		EntityID: target.EntityID,
		HeadYaw:  DegreesToAngle(pos.Yaw),
	})

	_ = viewer.WritePacket(&pkt.EntityTeleport{
		EntityID: target.EntityID,
		X:        FixedPoint(pos.X),
		Y:        FixedPoint(pos.Y),
		Z:        FixedPoint(pos.Z),
		Yaw:      DegreesToAngle(pos.Yaw),
		Pitch:    DegreesToAngle(pos.Pitch),
		OnGround: pos.OnGround,
	})

	// Send entity metadata (flags + skin parts).
	metaData := BuildEntityMetadata(target)
	_ = viewer.WritePacket(&pkt.EntityMetadata{Data: buildEntityMetadataData(target.EntityID, metaData)})

	// Send 5 equipment packets (held item + 4 armor slots).
	for _, eqData := range BuildEquipmentPackets(target.EntityID, target.Inventory) {
		_ = viewer.WritePacket(&pkt.EntityEquipment{Data: eqData})
	}

	viewer.Track(target.EntityID)
}

// buildEntityMetadataData prepends the entity ID (varint) to raw metadata bytes.
func buildEntityMetadataData(entityID int32, metadata []byte) []byte {
	var buf bytes.Buffer
	_, _ = mcnet.WriteVarInt(&buf, entityID)
	buf.Write(metadata)
	return buf.Bytes()
}

// buildSpawnNamedEntity encodes the SpawnNamedEntity data fields.
func buildSpawnNamedEntity(p *Player, pos Position) []byte {
	var buf bytes.Buffer

	_, _ = mcnet.WriteVarInt(&buf, p.EntityID)
	buf.Write(p.UUIDBytes[:])
	_ = binary.Write(&buf, binary.BigEndian, FixedPoint(pos.X))
	_ = binary.Write(&buf, binary.BigEndian, FixedPoint(pos.Y))
	_ = binary.Write(&buf, binary.BigEndian, FixedPoint(pos.Z))
	buf.WriteByte(byte(DegreesToAngle(pos.Yaw)))
	buf.WriteByte(byte(DegreesToAngle(pos.Pitch)))

	// Current item in hand.
	heldItem := p.Inventory.HeldItem()
	if heldItem.IsEmpty() {
		_ = binary.Write(&buf, binary.BigEndian, int16(0))
	} else {
		_ = binary.Write(&buf, binary.BigEndian, heldItem.BlockID)
	}

	// Entity metadata (flags + skin parts + terminator).
	buf.Write(BuildSpawnMetadata(p))

	return buf.Bytes()
}

// buildPlayerInfoAdd builds a PlayerInfo packet data with action=0 (Add Player).
func buildPlayerInfoAdd(p *Player) []byte {
	var buf bytes.Buffer

	_, _ = mcnet.WriteVarInt(&buf, 0) // action: Add Player
	_, _ = mcnet.WriteVarInt(&buf, 1) // count: 1
	buf.Write(p.UUIDBytes[:])
	_, _ = mcnet.WriteString(&buf, p.Username)

	_, _ = mcnet.WriteVarInt(&buf, int32(len(p.Properties)))
	for _, prop := range p.Properties {
		_, _ = mcnet.WriteString(&buf, prop.Name)
		_, _ = mcnet.WriteString(&buf, prop.Value)
		if prop.Signature != "" {
			buf.WriteByte(1)
			_, _ = mcnet.WriteString(&buf, prop.Signature)
		} else {
			buf.WriteByte(0)
		}
	}

	_, _ = mcnet.WriteVarInt(&buf, int32(p.GetGameMode())) // gamemode
	_, _ = mcnet.WriteVarInt(&buf, 0)                      // ping
	buf.WriteByte(0)                                       // no display name

	return buf.Bytes()
}

// BroadcastGameMode sends a PlayerInfo Update Gamemode packet to all players.
func (m *Manager) BroadcastGameMode(p *Player) {
	var buf bytes.Buffer

	_, _ = mcnet.WriteVarInt(&buf, 1) // action: Update Gamemode
	_, _ = mcnet.WriteVarInt(&buf, 1) // count: 1
	buf.Write(p.UUIDBytes[:])
	_, _ = mcnet.WriteVarInt(&buf, int32(p.GetGameMode()))

	data := buf.Bytes()
	m.Broadcast(&pkt.PlayerInfo{Data: data})
}

// buildPlayerInfoRemove builds a PlayerInfo packet data with action=4 (Remove Player).
func buildPlayerInfoRemove(p *Player) []byte {
	var buf bytes.Buffer

	_, _ = mcnet.WriteVarInt(&buf, 4) // action: Remove Player
	_, _ = mcnet.WriteVarInt(&buf, 1) // count: 1
	buf.Write(p.UUIDBytes[:])

	return buf.Bytes()
}

// buildDestroyEntities encodes the DestroyEntities data fields.
func buildDestroyEntities(ids []int32) []byte {
	var buf bytes.Buffer

	_, _ = mcnet.WriteVarInt(&buf, int32(len(ids)))
	for _, id := range ids {
		_, _ = mcnet.WriteVarInt(&buf, id)
	}

	return buf.Bytes()
}
