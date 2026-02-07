package net

import (
	"encoding/binary"
	"fmt"
	"io"
)

func ReadVarInt(r io.Reader) (int32, int, error) {
	var result uint32
	var numRead int
	buf := make([]byte, 1)

	for {
		if _, err := io.ReadFull(r, buf); err != nil {
			return 0, numRead, err
		}
		numRead++

		result |= uint32(buf[0]&0x7F) << (7 * (numRead - 1))

		if buf[0]&0x80 == 0 {
			break
		}

		if numRead >= 5 {
			return 0, numRead, fmt.Errorf("VarInt too long")
		}
	}

	return int32(result), numRead, nil
}

func WriteVarInt(w io.Writer, value int32) (int, error) {
	var buf [5]byte
	n := PutVarInt(buf[:], value)
	return w.Write(buf[:n])
}

func PutVarInt(buf []byte, value int32) int {
	val := uint32(value)
	n := 0
	for {
		b := byte(val & 0x7F)
		val >>= 7
		if val != 0 {
			b |= 0x80
		}
		buf[n] = b
		n++
		if val == 0 {
			break
		}
	}
	return n
}

func VarIntSize(value int32) int {
	val := uint32(value)
	size := 0
	for {
		size++
		val >>= 7
		if val == 0 {
			break
		}
	}
	return size
}

func ReadVarLong(r io.Reader) (int64, int, error) {
	var result uint64
	var numRead int
	buf := make([]byte, 1)

	for {
		if _, err := io.ReadFull(r, buf); err != nil {
			return 0, numRead, err
		}
		numRead++

		result |= uint64(buf[0]&0x7F) << (7 * (numRead - 1))

		if buf[0]&0x80 == 0 {
			break
		}

		if numRead >= 10 {
			return 0, numRead, fmt.Errorf("VarLong too long")
		}
	}

	return int64(result), numRead, nil
}

func WriteVarLong(w io.Writer, value int64) (int, error) {
	var buf [10]byte
	val := uint64(value)
	n := 0
	for {
		b := byte(val & 0x7F)
		val >>= 7
		if val != 0 {
			b |= 0x80
		}
		buf[n] = b
		n++
		if val == 0 {
			break
		}
	}
	return w.Write(buf[:n])
}

func EncodePosition(x, y, z int) int64 {
	return int64((int64(x)&0x3FFFFFF)<<38) | int64((int64(y)&0xFFF)<<26) | int64(int64(z)&0x3FFFFFF)
}

func DecodePosition(val int64) (x, y, z int) {
	x = int(val >> 38)
	y = int((val >> 26) & 0xFFF)
	z = int(val & 0x3FFFFFF)

	if x >= 1<<25 {
		x -= 1 << 26
	}
	if y >= 1<<11 {
		y -= 1 << 12
	}
	if z >= 1<<25 {
		z -= 1 << 26
	}
	return
}

func ReadString(r io.Reader) (string, error) {
	length, _, err := ReadVarInt(r)
	if err != nil {
		return "", fmt.Errorf("read string length: %w", err)
	}
	if length < 0 || length > 32767*4 {
		return "", fmt.Errorf("string length out of range: %d", length)
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", fmt.Errorf("read string data: %w", err)
	}
	return string(buf), nil
}

func WriteString(w io.Writer, s string) (int, error) {
	n1, err := WriteVarInt(w, int32(len(s)))
	if err != nil {
		return n1, err
	}
	n2, err := w.Write([]byte(s))
	return n1 + n2, err
}

func ReadByteArray(r io.Reader) ([]byte, error) {
	length, _, err := ReadVarInt(r)
	if err != nil {
		return nil, fmt.Errorf("read byte array length: %w", err)
	}
	if length < 0 {
		return nil, fmt.Errorf("negative byte array length: %d", length)
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, fmt.Errorf("read byte array data: %w", err)
	}
	return buf, nil
}

func WriteByteArray(w io.Writer, data []byte) (int, error) {
	n1, err := WriteVarInt(w, int32(len(data)))
	if err != nil {
		return n1, err
	}
	n2, err := w.Write(data)
	return n1 + n2, err
}

func ReadUUID(r io.Reader) ([16]byte, error) {
	var uuid [16]byte
	if _, err := io.ReadFull(r, uuid[:]); err != nil {
		return uuid, err
	}
	return uuid, nil
}

func WriteUUID(w io.Writer, uuid [16]byte) (int, error) {
	return w.Write(uuid[:])
}

func ReadI8(r io.Reader) (int8, error) {
	var buf [1]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}
	return int8(buf[0]), nil
}

func ReadU8(r io.Reader) (uint8, error) {
	var buf [1]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}
	return buf[0], nil
}

func ReadI16(r io.Reader) (int16, error) {
	var val int16
	if err := binary.Read(r, binary.BigEndian, &val); err != nil {
		return 0, err
	}
	return val, nil
}

func ReadU16(r io.Reader) (uint16, error) {
	var val uint16
	if err := binary.Read(r, binary.BigEndian, &val); err != nil {
		return 0, err
	}
	return val, nil
}

func ReadI32(r io.Reader) (int32, error) {
	var val int32
	if err := binary.Read(r, binary.BigEndian, &val); err != nil {
		return 0, err
	}
	return val, nil
}

func ReadI64(r io.Reader) (int64, error) {
	var val int64
	if err := binary.Read(r, binary.BigEndian, &val); err != nil {
		return 0, err
	}
	return val, nil
}

func ReadF32(r io.Reader) (float32, error) {
	var val float32
	if err := binary.Read(r, binary.BigEndian, &val); err != nil {
		return 0, err
	}
	return val, nil
}

func ReadF64(r io.Reader) (float64, error) {
	var val float64
	if err := binary.Read(r, binary.BigEndian, &val); err != nil {
		return 0, err
	}
	return val, nil
}

func ReadBool(r io.Reader) (bool, error) {
	b, err := ReadU8(r)
	return b != 0, err
}
