package server_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/go-theft-craft/server/config"
	"github.com/go-theft-craft/server/pkg/world"
	"github.com/go-theft-craft/server/server"
)

// The M11.3 storage measurements.
//
// The design set three thresholds, and crossing any of them reopens the
// question of whether the vanilla Anvil format is the right one to keep a
// world in. They run behind an environment variable because the world they
// need is 10,000 chunks and nobody wants that in every `task test`:
//
//	M11_MEASURE=1 devbox run -- go test -mod vendor -run TestStorageMeasurements -v ./server/
//
// The numbers this produced are in
// docs/verification/2026-08-17-m11-3-storage-measurements.md.

func measuring(t *testing.T) {
	t.Helper()

	if os.Getenv("M11_MEASURE") == "" {
		t.Skip("set M11_MEASURE=1 to run the storage measurements")
	}
}

// newMeasuredServer builds a server on dir with the named generator.
func newMeasuredServer(t *testing.T, dir, generator string) (*server.Server, *server.Storage) {
	t.Helper()

	settings := config.DefaultConfig()
	settings.GeneratorType = generator

	store, err := server.FileStore(dir, nil)
	if err != nil {
		t.Fatalf("FileStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	srv, err := server.New(append(store.Options(), server.WithSettings(settings))...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return srv, store
}

// TestStorageMeasurements runs all three at once, because they share the
// 10,000-chunk world that is the expensive part.
func TestStorageMeasurements(t *testing.T) {
	measuring(t)

	dir := t.TempDir()
	ctx := context.Background()
	srv, store := newMeasuredServer(t, dir, config.GeneratorFlat)
	w := srv.World()

	// A 100×100 world: 10,000 resident chunks.
	const radius = 49
	start := time.Now()
	resident := w.PreGenerateRadius(radius)
	t.Logf("generated %d chunks in %v", resident, time.Since(start))

	start = time.Now()
	if err := store.World().SaveSnapshot(ctx, server.DefaultWorld, w.Snapshot()); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}
	t.Logf("full save of %d chunks: %v", resident, time.Since(start))

	// Measurement 1: an incremental save of 100 dirty chunks. Threshold 250 ms.
	cobble := w.Registry().Intern("minecraft:cobblestone", nil)

	// Scattered: 100 chunks spread over ten regions, which is what a busy
	// server looks like and what the threshold is about.
	regions := map[[2]int]bool{}
	dirtied := 0
	for cx := -5; cx < 5 && dirtied < 100; cx++ {
		for cz := -10; cz < 10 && dirtied < 100; cz++ {
			w.SetBlock(world.BlockPos{X: cx*16 + 1, Y: 100, Z: cz*16 + 1}, cobble)
			regions[[2]int{cx >> 5, cz >> 5}] = true
			dirtied++
		}
	}

	start = time.Now()
	if err := store.World().SaveSnapshot(ctx, server.DefaultWorld, w.Snapshot()); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}
	incremental := time.Since(start)
	t.Logf("MEASUREMENT incremental save of %d dirty chunks across %d regions: %v (threshold 250ms)",
		dirtied, len(regions), incremental)

	// Clustered: the same 100 chunks, all inside one region. The difference
	// between the two is the whole cost model — a region is the unit of write,
	// so the price is the number of regions touched, not the number of chunks.
	for i := range 100 {
		w.SetBlock(world.BlockPos{X: (i%10)*16 + 2, Y: 101, Z: (i/10)*16 + 2}, cobble)
	}
	start = time.Now()
	if err := store.World().SaveSnapshot(ctx, server.DefaultWorld, w.Snapshot()); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}
	t.Logf("MEASUREMENT incremental save of 100 dirty chunks in 1 region: %v", time.Since(start))

	// Measurement 2: a cold load of a 25-chunk view. Threshold 500 ms.
	cold, _ := newMeasuredServer(t, dir, config.GeneratorFlat)
	coldWorld := cold.World()

	start = time.Now()
	loaded := 0
	for cx := -2; cx <= 2; cx++ {
		for cz := -2; cz <= 2; cz++ {
			coldWorld.Chunk(world.ChunkPos{X: cx, Z: cz})
			loaded++
		}
	}
	coldLoad := time.Since(start)
	t.Logf("MEASUREMENT cold load of %d chunks: %v (threshold 500ms)", loaded, coldLoad)
	if err := coldWorld.GenerationError(); err != nil {
		t.Fatalf("the cold load reported %v", err)
	}

	// Measurement 3: bytes on disk per chunk. Threshold 3x the in-memory
	// section data. Flat terrain is one section per column and compresses
	// unusually well, so this is also measured on generated terrain below.
	t.Logf("MEASUREMENT flat world: %s", diskPerChunk(t, dir, resident))
}

