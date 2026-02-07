package packet

// StatusRequest is sent by the client to request server status (serverbound 0x00 in Status state).
type StatusRequest struct{}

func (StatusRequest) PacketID() int32 { return 0x00 }

// StatusResponse is sent by the server with JSON status (clientbound 0x00 in Status state).
type StatusResponse struct {
	JSONResponse string `mc:"string"`
}

func (StatusResponse) PacketID() int32 { return 0x00 }

// StatusPing is sent by the client with a timestamp (serverbound 0x01 in Status state).
type StatusPing struct {
	Payload int64 `mc:"i64"`
}

func (StatusPing) PacketID() int32 { return 0x01 }

// StatusPong is sent by the server echoing the ping payload (clientbound 0x01 in Status state).
type StatusPong struct {
	Payload int64 `mc:"i64"`
}

func (StatusPong) PacketID() int32 { return 0x01 }
