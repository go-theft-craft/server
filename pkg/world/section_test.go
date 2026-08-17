package world

import "testing"

// sectionSink keeps an allocation assertion's result reachable.
var sectionSink *Section

func TestWithReturnsACopyAndLeavesTheReceiverAlone(t *testing.T) {
	base := new(Section)
	base.states[7] = 3

	next := base.With(7, 9)

	if got := base.At(7); got != 3 {
		t.Fatalf("the receiver changed: At(7) = %d, want 3", got)
	}
	if got := next.At(7); got != 9 {
		t.Fatalf("the copy did not take the write: At(7) = %d, want 9", got)
	}
	if base == next {
		t.Fatal("With returned the receiver")
	}
}

// TestWithManyCopiesOnce is the property that keeps a MultiBlockChange
// affordable: a multi-block change to one section allocates one section, not
// one per block.
func TestWithManyCopiesOnce(t *testing.T) {
	base := new(Section)
	changes := make([]Change, 64)
	for i := range changes {
		changes[i] = Change{Index: i * 13, State: State(i + 1)}
	}

	// The result has to escape, or the compiler proves it dead and the count
	// measures nothing.
	allocs := testing.AllocsPerRun(20, func() {
		sectionSink = base.WithMany(changes)
	})
	if allocs != 1 {
		t.Fatalf("WithMany allocated %v times for %d changes, want 1", allocs, len(changes))
	}

	next := base.WithMany(changes)
	for _, c := range changes {
		if got := next.At(c.Index); got != c.State {
			t.Fatalf("At(%d) = %d, want %d", c.Index, got, c.State)
		}
		if got := base.At(c.Index); got != 0 {
			t.Fatalf("the receiver changed at %d", c.Index)
		}
	}
}

func TestIsAirIgnoresANilSection(t *testing.T) {
	var s *Section
	if !s.IsAir(0) {
		t.Fatal("a nil section is not air")
	}

	filled := s.With(0, 5)
	if filled.IsAir(0) {
		t.Fatal("a section with a block in it reports as air")
	}
}

func TestABuilderOutsideTheDimensionErrors(t *testing.T) {
	dim := Overworld18()
	b := NewBuilder(dim, ChunkPos{}, 0)

	for _, tc := range []struct{ x, y, z int }{
		{0, -1, 0},
		{0, 256, 0},
		{16, 0, 0},
		{0, 0, -1},
	} {
		if err := b.Set(tc.x, tc.y, tc.z, 1); err == nil {
			t.Fatalf("Set(%d,%d,%d) outside the chunk did not report", tc.x, tc.y, tc.z)
		}
	}
}

func TestBuildProducesSectionsThatShareNothingWithTheBuilder(t *testing.T) {
	dim := Overworld18()
	b := NewBuilder(dim, ChunkPos{X: 1, Z: 2}, 0)
	if err := b.Set(3, 70, 4, 11); err != nil {
		t.Fatalf("Set: %v", err)
	}
	b.SetBiome(3, 4, 5)

	c := b.Build()

	if c.Pos != (ChunkPos{X: 1, Z: 2}) {
		t.Fatalf("Pos = %v, want {1 2}", c.Pos)
	}
	if got := c.At(dim, 3, 70, 4, 0); got != 11 {
		t.Fatalf("At(3,70,4) = %d, want 11", got)
	}
	if c.Biomes[4*16+3] != 5 {
		t.Fatalf("Biomes = %d, want 5", c.Biomes[4*16+3])
	}
	if len(c.Sections) != dim.Sections() {
		t.Fatalf("len(Sections) = %d, want %d", len(c.Sections), dim.Sections())
	}

	// A builder reused after Build must fail rather than mutate the chunk.
	if err := b.Set(3, 70, 4, 12); err == nil {
		t.Fatal("Set on a built builder did not report")
	}
	if got := c.At(dim, 3, 70, 4, 0); got != 11 {
		t.Fatalf("the published chunk changed under a reused builder: %d", got)
	}
}

func TestBuilderSkipsAirInAnEmptySection(t *testing.T) {
	dim := Overworld18()
	b := NewBuilder(dim, ChunkPos{}, 0)
	if err := b.Set(0, 200, 0, 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if c := b.Build(); c.Sections[dim.SectionIndex(200)] != nil {
		t.Fatal("writing air into an empty section allocated it")
	}
}

func TestDimensionBounds(t *testing.T) {
	dim := Overworld18()

	if dim.Sections() != 16 {
		t.Fatalf("Sections() = %d, want 16", dim.Sections())
	}
	if !dim.Contains(0) || !dim.Contains(255) {
		t.Fatal("the overworld does not contain its own bounds")
	}
	if dim.Contains(-1) || dim.Contains(256) {
		t.Fatal("the overworld contains a y outside it")
	}
	if got := dim.SectionIndex(70); got != 4 {
		t.Fatalf("SectionIndex(70) = %d, want 4", got)
	}

	modern := Dimension{Name: "minecraft:overworld", MinY: -64, Height: 384}
	if got := modern.SectionIndex(-64); got != 0 {
		t.Fatalf("SectionIndex(-64) = %d, want 0", got)
	}
	if got := modern.Sections(); got != 24 {
		t.Fatalf("Sections() = %d, want 24", got)
	}
}
