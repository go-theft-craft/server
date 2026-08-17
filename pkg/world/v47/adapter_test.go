package v47

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/server/pkg/world"
	"github.com/go-theft-craft/server/pkg/world/gen"
)

func newAdapter(t testing.TB) *Adapter {
	t.Helper()

	set, err := v1_8.Data()
	if err != nil {
		t.Fatalf("v1_8.Data: %v", err)
	}
	reg, err := world.NewJavaRegistry(set)
	if err != nil {
		t.Fatalf("NewJavaRegistry: %v", err)
	}
	a, err := New(reg, set)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return a
}

// chunkFromGenerator binds a generator to the adapter's registry and builds
// one column with it.
func chunkFromGenerator(t *testing.T, a *Adapter, g gen.Generator, pos world.ChunkPos) *world.Chunk {
	t.Helper()

	binder, ok := g.(world.Binder)
	if !ok {
		t.Fatalf("%T does not bind to a registry", g)
	}
	if err := binder.Bind(a.Registry()); err != nil {
		t.Fatalf("Bind: %v", err)
	}

	b := world.NewBuilder(a.Dimension(), pos, a.Registry().Air())
	if err := g.Generate(pos, b); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	return b.Build()
}

func readFixture(t *testing.T, name string) (uint16, []byte) {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	return binary.LittleEndian.Uint16(raw), raw[2:]
}

func assertMatchesFixture(t *testing.T, name string, p world.Packet) {
	t.Helper()

	chunk, ok := p.(*v1_8.PlayClientboundMapChunk)
	if !ok {
		t.Fatalf("adapter returned %T, want *v1_8.PlayClientboundMapChunk", p)
	}
	wantBitMap, wantData := readFixture(t, name)
	if chunk.BitMap != wantBitMap {
		t.Fatalf("%s: bitmap %016b, want %016b", name, chunk.BitMap, wantBitMap)
	}
	if len(chunk.ChunkData) != len(wantData) {
		t.Fatalf("%s: %d data bytes, want %d", name, len(chunk.ChunkData), len(wantData))
	}
	for i := range wantData {
		if chunk.ChunkData[i] != wantData[i] {
			t.Fatalf("%s: byte %d is 0x%02X, want 0x%02X", name, i, chunk.ChunkData[i], wantData[i])
		}
	}
}

// TestAChunkEncodesToTheSameBytesAsBeforeM112 is the check that matters: the
// fixtures were captured from the encoder this adapter replaces, and a byte
// that moves is a bug in the adapter, not a new baseline.
func TestAChunkEncodesToTheSameBytesAsBeforeM112(t *testing.T) {
	a := newAdapter(t)

	for _, tc := range []struct {
		fixture string
		g       gen.Generator
		pos     world.ChunkPos
	}{
		{"flat_0_0.bin", gen.NewFlatGenerator(0), world.ChunkPos{}},
		{"default_7_-3.bin", gen.NewDefaultGenerator(12345), world.ChunkPos{X: 7, Z: -3}},
	} {
		c := chunkFromGenerator(t, a, tc.g, tc.pos)
		p, err := a.EncodeChunk(c)
		if err != nil {
			t.Fatalf("EncodeChunk: %v", err)
		}
		assertMatchesFixture(t, tc.fixture, p)
	}
}

// TestASectionAPlayerBuiltInIsPresentInTheBitmap is the regression 0a7fc68
// fixed, re-expressed against the model: a block placed above the terrain used
// to vanish from every later chunk send.
func TestASectionAPlayerBuiltInIsPresentInTheBitmap(t *testing.T) {
	a := newAdapter(t)
	c := chunkFromGenerator(t, a, gen.NewFlatGenerator(0), world.ChunkPos{})

	chest := a.Registry().Intern("minecraft:chest", nil)
	const x, y, z = 5, 130, 5
	index := a.Dimension().SectionIndex(y)
	c.Sections[index] = c.Sections[index].With(world.SectionBlockIndex(x, y&0xF, z), chest)

	p, err := a.EncodeChunk(c)
	if err != nil {
		t.Fatalf("EncodeChunk: %v", err)
	}
	assertMatchesFixture(t, "flat_placed_high.bin", p)

	chunk := p.(*v1_8.PlayClientboundMapChunk)
	if chunk.BitMap&(1<<uint(index)) == 0 {
		t.Fatalf("section %d is absent from the bitmap %016b", index, chunk.BitMap)
	}
}

