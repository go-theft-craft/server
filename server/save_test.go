package server_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/go-theft-craft/server/pkg/world"
	"github.com/go-theft-craft/server/server"
)

// TestAnInterruptedSaveLeavesThePreviousRegionReadable is the crash-safety
// argument: a region is written to a temporary file in the same directory,
// synced, and renamed, so the file under the real name is always one whole
// version or the other.
func TestAnInterruptedSaveLeavesThePreviousRegionReadable(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	srv, store := newStoredServer(t, dir)
	w := srv.World()
	cobble := w.Registry().Intern("minecraft:cobblestone", nil)

	w.SetBlock(world.BlockPos{X: 1, Y: 100, Z: 1}, cobble)
	if err := store.World().SaveSnapshot(ctx, server.DefaultWorld, w.Snapshot()); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}

	regionPath := filepath.Join(dir, "world", server.DefaultWorld, "region", "r.0.0.mca")
	complete, err := os.ReadFile(regionPath)
	if err != nil {
		t.Fatalf("read region: %v", err)
	}

	// A save that died between the temporary write and the rename: the
	// temporary file is there, truncated, and the real one is untouched.
	if err := os.WriteFile(regionPath+".tmp", complete[:len(complete)/3], 0o644); err != nil {
		t.Fatalf("write partial temp: %v", err)
	}

	reopened, _ := newStoredServer(t, dir)
	if got := reopened.World().Block(world.BlockPos{X: 1, Y: 100, Z: 1}); got != cobble {
		t.Fatalf("the block came back as %d after an interrupted save, want cobblestone", got)
	}
	if reopened.World().GenerationError() != nil {
		t.Fatalf("loading after an interrupted save reported %v", reopened.World().GenerationError())
	}
}

// TestSaveUnderSustainedWritesLosesNothing is what "the save does not touch
// the tick" means in a form a test can assert.
//
// A timing assertion — that the tick period does not move — measures the
// machine as much as the code and fails on a busy CI runner. The property
// underneath it is deterministic: a snapshot is a map copy of immutable chunk
// pointers, so writers never wait for a save and a save never sees a
// half-written world. This runs both at once and checks that every write
// landed and the save succeeded.
func TestSaveUnderSustainedWritesLosesNothing(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	srv, store := newStoredServer(t, dir)
	w := srv.World()
	cobble := w.Registry().Intern("minecraft:cobblestone", nil)
	w.PreGenerateRadius(2)

	const writers, perWriter = 4, 250

	var wg sync.WaitGroup
	for g := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range perWriter {
				n := g*perWriter + i
				w.SetBlock(world.BlockPos{X: n % 16, Y: 100 + n/256, Z: (n / 16) % 16}, cobble)
			}
		}()
	}

	saveErr := make(chan error, 1)
	go func() {
		saveErr <- store.World().SaveSnapshot(ctx, server.DefaultWorld, w.Snapshot())
	}()

	wg.Wait()
	if err := <-saveErr; err != nil {
		t.Fatalf("SaveSnapshot under load: %v", err)
	}

	for n := range writers * perWriter {
		pos := world.BlockPos{X: n % 16, Y: 100 + n/256, Z: (n / 16) % 16}
		if got := w.Block(pos); got != cobble {
			t.Fatalf("write %d at %v was lost during a save: read %d", n, pos, got)
		}
	}

	// And the world is still whole on disk once the writes have stopped.
	if err := store.World().SaveSnapshot(ctx, server.DefaultWorld, w.Snapshot()); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}
	reopened, _ := newStoredServer(t, dir)
	if got := reopened.World().Block(world.BlockPos{X: 0, Y: 100, Z: 0}); got != cobble {
		t.Fatalf("a block written during a save came back as %d", got)
	}
}
