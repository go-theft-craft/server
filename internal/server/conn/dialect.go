package conn

import (
	protocol "github.com/go-theft-craft/minecraft-protocol"

	"github.com/go-theft-craft/server/internal/server/player"
	"github.com/go-theft-craft/server/pkg/world"
)

// A Dialect is the version boundary of the play path. Above it, a handler
// reasons about what a player did; below it, a generated packet type says how
// one version spells that. It is the same seam world.Adapter is for blocks, and
// it exists for the same reason: the code that decides what happens when a
// player breaks a block is not version-specific, and until this existed it was
// written as though it were.
//
// Only dialect_v47.go may name a generated version package in this directory,
// and TestThePlayPathNamesNoVersion is what keeps that true.

// Action is what a client asked for, in terms no version owns.
//
// Every action carries exactly the fields its packet carries, with the same
// names and version-neutral types. Where two versions size a field
// differently, the action takes the wider and the narrower dialect converts.
type Action interface{ action() }

// MoveAction is any of the four movement packets. HasPos and HasLook say which
// halves the client sent; the flying heartbeat carries neither.
type MoveAction struct {
	X, Y, Z    float64
	Yaw, Pitch float32
	OnGround   bool
	HasPos     bool
	HasLook    bool
}

// DigStatus is what a dig packet reports.
type DigStatus int32

const (
	DigStarted   DigStatus = 0
	DigCancelled DigStatus = 1
	DigFinished  DigStatus = 2
	DigDropStack DigStatus = 3
	DigDropItem  DigStatus = 4
)

// BlockFace is the face of a block a client aimed at.
type BlockFace int8

type DigAction struct {
	Pos    world.BlockPos
	Status DigStatus
	Face   BlockFace
}

// PlaceAction is a right-click with an item. A Pos of (-1, -1, -1) is the item
// being used rather than placed, which is how protocol 47 says it.
//
// HeldItem is what the client claims to hold. The handler places what the
// server says is in hand instead; the claim is read only to decide which armor
// slot a right-click equips.
type PlaceAction struct {
	Pos      world.BlockPos
	Face     BlockFace
	HeldItem player.Slot
	Cursor   [3]int8
}

type ClickAction struct {
	WindowID    int8
	Slot        int16
	MouseButton int8
	Action      int16
	Mode        int8
	Item        player.Slot
}

type CloseWindowAction struct {
	WindowID int8
}

type ChatAction struct {
	Message string
}

// KeepAliveAction takes the wider of the two versions' IDs: protocol 47 sends
// a VarInt and later versions an int64.
type KeepAliveAction struct {
	ID int64
}

type HeldSlotAction struct {
	Slot int16
}

type ArmSwingAction struct{}

type EntityActionAction struct {
	EntityID  int32
	ActionID  int32
	JumpBoost int32
}

// UseEntityAction is an interaction with an entity. The interact-at hit
// position the packet carries for Mouse == 2 is decoded and not needed.
type UseEntityAction struct {
	Target int32
	Mouse  int32
}

type SettingsAction struct {
	Locale       string
	ViewDistance int8
	ChatFlags    int8
	ChatColors   bool
	SkinParts    uint8
}

type AbilitiesAction struct {
	Flags        int8
	FlyingSpeed  float32
	WalkingSpeed float32
}

// TabCompleteAction is a completion request. The looked-at block the packet
// carries is not needed: see handleTabComplete.
type TabCompleteAction struct {
	Text string
}

type CreativeSlotAction struct {
	Slot int16
	Item player.Slot
}

type SignAction struct {
	Pos                        world.BlockPos
	Text1, Text2, Text3, Text4 string
}

type TransactionAction struct {
	WindowID int8
	Action   int16
	Accepted bool
}

type ClientCommandAction struct {
	Payload int32
}

type PayloadAction struct {
	Channel string
	Data    []byte
}

// SpectateAction names its target by hyphenated UUID, the form the player
// manager looks players up by.
type SpectateAction struct {
	Target string
}

