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

// LoadWorld reads overrides.json and bulk-loads block overrides into the world.
func (s *Storage) LoadWorld(w *world.World) error {
	path := filepath.Join(s.dir, "world", "overrides.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read world overrides: %w", err)
	}

	var wd WorldData
	if err := json.Unmarshal(data, &wd); err != nil {
		return fmt.Errorf("parse world overrides: %w", err)
	}

	overrides := make(map[world.BlockPos]int32, len(wd.Overrides))
	for _, o := range wd.Overrides {
		overrides[world.BlockPos{X: o.X, Y: o.Y, Z: o.Z}] = o.StateID
	}

	w.LoadOverrides(overrides)
	s.log.Info("loaded world overrides", "count", len(overrides))
	return nil
}

// SaveWorld writes all block overrides to overrides.json atomically.
func (s *Storage) SaveWorld(w *world.World) error {
	var wd WorldData
	w.ForEachOverride(func(pos world.BlockPos, stateID int32) {
		wd.Overrides = append(wd.Overrides, BlockOverride{
			X: pos.X, Y: pos.Y, Z: pos.Z, StateID: stateID,
		})
	})

	path := filepath.Join(s.dir, "world", "overrides.json")
	return s.atomicWriteJSON(path, &wd)
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
