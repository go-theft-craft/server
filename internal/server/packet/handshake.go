package packet

// Handshake is sent by the client to begin a connection (serverbound 0x00).
type Handshake struct {
	ProtocolVersion int32  `mc:"varint"`
	ServerAddress   string `mc:"string"`
	ServerPort      uint16 `mc:"u16"`
	NextState       int32  `mc:"varint"`
}

func (Handshake) PacketID() int32 { return 0x00 }
