package nbt

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// maxDepth bounds how deeply a document may nest. An NBT document is
// attacker-controlled the moment someone hands the server a world, and an
// unbounded recursion is a stack overflow. 512 is three orders of magnitude
// past anything vanilla writes.
const maxDepth = 512

// ErrTruncated reports a payload that ended inside a tag.
var ErrTruncated = errors.New("nbt: truncated document")

// Compound is a decoded compound tag: names to values.
//
// A value is one of byte, int16, int32, int64, float32, float64, []byte,
// string, []int32, List, or Compound — the Go types the writer's methods take,
// so a round trip through Decode gives back what was written.
type Compound map[string]any

// List is a decoded list tag. Elem is the tag type its entries carry, which a
// caller needs when the list is empty and there is nothing to type-assert.
type List struct {
	Elem  byte
	Items []any
}

// Decode reads one root compound.
//
// It is strict: an unknown tag type, a truncated payload, or a list whose
// element type disagrees with its entries is an error rather than a best
// guess, because a reader that guesses turns a corrupt world into a subtly
// wrong one.
func Decode(data []byte) (Compound, error) {
	r := &reader{data: data}

	tag, err := r.tagType()
	if err != nil {
		return nil, err
	}
	if tag != TagCompound {
		return nil, fmt.Errorf("nbt: root tag is %d, want a compound", tag)
	}
	if _, err := r.string(); err != nil {
		return nil, fmt.Errorf("nbt: root name: %w", err)
	}

	root, err := r.compound(0)
	if err != nil {
		return nil, err
	}
	if r.pos != len(r.data) {
		return nil, fmt.Errorf("nbt: %d bytes after the root compound", len(r.data)-r.pos)
	}

	return root, nil
}

type reader struct {
	data []byte
	pos  int
}

func (r *reader) take(n int) ([]byte, error) {
	if n < 0 || len(r.data)-r.pos < n {
		return nil, ErrTruncated
	}
	b := r.data[r.pos : r.pos+n]
	r.pos += n

	return b, nil
}

func (r *reader) tagType() (byte, error) {
	b, err := r.take(1)
	if err != nil {
		return 0, err
	}

	return b[0], nil
}

func (r *reader) int16() (int16, error) {
	b, err := r.take(2)
	if err != nil {
		return 0, err
	}

	return int16(binary.BigEndian.Uint16(b)), nil
}

func (r *reader) int32() (int32, error) {
	b, err := r.take(4)
	if err != nil {
		return 0, err
	}

	return int32(binary.BigEndian.Uint32(b)), nil
}

func (r *reader) int64() (int64, error) {
	b, err := r.take(8)
	if err != nil {
		return 0, err
	}

	return int64(binary.BigEndian.Uint64(b)), nil
}

func (r *reader) string() (string, error) {
	n, err := r.take(2)
	if err != nil {
		return "", err
	}
	b, err := r.take(int(binary.BigEndian.Uint16(n)))
	if err != nil {
		return "", err
	}

	return string(b), nil
}

// length reads a signed 32-bit count and refuses a negative one, which is how
// a malformed file asks for an enormous allocation.
func (r *reader) length(what string) (int, error) {
	n, err := r.int32()
	if err != nil {
		return 0, err
	}
	if n < 0 {
		return 0, fmt.Errorf("nbt: %s has length %d", what, n)
	}
	// A length is only believable if the bytes for it are actually present,
	// which is what keeps a corrupt header from allocating gigabytes.
	if int(n) > len(r.data)-r.pos {
		return 0, fmt.Errorf("nbt: %s claims %d entries with %d bytes left: %w",
			what, n, len(r.data)-r.pos, ErrTruncated)
	}

	return int(n), nil
}

func (r *reader) compound(depth int) (Compound, error) {
	if depth >= maxDepth {
		return nil, fmt.Errorf("nbt: nesting deeper than %d levels", maxDepth)
	}

	out := Compound{}
	for {
		tag, err := r.tagType()
		if err != nil {
			return nil, err
		}
		if tag == TagEnd {
			return out, nil
		}
		name, err := r.string()
		if err != nil {
			return nil, err
		}
		value, err := r.value(tag, depth+1)
		if err != nil {
			return nil, fmt.Errorf("nbt: %q: %w", name, err)
		}
		out[name] = value
	}
}

