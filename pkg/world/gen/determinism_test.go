package gen

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/server/pkg/world"
	"github.com/go-theft-craft/server/pkg/world/v47"
)

var updateGolden = flag.Bool("update", false, "rewrite the generator golden table")

// The determinism contract.
//
// A generator has to produce the same terrain from the same seed and the same
// parameters, run after run and build after build. The golden table is what
// says so: each entry is (generator, version, seed, parameter hash) and the
// chunk hashes that combination produces, so a parameter change that alters
// terrain is caught rather than absorbed.
//
// Regenerate it deliberately, and only in the commit that changes an
// algorithm:
//
//	devbox run -- go test -mod vendor -run TestGeneratorGolden ./pkg/world/gen/ -update
//
// A hash that moved means terrain moved, which means the world someone has
// built in now generates differently at its edges. The commit message has to
// say why.

// goldenChunks is the fixed chunk set every entry covers: the origin, a nearby
// chunk in a different quadrant, and two far ones.
var goldenChunks = []world.ChunkPos{
	{X: 0, Z: 0},
	{X: 7, Z: -3},
	{X: 100, Z: 100},
	{X: -40, Z: 60},
}

const goldenSeed = 12345

// goldenEntry is one (generator, version, seed, parameters) combination.
type goldenEntry struct {
	Generator string            `json:"generator"`
	Version   int               `json:"version"`
	Seed      int64             `json:"seed"`
	Params    string            `json:"params_sha256"`
	Chunks    map[string]string `json:"chunks"`
}

// goldenCase is what a table entry is generated from.
type goldenCase struct {
	name   string
	seed   int64
	params Params
}

// goldenCases covers each generator at its defaults, plus one non-default
// parameter set each, so a change that only affects a configured world is
// caught too.
func goldenCases() []goldenCase {
	deepSea := DefaultDefaults()
	deepSea.SeaLevel = 40
	deepSea.Caves.Threshold = 0.5

	thin := FlatParams{
		Layers: []FlatLayer{
			{Block: "minecraft:bedrock", Thickness: 1},
			{Block: "minecraft:sand", Thickness: 2},
		},
		Biome: "minecraft:desert",
	}

	return []goldenCase{
		{name: DefaultName, seed: goldenSeed, params: DefaultDefaults()},
		{name: DefaultName, seed: goldenSeed, params: deepSea},
		{name: DefaultName, seed: 7, params: DefaultDefaults()},
		{name: FlatName, seed: goldenSeed, params: FlatDefaults()},
		{name: FlatName, seed: goldenSeed, params: thin},
	}
}

func TestGeneratorGolden(t *testing.T) {
	reg, adapter := goldenCodec(t)
	registry := DefaultRegistry()

	got := make([]goldenEntry, 0, len(goldenCases()))
	for _, c := range goldenCases() {
		factory, ok := registry.Lookup(c.name)
		if !ok {
			t.Fatalf("no factory named %q", c.name)
		}
		got = append(got, goldenEntry{
			Generator: c.name,
			Version:   factory.Version(),
			Seed:      c.seed,
			Params:    paramsHash(t, c.params),
			Chunks:    generateChunkHashes(t, factory, c, reg, adapter),
		})
	}

	path := filepath.Join("testdata", "golden.json")
	rendered, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatalf("marshal golden: %v", err)
	}
	rendered = append(rendered, '\n')

	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, rendered, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Log("golden table rewritten; the commit message has to say what moved and why")

		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with -update to create it): %v", err)
	}
	if string(rendered) != string(want) {
		t.Fatalf("generator output moved.\n"+
			"A hash here moving means terrain moved: a world someone has built in\n"+
			"now generates differently at its edges. If that is intended, rerun with\n"+
			"-update and say why in the commit message.\n\n--- want ---\n%s\n--- got ---\n%s",
			want, rendered)
	}
}

