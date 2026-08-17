package world

import (
	"errors"
	"testing"
)

// The world's own tests cannot use pkg/world/gen or pkg/world/v47: both import
// this package. They use a registry built by hand, a generator that lays four
// flat layers, and an adapter whose encoding is the handle itself.

type stubAdapter struct {
	reg StateRegistry
	dim Dimension
}

func (a stubAdapter) Registry() StateRegistry { return a.reg }
func (a stubAdapter) Dimension() Dimension    { return a.dim }

func (a stubAdapter) EncodeChunk(*Chunk) (Packet, error) {
	return nil, errors.New("stub adapter does not encode chunks")
}

func (a stubAdapter) EncodeUnload(ChunkPos) (Packet, error) {
	return nil, errors.New("stub adapter does not encode chunks")
}

func (a stubAdapter) EncodeState(s State) (int32, error) { return int32(s), nil }

func (a stubAdapter) DecodeState(v int32) (State, error) {
	if v < 0 || int(v) >= a.reg.Len() {
		return 0, errors.New("unknown state")
	}

	return State(v), nil
}

// flatStub lays bedrock, two stone, dirt, and grass, like the flat generator.
type flatStub struct {
	bedrock, stone, dirt, grass State
	fail                        bool
}

func (g *flatStub) Bind(reg StateRegistry) error {
	g.bedrock = reg.Intern("minecraft:bedrock", nil)
	g.stone = reg.Intern("minecraft:stone", nil)
	g.dirt = reg.Intern("minecraft:dirt", nil)
	g.grass = reg.Intern("minecraft:grass", nil)

	return nil
}

