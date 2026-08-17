package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/go-theft-craft/server/internal/server/storage"
	"github.com/go-theft-craft/server/pkg/world"
	"github.com/go-theft-craft/server/pkg/world/anvil"
)

// StoreBinder is the optional interface a store implements when it needs to
// know the world it is storing.
//
// A store is constructed by the application, before server.New has built the
// block state registry, and a handle minted by another registry means nothing.
// So the server binds every store it is given, once, before the first load or
// save — the same arrangement a generator gets.
type StoreBinder interface {
	BindWorld(w *world.World) error
}

// anvilStore keeps the world in Minecraft's region files.
type anvilStore struct {
	root string
	log  *slog.Logger

	mu    sync.Mutex
	bound bool
	dim   world.Dimension
	codec interface {
		anvil.StateEncoder
		anvil.StateDecoder
	}
	air world.State

	// written is what this store last put on disk, per chunk. It is the
	// store's own bookkeeping and not the world's: a second store with its
	// own idea of what it has written must not share state with the first.
	written map[world.ChunkPos]world.Generation
}

func newAnvilStore(dir string, log *slog.Logger) (*anvilStore, error) {
	root := filepath.Join(dir, "world")
	if err := storage.EnsureDir(root); err != nil {
		return nil, fmt.Errorf("create world directory: %w", err)
	}

	return &anvilStore{
		root:    root,
		log:     log,
		written: make(map[world.ChunkPos]world.Generation),
	}, nil
}

// BindWorld implements StoreBinder.
//
// The codec is the running server's adapter. That is right while this server
// speaks one version, because the Anvil format's numbering *is* protocol 47's.
// A server serving a flattened version would have to hand a 1.8 codec here
// instead, and the parameter is what makes that a change of argument rather
// than a change of store.
func (s *anvilStore) BindWorld(w *world.World) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.dim = w.Dimension()
	s.codec = w.Adapter()
	s.air = w.Air()
	s.bound = true

	return nil
}

func (s *anvilStore) regionDir(name string) string { return filepath.Join(s.root, name, "region") }

func (s *anvilStore) LoadChunk(ctx context.Context, name string, pos world.ChunkPos) (*world.Chunk, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.bound {
		return nil, errors.New("anvil store: not bound to a world")
	}

	rx, rz := anvil.RegionOf(pos)
	region, err := anvil.OpenRegion(s.regionDir(name), rx, rz, s.dim, s.codec, s.air)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, fmt.Errorf("open region %d,%d: %w", rx, rz, err)
	}

	c, present, err := region.Chunk(pos)
	if err != nil {
		return nil, fmt.Errorf("read chunk %v: %w", pos, err)
	}
	if !present {
		return nil, nil
	}

	return c, nil
}

func (s *anvilStore) SaveSnapshot(ctx context.Context, name string, snap world.Snapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.bound {
		return errors.New("anvil store: not bound to a world")
	}

	dir := s.regionDir(name)
	if err := storage.EnsureDir(dir); err != nil {
		return err
	}

	// Only the regions holding a chunk whose generation moved are rewritten.
	dirty := map[[2]int]map[world.ChunkPos]*world.Chunk{}
	for pos, c := range snap.Chunks {
		// A column the store could not read is empty, and writing it back
		// would replace whatever is really there with that emptiness.
		if c.Unreadable {
			continue
		}
		if seen, ok := s.written[pos]; ok && seen == c.Gen {
			continue
		}
		rx, rz := anvil.RegionOf(pos)
		key := [2]int{rx, rz}
		if dirty[key] == nil {
			dirty[key] = map[world.ChunkPos]*world.Chunk{}
		}
		dirty[key][pos] = c
	}

	for key, chunks := range dirty {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.saveRegion(dir, key[0], key[1], chunks); err != nil {
			return err
		}
		for pos, c := range chunks {
			s.written[pos] = c.Gen
		}
	}

	return nil
}

// saveRegion rewrites one region file, carrying forward every column the
// snapshot does not hold. A region has 1,024 of them and a snapshot has only
// the resident ones, so writing just the snapshot would delete the world
// outside the players.
func (s *anvilStore) saveRegion(dir string, rx, rz int, chunks map[world.ChunkPos]*world.Chunk) error {
	payloads := map[world.ChunkPos]anvil.Payload{}

	existing, err := anvil.OpenRegion(dir, rx, rz, s.dim, s.codec, s.air)
	switch {
	case err == nil:
		// Carried through still compressed: the columns the snapshot does not
		// hold are not re-encoded to change the ones it does.
		payloads, err = existing.Payloads()
		if err != nil {
			return fmt.Errorf("read region %d,%d before rewriting it: %w", rx, rz, err)
		}
	case errors.Is(err, os.ErrNotExist):
		// A region nobody has written yet; nothing to carry forward.
	default:
		return fmt.Errorf("open region %d,%d: %w", rx, rz, err)
	}

	for pos, c := range chunks {
		payload, err := anvil.EncodeChunkNBT(c, s.codec)
		if err != nil {
			return fmt.Errorf("encode chunk %v: %w", pos, err)
		}
		payloads[pos] = anvil.Payload{NBT: payload}
	}

	return anvil.SaveRegion(dir, rx, rz, payloads)
}

func (s *anvilStore) levelPath(name string) string {
	return filepath.Join(s.root, name, "level.json")
}

func (s *anvilStore) Level(_ context.Context, name string) (LevelData, bool, error) {
	var data LevelData

	found, err := storage.ReadJSON(s.levelPath(name), &data)
	if err != nil {
		return LevelData{}, false, fmt.Errorf("read level data: %w", err)
	}
	if found {
		return data, true, nil
	}

	// A world saved by the pre-M11.3 server kept age and time in world.json,
	// and its field names are the ones LevelData carries. Reading it here is
	// what makes an existing data directory keep its clock across the change.
	found, err = storage.ReadJSON(filepath.Join(s.root, "world.json"), &data)
	if err != nil {
		return LevelData{}, false, fmt.Errorf("read legacy world data: %w", err)
	}

	return data, found, nil
}

func (s *anvilStore) SaveLevel(_ context.Context, name string, data LevelData) error {
	if err := storage.EnsureDir(filepath.Join(s.root, name)); err != nil {
		return err
	}

	return storage.WriteJSONAtomic(s.levelPath(name), data)
}

func (s *anvilStore) Close() error { return nil }
