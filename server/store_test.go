package server_test

import (
	"context"
	"errors"
	"testing"

	"github.com/go-theft-craft/server/pkg/world"
	"github.com/go-theft-craft/server/server"
)

// recordingWorldStore is what an application outside this module writes. It
// compiles only if WorldStore names no internal type, which is the property
// the split seam exists to create.
type recordingWorldStore struct {
	level    server.LevelData
	hasLevel bool

	loaded []world.ChunkPos
	saved  int

	chunks   map[world.ChunkPos]*world.Chunk
	failLoad error
}

func (s *recordingWorldStore) LoadChunk(_ context.Context, _ string, pos world.ChunkPos) (*world.Chunk, error) {
	s.loaded = append(s.loaded, pos)
	if s.failLoad != nil {
		return nil, s.failLoad
	}

	return s.chunks[pos], nil
}

func (s *recordingWorldStore) SaveSnapshot(_ context.Context, _ string, snap world.Snapshot) error {
	s.saved += len(snap.Chunks)

	return nil
}

func (s *recordingWorldStore) Level(context.Context, string) (server.LevelData, bool, error) {
	return s.level, s.hasLevel, nil
}

func (s *recordingWorldStore) SaveLevel(_ context.Context, _ string, data server.LevelData) error {
	s.level, s.hasLevel = data, true

	return nil
}

func (s *recordingWorldStore) Close() error { return nil }

// TestAServerWithNoStoresIsValid: the interoperability lane and the minimal
// example both run without persistence, so no store is a supported
// configuration rather than an oversight.
func TestAServerWithNoStoresIsValid(t *testing.T) {
	srv, err := server.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if srv.WorldStore() != nil {
		t.Error("New invented a store")
	}
}

func TestTheStoreOptionsRejectNil(t *testing.T) {
	if _, err := server.New(server.WithWorldStore(nil)); !errors.Is(err, server.ErrInvalidOption) {
		t.Error("WithWorldStore accepted nil; use no option at all to run without persistence")
	}
	if _, err := server.New(server.WithSideStore(nil)); !errors.Is(err, server.ErrInvalidOption) {
		t.Error("WithSideStore accepted nil")
	}
	if _, err := server.New(server.WithPlayerStore(nil)); !errors.Is(err, server.ErrInvalidOption) {
		t.Error("WithPlayerStore accepted nil")
	}
}

func TestAnExternalTypeSatisfiesWorldStore(t *testing.T) {
	store := &recordingWorldStore{}

	srv, err := server.New(server.WithWorldStore(store))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if srv.WorldStore() != store {
		t.Error("New did not use the supplied store")
	}
}

// TestLoadChunkAbsentMeansGenerate is one half of the distinction the whole
// load path rests on.
func TestLoadChunkAbsentMeansGenerate(t *testing.T) {
	store := &recordingWorldStore{chunks: map[world.ChunkPos]*world.Chunk{}}

	srv, err := server.New(server.WithWorldStore(store))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	w := srv.World()
	if got := w.Block(world.BlockPos{X: 0, Y: 0, Z: 0}); got == w.Air() {
		t.Fatal("an absent chunk did not generate: bedrock is missing at y=0")
	}
	if len(store.loaded) == 0 {
		t.Fatal("the world generated without asking the store first")
	}
}

// TestLoadChunkErrorDoesNotRegenerate is the other half, and the one that
// protects a world from a disk fault: an error must not fall through to
// generation, because a world that quietly regenerates looks like a world that
// was deleted.
func TestLoadChunkErrorDoesNotRegenerate(t *testing.T) {
	store := &recordingWorldStore{failLoad: errors.New("disk on fire")}

	srv, err := server.New(server.WithWorldStore(store))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	w := srv.World()
	if got := w.Block(world.BlockPos{X: 0, Y: 0, Z: 0}); got != w.Air() {
		t.Fatalf("a chunk the store could not read generated terrain: block %d at y=0", got)
	}
	if w.GenerationError() == nil {
		t.Fatal("a failed load left no error behind")
	}
	if c := w.Chunk(world.ChunkPos{}); !c.Unreadable {
		t.Fatal("a chunk the store could not read is not marked unreadable")
	}
}

func TestFileStoreProvidesAllThreeHalves(t *testing.T) {
	store, err := server.FileStore(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("FileStore: %v", err)
	}
	if store.World() == nil || store.Side() == nil || store.Players() == nil {
		t.Fatal("FileStore left one of the three nil")
	}
	if len(store.Options()) != 4 {
		t.Fatalf("Options returned %d options, want 4 (three stores and the legacy migration)",
			len(store.Options()))
	}

	srv, err := server.New(store.Options()...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if srv.WorldStore() != store.World() {
		t.Error("New did not use the supplied world store")
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
