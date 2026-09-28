package v775

import (
	"errors"
	"strings"
	"testing"

	"github.com/go-theft-craft/minecraft-protocol/data"
	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"
	v26_1 "github.com/go-theft-craft/minecraft-protocol/generated/java/v26_1"

	"github.com/go-theft-craft/server/pkg/world"
	"github.com/go-theft-craft/server/pkg/world/v47"
)

var _ world.Adapter = (*Adapter)(nil)

func newAdapter(t testing.TB) (*Adapter, *data.Set) {
	t.Helper()

	set, err := v26_1.Data()
	if err != nil {
		t.Fatalf("v26_1.Data: %v", err)
	}
	reg, err := world.NewJavaRegistry(set)
	if err != nil {
		t.Fatalf("NewJavaRegistry: %v", err)
	}
	a, err := New(reg, set)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return a, set
}

func TestEveryStateRoundTrips(t *testing.T) {
	a, _ := newAdapter(t)
	reg := a.Registry()

	for s := world.State(0); int(s) < reg.Len(); s++ {
		v, err := a.EncodeState(s)
		if err != nil {
			t.Fatalf("EncodeState(%d): %v", s, err)
		}
		back, err := a.DecodeState(v)
		if err != nil {
			t.Fatalf("DecodeState(%d): %v", v, err)
		}
		if back != s {
			name, props, _ := reg.Lookup(s)
			t.Fatalf("%s %v encoded to %d and decoded to handle %d, want %d", name, props, v, back, s)
		}
	}
}

// TestEveryStateIDTheDataSetDefinesDecodes asserts the encoding is onto: each
// block's whole range is covered, so a client can never be sent a number this
// adapter would not read back, and no state was dropped.
func TestEveryStateIDTheDataSetDefinesDecodes(t *testing.T) {
	a, set := newAdapter(t)

	for _, block := range set.Blocks().All() {
		for id := block.MinStateID; id <= block.MaxStateID; id++ {
			s, err := a.DecodeState(int32(id))
			if err != nil {
				t.Fatalf("%s state %d: %v", block.Name, id, err)
			}
			name, _, _ := a.Registry().Lookup(s)
			if name != "minecraft:"+block.Name {
				t.Fatalf("state %d decoded to %s, want minecraft:%s", id, name, block.Name)
			}
		}
	}
}

// TestEveryDefaultStateEncodesToTheDataSetsDefault is what holds the property
// arithmetic to the registry's. A round trip passes for any consistent
// permutation of a block's states; the default is the one state whose number
// the data set publishes independently, so a digit order that disagrees with
// the registry's shows up here, for every block that has properties.
func TestEveryDefaultStateEncodesToTheDataSetsDefault(t *testing.T) {
	a, set := newAdapter(t)

	for _, block := range set.Blocks().All() {
		name := "minecraft:" + block.Name
		s, ok := a.Registry().TryIntern(name, nil)
		if !ok {
			t.Fatalf("the registry has no default for %s", name)
		}
		v, err := a.EncodeState(s)
		if err != nil {
			t.Fatalf("EncodeState(%s): %v", name, err)
		}
		if v != int32(block.DefaultState) {
			t.Errorf("%s's default encodes to %d, want %d", name, v, block.DefaultState)
		}
	}
}

func TestStoneEncodesToItsDefaultState(t *testing.T) {
	a, set := newAdapter(t)

	stone, ok := set.Blocks().ByName("stone")
	if !ok {
		t.Fatal("26.1 has no stone")
	}
	v, err := a.EncodeState(a.Registry().Intern("minecraft:stone", nil))
	if err != nil {
		t.Fatalf("EncodeState: %v", err)
	}
	if v != int32(stone.DefaultState) {
		t.Fatalf("stone encodes to %d, want %d", v, stone.DefaultState)
	}
}

