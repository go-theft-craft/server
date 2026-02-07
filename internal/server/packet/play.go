package packet

// Clientbound play packets

// GameMode constants.
const (
	GameModeSurvival  uint8 = 0
	GameModeCreative  uint8 = 1
	GameModeAdventure uint8 = 2
	GameModeSpectator uint8 = 3
)

// Dimension constants.
const (
	DimensionNether    int8 = -1
	DimensionOverworld int8 = 0
	DimensionEnd       int8 = 1
)

// Difficulty constants.
const (
	DifficultyPeaceful uint8 = 0
	DifficultyEasy     uint8 = 1
	DifficultyNormal   uint8 = 2
	DifficultyHard     uint8 = 3
)

// PlayerAbility flag bits.
const (
	AbilityInvulnerable int8 = 0x01
	AbilityFlying       int8 = 0x02
	AbilityAllowFlight  int8 = 0x04
	AbilityCreativeMode int8 = 0x08
)

// KeepAliveClientbound is sent by the server periodically (clientbound 0x00).
type KeepAliveClientbound struct {
	KeepAliveID int32 `mc:"varint"`
}

func (KeepAliveClientbound) PacketID() int32 { return 0x00 }

// JoinGame is sent after login success to initialize the player (clientbound 0x01).
type JoinGame struct {
	EntityID         int32  `mc:"i32"`
	GameMode         uint8  `mc:"u8"`
	Dimension        int8   `mc:"i8"`
	Difficulty       uint8  `mc:"u8"`
	MaxPlayers       uint8  `mc:"u8"`
	LevelType        string `mc:"string"`
	ReducedDebugInfo bool   `mc:"bool"`
}

func (JoinGame) PacketID() int32 { return 0x01 }

// ChatMessage sends a chat message to the client (clientbound 0x02).
type ChatMessage struct {
	JSONData string `mc:"string"`
	Position int8   `mc:"i8"`
}

func (ChatMessage) PacketID() int32 { return 0x02 }

// SpawnPosition sets the compass target (clientbound 0x05).
type SpawnPosition struct {
	Location int64 `mc:"position"`
}

func (SpawnPosition) PacketID() int32 { return 0x05 }

// PlayerPositionAndLook teleports the player (clientbound 0x08).
type PlayerPositionAndLook struct {
	X     float64 `mc:"f64"`
	Y     float64 `mc:"f64"`
	Z     float64 `mc:"f64"`
	Yaw   float32 `mc:"f32"`
	Pitch float32 `mc:"f32"`
	Flags int8    `mc:"i8"`
}

func (PlayerPositionAndLook) PacketID() int32 { return 0x08 }

// ChunkData sends chunk column data to the client (clientbound 0x21).
type ChunkData struct {
	ChunkX         int32  `mc:"i32"`
	ChunkZ         int32  `mc:"i32"`
	GroundUp       bool   `mc:"bool"`
	PrimaryBitMask uint16 `mc:"u16"`
	Data           []byte `mc:"bytearray"`
}

func (ChunkData) PacketID() int32 { return 0x21 }

// PlayerAbilities sets player ability flags (clientbound 0x39).
type PlayerAbilities struct {
	Flags        int8    `mc:"i8"`
	FlyingSpeed  float32 `mc:"f32"`
	WalkingSpeed float32 `mc:"f32"`
}

func (PlayerAbilities) PacketID() int32 { return 0x39 }

// HeldItemChange sets the selected hotbar slot (clientbound 0x09).
type HeldItemChange struct {
	Slot int8 `mc:"i8"`
}

func (HeldItemChange) PacketID() int32 { return 0x09 }

// PlayDisconnect disconnects the player during play (clientbound 0x40).
type PlayDisconnect struct {
	Reason string `mc:"string"`
}

func (PlayDisconnect) PacketID() int32 { return 0x40 }

// PlayerInfo updates the tab list (clientbound 0x38).
// This packet has a complex structure — we encode it manually.
type PlayerInfo struct {
	Data []byte `mc:"rest"`
}

func (PlayerInfo) PacketID() int32 { return 0x38 }

// BlockChange notifies the client of a single block change (clientbound 0x23).
type BlockChange struct {
	Location int64 `mc:"position"`
	BlockID  int32 `mc:"varint"`
}

func (BlockChange) PacketID() int32 { return 0x23 }

// PluginMessage sends a custom plugin channel message (clientbound 0x3F).
type PluginMessage struct {
	Channel string `mc:"string"`
	Data    []byte `mc:"rest"`
}

func (PluginMessage) PacketID() int32 { return 0x3F }
