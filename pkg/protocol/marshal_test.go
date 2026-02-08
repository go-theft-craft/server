package protocol

import (
	"testing"
)

type testPacket struct {
	EntityID    int32  `mc:"i32"`
	GameMode    uint8  `mc:"u8"`
	Dimension   int8   `mc:"i8"`
	Difficulty  uint8  `mc:"u8"`
	MaxPlayers  uint8  `mc:"u8"`
	LevelType   string `mc:"string"`
	ReducedInfo bool   `mc:"bool"`
}

func (testPacket) PacketID() int32 { return 0x01 }

func TestMarshalUnmarshal(t *testing.T) {
	original := &testPacket{
		EntityID:    42,
		GameMode:    1,
		Dimension:   0,
		Difficulty:  1,
		MaxPlayers:  20,
		LevelType:   "flat",
		ReducedInfo: false,
	}

	data, err := Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	decoded := &testPacket{}
	if err := Unmarshal(data, decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if *original != *decoded {
		t.Errorf("round-trip mismatch:\n  got  %+v\n  want %+v", decoded, original)
	}
}

type testVarIntPacket struct {
	ProtocolVersion int32  `mc:"varint"`
	ServerAddress   string `mc:"string"`
	ServerPort      uint16 `mc:"u16"`
	NextState       int32  `mc:"varint"`
}

func (testVarIntPacket) PacketID() int32 { return 0x00 }

func TestMarshalVarInt(t *testing.T) {
	original := &testVarIntPacket{
		ProtocolVersion: 47,
		ServerAddress:   "localhost",
		ServerPort:      25565,
		NextState:       2,
	}

	data, err := Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	decoded := &testVarIntPacket{}
	if err := Unmarshal(data, decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if *original != *decoded {
		t.Errorf("round-trip mismatch:\n  got  %+v\n  want %+v", decoded, original)
	}
}

type testRestPacket struct {
	ID   int32  `mc:"varint"`
	Data []byte `mc:"rest"`
}

func (testRestPacket) PacketID() int32 { return 0xFF }

func TestMarshalRest(t *testing.T) {
	original := &testRestPacket{
		ID:   5,
		Data: []byte{0xDE, 0xAD, 0xBE, 0xEF},
	}

	data, err := Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	decoded := &testRestPacket{}
	if err := Unmarshal(data, decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if original.ID != decoded.ID {
		t.Errorf("ID mismatch: got %d, want %d", decoded.ID, original.ID)
	}
	if string(original.Data) != string(decoded.Data) {
		t.Errorf("Data mismatch: got %x, want %x", decoded.Data, original.Data)
	}
}