func TestTheUnloadPacketIsBitmapZeroAndEmptyData(t *testing.T) {
	a := newAdapter(t)

	p, err := a.EncodeUnload(world.ChunkPos{X: 3, Z: -2})
	if err != nil {
		t.Fatalf("EncodeUnload: %v", err)
	}
	chunk, ok := p.(*v1_8.PlayClientboundMapChunk)
	if !ok {
		t.Fatalf("EncodeUnload returned %T", p)
	}
	if chunk.X != 3 || chunk.Z != -2 {
		t.Fatalf("coords (%d,%d), want (3,-2)", chunk.X, chunk.Z)
	}
	if !chunk.GroundUp || chunk.BitMap != 0 || len(chunk.ChunkData) != 0 {
		t.Fatalf("unload packet is GroundUp=%v BitMap=%d len(data)=%d",
			chunk.GroundUp, chunk.BitMap, len(chunk.ChunkData))
	}
}

func TestEncodingIsMemoizedPerSectionPointer(t *testing.T) {
	a := newAdapter(t)
	c := chunkFromGenerator(t, a, gen.NewFlatGenerator(0), world.ChunkPos{})

	if _, err := a.EncodeChunk(c); err != nil {
		t.Fatalf("EncodeChunk: %v", err)
	}
	first := a.cache.len()
	if first == 0 {
		t.Fatal("encoding a chunk cached nothing")
	}

	if _, err := a.EncodeChunk(c); err != nil {
		t.Fatalf("EncodeChunk: %v", err)
	}
	if got := a.cache.len(); got != first {
		t.Fatalf("re-encoding the same chunk grew the cache from %d to %d", first, got)
	}
}

func TestAWriteInvalidatesExactlyOneSectionEntry(t *testing.T) {
	a := newAdapter(t)
	c := chunkFromGenerator(t, a, gen.NewDefaultGenerator(12345), world.ChunkPos{X: 7, Z: -3})

	if _, err := a.EncodeChunk(c); err != nil {
		t.Fatalf("EncodeChunk: %v", err)
	}
	before := a.cache.len()

	// One write produces one new section pointer, so the next encode adds one
	// entry and reuses every other section's bytes.
	stone := a.Registry().Intern("minecraft:stone", nil)
	next := &world.Chunk{Pos: c.Pos, Biomes: c.Biomes, Sections: append([]*world.Section(nil), c.Sections...)}
	next.Sections[0] = next.Sections[0].With(0, stone)

	if _, err := a.EncodeChunk(next); err != nil {
		t.Fatalf("EncodeChunk: %v", err)
	}
	if got := a.cache.len(); got != before+1 {
		t.Fatalf("cache grew from %d to %d after one write, want %d", before, got, before+1)
	}
}

func TestStatesRoundTripThroughTheWire(t *testing.T) {
	a := newAdapter(t)

	for _, name := range []string{"minecraft:stone", "minecraft:grass", "minecraft:chest", "minecraft:air"} {
		s := a.Registry().Intern(name, nil)
		v, err := a.EncodeState(s)
		if err != nil {
			t.Fatalf("EncodeState(%s): %v", name, err)
		}
		back, err := a.DecodeState(v)
		if err != nil {
			t.Fatalf("DecodeState(%d): %v", v, err)
		}
		if back != s {
			t.Fatalf("%s round-tripped to a different handle", name)
		}
	}

	// Metadata variants keep their nibble.
	spruce := a.Registry().Intern("minecraft:log", world.Properties{{Key: "metadata", Value: "1"}})
	v, err := a.EncodeState(spruce)
	if err != nil {
		t.Fatalf("EncodeState: %v", err)
	}
	if v != 17<<4|1 {
		t.Fatalf("spruce log encodes to %d, want %d", v, 17<<4|1)
	}
}
