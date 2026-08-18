package server_test

import (
	"context"
	"sync"
	"testing"

	"github.com/go-theft-craft/server/config"
	"github.com/go-theft-craft/server/pkg/world"
	"github.com/go-theft-craft/server/server"
)

// Chunk attribution.
//
// The number that motivated the milestone is 625: a player joining at the
// default view distance of 12 triggers that many chunk encodes before they can
// move, and until now nothing said so.

// collector keeps every sample, grouped by feature.
type collector struct {
	mu  sync.Mutex
	got []server.Sample
}

func (c *collector) Observe(s server.Sample) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.got = append(c.got, s)
}

// byFeature returns every sample carrying one feature.
func (c *collector) byFeature(f server.Feature) []server.Sample {
	c.mu.Lock()
	defer c.mu.Unlock()

	var out []server.Sample
	for _, s := range c.got {
		if s.Labels.Feature == f {
			out = append(out, s)
		}
	}

	return out
}

func TestJoiningEmitsOneEncodeSamplePerChunkSent(t *testing.T) {
	obs := &collector{}
	settings := config.DefaultConfig()
	settings.GeneratorType = config.GeneratorFlat
	settings.ViewDistance = 12

	srv, err := server.New(server.WithSettings(settings), server.WithObserver(obs))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// The encode the join does, without a connection: a join at view distance
	// 12 is a 25×25 square, and 625 is the number the original todo item asked
	// about.
	const view = 12
	sent := 0
	for cx := -view; cx <= view; cx++ {
		for cz := -view; cz <= view; cz++ {
			if _, err := srv.World().EncodeChunk(world.ChunkPos{X: cx, Z: cz}); err != nil {
				t.Fatalf("EncodeChunk: %v", err)
			}
			sent++
		}
	}
	if sent != 625 {
		t.Fatalf("the join square is %d chunks, want 625", sent)
	}

	srv.DrainSamples()
	if got := len(obs.byFeature(server.FeatureChunkEncode)); got != 625 {
		t.Errorf("625 encodes produced %d encode samples, want one each", got)
	}
}

func TestChunkSamplesCarryThePlayerAndTheRegion(t *testing.T) {
	obs := &collector{}
	settings := config.DefaultConfig()
	settings.GeneratorType = config.GeneratorFlat

	srv, err := server.New(server.WithSettings(settings), server.WithObserver(obs))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// A chunk in region -1,-1: the negative case, which is where a region
	// computed by division would name the wrong file.
	if _, err := srv.World().EncodeChunk(world.ChunkPos{X: -1, Z: -33}); err != nil {
		t.Fatalf("EncodeChunk: %v", err)
	}
	srv.DrainSamples()

	got := obs.byFeature(server.FeatureChunkEncode)
	if len(got) == 0 {
		t.Fatal("no encode sample")
	}
	if want := "r.-1.-2"; got[0].Labels.Region.String() != want {
		t.Errorf("region label is %q, want %q", got[0].Labels.Region.String(), want)
	}
	if got[0].Labels.World != server.DefaultWorld {
		t.Errorf("world label is %q, want %q", got[0].Labels.World, server.DefaultWorld)
	}
	// The exact chunk is not labelled unless somebody asked. See
	// TestChunkLabelIsEmptyByDefaultAndSetUnderWithChunkDetail.
	if got[0].Labels.Chunk != "" {
		t.Errorf("chunk label is %q by default, want empty", got[0].Labels.Chunk)
	}
}

func TestGenerateLoadAndSaveAreDistinguishable(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	// Run one generates and saves.
	first := &collector{}
	srv, store := newObservedStoredServer(t, dir, first)
	w := srv.World()
	w.SetBlock(world.BlockPos{X: 2, Y: 80, Z: 2}, w.Registry().Intern("minecraft:cobblestone", nil))
	if err := store.World().SaveSnapshot(ctx, server.DefaultWorld, w.Snapshot()); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}
	srv.DrainSamples()

	if got := len(first.byFeature(server.FeatureChunkGenerate)); got == 0 {
		t.Error("a world with nothing on disk emitted no generate samples")
	}
	if got := len(first.byFeature(server.FeatureChunkSave)); got == 0 {
		t.Error("a save emitted no save samples")
	}
	// Nothing was on disk to load, so no load sample is the correct answer
	// rather than a missing one.
	if got := len(first.byFeature(server.FeatureChunkLoad)); got != 0 {
		t.Errorf("a first run emitted %d load samples, want 0", got)
	}

	// Run two loads what run one wrote.
	second := &collector{}
	reopened, _ := newObservedStoredServer(t, dir, second)
	reopened.World().Block(world.BlockPos{X: 2, Y: 80, Z: 2})
	reopened.DrainSamples()

	if got := len(second.byFeature(server.FeatureChunkLoad)); got == 0 {
		t.Fatal("reading a stored column emitted no load samples")
	}
	// The point of separating them: a column that came off disk did not cost
	// what a column the generator made costs, and a graph that added them
	// together could not say which.
	if got := len(second.byFeature(server.FeatureChunkGenerate)); got != 0 {
		t.Errorf("a stored column emitted %d generate samples, want 0", got)
	}
}

func newObservedStoredServer(t *testing.T, dir string, obs server.Observer) (*server.Server, *server.Storage) {
	t.Helper()

	settings := config.DefaultConfig()
	settings.GeneratorType = config.GeneratorFlat

	store, err := server.FileStore(dir, nil)
	if err != nil {
		t.Fatalf("FileStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	opts := append(store.Options(), server.WithSettings(settings), server.WithObserver(obs))
	srv, err := server.New(opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return srv, store
}

func TestChunkLabelIsEmptyByDefaultAndSetUnderWithChunkDetail(t *testing.T) {
	settings := config.DefaultConfig()
	settings.GeneratorType = config.GeneratorFlat
	pos := world.ChunkPos{X: -1, Z: 40}

	byDefault := &collector{}
	plain, err := server.New(server.WithSettings(settings), server.WithObserver(byDefault))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := plain.World().EncodeChunk(pos); err != nil {
		t.Fatalf("EncodeChunk: %v", err)
	}
	plain.DrainSamples()

	got := byDefault.byFeature(server.FeatureChunkEncode)
	if len(got) == 0 {
		t.Fatal("no encode sample")
	}
	if got[0].Labels.Chunk != "" {
		t.Errorf("chunk label is %q by default, want empty", got[0].Labels.Chunk)
	}

	detailed := &collector{}
	srv, err := server.New(
		server.WithSettings(settings),
		server.WithObserver(detailed),
		server.WithChunkDetail(),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := srv.World().EncodeChunk(pos); err != nil {
		t.Fatalf("EncodeChunk: %v", err)
	}
	srv.DrainSamples()

	got = detailed.byFeature(server.FeatureChunkEncode)
	if len(got) == 0 {
		t.Fatal("no encode sample under WithChunkDetail")
	}
	if want := "-1,40"; got[0].Labels.Chunk != want {
		t.Errorf("chunk label is %q, want %q", got[0].Labels.Chunk, want)
	}
	// The region stays set, so a query written against regions keeps working
	// while somebody is investigating one column.
	if want := "r.-1.1"; got[0].Labels.Region.String() != want {
		t.Errorf("region label is %q under chunk detail, want %q", got[0].Labels.Region.String(), want)
	}
}
