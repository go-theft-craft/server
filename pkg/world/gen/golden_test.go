package gen

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/server/pkg/world"
	"github.com/go-theft-craft/server/pkg/world/v47"
)

var updateGolden = flag.Bool("update", false, "rewrite the generator golden table")

// goldenChunks is the fixed chunk set the golden table covers: the origin, a
// nearby chunk in a different quadrant, and two far ones.
var goldenChunks = []world.ChunkPos{
	{X: 0, Z: 0},
	{X: 7, Z: -3},
	{X: 100, Z: 100},
	{X: -40, Z: 60},
}

const goldenSeed = 12345

// TestGeneratorGolden pins what the generators produce. It is written against
// the generator as it stands and must not move while the world model changes
// underneath it: a hash that moves means a block landed somewhere else, which
// is exactly the failure a mechanical rewrite of forty SetBlock calls makes.
func TestGeneratorGolden(t *testing.T) {
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
	dim := world.Overworld18()

	got := map[string]string{}
	for name, g := range map[string]Generator{
		"flat":    NewFlatGenerator(goldenSeed),
		"default": NewDefaultGenerator(goldenSeed),
	} {
		if err := g.(world.Binder).Bind(reg); err != nil {
			t.Fatalf("Bind %s: %v", name, err)
		}
		for _, pos := range goldenChunks {
			b := world.NewBuilder(dim, pos, reg.Air())
			if err := g.Generate(pos, b); err != nil {
				t.Fatalf("Generate %s %v: %v", name, pos, err)
			}
			key := fmt.Sprintf("%s/%d,%d", name, pos.X, pos.Z)
			got[key] = hashChunk(t, adapter, b.Build())
		}
	}

	path := filepath.Join("testdata", "generator_golden.txt")
	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, []byte(renderGolden(got)), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}

		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with -update to create it): %v", err)
	}
	if diff := renderGolden(got); diff != string(want) {
		t.Fatalf("generator output moved.\n--- want ---\n%s\n--- got ---\n%s", want, diff)
	}
}

func renderGolden(entries map[string]string) string {
	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	sortStrings(keys)

	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s %s\n", k, entries[k])
	}

	return b.String()
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
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
