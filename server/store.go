package server

import (
	"fmt"
	"log/slog"

	"github.com/go-theft-craft/server/internal/server/storage"
	"github.com/go-theft-craft/server/pkg/world"
)

// Store is world persistence. The server depends on this rather than on a
// concrete type, so an application can supply its own.
//
// It covers the world only. Player persistence still runs through the
// concrete path inside this package, because storage.SavePlayer takes an
// internal type and no external implementer could satisfy a method naming it.
// M11.3 folds player data in when the player model has a public shape.
//
// SaveWorldAnvil names a format, which a seam should not. It is here because
// it is what the server calls today, and inventing the version-neutral
// WorldStore now would be designing M11.3 without its research.
type Store interface {
	HasSavedWorld() bool
	LoadWorld(w *world.World) error
	SaveWorld(w *world.World) error
	LoadBlockOverrides(w *world.World) error
	SaveBlockOverrides(w *world.World) error
	LoadChests(w *world.World) error
	SaveChests(w *world.World) error
	SaveWorldAnvil(w *world.World) error
}

// WithStore supplies persistence. Omit the option entirely to run without it;
// a nil store is an error rather than a silent no-op, because "I passed a
// store and nothing was saved" is the harder failure to diagnose.
func WithStore(store Store) Option {
	return func(b *builder) error {
		if store == nil {
			return fmt.Errorf("%w: nil store, omit WithStore to run without persistence", ErrInvalidOption)
		}
		b.store = store

		return nil
	}
}

// FileStore returns the framework's default store, which keeps config,
// world, and player data as files under dir.
//
// It exists so an application in another module can use the default without
// importing an internal package. A nil logger is replaced with a discarding
// one.
func FileStore(dir string, log *slog.Logger) (Store, error) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	store, err := storage.New(dir, log)
	if err != nil {
		return nil, fmt.Errorf("create file store: %w", err)
	}

	return store, nil
}
