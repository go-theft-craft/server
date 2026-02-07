package nbt

import (
	"encoding/binary"
	"io"
	"math"
)

// NBT tag type IDs.
const (
	TagEnd       byte = 0
	TagByte      byte = 1
	TagShort     byte = 2
	TagInt       byte = 3
	TagLong      byte = 4
	TagFloat     byte = 5
	TagDouble    byte = 6
	TagByteArray byte = 7
	TagString    byte = 8
	TagList      byte = 9
	TagCompound  byte = 10
	TagIntArray  byte = 11
)

// Writer writes NBT binary data to an io.Writer in big-endian format.
// All write methods accumulate errors internally; call Err() after writing
// to check for failures.
type Writer struct {
	w   io.Writer
	err error
}

// NewWriter creates a new NBT Writer.
func NewWriter(w io.Writer) *Writer {
	return &Writer{w: w}
}

// Err returns the first error encountered during writing.
func (w *Writer) Err() error {
	return w.err
}

func (w *Writer) write(data []byte) {
	if w.err != nil {
		return
	}
	_, w.err = w.w.Write(data)
}

func (w *Writer) putByte(v byte) {
	w.write([]byte{v})
}

func (w *Writer) putUint16(v uint16) {
	var buf [2]byte
	binary.BigEndian.PutUint16(buf[:], v)
	w.write(buf[:])
}

func (w *Writer) putInt32(v int32) {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], uint32(v))
	w.write(buf[:])
}

func (w *Writer) putInt64(v int64) {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(v))
	w.write(buf[:])
}

func (w *Writer) writeTagHeader(tagType byte, name string) {
	w.putByte(tagType)
	w.putUint16(uint16(len(name)))
	if len(name) > 0 {
		w.write([]byte(name))
	}
}

// BeginCompound writes a compound tag header. Use name="" for list elements.
func (w *Writer) BeginCompound(name string) {
	w.writeTagHeader(TagCompound, name)
}

// EndCompound writes an End tag to close a compound.
func (w *Writer) EndCompound() {
	w.putByte(TagEnd)
}

// WriteTagByte writes a named byte tag.
func (w *Writer) WriteTagByte(name string, v byte) {
	w.writeTagHeader(TagByte, name)
	w.putByte(v)
}

// WriteShort writes a named short tag.
func (w *Writer) WriteShort(name string, v int16) {
	w.writeTagHeader(TagShort, name)
	w.putUint16(uint16(v))
}

// WriteInt writes a named int tag.
func (w *Writer) WriteInt(name string, v int32) {
	w.writeTagHeader(TagInt, name)
	w.putInt32(v)
}

// WriteLong writes a named long tag.
func (w *Writer) WriteLong(name string, v int64) {
	w.writeTagHeader(TagLong, name)
	w.putInt64(v)
}

// WriteFloat writes a named float tag.
func (w *Writer) WriteFloat(name string, v float32) {
	w.writeTagHeader(TagFloat, name)
	w.putInt32(int32(math.Float32bits(v)))
}

// WriteDouble writes a named double tag.
func (w *Writer) WriteDouble(name string, v float64) {
	w.writeTagHeader(TagDouble, name)
	w.putInt64(int64(math.Float64bits(v)))
}

// WriteByteArray writes a named byte array tag.
func (w *Writer) WriteByteArray(name string, v []byte) {
	w.writeTagHeader(TagByteArray, name)
	w.putInt32(int32(len(v)))
	w.write(v)
}

// WriteString writes a named string tag.
func (w *Writer) WriteString(name string, v string) {
	w.writeTagHeader(TagString, name)
	w.putUint16(uint16(len(v)))
	if len(v) > 0 {
		w.write([]byte(v))
	}
}

// WriteIntArray writes a named int array tag.
func (w *Writer) WriteIntArray(name string, v []int32) {
	w.writeTagHeader(TagIntArray, name)
	w.putInt32(int32(len(v)))
	for _, val := range v {
		w.putInt32(val)
	}
}

// BeginList writes a named list tag header.
func (w *Writer) BeginList(name string, elemType byte, count int32) {
	w.writeTagHeader(TagList, name)
	w.putByte(elemType)
	w.putInt32(count)
}