// TestAGeneratorProducesTheSameChunkTwice catches a generator that accumulated
// state between chunks — the failure a table of per-run hashes would miss,
// because it would record the accumulated result as the expected one.
func TestAGeneratorProducesTheSameChunkTwice(t *testing.T) {
	reg, adapter := goldenCodec(t)
	registry := DefaultRegistry()

	for _, c := range goldenCases() {
		factory, _ := registry.Lookup(c.name)
		g, err := factory.New(c.seed, c.params, reg)
		if err != nil {
			t.Fatalf("%s: New: %v", c.name, err)
		}

		// The same chunk, generated after every other chunk in the set has
		// been generated through the same instance.
		first := hashOneChunk(t, g, goldenChunks[0], reg, adapter)
		for _, pos := range goldenChunks[1:] {
			hashOneChunk(t, g, pos, reg, adapter)
		}
		second := hashOneChunk(t, g, goldenChunks[0], reg, adapter)

		if first != second {
			t.Errorf("%s generated chunk %v differently the second time: the generator keeps state between chunks",
				c.name, goldenChunks[0])
		}
	}
}

// TestTwoInstancesWithTheSameSeedAgree is the other half: a generator built
// twice from the same seed and parameters produces the same world.
func TestTwoInstancesWithTheSameSeedAgree(t *testing.T) {
	reg, adapter := goldenCodec(t)
	registry := DefaultRegistry()

	for _, c := range goldenCases() {
		factory, _ := registry.Lookup(c.name)

		a, err := factory.New(c.seed, c.params, reg)
		if err != nil {
			t.Fatalf("%s: New: %v", c.name, err)
		}
		b, err := factory.New(c.seed, c.params, reg)
		if err != nil {
			t.Fatalf("%s: New: %v", c.name, err)
		}

		for _, pos := range goldenChunks {
			if hashOneChunk(t, a, pos, reg, adapter) != hashOneChunk(t, b, pos, reg, adapter) {
				t.Errorf("%s: two instances with seed %d disagree at %v", c.name, c.seed, pos)
			}
		}
	}
}

