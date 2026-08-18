package server

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/go-theft-craft/server/internal/server/storage"
	"github.com/go-theft-craft/server/pkg/world"
)

// sidecarStore keeps one file per region, chunk-keyed inside.
//
// M11.3 wrote the container empty so that M11.5 could add contents to a format
// people already had rather than introduce one alongside it. What it holds now
// is whatever the SidecarSource hands back per chunk, which today is block
// identity and nothing else.
type sidecarStore struct {
	root string

	mu      sync.Mutex
	source  SidecarSource
	written map[world.ChunkPos]world.Generation
}

// SetSidecarSource gives the store somewhere to ask what a chunk's sidecar
// holds. Without one it writes generation stamps and nothing else, which is
// what M11.3 shipped.
func (s *sidecarStore) SetSidecarSource(src SidecarSource) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.source = src
}

// regionSidecar is one region's file.
type regionSidecar struct {
	Version int                       `json:"version"`
	Chunks  map[string]Sidecar        `json:"chunks"`
	Extra   map[string]map[string]any `json:"extra,omitempty"`
}

func newSidecarStore(dir string) (*sidecarStore, error) {
	root := filepath.Join(dir, "world")
	if err := storage.EnsureDir(root); err != nil {
		return nil, fmt.Errorf("create world directory: %w", err)
	}

	return &sidecarStore{root: root, written: make(map[world.ChunkPos]world.Generation)}, nil
}

func (s *sidecarStore) path(name string, rx, rz int) string {
	return filepath.Join(s.root, name, "sidecar", fmt.Sprintf("s.%d.%d.json", rx, rz))
}

// chunkKey is how a chunk is named inside a region's file.
func chunkKey(pos world.ChunkPos) string { return fmt.Sprintf("%d,%d", pos.X, pos.Z) }

func (s *sidecarStore) SaveSnapshot(ctx context.Context, name string, snap world.Snapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dirty := map[[2]int]map[world.ChunkPos]world.Generation{}
	for pos, c := range snap.Chunks {
		if c.Unreadable {
			continue
		}
		if seen, ok := s.written[pos]; ok && seen == c.Gen {
			continue
		}
		key := [2]int{pos.X >> 5, pos.Z >> 5}
		if dirty[key] == nil {
			dirty[key] = map[world.ChunkPos]world.Generation{}
		}
		dirty[key][pos] = c.Gen
	}

	for key, chunks := range dirty {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.saveRegion(name, key[0], key[1], chunks); err != nil {
			return err
		}
		for pos, gen := range chunks {
			s.written[pos] = gen
		}
	}

	return nil
}

func (s *sidecarStore) saveRegion(name string, rx, rz int, chunks map[world.ChunkPos]world.Generation) error {
	path := s.path(name, rx, rz)
	if err := storage.EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}

	file := regionSidecar{Version: SidecarVersion, Chunks: map[string]Sidecar{}}
	if _, err := storage.ReadJSON(path, &file); err != nil {
		return fmt.Errorf("read sidecar %d,%d: %w", rx, rz, err)
	}
	if file.Chunks == nil {
		file.Chunks = map[string]Sidecar{}
	}
	file.Version = SidecarVersion

	for pos, gen := range chunks {
		entry := file.Chunks[chunkKey(pos)]
		entry.Version = SidecarVersion
		entry.Generation = gen
		if s.source != nil {
			entry.BlockIdentity = s.source(pos).BlockIdentity
		}
		file.Chunks[chunkKey(pos)] = entry
	}

	return storage.WriteJSONAtomic(path, file)
}

func (s *sidecarStore) Load(_ context.Context, name string, pos world.ChunkPos, gen world.Generation) (Sidecar, bool, error) {
	var file regionSidecar

	found, err := storage.ReadJSON(s.path(name, pos.X>>5, pos.Z>>5), &file)
	if err != nil {
		return Sidecar{}, false, fmt.Errorf("read sidecar for %v: %w", pos, err)
	}
	if !found {
		return Sidecar{}, false, nil
	}

	entry, ok := file.Chunks[chunkKey(pos)]
	if !ok {
		return Sidecar{}, false, nil
	}
	if entry.Generation != gen {
		// Reported, not absorbed. A sidecar older than the world is the
		// crash-between-writes case; a sidecar newer than it means something
		// outside this server rewrote the region file. Both are the caller's
		// to reconcile, and M11.5 gives that a durable destination.
		return entry, true, fmt.Errorf("sidecar for %v is generation %d, world is %d",
			pos, entry.Generation, gen)
	}

	return entry, true, nil
}

func (s *sidecarStore) Close() error { return nil }
