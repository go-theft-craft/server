package player

import (
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"

	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"
	"github.com/go-theft-craft/minecraft-protocol/wire/java"

	"github.com/go-theft-craft/server/pkg/world"
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

	// index is the item index, or nil when item identity is off, which is the
	// default. A dropped item is the one place items live outside a window, so
	// the manager is on the write path as much as the click handlers are.
	index world.ItemIndex
	log   *slog.Logger
}

// NewManager creates a new player manager with the given view distance (in chunks).
func NewManager(viewDistance int) *Manager {
	mgr := &Manager{
		players:      make(map[int32]*Player),
		byUUID:       make(map[string]int32),
		viewDistance: viewDistance,
		itemEntities: make(map[int32]*ItemEntity),
		log:          slog.New(slog.DiscardHandler),
	}
	return mgr
}

// SetItemIndex puts the manager on the item index's write path. A server built
// without item identity never calls it and the manager stays as it was.
func (m *Manager) SetItemIndex(index world.ItemIndex, log *slog.Logger) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	m.index = index
	m.log = log
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
		tp := &v1_8.PlayClientboundEntityTeleport{
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

	newPlayerInfo := playerInfoAdd(p)
	cx, cz := p.ChunkX(), p.ChunkZ()

	// Send the player their own PlayerInfo so the client knows its skin for the inventory.
	_ = p.WritePacket(newPlayerInfo)

	for _, other := range m.players {
		if other.EntityID == p.EntityID {
			continue
		}

		// Send existing player's info to the new player.
		_ = p.WritePacket(playerInfoAdd(other))

		// Send new player's info to existing players.
		_ = other.WritePacket(newPlayerInfo)

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
		spawn *v1_8.PlayClientboundSpawnEntity
		meta  *v1_8.PlayClientboundEntityMetadata
	}

	m.itemMu.Lock()
	items := make([]itemSnapshot, 0, len(m.itemEntities))
	for _, ie := range m.itemEntities {
		items = append(items, itemSnapshot{
			spawn: spawnItemEntityValue(ie, ie.X, ie.Y, ie.Z, false),
			meta:  itemMetadataPacket(ie),
		})
	}
	m.itemMu.Unlock()

	for _, it := range items {
		_ = p.WritePacket(it.spawn)
		_ = p.WritePacket(it.meta)
	}
}

// Remove unregisters a player and cleans up tracking/tab list for all others.
func (m *Manager) Remove(p *Player) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.players, p.EntityID)
	delete(m.byUUID, p.UUID)

	removeInfo := playerInfoRemove(p)

	for _, other := range m.players {
		_ = other.WritePacket(removeInfo)

		if other.IsTracking(p.EntityID) {
			_ = other.WritePacket(&v1_8.PlayClientboundEntityDestroy{EntityIds: []int32{p.EntityID}})
			other.Untrack(p.EntityID)
		}
	}
}

// Broadcast sends a packet to all connected players.
func (m *Manager) Broadcast(p java.PacketValue) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, pl := range m.players {
		_ = pl.WritePacket(p)
	}
}

// BroadcastExcept sends a packet to all players except the one with excludeEntityID.
func (m *Manager) BroadcastExcept(p java.PacketValue, excludeEntityID int32) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, pl := range m.players {
		if pl.EntityID != excludeEntityID {
			_ = pl.WritePacket(p)
		}
	}
}

// BroadcastToTrackers sends a packet to all players tracking the given entity.
func (m *Manager) BroadcastToTrackers(p java.PacketValue, entityID int32) {
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
			_ = other.WritePacket(&v1_8.PlayClientboundEntityDestroy{EntityIds: []int32{moved.EntityID}})
			other.Untrack(moved.EntityID)

			if movedTracksOther {
				_ = moved.WritePacket(&v1_8.PlayClientboundEntityDestroy{EntityIds: []int32{other.EntityID}})
				moved.Untrack(other.EntityID)
			}
		}
	}
}

