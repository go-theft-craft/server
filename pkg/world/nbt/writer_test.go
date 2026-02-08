package nbt

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestWriteByte(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	w.WriteTagByte("test", 42)

	data := buf.Bytes()
	if data[0] != TagByte {
		t.Fatalf("expected tag type %d, got %d", TagByte, data[0])
	}
	nameLen := binary.BigEndian.Uint16(data[1:3])
	if nameLen != 4 {
		t.Fatalf("expected name length 4, got %d", nameLen)
	}
	if string(data[3:7]) != "test" {
		t.Fatalf("expected name 'test', got %q", string(data[3:7]))
	}
	if data[7] != 42 {
		t.Fatalf("expected value 42, got %d", data[7])
	}
}

func TestWriteInt(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	w.WriteInt("x", 12345)

	data := buf.Bytes()
	if data[0] != TagInt {
		t.Fatalf("expected tag type %d, got %d", TagInt, data[0])
	}
	// skip tag(1) + name_len(2) + name(1) = 4 bytes
	val := int32(binary.BigEndian.Uint32(data[4:8]))
	if val != 12345 {
		t.Fatalf("expected 12345, got %d", val)
	}
}

func TestWriteByteArray(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	w.WriteByteArray("ba", []byte{1, 2, 3})

	data := buf.Bytes()
	if data[0] != TagByteArray {
		t.Fatalf("expected tag type %d, got %d", TagByteArray, data[0])
	}
	// tag(1) + name_len(2) + name(2) = 5, then length(4) + data(3)
	arrLen := int32(binary.BigEndian.Uint32(data[5:9]))
	if arrLen != 3 {
		t.Fatalf("expected array length 3, got %d", arrLen)
	}
	if !bytes.Equal(data[9:12], []byte{1, 2, 3}) {
		t.Fatalf("expected [1,2,3], got %v", data[9:12])
	}
}

func TestWriteString(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	w.WriteString("s", "hello")

	data := buf.Bytes()
	if data[0] != TagString {
		t.Fatalf("expected tag type %d, got %d", TagString, data[0])
	}
	// tag(1) + name_len(2) + name(1) = 4, then string_len(2) + string(5)
	strLen := binary.BigEndian.Uint16(data[4:6])
	if strLen != 5 {
		t.Fatalf("expected string length 5, got %d", strLen)
	}
	if string(data[6:11]) != "hello" {
		t.Fatalf("expected 'hello', got %q", string(data[6:11]))
	}
}

func TestCompound(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	w.BeginCompound("")
	w.WriteTagByte("Y", 5)
	w.EndCompound()

	data := buf.Bytes()
	// Compound tag(1) + name_len(2, = 0) = 3
	if data[0] != TagCompound {
		t.Fatalf("expected compound tag")
	}
	// Last byte should be End tag
	if data[len(data)-1] != TagEnd {
		t.Fatalf("expected end tag at end, got %d", data[len(data)-1])
	}
}

func TestWriteIntArray(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	w.WriteIntArray("ia", []int32{100, 200})

	data := buf.Bytes()
	if data[0] != TagIntArray {
		t.Fatalf("expected tag type %d, got %d", TagIntArray, data[0])
	}
	// tag(1) + name_len(2) + name(2) = 5, then count(4) + ints(8)
	count := int32(binary.BigEndian.Uint32(data[5:9]))
	if count != 2 {
		t.Fatalf("expected count 2, got %d", count)
	}
	v0 := int32(binary.BigEndian.Uint32(data[9:13]))
	v1 := int32(binary.BigEndian.Uint32(data[13:17]))
	if v0 != 100 || v1 != 200 {
		t.Fatalf("expected [100,200], got [%d,%d]", v0, v1)
	}
}

func TestBeginList(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	w.BeginList("items", TagCompound, 2)

	data := buf.Bytes()
	if data[0] != TagList {
		t.Fatalf("expected list tag")
	}
	// tag(1) + name_len(2) + name(5) = 8, then elem_type(1) + count(4)
	elemType := data[8]
	if elemType != TagCompound {
		t.Fatalf("expected elem type %d, got %d", TagCompound, elemType)
	}
	count := int32(binary.BigEndian.Uint32(data[9:13]))
	if count != 2 {
		t.Fatalf("expected count 2, got %d", count)
	}
}

func TestWriteLong(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	w.WriteLong("L", 0x123456789ABCDEF0)

	data := buf.Bytes()
	if data[0] != TagLong {
		t.Fatalf("expected tag type %d, got %d", TagLong, data[0])
	}
	// tag(1) + name_len(2) + name(1) = 4, then long(8)
	val := int64(binary.BigEndian.Uint64(data[4:12]))
	if val != 0x123456789ABCDEF0 {
		t.Fatalf("expected 0x123456789ABCDEF0, got 0x%X", val)
	}
}

func TestNestedCompound(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	w.BeginCompound("")
	w.BeginCompound("Level")
	w.WriteInt("xPos", 3)
	w.WriteInt("zPos", 5)
	w.EndCompound()
	w.EndCompound()

	if w.Err() != nil {
		t.Fatalf("unexpected error: %v", w.Err())
	}

	data := buf.Bytes()
	// Outer: TAG_Compound("") = 10 + 00 00
	// Inner: TAG_Compound("Level") = 10 + 00 05 + "Level"
	// xPos: TAG_Int("xPos") = 03 + 00 04 + "xPos" + 00 00 00 03
	// zPos: TAG_Int("zPos") = 03 + 00 04 + "zPos" + 00 00 00 05
	// End inner: 00
	// End outer: 00
	if data[0] != TagCompound {
		t.Fatal("expected outer compound")
	}
	if data[3] != TagCompound {
		t.Fatal("expected inner compound")
	}
	// Last two bytes should be End tags
	if data[len(data)-1] != TagEnd || data[len(data)-2] != TagEnd {
		t.Fatal("expected two end tags at end")
	}
}
