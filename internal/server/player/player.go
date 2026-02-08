package player

import (
	"math"
	"sync"

	mcnet "github.com/OCharnyshevich/minecraft-server/internal/server/net"
)

// SkinProperty holds a single Mojang skin/cape property.
type SkinProperty struct {
	Name      string
	Value     string
	Signature string
}

// Position holds a player's world position and orientation.
type Position struct {
	X, Y, Z    float64
	Yaw, Pitch float32
	OnGround   bool
}

// Player represents a connected player.
type Player struct {
	mu         sync.RWMutex
	EntityID   int32
	UUID       string   // hyphenated
	UUIDBytes  [16]byte // for protocol encoding
	Username   string
	Properties []SkinProperty

	pos        Position
	lastFixedX int32
	lastFixedY int32
	lastFixedZ int32

	Inventory   *Inventory
	gameMode    uint8   // 0=survival, 1=creative, 2=adventure, 3=spectator
	entityFlags byte    // bit 1 = sneaking, bit 3 = sprinting
	skinParts   byte    // from ClientSettings
	flying      bool    // currently flying (set by AbilitiesSB)
	Height      float64 // 1.8 normal, 1.65 sneaking

	WritePacket    func(mcnet.Packet) error
	trackedPlayers map[int32]struct{}
}

// NewPlayer creates a new Player with its initial spawn position.
func NewPlayer(entityID int32, uuid string, uuidBytes [16]byte, username string, props []SkinProperty, writePacket func(mcnet.Packet) error) *Player {
	spawnPos := Position{X: 0.5, Y: 4.0, Z: 0.5}
	inv := NewInventory()
	inv.DefaultLoadout()

	return &Player{
		EntityID:       entityID,
		UUID:           uuid,
		UUIDBytes:      uuidBytes,
		Username:       username,
		Properties:     props,
		pos:            spawnPos,
		lastFixedX:     FixedPoint(spawnPos.X),
		lastFixedY:     FixedPoint(spawnPos.Y),
		lastFixedZ:     FixedPoint(spawnPos.Z),
		Inventory:      inv,
		Height:         1.8,
		WritePacket:    writePacket,
		trackedPlayers: make(map[int32]struct{}),
	}
}

// GetPosition returns a copy of the player's current position.
func (p *Player) GetPosition() Position {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.pos
}

// SetPosition updates the player's position and returns old and new fixed-point coordinates.
func (p *Player) SetPosition(x, y, z float64, yaw, pitch float32, onGround bool) (oldFX, oldFY, oldFZ, newFX, newFY, newFZ int32) {
	p.mu.Lock()
	defer p.mu.Unlock()

	oldFX = p.lastFixedX
	oldFY = p.lastFixedY
	oldFZ = p.lastFixedZ

	p.pos.X = x
	p.pos.Y = y
	p.pos.Z = z
	p.pos.Yaw = yaw
	p.pos.Pitch = pitch
	p.pos.OnGround = onGround

	newFX = FixedPoint(x)
	newFY = FixedPoint(y)
	newFZ = FixedPoint(z)

	p.lastFixedX = newFX
	p.lastFixedY = newFY
	p.lastFixedZ = newFZ

	return
}

// UpdateLook updates only the player's look direction.
func (p *Player) UpdateLook(yaw, pitch float32, onGround bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pos.Yaw = yaw
	p.pos.Pitch = pitch
	p.pos.OnGround = onGround
}

// ChunkX returns the chunk X coordinate for the player's current position.
func (p *Player) ChunkX() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return int(math.Floor(p.pos.X)) >> 4
}

// ChunkZ returns the chunk Z coordinate for the player's current position.
func (p *Player) ChunkZ() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return int(math.Floor(p.pos.Z)) >> 4
}

// IsTracking returns whether this player is tracking the given entity.
func (p *Player) IsTracking(entityID int32) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	_, ok := p.trackedPlayers[entityID]
	return ok
}

// Track marks an entity as tracked by this player.
func (p *Player) Track(entityID int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.trackedPlayers[entityID] = struct{}{}
}

// Untrack removes an entity from this player's tracking set.
func (p *Player) Untrack(entityID int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.trackedPlayers, entityID)
}

// TrackedEntities returns a copy of the tracked entity ID set.
func (p *Player) TrackedEntities() map[int32]struct{} {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make(map[int32]struct{}, len(p.trackedPlayers))
	for id := range p.trackedPlayers {
		result[id] = struct{}{}
	}
	return result
}

// SetSneaking sets or clears the sneaking flag (bit 1 of entityFlags).
func (p *Player) SetSneaking(sneaking bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if sneaking {
		p.entityFlags |= 0x02
		p.Height = 1.65
	} else {
		p.entityFlags &^= 0x02
		p.Height = 1.8
	}
}

// IsSneaking returns whether the player is sneaking.
func (p *Player) IsSneaking() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.entityFlags&0x02 != 0
}

// SetSprinting sets or clears the sprinting flag (bit 3 of entityFlags).
func (p *Player) SetSprinting(sprinting bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if sprinting {
		p.entityFlags |= 0x08
	} else {
		p.entityFlags &^= 0x08
	}
}

// IsSprinting returns whether the player is sprinting.
func (p *Player) IsSprinting() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.entityFlags&0x08 != 0
}

// SetFlying sets or clears the flying state.
func (p *Player) SetFlying(flying bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.flying = flying
}

// IsFlying returns whether the player is currently flying.
func (p *Player) IsFlying() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.flying
}

// SetSkinParts sets the skin parts bitmask from ClientSettings.
func (p *Player) SetSkinParts(parts byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.skinParts = parts
}

// GetSkinParts returns the current skin parts bitmask.
func (p *Player) GetSkinParts() byte {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.skinParts
}

// GetEntityFlags returns the current entity flags byte.
func (p *Player) GetEntityFlags() byte {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.entityFlags
}

// GetGameMode returns the player's current game mode.
func (p *Player) GetGameMode() uint8 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.gameMode
}

// SetGameMode sets the player's game mode.
func (p *Player) SetGameMode(mode uint8) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.gameMode = mode
}

// ApplyData restores a player's saved state (position, game mode, inventory).
func (p *Player) ApplyData(pos Position, gameMode uint8, slots [36]Slot, armor [4]Slot, heldSlot int16) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.pos = pos
	p.lastFixedX = FixedPoint(pos.X)
	p.lastFixedY = FixedPoint(pos.Y)
	p.lastFixedZ = FixedPoint(pos.Z)
	p.gameMode = gameMode

	p.Inventory.ApplyState(slots, armor, heldSlot)
}
