package server_test

import (
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-theft-craft/server/config"
	"github.com/go-theft-craft/server/pkg/world"
	"github.com/go-theft-craft/server/server"
)

// newStoredServer builds a server whose world is backed by the file store in
// dir, which is what an application gets from FileStore.
func newStoredServer(t *testing.T, dir string) (*server.Server, *server.Storage) {
	t.Helper()

	settings := config.DefaultConfig()
	settings.GeneratorType = config.GeneratorFlat

	store, err := server.FileStore(dir, nil)
	if err != nil {
		t.Fatalf("FileStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	opts := append(store.Options(), server.WithSettings(settings))
	srv, err := server.New(opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return srv, store
}

// TestTheServerReadsTheWorldItWrites is the milestone in one test: place a
// block, save, throw the world away, and find the block still there.
func TestTheServerReadsTheWorldItWrites(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	first, store := newStoredServer(t, dir)
	w := first.World()
	cobble := w.Registry().Intern("minecraft:cobblestone", nil)

	const x, y, z = 5, 130, 5
	if !w.SetBlock(world.BlockPos{X: x, Y: y, Z: z}, cobble) {
		t.Fatal("placing a block above the terrain changed nothing")
	}
	// A block broken out of the flat terrain too, so the save has to carry an
	// absence as well as a presence.
	if !w.SetBlock(world.BlockPos{X: 1, Y: 4, Z: 1}, w.Air()) {
		t.Fatal("breaking the surface changed nothing")
	}

	if err := store.World().SaveSnapshot(ctx, server.DefaultWorld, w.Snapshot()); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}

	second, _ := newStoredServer(t, dir)
	reloaded := second.World()

	if got := reloaded.Block(world.BlockPos{X: x, Y: y, Z: z}); got != cobble {
		t.Errorf("the placed block came back as %d, want cobblestone", got)
	}
	if got := reloaded.Block(world.BlockPos{X: 1, Y: 4, Z: 1}); got != reloaded.Air() {
		t.Errorf("the broken block came back as %d, want air", got)
	}
	// Everything the player did not touch is still the terrain.
	if got := reloaded.Block(world.BlockPos{X: 2, Y: 4, Z: 2}); got != reloaded.Registry().Intern("minecraft:grass", nil) {
		t.Errorf("untouched terrain came back as %d, want grass", got)
	}
	if reloaded.GenerationError() != nil {
		t.Errorf("loading reported %v", reloaded.GenerationError())
	}
}

// TestSavingCarriesForwardChunksTheSnapshotDoesNotHold is the failure mode of
// rewriting a region from a snapshot: a region holds 1,024 columns and the
// snapshot holds only the resident ones.
func TestSavingCarriesForwardChunksTheSnapshotDoesNotHold(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	first, store := newStoredServer(t, dir)
	w := first.World()
	cobble := w.Registry().Intern("minecraft:cobblestone", nil)

	// Two chunks in the same region, saved in two separate passes.
	w.SetBlock(world.BlockPos{X: 1, Y: 100, Z: 1}, cobble)
	if err := store.World().SaveSnapshot(ctx, server.DefaultWorld, w.Snapshot()); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}

	second, secondStore := newStoredServer(t, dir)
	w2 := second.World()
	w2.SetBlock(world.BlockPos{X: 200, Y: 100, Z: 200}, cobble)
	if err := secondStore.World().SaveSnapshot(ctx, server.DefaultWorld, w2.Snapshot()); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}

	third, _ := newStoredServer(t, dir)
	w3 := third.World()
	for _, pos := range []world.BlockPos{{X: 1, Y: 100, Z: 1}, {X: 200, Y: 100, Z: 200}} {
		if got := w3.Block(pos); got != cobble {
			t.Errorf("block at %v came back as %d, want cobblestone", pos, got)
		}
	}
}

// TestAnAutosaveWithNoEditsWritesNoBytes: the store tracks what it last wrote
// per chunk, so a save with nothing changed touches no file.
func TestAnAutosaveWithNoEditsWritesNoBytes(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	srv, store := newStoredServer(t, dir)
	w := srv.World()
	w.PreGenerateRadius(1)

	if err := store.World().SaveSnapshot(ctx, server.DefaultWorld, w.Snapshot()); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}
	before := hashTree(t, dir)

	if err := store.World().SaveSnapshot(ctx, server.DefaultWorld, w.Snapshot()); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}
	if after := hashTree(t, dir); after != before {
		t.Fatal("a second save with no edits rewrote the world")
	}

	// One edit, and exactly the region holding it moves.
	w.SetBlock(world.BlockPos{X: 0, Y: 100, Z: 0}, w.Registry().Intern("minecraft:cobblestone", nil))
	if err := store.World().SaveSnapshot(ctx, server.DefaultWorld, w.Snapshot()); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}
	if hashTree(t, dir) == before {
		t.Fatal("a save after an edit wrote nothing")
	}
}

// hashTree digests every file under dir, so a test can say "nothing changed"
// without knowing which files there are.
func hashTree(t *testing.T, dir string) string {
	t.Helper()

	h := sha256.New()
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		h.Write([]byte(path))
		h.Write(data)

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}

	return string(h.Sum(nil))
}
