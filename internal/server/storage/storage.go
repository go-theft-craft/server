package storage

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/OCharnyshevich/minecraft-server/internal/server/config"
	"github.com/OCharnyshevich/minecraft-server/internal/server/player"
	"github.com/OCharnyshevich/minecraft-server/internal/server/world"
	"github.com/OCharnyshevich/minecraft-server/internal/server/world/anvil"
	"github.com/OCharnyshevich/minecraft-server/internal/server/world/gen"
)

// Storage handles file-based persistence for config, world, and player data.
type Storage struct {
	dir string
	log *slog.Logger
}

// New creates a new Storage rooted at dir, creating subdirectories as needed.
func New(dir string, log *slog.Logger) (*Storage, error) {
	dirs := []string{
		dir,
		filepath.Join(dir, "world"),
		filepath.Join(dir, "players"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, fmt.Errorf("create directory %s: %w", d, err)
		}
	}
	return &Storage{dir: dir, log: log}, nil
}

// LoadConfig reads config.json into cfg. If the file does not exist, cfg is unchanged.
func (s *Storage) LoadConfig(cfg *config.Config) error {
	path := filepath.Join(s.dir, "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read config: %w", err)
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	s.log.Info("loaded config from file", "path", path)
	return nil
}

// SaveConfig writes cfg to config.json atomically.
func (s *Storage) SaveConfig(cfg *config.Config) error {
	path := filepath.Join(s.dir, "config.json")
	return s.atomicWriteJSON(path, cfg)
}

// LoadWorld reads world.json and restores world-level state (time).
func (s *Storage) LoadWorld(w *world.World) error {
	path := filepath.Join(s.dir, "world", "world.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read world data: %w", err)
	}

	var wd WorldData
	if err := json.Unmarshal(data, &wd); err != nil {
		return fmt.Errorf("parse world data: %w", err)
	}

	w.SetTime(wd.Age, wd.TimeOfDay)
	s.log.Info("loaded world data", "age", wd.Age, "timeOfDay", wd.TimeOfDay)
	return nil
}

// SaveWorld writes world-level state (time) to world.json atomically.
func (s *Storage) SaveWorld(w *world.World) error {
	age, timeOfDay := w.GetTime()
	wd := WorldData{
		Age:       age,
		TimeOfDay: timeOfDay,
	}

	path := filepath.Join(s.dir, "world", "world.json")
	return s.atomicWriteJSON(path, &wd)
}

// HasSavedWorld returns true if the world was previously saved to disk.
func (s *Storage) HasSavedWorld() bool {
	path := filepath.Join(s.dir, "world", "world.json")
	_, err := os.Stat(path)
	return err == nil
}

// SaveBlockOverrides writes the block overrides map to world/overrides.json.
func (s *Storage) SaveBlockOverrides(w *world.World) error {
	overrides := w.GetBlockOverrides()
	entries := make([]BlockOverrideEntry, 0, len(overrides))
	for pos, stateID := range overrides {
		entries = append(entries, BlockOverrideEntry{
			X: pos.X, Y: pos.Y, Z: pos.Z, StateID: stateID,
		})
	}

	path := filepath.Join(s.dir, "world", "overrides.json")
	return s.atomicWriteJSON(path, entries)
}

// LoadBlockOverrides reads world/overrides.json and restores block overrides.
func (s *Storage) LoadBlockOverrides(w *world.World) error {
	path := filepath.Join(s.dir, "world", "overrides.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read block overrides: %w", err)
	}

	var entries []BlockOverrideEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("parse block overrides: %w", err)
	}

	overrides := make(map[world.BlockPos]int32, len(entries))
	for _, e := range entries {
		overrides[world.BlockPos{X: e.X, Y: e.Y, Z: e.Z}] = e.StateID
	}

	w.SetBlockOverrides(overrides)
	s.log.Info("loaded block overrides", "count", len(overrides))
	return nil
}

// SaveWorldAnvil writes the world in Minecraft's Anvil region file format (.mca).
func (s *Storage) SaveWorldAnvil(w *world.World) error {
	regionDir := filepath.Join(s.dir, "world", "region")
	if err := os.MkdirAll(regionDir, 0o755); err != nil {
		return fmt.Errorf("create region dir: %w", err)
	}

	// First pass: collect chunk positions under a single read lock.
	// We must NOT call OverridesForChunk inside ForEachChunk — both acquire
	// w.mu.RLock, and a pending w.mu.Lock (from the tick loop) would cause
	// the second RLock to deadlock.
	type chunkEntry struct {
		pos   gen.ChunkPos
		chunk *gen.ChunkData
	}
	var chunks []chunkEntry
	w.ForEachChunk(func(pos gen.ChunkPos, chunk *gen.ChunkData) {
		chunks = append(chunks, chunkEntry{pos, chunk})
	})

	// Second pass: encode each chunk with its overrides (locks acquired sequentially).
	type regionKey struct{ rx, rz int }
	regions := make(map[regionKey]map[gen.ChunkPos][]byte)

	for _, ce := range chunks {
		overrides := w.OverridesForChunk(ce.pos.X, ce.pos.Z)

		nbtData, err := anvil.EncodeChunkNBT(ce.pos.X, ce.pos.Z, ce.chunk, overrides)
		if err != nil {
			s.log.Error("encode chunk NBT", "cx", ce.pos.X, "cz", ce.pos.Z, "error", err)
			continue
		}

		rk := regionKey{rx: ce.pos.X >> 5, rz: ce.pos.Z >> 5}
		if regions[rk] == nil {
			regions[rk] = make(map[gen.ChunkPos][]byte)
		}
		regions[rk][ce.pos] = nbtData
	}

	for rk, chunks := range regions {
		if err := anvil.SaveRegion(regionDir, rk.rx, rk.rz, chunks); err != nil {
			s.log.Error("save region", "rx", rk.rx, "rz", rk.rz, "error", err)
			return err
		}
	}

	return nil
}

// LoadPlayer reads players/<uuid>.json and returns the data, or nil if not found.
func (s *Storage) LoadPlayer(uuid string) (*PlayerData, error) {
	path := filepath.Join(s.dir, "players", uuid+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read player %s: %w", uuid, err)
	}

	var pd PlayerData
	if err := json.Unmarshal(data, &pd); err != nil {
		return nil, fmt.Errorf("parse player %s: %w", uuid, err)
	}
	return &pd, nil
}

// SavePlayer persists the current state of a player to disk.
func (s *Storage) SavePlayer(p *player.Player) error {
	pd := PlayerDataFromPlayer(p)
	path := filepath.Join(s.dir, "players", p.UUID+".json")
	return s.atomicWriteJSON(path, pd)
}

// atomicWriteJSON marshals v to JSON and writes it atomically using a temp file + rename.
func (s *Storage) atomicWriteJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}
	data = append(data, '\n')

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}
