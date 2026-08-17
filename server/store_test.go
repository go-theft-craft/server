package server_test

import (
	"errors"
	"testing"

	"github.com/go-theft-craft/server/pkg/world"
	"github.com/go-theft-craft/server/server"
)

// recordingStore records which methods a server called, so a test can assert
// on the persistence contract without touching a disk.
type recordingStore struct {
	loadedWorld     bool
	loadedOverrides bool
	savedWorld      bool
	hasSaved        bool
	failLoad        error
}

func (s *recordingStore) HasSavedWorld() bool { return s.hasSaved }

func (s *recordingStore) LoadWorld(*world.World) error {
	s.loadedWorld = true

	return s.failLoad
}

func (s *recordingStore) SaveWorld(*world.World) error {
	s.savedWorld = true

	return nil
}

func (s *recordingStore) LoadBlockOverrides(*world.World) error {
	s.loadedOverrides = true

	return nil
}

func (s *recordingStore) SaveBlockOverrides(*world.World) error { return nil }

func (s *recordingStore) LoadChests(*world.World) error { return nil }

func (s *recordingStore) SaveChests(*world.World) error { return nil }

func (s *recordingStore) SaveWorldAnvil(*world.World) error { return nil }

func TestAServerWithNoStoreIsValid(t *testing.T) {
	// The interoperability lane and the minimal example both run without
	// persistence, so no store is a supported configuration rather than an
	// oversight.
	srv, err := server.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if srv.Store() != nil {
		t.Error("New invented a store")
	}
}

func TestWithStoreRejectsNil(t *testing.T) {
	if _, err := server.New(server.WithStore(nil)); !errors.Is(err, server.ErrInvalidOption) {
		t.Error("WithStore accepted nil; use no option at all to run without persistence")
	}
}

func TestAnExternalTypeSatisfiesTheStoreSeam(t *testing.T) {
	// This is the property the milestone exists to create: a type declared
	// outside the server package, implementing only exported types, is a
	// valid store.
	var store server.Store = &recordingStore{}

	srv, err := server.New(server.WithStore(store))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if srv.Store() != store {
		t.Error("New did not use the supplied store")
	}
}

func TestFileStoreSatisfiesTheSeam(t *testing.T) {
	store, err := server.FileStore(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("FileStore: %v", err)
	}
	if store == nil {
		t.Fatal("FileStore returned a nil store and no error")
	}
}