type ResourcePackAction struct {
	Hash   string
	Result int32
}

func (MoveAction) action()          {}
func (DigAction) action()           {}
func (PlaceAction) action()         {}
func (ClickAction) action()         {}
func (CloseWindowAction) action()   {}
func (ChatAction) action()          {}
func (KeepAliveAction) action()     {}
func (HeldSlotAction) action()      {}
func (ArmSwingAction) action()      {}
func (EntityActionAction) action()  {}
func (UseEntityAction) action()     {}
func (SettingsAction) action()      {}
func (AbilitiesAction) action()     {}
func (TabCompleteAction) action()   {}
func (CreativeSlotAction) action()  {}
func (SignAction) action()          {}
func (TransactionAction) action()   {}
func (ClientCommandAction) action() {}
func (PayloadAction) action()       {}
func (SpectateAction) action()      {}
func (ResourcePackAction) action()  {}

// Dialect turns one version's packets into actions and back.
//
// The write half returns the packet its version spells a message with, and the
// caller sends it. No method returns an error, and that departs from the plan
// that introduced this seam: protocol 47 cannot fail to spell any of these, and
// the way a later version can -- an item it has no mapping for -- is shaped when
// that version has a caller rather than guessed at now.
//
// The interface is complete when it covers what the protocol 47 path sends.
// A method added because a later version might want it is a method designed
// without a caller. The chunk column and its unload are not here because the
// world adapter already owns them, and the disconnect a stream shutdown sends
// is the stream's.
type Dialect interface {
	// Protocol is the descriptor the connection's session is built from.
	Protocol() protocol.Protocol

	// Read turns a decoded inbound packet into an action. A packet this
	// version defines and this server ignores returns (nil, true); one it
	// does not know returns (nil, false). The distinction is the
	// connection's to log.
	Read(packet protocol.Packet) (Action, bool)

	Join(JoinFields) world.Packet
	Position(PositionFields) world.Packet
	KeepAlive(id int64) world.Packet
	Chat(message string, position int8) world.Packet
	BlockChange(pos world.BlockPos, state int32) world.Packet
	WindowItems(window int8, slots []player.Slot) world.Packet
	SetSlot(window int8, slot int16, item player.Slot) world.Packet
	OpenWindow(OpenWindowFields) world.Packet
	Transaction(window int8, action int16, accepted bool) world.Packet
	Abilities(flags int8, flying, walking float32) world.Packet
	UpdateHealth(health float32, food int32, saturation float32) world.Packet
	UpdateTime(age, time int64) world.Packet
	SpawnPosition(pos world.BlockPos) world.Packet
	Respawn(RespawnFields) world.Packet
	GameStateChange(reason uint8, value float32) world.Packet
	Kick(reason string) world.Packet
	TabComplete(matches []string) world.Packet
	CustomPayload(channel string, payload []byte) world.Packet
	BlockBreakAnimation(entity int32, pos world.BlockPos, stage int8) world.Packet
	WorldEvent(effect int32, pos world.BlockPos, data int32, global bool) world.Packet
	SprintParticles(x, y, z float64, state int32) world.Packet

	// The entity half, which the player manager drives through the narrow
	// interface it declares for itself.
	player.Packets
}

// JoinFields is the argument list of the join packet.
type JoinFields struct {
	EntityID         int32
	GameMode         uint8
	Dimension        int8
	Difficulty       uint8
	MaxPlayers       uint8
	LevelType        string
	ReducedDebugInfo bool
}

// PositionFields is the argument list of the packet that sets where the
// client's own player is.
type PositionFields struct {
	X, Y, Z    float64
	Yaw, Pitch float32
	Flags      int8
}

// OpenWindowFields is the argument list of the packet that opens a window.
type OpenWindowFields struct {
	WindowID      int8
	InventoryType string
	WindowTitle   string
	SlotCount     uint8
}

// RespawnFields is the argument list of the respawn packet.
type RespawnFields struct {
	Dimension  int32
	Difficulty uint8
	GameMode   uint8
	LevelType  string
}
