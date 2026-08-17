package gen

import (
	"testing"

	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/server/pkg/world"
)

// testRegistry is the Java 1.8 registry every generator test binds to.
func testRegistry(t *testing.T) world.StateRegistry {
	t.Helper()

	set, err := v1_8.Data()
	if err != nil {
		t.Fatalf("v1_8.Data: %v", err)
	}
	reg, err := world.NewJavaRegistry(set)
	if err != nil {
		t.Fatalf("NewJavaRegistry: %v", err)
	}

	return reg
}

// generate binds a generator and builds one column with it.
func generate(t *testing.T, reg world.StateRegistry, g Generator, cx, cz int) *world.Chunk {
	t.Helper()

	if err := g.(world.Binder).Bind(reg); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	pos := world.ChunkPos{X: cx, Z: cz}
	b := world.NewBuilder(world.Overworld18(), pos, reg.Air())
	if err := g.Generate(pos, b); err != nil {
		t.Fatalf("Generate(%d,%d): %v", cx, cz, err)
	}

	return b.Build()
}

// blockAt reads a chunk-local block.
func blockAt(c *world.Chunk, x, y, z int) world.State {
	sec := c.Sections[y>>4]
	if sec == nil {
		return 0
	}

	return sec.At(world.SectionBlockIndex(x, y&0xF, z))
}

func sectionsEqual(a, b *world.Section) bool {
	if a == nil || b == nil {
		return a == b
	}
	as, bs := a.States(), b.States()
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}

	return true
}

func TestDefaultGeneratorDeterministic(t *testing.T) {
	reg := testRegistry(t)
	c1 := generate(t, reg, NewDefaultGenerator(42), 0, 0)
	c2 := generate(t, reg, NewDefaultGenerator(42), 0, 0)

	for i, sec := range c1.Sections {
		if !sectionsEqual(sec, c2.Sections[i]) {
			t.Fatalf("section %d differs", i)
		}
	}
	if c1.Biomes != c2.Biomes {
		t.Fatal("biomes differ")
	}
}

func TestDefaultGeneratorBedrockAtY0(t *testing.T) {
	reg := testRegistry(t)
	bedrock := reg.Intern("minecraft:bedrock", nil)
	c := generate(t, reg, NewDefaultGenerator(12345), 0, 0)

	for x := range 16 {
		for z := range 16 {
			if got := blockAt(c, x, 0, z); got != bedrock {
				t.Errorf("block at (%d,0,%d) = %d, want bedrock", x, z, got)
			}
		}
	}
}

func TestDefaultGeneratorHeightReasonable(t *testing.T) {
	g := NewDefaultGenerator(999)
	h := g.HeightAt(0, 0)
	if h < 1 || h > 250 {
		t.Errorf("HeightAt(0,0) = %d, want 1..250", h)
	}
}

func TestDefaultGeneratorDifferentSeeds(t *testing.T) {
	reg := testRegistry(t)
	c1 := generate(t, reg, NewDefaultGenerator(1), 0, 0)
	c2 := generate(t, reg, NewDefaultGenerator(2), 0, 0)

	for i := range c1.Sections {
		if c1.Sections[i] == nil || c2.Sections[i] == nil {
			continue
		}
		if !sectionsEqual(c1.Sections[i], c2.Sections[i]) {
			return
		}
	}
	t.Error("different seeds should produce different terrain")
}

func TestFlatGeneratorLayers(t *testing.T) {
	reg := testRegistry(t)
	c := generate(t, reg, NewFlatGenerator(0), 0, 0)

	tests := []struct {
		y    int
		want world.State
		name string
	}{
		{0, reg.Intern("minecraft:bedrock", nil), "bedrock"},
		{1, reg.Intern("minecraft:stone", nil), "stone"},
		{2, reg.Intern("minecraft:stone", nil), "stone"},
		{3, reg.Intern("minecraft:dirt", nil), "dirt"},
		{4, reg.Intern("minecraft:grass", nil), "grass"},
		{5, reg.Air(), "air"},
	}

	for _, tt := range tests {
		if got := blockAt(c, 0, tt.y, 0); got != tt.want {
			t.Errorf("y=%d: got %d, want %d (%s)", tt.y, got, tt.want, tt.name)
		}
	}
}

func TestDefaultGeneratorMultipleChunks(t *testing.T) {
	reg := testRegistry(t)
	bedrock := reg.Intern("minecraft:bedrock", nil)
	g := NewDefaultGenerator(12345)

	for cx := -2; cx <= 2; cx++ {
		for cz := -2; cz <= 2; cz++ {
			c := generate(t, reg, g, cx, cz)
			for x := range 16 {
				if got := blockAt(c, x, 0, 0); got != bedrock {
					t.Errorf("chunk(%d,%d) block at (%d,0,0) = %d, want bedrock", cx, cz, x, got)
				}
			}
		}
	}
}
