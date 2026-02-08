package gen

import "testing"

func TestDefaultGeneratorDeterministic(t *testing.T) {
	g1 := NewDefaultGenerator(42)
	g2 := NewDefaultGenerator(42)

	c1 := g1.Generate(0, 0)
	c2 := g2.Generate(0, 0)

	for i, sec := range c1.Sections {
		if sec == nil && c2.Sections[i] == nil {
			continue
		}
		if (sec == nil) != (c2.Sections[i] == nil) {
			t.Fatalf("section %d nil mismatch", i)
		}
		if sec.Blocks != c2.Sections[i].Blocks {
			t.Fatalf("section %d blocks differ", i)
		}
	}
	if c1.Biomes != c2.Biomes {
		t.Fatal("biomes differ")
	}
}

func TestDefaultGeneratorBedrockAtY0(t *testing.T) {
	g := NewDefaultGenerator(12345)
	c := g.Generate(0, 0)

	for x := 0; x < 16; x++ {
		for z := 0; z < 16; z++ {
			block := c.GetBlock(x, 0, z)
			if block != blockBedrock<<4 {
				t.Errorf("block at (%d,0,%d) = %d, want %d (bedrock)", x, z, block, blockBedrock<<4)
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
	g1 := NewDefaultGenerator(1)
	g2 := NewDefaultGenerator(2)

	c1 := g1.Generate(0, 0)
	c2 := g2.Generate(0, 0)

	different := false
	for i := range c1.Sections {
		if c1.Sections[i] == nil || c2.Sections[i] == nil {
			continue
		}
		if c1.Sections[i].Blocks != c2.Sections[i].Blocks {
			different = true
			break
		}
	}
	if !different {
		t.Error("different seeds should produce different terrain")
	}
}

func TestFlatGeneratorLayers(t *testing.T) {
	g := NewFlatGenerator(0)
	c := g.Generate(0, 0)

	// y=0: bedrock, y=1-2: stone, y=3: dirt, y=4: grass
	tests := []struct {
		y     int
		block uint16
		name  string
	}{
		{0, blockBedrock << 4, "bedrock"},
		{1, blockStone << 4, "stone"},
		{2, blockStone << 4, "stone"},
		{3, blockDirt << 4, "dirt"},
		{4, blockGrass << 4, "grass"},
		{5, 0, "air"},
	}

	for _, tt := range tests {
		got := c.GetBlock(0, tt.y, 0)
		if got != tt.block {
			t.Errorf("y=%d: got %d, want %d (%s)", tt.y, got, tt.block, tt.name)
		}
	}
}

func TestDefaultGeneratorMultipleChunks(t *testing.T) {
	g := NewDefaultGenerator(12345)

	// Generate a few chunks and verify they don't panic.
	for cx := -2; cx <= 2; cx++ {
		for cz := -2; cz <= 2; cz++ {
			c := g.Generate(cx, cz)
			if c == nil {
				t.Errorf("Generate(%d,%d) returned nil", cx, cz)
			}
			// Every chunk should have bedrock at y=0.
			for x := 0; x < 16; x++ {
				block := c.GetBlock(x, 0, 0)
				if block != blockBedrock<<4 {
					t.Errorf("chunk(%d,%d) block at (%d,0,0) = %d, want bedrock", cx, cz, x, block)
				}
			}
		}
	}
}