// TestADifferentSeedProducesDifferentTerrain guards the opposite mistake: a
// generator that ignores its seed would pass every determinism check above.
func TestADifferentSeedProducesDifferentTerrain(t *testing.T) {
	reg, adapter := goldenCodec(t)
	registry := DefaultRegistry()
	factory, _ := registry.Lookup(DefaultName)

	a, err := factory.New(1, DefaultDefaults(), reg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	b, err := factory.New(2, DefaultDefaults(), reg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if hashOneChunk(t, a, goldenChunks[0], reg, adapter) == hashOneChunk(t, b, goldenChunks[0], reg, adapter) {
		t.Fatal("two seeds produced the same chunk; the seed is not reaching the noise")
	}
}

func goldenCodec(t *testing.T) (world.StateRegistry, *v47.Adapter) {
	t.Helper()

	set, err := v1_8.Data()
	if err != nil {
		t.Fatalf("v1_8.Data: %v", err)
	}
	reg, err := world.NewJavaRegistry(set)
	if err != nil {
		t.Fatalf("NewJavaRegistry: %v", err)
	}
	adapter, err := v47.New(reg, set)
	if err != nil {
		t.Fatalf("v47.New: %v", err)
	}

	return reg, adapter
}

// paramsHash digests a parameter set, so the table's key says which one an
// entry covers without carrying the whole thing.
func paramsHash(t *testing.T, p Params) string {
	t.Helper()

	raw, err := MarshalParams(p)
	if err != nil {
		t.Fatalf("MarshalParams: %v", err)
	}
	sum := sha256.Sum256(raw)

	return hex.EncodeToString(sum[:8])
}

func generateChunkHashes(t *testing.T, factory Factory, c goldenCase, reg world.StateRegistry, adapter *v47.Adapter) map[string]string {
	t.Helper()

	g, err := factory.New(c.seed, c.params, reg)
	if err != nil {
		t.Fatalf("%s: New: %v", c.name, err)
	}

	out := map[string]string{}
	for _, pos := range goldenChunks {
		out[fmt.Sprintf("%d,%d", pos.X, pos.Z)] = hashOneChunk(t, g, pos, reg, adapter)
	}

	return out
}

func hashOneChunk(t *testing.T, g Generator, pos world.ChunkPos, reg world.StateRegistry, adapter *v47.Adapter) string {
	t.Helper()

	b := world.NewBuilder(world.Overworld18(), pos, reg.Air())
	if err := g.Generate(pos, b); err != nil {
		t.Fatalf("Generate %v: %v", pos, err)
	}

	return hashChunk(t, adapter, b.Build())
}

// hashChunk digests a chunk's block data and biomes. A nil section is a marker
// byte rather than 8192 zeros, so an all-air section and an absent one stay
// distinguishable. Block states are hashed as protocol 47 encodes them, which
// is what the table was created from.
func hashChunk(t *testing.T, enc *v47.Adapter, c *world.Chunk) string {
	t.Helper()

	h := sha256.New()
	var buf [2]byte
	for _, sec := range c.Sections {
		if sec == nil {
			h.Write([]byte{0xFF})

			continue
		}
		h.Write([]byte{0x00})
		for _, state := range sec.States() {
			v, err := enc.EncodeState(state)
			if err != nil {
				t.Fatalf("EncodeState: %v", err)
			}
			binary.LittleEndian.PutUint16(buf[:], uint16(v))
			h.Write(buf[:])
		}
	}
	for _, b := range c.Biomes {
		h.Write([]byte{byte(b)})
	}

	return hex.EncodeToString(h.Sum(nil))
}

// TestTheGoldenTableStillCoversTheOriginalHashes is the bridge from M11.2's
// table to this one: the two entries that were in it must still produce the
// hashes it recorded, or the parameter promotion moved terrain after all.
func TestTheGoldenTableStillCoversTheOriginalHashes(t *testing.T) {
	// Captured by M11.2, before a single constant became a parameter.
	inherited := map[string]map[string]string{
		DefaultName: {
			"0,0":     "1335818f12b00c61e83918a281b9842eeec6ef1b03c57099a8e301f3839a70f0",
			"7,-3":    "9303cb8727e7af69d6ca94c19081efea2596f547840651400e09d96a1999aa31",
			"100,100": "fcb3df4db63bf93b7cb16aa50ace09340a265d1bf2e0c7bfe758993cb9a43257",
			"-40,60":  "068be94b3df466c7ed7f55948eb3a8001ff8a3ab3adc219655ca940d3a8571cb",
		},
		FlatName: {
			"0,0":     "b7837382aef0f8ade52d0f04bb9c65e955f01e885fc485161342ee0e8cab7a22",
			"7,-3":    "b7837382aef0f8ade52d0f04bb9c65e955f01e885fc485161342ee0e8cab7a22",
			"100,100": "b7837382aef0f8ade52d0f04bb9c65e955f01e885fc485161342ee0e8cab7a22",
			"-40,60":  "b7837382aef0f8ade52d0f04bb9c65e955f01e885fc485161342ee0e8cab7a22",
		},
	}

	reg, adapter := goldenCodec(t)
	registry := DefaultRegistry()

	for name, want := range inherited {
		factory, _ := registry.Lookup(name)
		defaults := goldenCase{name: name, seed: goldenSeed, params: factory.Defaults()}
		got := generateChunkHashes(t, factory, defaults, reg, adapter)

		for _, key := range slices.Sorted(maps(want)) {
			if got[key] != want[key] {
				t.Errorf("%s chunk %s: %s, want the pre-M11.4 %s", name, key, got[key], want[key])
			}
		}
	}
}

// maps yields a map's keys, which slices.Sorted then orders.
func maps(m map[string]string) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}
