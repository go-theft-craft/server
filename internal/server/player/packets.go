package player

import "github.com/go-theft-craft/server/pkg/world"

// Packets is the entity half of the play path's version boundary: every
// clientbound packet this package builds, asked for in terms no version owns.
//
// It is declared here rather than taken from the connection's Dialect because
// this package sits below the connection's, and naming the connection's type
// from here is an import cycle. The connection's dialect satisfies it
// structurally, the way gen.Generator satisfies world.Generator.
//
// Each method returns the packet its version spells the message with; the
// caller writes it. None returns an error: protocol 47 cannot fail to spell any
// of them, and the way a later version can -- an item it has no mapping for --
// is decided when that version has a caller, not guessed at here.
type Packets interface {
	SpawnPlayer(SpawnPlayerFields) world.Packet
	SpawnEntity(SpawnEntityFields) world.Packet
	DestroyEntities(ids []int32) world.Packet
	EntityMove(id int32, dx, dy, dz int8, onGround bool) world.Packet
	EntityLook(id int32, yaw, pitch int8, onGround bool) world.Packet
	EntityMoveLook(EntityMoveLookFields) world.Packet
	EntityTeleport(EntityTeleportFields) world.Packet
	EntityHeadRotation(id int32, yaw int8) world.Packet
	EntityVelocity(id int32, dx, dy, dz int16) world.Packet
	EntityMetadata(id int32, entries []MetadataEntry) world.Packet
	EntityEquipment(id int32, slot int16, item Slot) world.Packet
	EntityStatus(id int32, status int8) world.Packet
	EntityAnimation(id int32, animation uint8) world.Packet
	CollectItem(collected, collector int32) world.Packet
	PlayerInfo(PlayerInfoFields) world.Packet
}

// MetadataKind is the type of one entity metadata entry.
type MetadataKind uint8

const (
	// MetadataByte is a single byte: entity flags, skin parts.
	MetadataByte MetadataKind = iota
	// MetadataItem is an item stack: what a dropped item entity shows.
	MetadataItem
)

// MetadataEntry is one entity metadata entry.
//
// It is a neutral value rather than a generated list because the two versions
// both encode an entry and terminate a list differently -- 0x7F against 0xFF,
// which is why protocolinfo.MetadataEnd is version-scoped -- and a caller that
// built one version's list would have to be rewritten for the other.
type MetadataEntry struct {
	Index uint8
	Kind  MetadataKind
	Byte  int8
	Item  Slot
}

// SpawnPlayerFields is the argument list of the packet that shows one player
// to another. Positions are fixed-point and angles are angle bytes, as they are
// on the wire; FixedPoint and DegreesToAngle produce them.
type SpawnPlayerFields struct {
	EntityID    int32
	UUID        [16]byte
	X, Y, Z     int32
	Yaw, Pitch  int8
	CurrentItem int16
	Metadata    []MetadataEntry
}

// SpawnEntityFields is the argument list of the packet that spawns an object.
// A non-zero ObjectData is what says the velocity follows.
type SpawnEntityFields struct {
	EntityID                        int32
	Type                            int8
	X, Y, Z                         int32
	Pitch, Yaw                      int8
	ObjectData                      int32
	VelocityX, VelocityY, VelocityZ int16
}

// EntityMoveLookFields is a relative move with a look.
type EntityMoveLookFields struct {
	EntityID   int32
	DX, DY, DZ int8
	Yaw, Pitch int8
	OnGround   bool
}

// EntityTeleportFields is an absolute move.
type EntityTeleportFields struct {
	EntityID   int32
	X, Y, Z    int32
	Yaw, Pitch int8
	OnGround   bool
}

// PlayerInfoAction is what a tab-list update does.
type PlayerInfoAction uint8

const (
	PlayerInfoAdd PlayerInfoAction = iota
	PlayerInfoUpdateGameMode
	PlayerInfoRemove
)

// PlayerInfoFields is one tab-list update. Every entry is read for the fields
// its action carries and no others: a removal names only the UUID.
type PlayerInfoFields struct {
	Action  PlayerInfoAction
	Players []PlayerInfoEntry
}

// PlayerInfoEntry is one player in a tab-list update.
type PlayerInfoEntry struct {
	UUID       [16]byte
	Name       string
	Properties []SkinProperty
	GameMode   int32
	Ping       int32
}
