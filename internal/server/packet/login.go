package packet

// LoginStart is sent by the client with their username (serverbound 0x00 in Login state).
type LoginStart struct {
	Name string `mc:"string"`
}

func (LoginStart) PacketID() int32 { return 0x00 }

// EncryptionRequest is sent by the server to initiate encryption (clientbound 0x01).
type EncryptionRequest struct {
	ServerID    string `mc:"string"`
	PublicKey   []byte `mc:"bytearray"`
	VerifyToken []byte `mc:"bytearray"`
}

func (EncryptionRequest) PacketID() int32 { return 0x01 }

// EncryptionResponse is sent by the client with encrypted data (serverbound 0x01).
type EncryptionResponse struct {
	SharedSecret []byte `mc:"bytearray"`
	VerifyToken  []byte `mc:"bytearray"`
}

func (EncryptionResponse) PacketID() int32 { return 0x01 }

// LoginSuccess is sent by the server after successful login (clientbound 0x02).
type LoginSuccess struct {
	UUID     string `mc:"string"`
	Username string `mc:"string"`
}

func (LoginSuccess) PacketID() int32 { return 0x02 }

// SetCompression tells the client to enable compression (clientbound 0x03).
type SetCompression struct {
	Threshold int32 `mc:"varint"`
}

func (SetCompression) PacketID() int32 { return 0x03 }

// LoginDisconnect tells the client they are disconnected during login (clientbound 0x00).
type LoginDisconnect struct {
	Reason string `mc:"string"`
}

func (LoginDisconnect) PacketID() int32 { return 0x00 }
