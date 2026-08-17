package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/go-theft-craft/server/pkg/world"
)

// Persistence.
//
// One store named a format — M11.1's Store had SaveWorldAnvil on it — and one
// store could not express player data at all, because saving a player named an
// internal type. Both are fixed by splitting the seam in three and giving each
// half a public value type:
//
//   - WorldStore holds what vanilla has fields for: blocks, biomes, and tile
//     entities. Its format is a detail; its interface names none.
//   - SideStore holds what vanilla has no field for. It is written from the
//     same snapshot as the world and stamped with the same generation, so a
//     mismatched pair is detected at load rather than trusted.
//   - PlayerStore holds per-player state as PlayerData.
//
// Each is a separate option, so an application can replace one and keep the
// defaults for the other two.

// DefaultWorld is the world name the server passes when it has only one.
// The parameter exists so a second dimension is not a signature change.
const DefaultWorld = "overworld"

// SidecarVersion is the sidecar format this build writes.
const SidecarVersion = 1

// WorldStore holds the vanilla world: blocks, biomes, and the tile entities
// the vanilla format has fields for.
type WorldStore interface {
	// LoadChunk returns the stored column, or nil for a position nothing has
	// been written to yet.
	//
	// nil, nil means "generate one". An error means the store failed, and the
	// server must not silently regenerate over data it could not read: a world
	// that quietly regenerates on a disk error looks like a world that was
	// deleted.
	LoadChunk(ctx context.Context, name string, pos world.ChunkPos) (*world.Chunk, error)
	// SaveSnapshot writes every chunk in the snapshot that has moved since
	// the store last wrote it.
	SaveSnapshot(ctx context.Context, name string, snap world.Snapshot) error
	// Level reports false for a world that has never been saved.
	Level(ctx context.Context, name string) (LevelData, bool, error)
	SaveLevel(ctx context.Context, name string, data LevelData) error
	Close() error
}

// SideStore holds what the vanilla format has no field for.
type SideStore interface {
	SaveSnapshot(ctx context.Context, name string, snap world.Snapshot) error
	// Load returns the sidecar written for a chunk, and reports whether one
	// was there. The generation the caller passes is the world's, so an
	// implementation can report a stamp mismatch rather than hide it.
	Load(ctx context.Context, name string, pos world.ChunkPos, gen world.Generation) (Sidecar, bool, error)
	Close() error
}

// LevelData is world-level metadata: what vanilla keeps in level.dat.
//
// The age and time-of-day tags are the ones world.json used, so an existing
// file still loads.
type LevelData struct {
	Age       int64 `json:"age"`
	TimeOfDay int64 `json:"time_of_day"`
	Seed      int64 `json:"seed,omitempty"`

	// GeneratorName, GeneratorVersion, and GeneratorParams are what generated
	// this world. They are the world's record, not the configuration's: when
	// the two disagree the world's name wins, because superflat's grass plane
	// growing mountains at the edge of what has been explored is not a thing
	// anyone asked for.
	GeneratorName    string          `json:"generator_name,omitempty"`
	GeneratorVersion int             `json:"generator_version,omitempty"`
	GeneratorParams  json.RawMessage `json:"generator_params,omitempty"`

	// ItemEpoch is the last run epoch this world handed out item IDs from.
	// It is persisted rather than derived from the clock: a clock that moves
	// backwards would mint colliding IDs, and collision is the one failure
	// that makes item identity worthless. See server/itemid.go.
	ItemEpoch uint32 `json:"item_epoch,omitempty"`
}

// Sidecar is the per-chunk record of everything vanilla cannot hold.
//
// In this milestone it carries nothing but its own version and the generation
// it was written at. The container is written empty on purpose: M11.5 adds
// contents to it rather than adding a format.
type Sidecar struct {
	Version    int              `json:"version"`
	Generation world.Generation `json:"generation"`
	// BlockIdentity maps a chunk-local block index to the identity M11.5
	// gives it. Empty here.
	BlockIdentity map[string]string `json:"block_identity,omitempty"`
}

// WithWorldStore supplies world persistence. Omit it to run without;
// a nil store is an error rather than a silent no-op.
func WithWorldStore(store WorldStore) Option {
	return func(b *builder) error {
		if store == nil {
			return fmt.Errorf("%w: nil world store, omit WithWorldStore to run without one", ErrInvalidOption)
		}
		b.worldStore = store

		return nil
	}
}

// WithSideStore supplies the sidecar. Omit it to run without one.
func WithSideStore(store SideStore) Option {
	return func(b *builder) error {
		if store == nil {
			return fmt.Errorf("%w: nil side store, omit WithSideStore to run without one", ErrInvalidOption)
		}
		b.sideStore = store

		return nil
	}
}

// WithLegacyMigration folds the JSON world files a pre-M11.3 server wrote into
// the world at startup, then renames them to *.migrated.
//
// FileStore turns it on for its own directory, so an application that used the
// framework's default persistence gets the migration without asking. An
// application with its own stores passes its data directory here, or does not,
// and nothing is read.
func WithLegacyMigration(dir string) Option {
	return func(b *builder) error {
		if dir == "" {
			return fmt.Errorf("%w: empty migration directory", ErrInvalidOption)
		}
		b.migrateFrom = dir

		return nil
	}
}

// Storage is the framework's default persistence: Anvil regions for the world,
// a chunk-keyed sidecar beside them, and one JSON file per player.
type Storage struct {
	dir     string
	world   WorldStore
	side    SideStore
	players PlayerStore
}

// World, Side, and Players are the three halves, for an application that wants
// to keep two of the defaults and replace the third.
func (s *Storage) World() WorldStore    { return s.world }
func (s *Storage) Side() SideStore      { return s.side }
func (s *Storage) Players() PlayerStore { return s.players }

// Options returns the options that install this storage, so the common case is
// one line at the call site. The legacy migration is among them: an
// application using the framework's own persistence has the old files to fold
// in by definition.
func (s *Storage) Options() []Option {
	return []Option{
		WithWorldStore(s.world),
		WithSideStore(s.side),
		WithPlayerStore(s.players),
		WithLegacyMigration(s.dir),
	}
}

// Close shuts all three down, returning the first failure.
func (s *Storage) Close() error {
	var first error
	for _, closer := range []interface{ Close() error }{s.world, s.side, s.players} {
		if err := closer.Close(); err != nil && first == nil {
			first = err
		}
	}

	return first
}

// FileStore returns the framework's default storage, rooted at dir.
//
// A nil logger is replaced with a discarding one.
func FileStore(dir string, log *slog.Logger) (*Storage, error) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	worldStore, err := newAnvilStore(dir, log)
	if err != nil {
		return nil, err
	}
	sideStore, err := newSidecarStore(dir)
	if err != nil {
		return nil, err
	}
	playerStore, err := FilePlayerStore(dir)
	if err != nil {
		return nil, err
	}

	return &Storage{dir: dir, world: worldStore, side: sideStore, players: playerStore}, nil
}
