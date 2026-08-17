package v47

import (
	"runtime"
	"testing"

	"github.com/go-theft-craft/server/pkg/world"
	"github.com/go-theft-craft/server/pkg/world/gen"
)

// viewDistance12 is 25×25 chunks, which is what a player joining at the
// largest view distance the server offers is sent. It is the number the encode
// cache exists for.
const viewDistance12 = 12

func benchChunks(tb testing.TB, a *Adapter) []*world.Chunk {
	tb.Helper()

	g := gen.NewDefaultGenerator(12345)
	if err := g.Bind(a.Registry()); err != nil {
		tb.Fatalf("Bind: %v", err)
	}

	var chunks []*world.Chunk
	for cx := -viewDistance12; cx <= viewDistance12; cx++ {
		for cz := -viewDistance12; cz <= viewDistance12; cz++ {
			pos := world.ChunkPos{X: cx, Z: cz}
			b := world.NewBuilder(a.Dimension(), pos, a.Registry().Air())
			if err := g.Generate(pos, b); err != nil {
				tb.Fatalf("Generate: %v", err)
			}
			chunks = append(chunks, b.Build())
		}
	}

	return chunks
}

// BenchmarkEncodeJoinCold is a player joining a world nobody has been sent
// yet: every section is encoded for the first time.
func BenchmarkEncodeJoinCold(b *testing.B) {
	a := newAdapter(b)
	chunks := benchChunks(b, a)

	for b.Loop() {
		a.cache = newCache()
		for _, c := range chunks {
			if _, err := a.EncodeChunk(c); err != nil {
				b.Fatalf("EncodeChunk: %v", err)
			}
		}
	}
}

// BenchmarkEncodeJoinWarm is the second player to join the same world, which
// is the case the cache turns from re-encoding 10,000 sections into 10,000 map
// lookups.
func BenchmarkEncodeJoinWarm(b *testing.B) {
	a := newAdapter(b)
	chunks := benchChunks(b, a)
	for _, c := range chunks {
		if _, err := a.EncodeChunk(c); err != nil {
			b.Fatalf("EncodeChunk: %v", err)
		}
	}

	for b.Loop() {
		for _, c := range chunks {
			if _, err := a.EncodeChunk(c); err != nil {
				b.Fatalf("EncodeChunk: %v", err)
			}
		}
	}
}

// TestResidentWorldSize reports what 625 resident chunks cost in memory. It is
// a measurement rather than an assertion: the number belongs in the milestone
// record, and a threshold here would fail on an unrelated allocator change.
func TestResidentWorldSize(t *testing.T) {
	if testing.Short() {
		t.Skip("allocates a 625-chunk world")
	}

	a := newAdapter(t)

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	chunks := benchChunks(t, a)

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	sections := 0
	for _, c := range chunks {
		for _, s := range c.Sections {
			if s != nil {
				sections++
			}
		}
	}

	t.Logf("%d chunks, %d sections, %d bytes resident (%d bytes per section)",
		len(chunks), sections,
		after.HeapAlloc-before.HeapAlloc,
		(after.HeapAlloc-before.HeapAlloc)/uint64(max(sections, 1)))

	runtime.KeepAlive(chunks)
}
