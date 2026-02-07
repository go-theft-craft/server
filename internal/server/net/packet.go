package net

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

type Packet interface {
	PacketID() int32
}

func ReadRawPacket(r io.Reader) (packetID int32, data []byte, err error) {
	length, _, err := ReadVarInt(r)
	if err != nil {
		return 0, nil, fmt.Errorf("read packet length: %w", err)
	}
	if length < 1 {
		return 0, nil, fmt.Errorf("packet length too small: %d", length)
	}
	if length > 1<<21 { // 2MB max
		return 0, nil, fmt.Errorf("packet too large: %d bytes", length)
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, fmt.Errorf("read packet payload: %w", err)
	}

	buf := bytes.NewReader(payload)
	packetID, _, err = ReadVarInt(buf)
	if err != nil {
		return 0, nil, fmt.Errorf("read packet ID: %w", err)
	}

	remaining := make([]byte, buf.Len())
	if _, err := io.ReadFull(buf, remaining); err != nil {
		return 0, nil, fmt.Errorf("read packet data: %w", err)
	}

	return packetID, remaining, nil
}

func WriteRawPacket(w io.Writer, packetID int32, data []byte) error {
	idSize := VarIntSize(packetID)
	totalLen := idSize + len(data)

	var buf bytes.Buffer
	buf.Grow(VarIntSize(int32(totalLen)) + totalLen)

	if _, err := WriteVarInt(&buf, int32(totalLen)); err != nil {
		return fmt.Errorf("write packet length: %w", err)
	}
	if _, err := WriteVarInt(&buf, packetID); err != nil {
		return fmt.Errorf("write packet ID: %w", err)
	}
	if _, err := buf.Write(data); err != nil {
		return fmt.Errorf("write packet data: %w", err)
	}

	if _, err := w.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("flush packet: %w", err)
	}
	return nil
}

func WritePacket(w io.Writer, p Packet) error {
	data, err := Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal packet 0x%02X: %w", p.PacketID(), err)
	}
	return WriteRawPacket(w, p.PacketID(), data)
}

func ReadPacket(r io.Reader, p Packet) error {
	packetID, data, err := ReadRawPacket(r)
	if err != nil {
		return err
	}
	if packetID != p.PacketID() {
		return fmt.Errorf("expected packet 0x%02X, got 0x%02X", p.PacketID(), packetID)
	}
	return Unmarshal(data, p)
}

func WriteField(w io.Writer, tag string, val any) error {
	switch tag {
	case "varint":
		_, err := WriteVarInt(w, val.(int32))
		return err
	case "varlong":
		_, err := WriteVarLong(w, val.(int64))
		return err
	case "i8":
		return binary.Write(w, binary.BigEndian, val.(int8))
	case "u8":
		return binary.Write(w, binary.BigEndian, val.(uint8))
	case "i16":
		return binary.Write(w, binary.BigEndian, val.(int16))
	case "u16":
		return binary.Write(w, binary.BigEndian, val.(uint16))
	case "i32":
		return binary.Write(w, binary.BigEndian, val.(int32))
	case "i64":
		return binary.Write(w, binary.BigEndian, val.(int64))
	case "f32":
		return binary.Write(w, binary.BigEndian, val.(float32))
	case "f64":
		return binary.Write(w, binary.BigEndian, val.(float64))
	case "bool":
		b := val.(bool)
		if b {
			return binary.Write(w, binary.BigEndian, uint8(1))
		}
		return binary.Write(w, binary.BigEndian, uint8(0))
	case "string":
		_, err := WriteString(w, val.(string))
		return err
	case "position":
		return binary.Write(w, binary.BigEndian, val.(int64))
	case "uuid":
		_, err := WriteUUID(w, val.([16]byte))
		return err
	case "bytearray":
		_, err := WriteByteArray(w, val.([]byte))
		return err
	case "rest":
		_, err := w.Write(val.([]byte))
		return err
	default:
		return fmt.Errorf("unknown field tag: %q", tag)
	}
}

func ReadField(r io.Reader, tag string) (any, error) {
	switch tag {
	case "varint":
		v, _, err := ReadVarInt(r)
		return v, err
	case "varlong":
		v, _, err := ReadVarLong(r)
		return v, err
	case "i8":
		return ReadI8(r)
	case "u8":
		return ReadU8(r)
	case "i16":
		return ReadI16(r)
	case "u16":
		return ReadU16(r)
	case "i32":
		return ReadI32(r)
	case "i64":
		return ReadI64(r)
	case "f32":
		return ReadF32(r)
	case "f64":
		return ReadF64(r)
	case "bool":
		return ReadBool(r)
	case "string":
		return ReadString(r)
	case "position":
		return ReadI64(r)
	case "uuid":
		return ReadUUID(r)
	case "bytearray":
		return ReadByteArray(r)
	case "rest":
		return io.ReadAll(r)
	default:
		return nil, fmt.Errorf("unknown field tag: %q", tag)
	}
}