// TestStorageBytesOnGeneratedTerrain measures the on-disk cost against terrain
// with caves, ores, and trees in it, which is what a real world looks like.
func TestStorageBytesOnGeneratedTerrain(t *testing.T) {
	measuring(t)

	dir := t.TempDir()
	ctx := context.Background()
	srv, store := newMeasuredServer(t, dir, config.GeneratorDefault)
	w := srv.World()

	const radius = 12 // 625 chunks; 10,000 of these would be about 800 MB resident
	resident := w.PreGenerateRadius(radius)

	if err := store.World().SaveSnapshot(ctx, server.DefaultWorld, w.Snapshot()); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}

	sections := 0
	w.ForEachChunk(func(_ world.ChunkPos, c *world.Chunk) {
		for _, s := range c.Sections {
			if s != nil {
				sections++
			}
		}
	})

	t.Logf("MEASUREMENT generated world: %s", diskPerChunk(t, dir, resident))
	t.Logf("  %d sections resident, %d bytes of section data in memory (4 bytes per block)",
		sections, sections*world.BlocksPerSection*4)
	t.Logf("  on the wire a section is %d bytes (2 bytes per block), so the in-memory figure the",
		sections*world.BlocksPerSection*2)
	t.Log("  threshold compares against is the wire's, not this process's")
}

// diskPerChunk sums the region files and reports the mean per chunk.
func diskPerChunk(t *testing.T, dir string, chunks int) string {
	t.Helper()

	var total int64
	root := filepath.Join(dir, "world", server.DefaultWorld, "region")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	perChunk := total / int64(max(chunks, 1))

	return formatDiskUsage(total, chunks, perChunk)
}

func formatDiskUsage(total int64, chunks int, perChunk int64) string {
	return fmt.Sprintf("%d bytes across %d chunks, %d bytes per chunk (a section is 8192 bytes on the wire)",
		total, chunks, perChunk)
}

// The M11.5 index measurement.
//
// The framework design flags the index's memory cost on a populated world as
// the risk no test reveals early: it holds every live item at once, and until
// the click paths routed through it there was nothing in it to measure. This
// mints what a busy server holds and reports the bytes per live item.
//
//	M11_MEASURE=1 devbox run -- go test -mod vendor -run TestItemIndexMemory -v ./server/
func TestItemIndexMemory(t *testing.T) {
	measuring(t)

	// A hundred players carrying a full inventory of full stacks: 45 slots of
	// 64 items each, which is the worst case a player can be in.
	const (
		players       = 100
		slots         = 45
		perSlot       = 64
		expectedItems = players * slots * perSlot
	)

	minter, err := world.NewMinter(1)
	if err != nil {
		t.Fatalf("NewMinter: %v", err)
	}

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	index := world.NewItemIndex(minter, world.DuplicateAllow, nil)
	for player := range players {
		uuid := fmt.Sprintf("00000000-0000-0000-0000-%012d", player)
		for slot := range slots {
			at := world.Location{Kind: world.LocationInventory, Player: uuid, Slot: slot}
			if _, err := index.Mint(perSlot, at, world.Actor{}); err != nil {
				t.Fatalf("Mint: %v", err)
			}
		}
	}

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	if index.Len() != expectedItems {
		t.Fatalf("index holds %d IDs, want %d", index.Len(), expectedItems)
	}

	held := after.HeapAlloc - before.HeapAlloc
	t.Logf("%d live items in %d bytes, %d bytes per item",
		index.Len(), held, held/uint64(index.Len()))

	// Keep the index alive past the second reading, or what is being measured
	// is an empty heap.
	runtime.KeepAlive(index)
}
