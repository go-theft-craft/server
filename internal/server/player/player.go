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

	WritePacket    func(mcnet.Packet) error
	trackedPlayers map[int32]struct{}
}

// NewPlayer creates a new Player with its initial spawn position.
func NewPlayer(entityID int32, uuid string, uuidBytes [16]byte, username string, props []SkinProperty, writePacket func(mcnet.Packet) error) *Player {
	spawnPos := Position{X: 0.5, Y: 4.0, Z: 0.5}
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
