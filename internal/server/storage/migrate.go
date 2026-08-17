package storage

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/go-theft-craft/server/pkg/world"
)

// The one-way migration off the JSON world files.
//
// Before M11.3 a world's blocks lived in world/overrides.json and its chests
// in world/chests.json, and the region files were written but never read. So
// on any world written by that server the JSON files are the truth and the
// regions are a stale copy — which is why the fold runs *after* the world can
// load its regions, so an override lands on top of what the region holds.
//
// The migration renames its inputs rather than deleting them. A migration that
// deletes its input leaves nobody anything to go back to, and a rename is one
// command to undo. It runs once because the renamed files no longer match.

// migratedSuffix is appended to a source file once it has been folded in.
const migratedSuffix = ".migrated"

// Report is what a migration did, for the log and for a test.
type Report struct {
	Ran       bool
	Overrides int
	Chests    int
	Age       int64
	TimeOfDay int64
	HasTime   bool
}

// Migrate folds the legacy JSON world files into w and renames them.
//
// It writes nothing to disk beyond the renames: the caller saves the world
// afterwards, through whatever store it was built with, which is what puts the
// folded data into the region files.
func Migrate(dir string, w *world.World, log *slog.Logger) (Report, error) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	root := filepath.Join(dir, "world")
	var report Report

	level, err := migrateLevel(root, &report)
	if err != nil {
		return report, err
	}
	overrides, err := migrateOverrides(root, w, &report)
	if err != nil {
		return report, err
	}
	chests, err := migrateChests(root, w, &report)
	if err != nil {
		return report, err
	}

	if !report.Ran {
		return report, nil
	}

	for _, path := range []string{level, overrides, chests} {
		if path == "" {
			continue
		}
		if err := os.Rename(path, path+migratedSuffix); err != nil {
			return report, fmt.Errorf("rename %s: %w", path, err)
		}
	}

	log.Info("migrated the legacy world files",
		"overrides", report.Overrides,
		"chests", report.Chests,
		"age", report.Age,
		"timeOfDay", report.TimeOfDay)

	return report, nil
}

func migrateLevel(root string, report *Report) (string, error) {
	path := filepath.Join(root, "world.json")

	var data WorldData
	found, err := ReadJSON(path, &data)
	if err != nil {
		return "", fmt.Errorf("read legacy world data: %w", err)
	}
	if !found {
		return "", nil
	}

	report.Ran = true
	report.Age, report.TimeOfDay, report.HasTime = data.Age, data.TimeOfDay, true

	return path, nil
}

func migrateOverrides(root string, w *world.World, report *Report) (string, error) {
	path := filepath.Join(root, "overrides.json")

	var entries []BlockOverrideEntry
	found, err := ReadJSON(path, &entries)
	if err != nil {
		return "", fmt.Errorf("read legacy block overrides: %w", err)
	}
	if !found {
		return "", nil
	}

	adapter := w.Adapter()
	for _, e := range entries {
		state, err := adapter.DecodeState(e.StateID)
		if err != nil {
			return "", fmt.Errorf("block override at %d,%d,%d: %w", e.X, e.Y, e.Z, err)
		}
		w.SetBlock(world.BlockPos{X: e.X, Y: e.Y, Z: e.Z}, state)
	}

	report.Ran = true
	report.Overrides = len(entries)

	return path, nil
}

func migrateChests(root string, w *world.World, report *Report) (string, error) {
	path := filepath.Join(root, "chests.json")

	var entries []ChestEntry
	found, err := ReadJSON(path, &entries)
	if err != nil {
		return "", fmt.Errorf("read legacy chests: %w", err)
	}
	if !found {
		return "", nil
	}

	for _, e := range entries {
		if len(e.Slots) != world.ChestSlots {
			return "", fmt.Errorf("chest at %d,%d,%d has %d slots, want %d",
				e.X, e.Y, e.Z, len(e.Slots), world.ChestSlots)
		}
		contents := world.EmptyChest()
		for i, slot := range e.Slots {
			if slot.ID <= 0 || slot.Count <= 0 {
				continue
			}
			contents[i] = world.ItemStack{ID: slot.ID, Count: slot.Count, Damage: slot.Damage}
		}
		w.SetChest(world.BlockPos{X: e.X, Y: e.Y, Z: e.Z}, contents)
	}

	report.Ran = true
	report.Chests = len(entries)

	return path, nil
}
