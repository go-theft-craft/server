package net

import (
	"bytes"
	"testing"
)

func TestVarIntRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		value int32
		size  int
	}{
		{"zero", 0, 1},
		{"one", 1, 1},
		{"127", 127, 1},
		{"128", 128, 2},
		{"255", 255, 2},
		{"25565", 25565, 3},
		{"max_varint", 2147483647, 5},
		{"negative_one", -1, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			n, err := WriteVarInt(&buf, tt.value)
			if err != nil {
				t.Fatalf("WriteVarInt(%d): %v", tt.value, err)
			}
			if n != tt.size {
				t.Errorf("WriteVarInt(%d) wrote %d bytes, want %d", tt.value, n, tt.size)
			}
			if VarIntSize(tt.value) != tt.size {
				t.Errorf("VarIntSize(%d) = %d, want %d", tt.value, VarIntSize(tt.value), tt.size)
			}

			got, bytesRead, err := ReadVarInt(&buf)
			if err != nil {
				t.Fatalf("ReadVarInt: %v", err)
			}
			if bytesRead != tt.size {
				t.Errorf("ReadVarInt read %d bytes, want %d", bytesRead, tt.size)
			}
			if got != tt.value {
				t.Errorf("ReadVarInt = %d, want %d", got, tt.value)
			}
		})
	}
}

func TestPutVarInt(t *testing.T) {
	var buf [5]byte
	n := PutVarInt(buf[:], 300)
	if n != 2 {
		t.Errorf("PutVarInt(300) = %d bytes, want 2", n)
	}
	// 300 = 0x12C → 0xAC 0x02
	if buf[0] != 0xAC || buf[1] != 0x02 {
		t.Errorf("PutVarInt(300) = %x %x, want AC 02", buf[0], buf[1])
	}
}

func TestPositionRoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		x, y, z int
	}{
		{"origin", 0, 0, 0},
		{"positive", 100, 64, 200},
		{"negative", -100, 0, -200},
		{"max_y", 0, 255, 0},
		{"mixed", -33554432, 0, 33554431}, // extreme 26-bit values
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := EncodePosition(tt.x, tt.y, tt.z)
			x, y, z := DecodePosition(encoded)
			if x != tt.x || y != tt.y || z != tt.z {
				t.Errorf("DecodePosition(EncodePosition(%d,%d,%d)) = (%d,%d,%d)",
					tt.x, tt.y, tt.z, x, y, z)
			}
		})
	}
}
