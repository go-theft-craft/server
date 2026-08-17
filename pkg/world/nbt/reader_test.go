package nbt

import (
	"bytes"
	"errors"
	"math"
	"reflect"
	"testing"
)

// encode runs the writer over fn and returns the bytes, which is how every
// case here builds a document the reader then has to agree with.
func encode(t *testing.T, fn func(w *Writer)) []byte {
	t.Helper()

	var buf bytes.Buffer
	w := NewWriter(&buf)
	w.BeginCompound("")
	fn(w)
	w.EndCompound()
	if w.Err() != nil {
		t.Fatalf("write: %v", w.Err())
	}

	return buf.Bytes()
}

func decode(t *testing.T, data []byte) Compound {
	t.Helper()

	c, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	return c
}

func TestEveryScalarTagRoundTrips(t *testing.T) {
	data := encode(t, func(w *Writer) {
		w.WriteTagByte("b", 7)
		w.WriteShort("s", -300)
		w.WriteInt("i", -70000)
		w.WriteLong("l", -5_000_000_000)
		w.WriteFloat("f", 1.5)
		w.WriteDouble("d", -2.25)
		w.WriteString("str", "hello")
	})

	c := decode(t, data)

	for _, tc := range []struct {
		name string
		want any
	}{
		{"b", byte(7)},
		{"s", int16(-300)},
		{"i", int32(-70000)},
		{"l", int64(-5_000_000_000)},
		{"f", float32(1.5)},
		{"d", -2.25},
		{"str", "hello"},
	} {
		if got := c[tc.name]; got != tc.want {
			t.Errorf("%s = %#v, want %#v", tc.name, got, tc.want)
		}
	}
}

func TestArrayTagsRoundTrip(t *testing.T) {
	bytesIn := []byte{0, 1, 2, 250}
	intsIn := []int32{-1, 0, 1 << 20}

	c := decode(t, encode(t, func(w *Writer) {
		w.WriteByteArray("bytes", bytesIn)
		w.WriteIntArray("ints", intsIn)
		w.WriteByteArray("empty", nil)
	}))

	got, ok := c.ByteArray("bytes")
	if !ok || !bytes.Equal(got, bytesIn) {
		t.Errorf("bytes = %v, want %v", got, bytesIn)
	}
	gotInts, ok := c.IntArray("ints")
	if !ok || !reflect.DeepEqual(gotInts, intsIn) {
		t.Errorf("ints = %v, want %v", gotInts, intsIn)
	}
	if empty, ok := c.ByteArray("empty"); !ok || len(empty) != 0 {
		t.Errorf("empty = %v, want an empty array", empty)
	}
}

func TestNestedCompoundsAndListsRoundTrip(t *testing.T) {
	c := decode(t, encode(t, func(w *Writer) {
		w.BeginCompound("level")
		w.WriteInt("x", 3)
		w.BeginCompound("inner")
		w.WriteString("name", "deep")
		w.EndCompound()
		w.EndCompound()

		w.BeginList("items", TagCompound, 2)
		for i := range 2 {
			w.BeginListCompound()
			w.WriteTagByte("slot", byte(i))
			w.EndCompound()
		}

		w.BeginList("empty", TagCompound, 0)
	}))

	level, ok := c.Compound("level")
	if !ok {
		t.Fatal("level is not a compound")
	}
	if x, _ := level.Int("x"); x != 3 {
		t.Errorf("level.x = %d, want 3", x)
	}
	inner, ok := level.Compound("inner")
	if !ok {
		t.Fatal("level.inner is not a compound")
	}
	if name, _ := inner.String("name"); name != "deep" {
		t.Errorf("level.inner.name = %q, want %q", name, "deep")
	}

	items, err := c.Compounds("items")
	if err != nil {
		t.Fatalf("Compounds: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items has %d entries, want 2", len(items))
	}
	for i, item := range items {
		if slot, _ := item.Byte("slot"); slot != byte(i) {
			t.Errorf("items[%d].slot = %d, want %d", i, slot, i)
		}
	}

	// An empty list is empty rather than an error, and so is an absent one.
	if got, err := c.Compounds("empty"); err != nil || len(got) != 0 {
		t.Errorf("empty list = %v, %v; want no entries and no error", got, err)
	}
	if got, err := c.Compounds("absent"); err != nil || got != nil {
		t.Errorf("absent list = %v, %v; want nil and no error", got, err)
	}
}