func (r *reader) value(tag byte, depth int) (any, error) {
	if depth >= maxDepth {
		return nil, fmt.Errorf("nbt: nesting deeper than %d levels", maxDepth)
	}

	switch tag {
	case TagByte:
		b, err := r.take(1)
		if err != nil {
			return nil, err
		}

		return b[0], nil

	case TagShort:
		return r.int16()

	case TagInt:
		return r.int32()

	case TagLong:
		return r.int64()

	case TagFloat:
		v, err := r.int32()
		if err != nil {
			return nil, err
		}

		return math.Float32frombits(uint32(v)), nil

	case TagDouble:
		v, err := r.int64()
		if err != nil {
			return nil, err
		}

		return math.Float64frombits(uint64(v)), nil

	case TagByteArray:
		n, err := r.length("byte array")
		if err != nil {
			return nil, err
		}
		b, err := r.take(n)
		if err != nil {
			return nil, err
		}

		// Copied, so a decoded document does not alias the caller's buffer.
		return append([]byte(nil), b...), nil

	case TagString:
		return r.string()

	case TagList:
		return r.list(depth)

	case TagCompound:
		return r.compound(depth)

	case TagIntArray:
		n, err := r.length("int array")
		if err != nil {
			return nil, err
		}
		out := make([]int32, n)
		for i := range out {
			v, err := r.int32()
			if err != nil {
				return nil, err
			}
			out[i] = v
		}

		return out, nil

	default:
		return nil, fmt.Errorf("nbt: unknown tag type %d", tag)
	}
}

func (r *reader) list(depth int) (List, error) {
	elem, err := r.tagType()
	if err != nil {
		return List{}, err
	}
	n, err := r.length("list")
	if err != nil {
		return List{}, err
	}
	if elem == TagEnd && n > 0 {
		return List{}, fmt.Errorf("nbt: list of end tags with %d entries", n)
	}

	out := List{Elem: elem, Items: make([]any, n)}
	for i := range out.Items {
		v, err := r.value(elem, depth+1)
		if err != nil {
			return List{}, fmt.Errorf("nbt: list entry %d: %w", i, err)
		}
		out.Items[i] = v
	}

	return out, nil
}

// The accessors below are what a decoder of a known format uses: each reports
// false when the tag is absent or is not the type asked for, so a caller
// distinguishes "vanilla omits this" from "this file is wrong".

// Byte reads a byte tag.
func (c Compound) Byte(name string) (byte, bool) {
	v, ok := c[name].(byte)

	return v, ok
}

// Short reads a short tag.
func (c Compound) Short(name string) (int16, bool) {
	v, ok := c[name].(int16)

	return v, ok
}

// Int reads an int tag.
func (c Compound) Int(name string) (int32, bool) {
	v, ok := c[name].(int32)

	return v, ok
}

// Long reads a long tag.
func (c Compound) Long(name string) (int64, bool) {
	v, ok := c[name].(int64)

	return v, ok
}

// String reads a string tag.
func (c Compound) String(name string) (string, bool) {
	v, ok := c[name].(string)

	return v, ok
}

// ByteArray reads a byte array tag.
func (c Compound) ByteArray(name string) ([]byte, bool) {
	v, ok := c[name].([]byte)

	return v, ok
}

// IntArray reads an int array tag.
func (c Compound) IntArray(name string) ([]int32, bool) {
	v, ok := c[name].([]int32)

	return v, ok
}

// List reads a list tag.
func (c Compound) List(name string) (List, bool) {
	v, ok := c[name].(List)

	return v, ok
}

// Compound reads a nested compound tag.
func (c Compound) Compound(name string) (Compound, bool) {
	v, ok := c[name].(Compound)

	return v, ok
}

// Compounds reads a list of compounds, which is the shape Sections,
// TileEntities, and Items all take. An absent list is empty rather than an
// error: vanilla omits a list it has nothing for.
func (c Compound) Compounds(name string) ([]Compound, error) {
	list, ok := c.List(name)
	if !ok {
		if _, present := c[name]; present {
			return nil, fmt.Errorf("nbt: %q is not a list", name)
		}

		return nil, nil
	}
	if len(list.Items) == 0 {
		return nil, nil
	}
	if list.Elem != TagCompound {
		return nil, fmt.Errorf("nbt: %q is a list of tag %d, want compounds", name, list.Elem)
	}

	out := make([]Compound, len(list.Items))
	for i, item := range list.Items {
		v, ok := item.(Compound)
		if !ok {
			return nil, fmt.Errorf("nbt: %q entry %d is %T, want a compound", name, i, item)
		}
		out[i] = v
	}

	return out, nil
}
