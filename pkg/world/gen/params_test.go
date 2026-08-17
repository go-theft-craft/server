package gen

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/go-theft-craft/server/pkg/world"
)

func TestDefaultRegistryHasDefaultAndFlat(t *testing.T) {
	r := DefaultRegistry()

	names := r.Names()
	if len(names) != 2 || names[0] != DefaultName || names[1] != FlatName {
		t.Fatalf("Names() = %v, want [default flat]", names)
	}
	for _, name := range names {
		f, ok := r.Lookup(name)
		if !ok {
			t.Fatalf("Lookup(%q) found nothing", name)
		}
		if f.Name() != name {
			t.Errorf("factory under %q calls itself %q", name, f.Name())
		}
		if f.Version() < 1 {
			t.Errorf("%s has version %d", name, f.Version())
		}
		if f.Defaults() == nil {
			t.Errorf("%s has no defaults", name)
		}
	}
}

func TestRegisteringADuplicateNameErrors(t *testing.T) {
	r := DefaultRegistry()

	if err := r.Register(flatFactory{}); err == nil {
		t.Fatal("registering flat twice was accepted")
	}
}

// TestTwoRegistriesDoNotShareRegistrations is why the registry is a value: the
// interoperability lane runs servers side by side in one test binary, and a
// package-level map would let one test's generator leak into another's.
func TestTwoRegistriesDoNotShareRegistrations(t *testing.T) {
	a, b := DefaultRegistry(), DefaultRegistry()

	if err := a.Register(stubFactory{}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, ok := b.Lookup((stubFactory{}).Name()); ok {
		t.Fatal("registering into one registry changed the other")
	}
}

func TestParamsRoundTripThroughJSON(t *testing.T) {
	for _, f := range []Factory{defaultFactory{}, flatFactory{}} {
		raw, err := MarshalParams(f.Defaults())
		if err != nil {
			t.Fatalf("%s: MarshalParams: %v", f.Name(), err)
		}

		back, err := f.Parse(raw)
		if err != nil {
			t.Fatalf("%s: Parse: %v", f.Name(), err)
		}
		again, err := MarshalParams(back)
		if err != nil {
			t.Fatalf("%s: MarshalParams: %v", f.Name(), err)
		}
		if string(again) != string(raw) {
			t.Fatalf("%s did not round-trip.\nfirst:  %s\nsecond: %s", f.Name(), raw, again)
		}
	}
}

// TestAnUnknownParameterKeyIsRejected: a typo in a parameter file is an error,
// not a line nobody reads.
func TestAnUnknownParameterKeyIsRejected(t *testing.T) {
	for _, tc := range []struct {
		factory Factory
		raw     string
	}{
		{defaultFactory{}, `{"sea_lvel": 40}`},
		{flatFactory{}, `{"layerz": []}`},
	} {
		if _, err := tc.factory.Parse(json.RawMessage(tc.raw)); err == nil {
			t.Errorf("%s accepted %s", tc.factory.Name(), tc.raw)
		}
	}
}

func TestEmptyParametersMeanTheDefaults(t *testing.T) {
	for _, raw := range []string{"", "null", "{}"} {
		p, err := (defaultFactory{}).Parse(json.RawMessage(raw))
		if err != nil {
			t.Fatalf("Parse(%q): %v", raw, err)
		}
		if p.(DefaultParams).SeaLevel != DefaultDefaults().SeaLevel {
			t.Errorf("Parse(%q) did not start from the defaults", raw)
		}
	}
}

// A parameter set naming a block nothing knows fails at construction, with the
// block in the message, rather than producing wrong terrain later.
func TestAnUnknownBlockNameIsAConstructionError(t *testing.T) {
	reg := testRegistry(t)

	params := DefaultDefaults()
	params.Ores[0].Block = "minecraft:unobtanium_ore"

	_, err := (defaultFactory{}).New(1, params, reg)
	if err == nil {
		t.Fatal("an unknown ore block was accepted")
	}
	if !strings.Contains(err.Error(), "unobtanium_ore") {
		t.Errorf("error does not name the block: %v", err)
	}

	flat := FlatDefaults()
	flat.Layers[0].Block = "minecraft:cheese"
	if _, err := (flatFactory{}).New(1, flat, reg); err == nil {
		t.Fatal("an unknown flat layer block was accepted")
	}
}

func TestParametersOfTheWrongTypeAreRefused(t *testing.T) {
	reg := testRegistry(t)

	if _, err := (defaultFactory{}).New(1, FlatDefaults(), reg); err == nil {
		t.Fatal("the noise generator accepted flat parameters")
	}
	if _, err := (flatFactory{}).New(1, DefaultDefaults(), reg); err == nil {
		t.Fatal("the flat generator accepted noise parameters")
	}
}

// A flat world built from a different layer list is that list.
func TestFlatLayersAreWhatGetsBuilt(t *testing.T) {
	reg := testRegistry(t)

	params := FlatParams{
		Layers: []FlatLayer{
			{Block: "minecraft:bedrock", Thickness: 1},
			{Block: "minecraft:sand", Thickness: 3},
		},
		Biome: "minecraft:desert",
	}
	g, err := (flatFactory{}).New(0, params, reg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := g.HeightAt(0, 0); got != 3 {
		t.Fatalf("HeightAt = %d, want 3", got)
	}

	b := world.NewBuilder(world.Overworld18(), world.ChunkPos{}, reg.Air())
	if err := g.Generate(world.ChunkPos{}, b); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	c := b.Build()

	for y, want := range []string{"minecraft:bedrock", "minecraft:sand", "minecraft:sand", "minecraft:sand"} {
		if got := blockAt(c, 0, y, 0); got != reg.Intern(want, nil) {
			t.Errorf("y=%d is %d, want %s", y, got, want)
		}
	}
	if got := blockAt(c, 0, 4, 0); got != reg.Air() {
		t.Errorf("y=4 is %d, want air", got)
	}
	if c.Biomes[0] != world.Biome(biomeDesert) {
		t.Errorf("biome = %d, want desert", c.Biomes[0])
	}
}

// stubFactory is a generator registered from a test, standing in for one an
// application would register.
type stubFactory struct{}

func (stubFactory) Name() string     { return "stub" }
func (stubFactory) Version() int     { return 1 }
func (stubFactory) Defaults() Params { return FlatDefaults() }

func (stubFactory) Parse(json.RawMessage) (Params, error) { return FlatDefaults(), nil }

func (stubFactory) New(seed int64, p Params, reg world.StateRegistry) (Generator, error) {
	return (flatFactory{}).New(seed, p, reg)
}