// BroadcastEntityMetadata sends an EntityMetadata packet to all trackers of the given player.
func (m *Manager) BroadcastEntityMetadata(p *Player) {
	m.BroadcastToTrackers(&v1_8.PlayClientboundEntityMetadata{
		EntityID: p.EntityID,
		Metadata: BuildEntityMetadata(p),
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

	_ = viewer.WritePacket(namedEntitySpawnValue(target, pos))

	_ = viewer.WritePacket(&v1_8.PlayClientboundEntityHeadRotation{
		EntityID: target.EntityID,
		HeadYaw:  DegreesToAngle(pos.Yaw),
	})

	_ = viewer.WritePacket(&v1_8.PlayClientboundEntityTeleport{
		EntityID: target.EntityID,
		X:        FixedPoint(pos.X),
		Y:        FixedPoint(pos.Y),
		Z:        FixedPoint(pos.Z),
		Yaw:      DegreesToAngle(pos.Yaw),
		Pitch:    DegreesToAngle(pos.Pitch),
		OnGround: pos.OnGround,
	})

	// Send entity metadata (flags + skin parts).
	_ = viewer.WritePacket(&v1_8.PlayClientboundEntityMetadata{
		EntityID: target.EntityID,
		Metadata: BuildEntityMetadata(target),
	})

	// Send 5 equipment packets (held item + 4 armor slots).
	eqs := BuildEquipmentValues(target.EntityID, target.Inventory)
	for i := range eqs {
		_ = viewer.WritePacket(&eqs[i])
	}

	viewer.Track(target.EntityID)
}

// namedEntitySpawnValue builds the NamedEntitySpawn packet for a player.
func namedEntitySpawnValue(p *Player, pos Position) *v1_8.PlayClientboundNamedEntitySpawn {
	// Current item in hand: the block ID, or 0 when the hand is empty.
	var currentItem int16
	if heldItem := p.Inventory.HeldItem(); !heldItem.IsEmpty() {
		currentItem = heldItem.BlockID
	}

	return &v1_8.PlayClientboundNamedEntitySpawn{
		EntityID:    p.EntityID,
		PlayerUUID:  java.UUID(p.UUIDBytes),
		X:           FixedPoint(pos.X),
		Y:           FixedPoint(pos.Y),
		Z:           FixedPoint(pos.Z),
		Yaw:         DegreesToAngle(pos.Yaw),
		Pitch:       DegreesToAngle(pos.Pitch),
		CurrentItem: currentItem,
		Metadata:    BuildSpawnMetadata(p),
	}
}

// playerInfoAdd builds a PlayerInfo packet with action=add_player for a player.
func playerInfoAdd(p *Player) *v1_8.PlayClientboundPlayerInfo {
	props := make([]v1_8.PlayClientboundPlayerInfoDataItemAnonymousSwitch1SwitchAddPlayerPropertiesItem, 0, len(p.Properties))
	for _, prop := range p.Properties {
		item := v1_8.PlayClientboundPlayerInfoDataItemAnonymousSwitch1SwitchAddPlayerPropertiesItem{
			Name:  prop.Name,
			Value: prop.Value,
		}
		if prop.Signature != "" {
			sig := prop.Signature
			item.Signature = &sig
		}
		props = append(props, item)
	}

	return &v1_8.PlayClientboundPlayerInfo{
		Action: "add_player",
		Data: []v1_8.PlayClientboundPlayerInfoDataItem{{
			UUID: java.UUID(p.UUIDBytes),
			AnonymousSwitch1: v1_8.PlayClientboundPlayerInfoDataItemAnonymousSwitch1Switch{
				AddPlayer: v1_8.PlayClientboundPlayerInfoDataItemAnonymousSwitch1SwitchAddPlayer{
					Name:       p.Username,
					Properties: props,
					Gamemode:   int32(p.GetGameMode()),
					Ping:       0,
				},
			},
		}},
	}
}

// BroadcastGameMode sends a PlayerInfo update_game_mode packet to all players.
func (m *Manager) BroadcastGameMode(p *Player) {
	m.Broadcast(&v1_8.PlayClientboundPlayerInfo{
		Action: "update_game_mode",
		Data: []v1_8.PlayClientboundPlayerInfoDataItem{{
			UUID: java.UUID(p.UUIDBytes),
			AnonymousSwitch1: v1_8.PlayClientboundPlayerInfoDataItemAnonymousSwitch1Switch{
				UpdateGameMode: v1_8.PlayClientboundPlayerInfoDataItemAnonymousSwitch1SwitchUpdateGameMode{
					Gamemode: int32(p.GetGameMode()),
				},
			},
		}},
	})
}

// playerInfoRemove builds a PlayerInfo packet with action=remove_player.
func playerInfoRemove(p *Player) *v1_8.PlayClientboundPlayerInfo {
	return &v1_8.PlayClientboundPlayerInfo{
		Action: "remove_player",
		Data: []v1_8.PlayClientboundPlayerInfoDataItem{{
			UUID: java.UUID(p.UUIDBytes),
		}},
	}
}
