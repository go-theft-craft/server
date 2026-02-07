package packet

// Serverbound play packets

// KeepAliveServerbound is sent by the client in response to keep alive (serverbound 0x00).
type KeepAliveServerbound struct {
	KeepAliveID int32 `mc:"varint"`
}

func (KeepAliveServerbound) PacketID() int32 { return 0x00 }

// ChatMessageServerbound is sent by the client when they type a chat message (serverbound 0x01).
type ChatMessageServerbound struct {
	Message string `mc:"string"`
}

func (ChatMessageServerbound) PacketID() int32 { return 0x01 }

// PlayerPosition is sent by the client when they move (serverbound 0x04).
type PlayerPosition struct {
	X        float64 `mc:"f64"`
	FeetY    float64 `mc:"f64"`
	Z        float64 `mc:"f64"`
	OnGround bool    `mc:"bool"`
}

func (PlayerPosition) PacketID() int32 { return 0x04 }

// PlayerLook is sent by the client when they look around (serverbound 0x05).
type PlayerLook struct {
	Yaw      float32 `mc:"f32"`
	Pitch    float32 `mc:"f32"`
	OnGround bool    `mc:"bool"`
}

func (PlayerLook) PacketID() int32 { return 0x05 }

// PlayerPositionAndLookServerbound is sent when the client moves and looks (serverbound 0x06).
type PlayerPositionAndLookServerbound struct {
	X        float64 `mc:"f64"`
	FeetY    float64 `mc:"f64"`
	Z        float64 `mc:"f64"`
	Yaw      float32 `mc:"f32"`
	Pitch    float32 `mc:"f32"`
	OnGround bool    `mc:"bool"`
}

func (PlayerPositionAndLookServerbound) PacketID() int32 { return 0x06 }

// ClientSettings is sent by the client with their settings (serverbound 0x15).
type ClientSettings struct {
	Locale       string `mc:"string"`
	ViewDistance int8   `mc:"i8"`
	ChatMode     int8   `mc:"i8"`
	ChatColors   bool   `mc:"bool"`
	SkinParts    uint8  `mc:"u8"`
}

func (ClientSettings) PacketID() int32 { return 0x15 }

// Player is sent by the client as a heartbeat (serverbound 0x03).
type Player struct {
	OnGround bool `mc:"bool"`
}

func (Player) PacketID() int32 { return 0x03 }
