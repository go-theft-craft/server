package storage

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"

	"github.com/go-theft-craft/server/config"
	"github.com/go-theft-craft/server/internal/server/player"
	"github.com/go-theft-craft/server/pkg/world"
	"github.com/go-theft-craft/server/pkg/world/anvil"
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
	entries, err := extractOverrides(w)
	if err != nil {
		return err
	}

	path := filepath.Join(s.dir, "world", "overrides.json")

	return s.atomicWriteJSON(path, entries)
}

// extractOverrides recovers the override list by diffing each resident chunk
// against what the generator produces for that position.
//
// It is O(resident chunks × 65,536) and it is temporary. The cost is real: a
// 500-radius pre-generated world is a million chunks and this diff would be
// unusable on it. It is acceptable for one milestone because SaveWorldAnvil
// already walks every resident chunk on every autosave, so the save path was
// already proportional to the resident world.
//
// DELETE IN M11.3, together with foldOverrides and overrides.json itself.
func extractOverrides(w *world.World) ([]BlockOverrideEntry, error) {
	dim := w.Dimension()
	adapter := w.Adapter()
	air := w.Air()

	var entries []BlockOverrideEntry
	var walkErr error

	w.ForEachChunk(func(pos world.ChunkPos, chunk *world.Chunk) {
		if walkErr != nil {
			return
		}
		pristine := w.Regenerate(pos)
		for section := range dim.Sections() {
			live, base := chunk.Sections[section], pristine.Sections[section]
			if live == base {
				continue
			}
			for index := range world.BlocksPerSection {
				got, want := live.At(index), base.At(index)
				if live == nil {
					got = air
				}
				if base == nil {
					want = air
				}
				if got == want {
					continue
				}
				stateID, err := adapter.EncodeState(got)
				if err != nil {
					walkErr = err

					return
				}
				entries = append(entries, BlockOverrideEntry{
					X:       pos.X*16 + index%16,
					Y:       dim.MinY + section*16 + index/256,
					Z:       pos.Z*16 + (index/16)%16,
					StateID: stateID,
				})
			}
		}
	})
	if walkErr != nil {
		return nil, fmt.Errorf("extract block overrides: %w", walkErr)
	}

	// Sorted so a save is reproducible. The map this replaced iterated in a
	// random order, so the file's ordering was never load-bearing.
	slices.SortFunc(entries, func(a, b BlockOverrideEntry) int {
		if a.X != b.X {
			return a.X - b.X
		}
		if a.Y != b.Y {
			return a.Y - b.Y
		}

		return a.Z - b.Z
	})

	return entries, nil
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

	if err := foldOverrides(w, entries); err != nil {
		return err
	}
	s.log.Info("loaded block overrides", "count", len(entries))

	return nil
}

// foldOverrides writes overrides.json's contents into the chunks they belong
// to. It exists because M11.2 deleted the override map while M11.3 still owns
// the on-disk format.
//
// DELETE IN M11.3, together with extractOverrides and overrides.json itself.
func foldOverrides(w *world.World, entries []BlockOverrideEntry) error {
	adapter := w.Adapter()
	for _, e := range entries {
		state, err := adapter.DecodeState(e.StateID)
		if err != nil {
			return fmt.Errorf("block override at %d,%d,%d: %w", e.X, e.Y, e.Z, err)
		}
		w.SetBlock(world.BlockPos{X: e.X, Y: e.Y, Z: e.Z}, state)
	}

	return nil
}

// SaveChests writes every stored container to world/chests.json.
func (s *Storage) SaveChests(w *world.World) error {
	chests := w.GetChests()
	entries := make([]ChestEntry, 0, len(chests))
	for pos, contents := range chests {
		slots := make([]ChestSlotEntry, world.ChestSlots)
		for i, stack := range contents {
			slots[i] = ChestSlotEntry{ID: stack.ID, Count: stack.Count, Damage: stack.Damage}
		}
		entries = append(entries, ChestEntry{X: pos.X, Y: pos.Y, Z: pos.Z, Slots: slots})
	}

	path := filepath.Join(s.dir, "world", "chests.json")

	return s.atomicWriteJSON(path, entries)
}

// LoadChests reads world/chests.json and restores container contents.
func (s *Storage) LoadChests(w *world.World) error {
	path := filepath.Join(s.dir, "world", "chests.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return fmt.Errorf("read chests: %w", err)
	}

	var entries []ChestEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("parse chests: %w", err)
	}

	chests := make(map[world.BlockPos]world.ChestContents, len(entries))
	for _, e := range entries {
		if len(e.Slots) != world.ChestSlots {
			return fmt.Errorf("chest at %d,%d,%d has %d slots, want %d", e.X, e.Y, e.Z, len(e.Slots), world.ChestSlots)
		}

		contents := world.EmptyChest()
		for i, slot := range e.Slots {
			if slot.ID <= 0 || slot.Count <= 0 {
				continue
			}
			contents[i] = world.ItemStack{ID: slot.ID, Count: slot.Count, Damage: slot.Damage}
		}
		chests[world.BlockPos{X: e.X, Y: e.Y, Z: e.Z}] = contents
	}

	w.SetChests(chests)
	s.log.Info("loaded chests", "count", len(chests))

	return nil
}

// SaveWorldAnvil writes the world in Minecraft's Anvil region file format (.mca).
func (s *Storage) SaveWorldAnvil(w *world.World) error {
	regionDir := filepath.Join(s.dir, "world", "region")
	if err := os.MkdirAll(regionDir, 0o755); err != nil {
		return fmt.Errorf("create region dir: %w", err)
	}

	// A snapshot is a pointer copy, and the chunks in it are immutable, so
	// encoding runs against a consistent world while the tick loop keeps
	// writing.
	snapshot := w.Snapshot()

	type regionKey struct{ rx, rz int }
	regions := make(map[regionKey]map[world.ChunkPos][]byte)

	for pos, chunk := range snapshot.Chunks {
		nbtData, err := anvil.EncodeChunkNBT(chunk, w.Adapter())
		if err != nil {
			s.log.Error("encode chunk NBT", "cx", pos.X, "cz", pos.Z, "error", err)

			continue
		}

		rk := regionKey{rx: pos.X >> 5, rz: pos.Z >> 5}
		if regions[rk] == nil {
			regions[rk] = make(map[world.ChunkPos][]byte)
		}
		regions[rk][pos] = nbtData
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