func (g *flatStub) Generate(_ ChunkPos, into *Builder) error {
	if g.fail {
		return errors.New("stub generator refuses")
	}
	for x := range 16 {
		for z := range 16 {
			for _, layer := range []struct {
				y int
				s State
			}{{0, g.bedrock}, {1, g.stone}, {2, g.stone}, {3, g.dirt}, {4, g.grass}} {
				if err := into.Set(x, layer.y, z, layer.s); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (g *flatStub) HeightAt(_, _ int) int { return 4 }

func newTestWorld(t *testing.T) (*World, *flatStub, StateRegistry) {
	t.Helper()

	reg := buildRegistry(t, []string{"air", "bedrock", "stone", "dirt", "grass", "cobblestone"})
	gen := &flatStub{}
	w, err := NewWorld(Overworld18(), stubAdapter{reg: reg, dim: Overworld18()}, gen)
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}

	return w, gen, reg
}

func TestWorldBaseStateFlatGenerator(t *testing.T) {
	w, g, reg := newTestWorld(t)

	for _, tc := range []struct {
		pos  BlockPos
		want State
		name string
	}{
		{BlockPos{0, 0, 0}, g.bedrock, "bedrock"},
		{BlockPos{0, 1, 0}, g.stone, "stone"},
		{BlockPos{0, 4, 0}, g.grass, "grass"},
		{BlockPos{5, 64, 10}, reg.Air(), "air"},
	} {
		if got := w.Block(tc.pos); got != tc.want {
			t.Errorf("Block(%v) = %d, want %d (%s)", tc.pos, got, tc.want, tc.name)
		}
	}
}

func TestWorldSetBlock(t *testing.T) {
	w, g, reg := newTestWorld(t)
	cobble := reg.Intern("minecraft:cobblestone", nil)

	if !w.SetBlock(BlockPos{3, 10, 5}, cobble) {
		t.Fatal("placing a block in air reported no change")
	}
	if got := w.Block(BlockPos{3, 10, 5}); got != cobble {
		t.Errorf("Block(3,10,5) = %d, want %d", got, cobble)
	}

	if !w.SetBlock(BlockPos{0, 4, 0}, reg.Air()) {
		t.Fatal("breaking grass reported no change")
	}
	if got := w.Block(BlockPos{0, 4, 0}); got != reg.Air() {
		t.Errorf("Block(0,4,0) after break = %d, want air", got)
	}

	if !w.SetBlock(BlockPos{0, 4, 0}, g.grass) {
		t.Fatal("restoring grass reported no change")
	}
	if got := w.Block(BlockPos{0, 4, 0}); got != g.grass {
		t.Errorf("Block(0,4,0) after restore = %d, want %d", got, g.grass)
	}
}

func TestWorldSetBlockReportsANoOp(t *testing.T) {
	w, _, reg := newTestWorld(t)

	// Air over air changes nothing, so no column is allocated and no encode
	// cache entry is invalidated.
	if w.SetBlock(BlockPos{0, 10, 0}, reg.Air()) {
		t.Error("setting air inside the generated section reported a change")
	}
	if c := w.Chunk(ChunkPos{}); c.Sections[0] == nil {
		t.Fatal("the generated section vanished")
	}

	const emptySection = 100 >> 4
	if w.SetBlock(BlockPos{0, 100, 0}, reg.Air()) {
		t.Error("setting air in an empty section reported a change")
	}
	if c := w.Chunk(ChunkPos{}); c.Sections[emptySection] != nil {
		t.Error("a no-op write allocated an empty section")
	}
}

func TestWorldRejectsAYOutsideTheDimension(t *testing.T) {
	w, _, reg := newTestWorld(t)
	stone := reg.Intern("minecraft:stone", nil)

	for _, y := range []int{-1, 256, 1000} {
		if w.SetBlock(BlockPos{0, y, 0}, stone) {
			t.Errorf("SetBlock at y=%d reported a change", y)
		}
		if got := w.Block(BlockPos{0, y, 0}); got != reg.Air() {
			t.Errorf("Block at y=%d = %d, want air", y, got)
		}
	}
}

func TestWorldSpawnHeight(t *testing.T) {
	w, _, _ := newTestWorld(t)

	if got := w.SpawnHeight(); got != 5 {
		t.Errorf("SpawnHeight() = %d, want 5", got)
	}
}

func TestPreGenerateRadius(t *testing.T) {
	w, _, _ := newTestWorld(t)

	if count := w.PreGenerateRadius(2); count != 25 {
		t.Errorf("PreGenerateRadius(2) returned %d, want 25", count)
	}

	resident := w.Snapshot()
	if len(resident.Chunks) != 25 {
		t.Fatalf("%d chunks resident, want 25", len(resident.Chunks))
	}
	for cx := -2; cx <= 2; cx++ {
		for cz := -2; cz <= 2; cz++ {
			if _, ok := resident.Chunks[ChunkPos{X: cx, Z: cz}]; !ok {
				t.Errorf("chunk (%d,%d) not pre-generated", cx, cz)
			}
		}
	}
}

func TestAGeneratorErrorIsKept(t *testing.T) {
	w, g, _ := newTestWorld(t)
	g.fail = true

	w.Chunk(ChunkPos{X: 9, Z: 9})
	if w.GenerationError() == nil {
		t.Fatal("a failing generator left no error behind")
	}
}

func TestWorldTick(t *testing.T) {
	w, _, _ := newTestWorld(t)

	age, tod := w.GetTime()
	if age != 0 || tod != 0 {
		t.Errorf("initial time = (%d, %d), want (0, 0)", age, tod)
	}

	age, tod = w.Tick()
	if age != 1 || tod != 1 {
		t.Errorf("after 1 tick = (%d, %d), want (1, 1)", age, tod)
	}

	w.SetTime(100, 23999)
	age, tod = w.Tick()
	if age != 101 || tod != 0 {
		t.Errorf("after wrap = (%d, %d), want (101, 0)", age, tod)
	}
}

func TestWorldTickFrozenTime(t *testing.T) {
	w, _, _ := newTestWorld(t)

	w.SetTimeOfDay(-6000)
	if _, tod := w.GetTime(); tod != -6000 {
		t.Errorf("frozen time = %d, want -6000", tod)
	}

	age, tod := w.Tick()
	if age != 1 || tod != -6000 {
		t.Errorf("after tick with frozen time = (%d, %d), want (1, -6000)", age, tod)
	}
}

func TestWorldGetSetTime(t *testing.T) {
	w, _, _ := newTestWorld(t)

	w.SetTime(5000, 12000)
	age, tod := w.GetTime()
	if age != 5000 || tod != 12000 {
		t.Errorf("GetTime() = (%d, %d), want (5000, 12000)", age, tod)
	}

	w.SetTimeOfDay(18000)
	age, tod = w.GetTime()
	if age != 5000 || tod != 18000 {
		t.Errorf("after SetTimeOfDay = (%d, %d), want (5000, 18000)", age, tod)
	}
}

func TestTheProtocolShimsRoundTrip(t *testing.T) {
	w, _, reg := newTestWorld(t)
	cobble := reg.Intern("minecraft:cobblestone", nil)

	w.SetBlockID(1, 20, 1, int32(cobble))
	if got := w.GetBlockID(1, 20, 1); got != int32(cobble) {
		t.Fatalf("GetBlockID = %d, want %d", got, cobble)
	}
	if got := w.Block(BlockPos{1, 20, 1}); got != cobble {
		t.Fatalf("Block = %d, want %d", got, cobble)
	}
}