// TestAPreFlatteningRegistryIsRefused is the construction-time error the
// adapter promises: a 1.8 registry names every state by a metadata property
// no 26.1 block defines.
func TestAPreFlatteningRegistryIsRefused(t *testing.T) {
	old, err := v1_8.Data()
	if err != nil {
		t.Fatalf("v1_8.Data: %v", err)
	}
	oldReg, err := world.NewJavaRegistry(old)
	if err != nil {
		t.Fatalf("NewJavaRegistry(1.8): %v", err)
	}
	modern, err := v26_1.Data()
	if err != nil {
		t.Fatalf("v26_1.Data: %v", err)
	}

	if _, err := New(oldReg, modern); err == nil {
		t.Fatal("New accepted a 1.8 registry against the 26.1 data set")
	}
}

func TestTheDimensionIsTheModernOverworld(t *testing.T) {
	a, _ := newAdapter(t)

	if got, want := a.Dimension(), world.Overworld261(); got != want {
		t.Fatalf("Dimension() = %+v, want %+v", got, want)
	}
	if got := a.Dimension().Sections(); got != 24 {
		t.Fatalf("the overworld has %d sections, want 24", got)
	}
}

func TestEncodeUnloadNamesTheColumn(t *testing.T) {
	a, _ := newAdapter(t)

	p, err := a.EncodeUnload(world.ChunkPos{X: 3, Z: -7})
	if err != nil {
		t.Fatalf("EncodeUnload: %v", err)
	}
	unload, ok := p.(*v26_1.PlayClientboundUnloadChunk)
	if !ok {
		t.Fatalf("EncodeUnload returned %T", p)
	}
	if unload.ChunkX != 3 || unload.ChunkZ != -7 {
		t.Fatalf("unload names (%d, %d), want (3, -7)", unload.ChunkX, unload.ChunkZ)
	}
}

// TestEncodeChunkWaitsOnTheColumnEncoder pins the gap rather than hiding it.
// It is replaced by the column tests when wire/java/chunk can write a 775
// column.
func TestEncodeChunkWaitsOnTheColumnEncoder(t *testing.T) {
	a, _ := newAdapter(t)

	b := world.NewBuilder(a.Dimension(), world.ChunkPos{}, a.Registry().Air())
	if _, err := a.EncodeChunk(b.Build()); !errors.Is(err, ErrNoColumnEncoder) {
		t.Fatalf("EncodeChunk = %v, want ErrNoColumnEncoder", err)
	}
}

// TestTheTwoAdaptersAgreeOnBlocksAndNotOnNumbers is
// TestTheTwoJavaRegistriesAgreeOnNamesAndNotOnHandles one layer up. The same
// canonical name, interned into each version's registry and encoded by each
// version's adapter, becomes two different numbers that each decode back to
// that name: the world model stayed version-neutral while gaining a second
// version.
func TestTheTwoAdaptersAgreeOnBlocksAndNotOnNumbers(t *testing.T) {
	modern, _ := newAdapter(t)

	oldSet, err := v1_8.Data()
	if err != nil {
		t.Fatalf("v1_8.Data: %v", err)
	}
	oldReg, err := world.NewJavaRegistry(oldSet)
	if err != nil {
		t.Fatalf("NewJavaRegistry(1.8): %v", err)
	}
	old, err := v47.New(oldReg, oldSet)
	if err != nil {
		t.Fatalf("v47.New: %v", err)
	}

	for _, name := range []string{"minecraft:stone", "minecraft:dirt", "minecraft:sand", "minecraft:bedrock"} {
		oldValue, err := old.EncodeState(old.Registry().Intern(name, nil))
		if err != nil {
			t.Fatalf("v47 %s: %v", name, err)
		}
		modernValue, err := modern.EncodeState(modern.Registry().Intern(name, nil))
		if err != nil {
			t.Fatalf("v775 %s: %v", name, err)
		}
		if oldValue == modernValue {
			t.Errorf("%s encodes to %d in both versions; this test no longer distinguishes them",
				name, oldValue)
		}

		for _, side := range []struct {
			adapter world.Adapter
			value   int32
		}{{old, oldValue}, {modern, modernValue}} {
			s, err := side.adapter.DecodeState(side.value)
			if err != nil {
				t.Fatalf("decode %d: %v", side.value, err)
			}
			got, _, _ := side.adapter.Registry().Lookup(s)
			if !strings.EqualFold(got, name) {
				t.Errorf("%d decoded to %s, want %s", side.value, got, name)
			}
		}
	}
}
