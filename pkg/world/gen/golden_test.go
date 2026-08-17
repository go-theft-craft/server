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
)

var updateGolden = flag.Bool("update", false, "rewrite the generator golden table")

// goldenChunks is the fixed chunk set the golden table covers: the origin, a
// nearby chunk in a different quadrant, and two far ones.
var goldenChunks = []ChunkPos{
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
	got := map[string]string{}
	for name, g := range map[string]Generator{
		"flat":    NewFlatGenerator(goldenSeed),
		"default": NewDefaultGenerator(goldenSeed),
	} {
		for _, pos := range goldenChunks {
			key := fmt.Sprintf("%s/%d,%d", name, pos.X, pos.Z)
			got[key] = hashChunk(g.Generate(pos.X, pos.Z))
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
// distinguishable.
func hashChunk(c *ChunkData) string {
	h := sha256.New()
	var buf [2]byte
	for _, sec := range c.Sections {
		if sec == nil {
			h.Write([]byte{0xFF})

			continue
		}
		h.Write([]byte{0x00})
		for _, state := range sec.Blocks {
			binary.LittleEndian.PutUint16(buf[:], state)
			h.Write(buf[:])
		}
	}
	h.Write(c.Biomes[:])

	return hex.EncodeToString(h.Sum(nil))
}
