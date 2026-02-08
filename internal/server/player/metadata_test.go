package player

import (
	"testing"

	pkt "github.com/go-theft-craft/server/pkg/gamedata/versions/pc_1_8"
	mcnet "github.com/go-theft-craft/server/pkg/protocol"
)

func TestBuildEntityMetadataDefault(t *testing.T) {
	p := newTestPlayerSimple()

	data := BuildEntityMetadata(p)

	// Expect: index 0 type byte (header=0x00), value=0x00,
	//         index 10 type byte (header=0x0A), value=0x00,
	//         terminator 0x7F
	if len(data) != 5 {
		t.Fatalf("expected 5 bytes, got %d: %X", len(data), data)
	}

	// Index 0, type 0 (byte): header = (0 & 0x1F) | (0 << 5) = 0x00
	if data[0] != 0x00 {
		t.Errorf("expected header 0x00 for index 0, got %02X", data[0])
	}
	if data[1] != 0x00 {
		t.Errorf("expected entityFlags 0x00, got %02X", data[1])
	}

	// Index 10, type 0 (byte): header = (10 & 0x1F) | (0 << 5) = 0x0A
	if data[2] != 0x0A {
		t.Errorf("expected header 0x0A for index 10, got %02X", data[2])
	}
	if data[3] != 0x00 {
		t.Errorf("expected skinParts 0x00, got %02X", data[3])
	}

	// Terminator
	if data[4] != pkt.MetadataEnd {
		t.Errorf("expected terminator 0x7F, got %02X", data[4])
	}
}

func TestBuildEntityMetadataWithFlags(t *testing.T) {
	p := newTestPlayerSimple()
	p.SetSneaking(true)
	p.SetSkinParts(0xFF)

	data := BuildEntityMetadata(p)

	if len(data) != 5 {
		t.Fatalf("expected 5 bytes, got %d", len(data))
	}

	// entityFlags should have bit 1 set (0x02 = sneaking)
	if data[1] != 0x02 {
		t.Errorf("expected entityFlags 0x02, got %02X", data[1])
	}

	// skinParts should be 0xFF
	if data[3] != 0xFF {
		t.Errorf("expected skinParts 0xFF, got %02X", data[3])
	}
}

func TestBuildEntityMetadataSprinting(t *testing.T) {
	p := newTestPlayerSimple()
	p.SetSprinting(true)

	data := BuildEntityMetadata(p)

	// entityFlags should have bit 3 set (0x08 = sprinting)
	if data[1] != 0x08 {
		t.Errorf("expected entityFlags 0x08, got %02X", data[1])
	}
}

func TestBuildSpawnMetadata(t *testing.T) {
	p := newTestPlayerSimple()
	data := BuildSpawnMetadata(p)

	// Should be identical to BuildEntityMetadata.
	expected := BuildEntityMetadata(p)
	if len(data) != len(expected) {
		t.Fatalf("spawn metadata length %d != entity metadata length %d", len(data), len(expected))
	}
	for i := range data {
		if data[i] != expected[i] {
			t.Errorf("byte %d: spawn %02X != entity %02X", i, data[i], expected[i])
		}
	}
}

func newTestPlayerSimple() *Player {
	uuid := [16]byte{0x01}
	return NewPlayer(1, "test-uuid", uuid, "testplayer", nil, func(p mcnet.Packet) error {
		return nil
	})
}