func TestATruncatedTagErrors(t *testing.T) {
	full := encode(t, func(w *Writer) {
		w.WriteIntArray("ints", []int32{1, 2, 3, 4})
	})

	for cut := 1; cut < len(full); cut++ {
		if _, err := Decode(full[:cut]); err == nil {
			t.Fatalf("a document cut to %d of %d bytes decoded without error", cut, len(full))
		}
	}
}

func TestAnUnknownTagTypeErrors(t *testing.T) {
	// A root compound holding one tag of type 99.
	data := []byte{TagCompound, 0, 0, 99, 0, 1, 'x', 0}

	if _, err := Decode(data); err == nil {
		t.Fatal("an unknown tag type decoded without error")
	}
}

func TestADeeplyNestedDocumentIsBounded(t *testing.T) {
	// maxDepth+2 nested compounds, which a recursive reader without a bound
	// would follow all the way down.
	var buf bytes.Buffer
	buf.Write([]byte{TagCompound, 0, 0})
	for range maxDepth + 2 {
		buf.Write([]byte{TagCompound, 0, 1, 'a'})
	}
	for range maxDepth + 3 {
		buf.WriteByte(TagEnd)
	}

	if _, err := Decode(buf.Bytes()); err == nil {
		t.Fatal("a document nested past the bound decoded without error")
	}
}

func TestANegativeLengthErrors(t *testing.T) {
	// A byte array claiming -1 entries.
	data := []byte{
		TagCompound, 0, 0,
		TagByteArray, 0, 1, 'a', 0xFF, 0xFF, 0xFF, 0xFF,
		TagEnd,
	}

	if _, err := Decode(data); err == nil {
		t.Fatal("a negative array length decoded without error")
	}
}

func TestTrailingBytesError(t *testing.T) {
	data := append(encode(t, func(_ *Writer) {}), 0x00)

	if _, err := Decode(data); err == nil {
		t.Fatal("trailing bytes after the root compound decoded without error")
	}
}

func TestANonCompoundRootErrors(t *testing.T) {
	if _, err := Decode([]byte{TagInt, 0, 0, 0, 0, 0, 1}); err == nil {
		t.Fatal("an int root decoded without error")
	}
	if _, err := Decode(nil); !errors.Is(err, ErrTruncated) {
		t.Fatalf("empty input gave %v, want ErrTruncated", err)
	}
}

// TestEveryWriterOutputDecodes feeds the writer's own test vectors back
// through Decode: whatever the writer has always produced, the reader reads.
func TestEveryWriterOutputDecodes(t *testing.T) {
	c := decode(t, encode(t, func(w *Writer) {
		w.WriteTagByte("byte", 1)
		w.WriteShort("short", 2)
		w.WriteInt("int", 3)
		w.WriteLong("long", 4)
		w.WriteFloat("float", math.MaxFloat32)
		w.WriteDouble("double", math.MaxFloat64)
		w.WriteByteArray("bytes", []byte{9, 8})
		w.WriteString("string", "")
		w.WriteIntArray("ints", []int32{7})
		w.BeginList("list", TagCompound, 1)
		w.BeginListCompound()
		w.WriteInt("n", 11)
		w.EndCompound()
		w.BeginCompound("compound")
		w.EndCompound()
	}))

	if len(c) != 11 {
		t.Fatalf("decoded %d tags, want 11: %v", len(c), c)
	}
	entries, err := c.Compounds("list")
	if err != nil || len(entries) != 1 {
		t.Fatalf("list = %v, %v; want one compound", entries, err)
	}
	if n, _ := entries[0].Int("n"); n != 11 {
		t.Fatalf("list[0].n = %d, want 11", n)
	}
}
