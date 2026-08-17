package server_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-theft-craft/server/pkg/world"
	"github.com/go-theft-craft/server/server"
)

// The shapes the pre-M11.3 server wrote, restated here so the fixture is what
// a real data directory held rather than whatever the current types say.
type legacyOverride struct {
	X       int   `json:"x"`
	Y       int   `json:"y"`
	Z       int   `json:"z"`
	StateID int32 `json:"state_id"`
}

type legacyChestSlot struct {
	ID     int16 `json:"id"`
	Count  int8  `json:"count"`
	Damage int16 `json:"damage"`
}

type legacyChest struct {
	X     int               `json:"x"`
	Y     int               `json:"y"`
	Z     int               `json:"z"`
	Slots []legacyChestSlot `json:"slots"`
}

type legacyWorld struct {
	Age       int64 `json:"age"`
	TimeOfDay int64 `json:"time_of_day"`
}

// writeLegacyWorld builds a data directory exactly as the pre-M11.3 server
// left one: block edits in overrides.json, containers in chests.json, the
// clock in world.json, and region files nothing ever read back.
func writeLegacyWorld(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	worldDir := filepath.Join(dir, "world")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	write := func(name string, v any) {
		t.Helper()
		data, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			t.Fatalf("marshal %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(worldDir, name), append(data, '\n'), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	write("world.json", legacyWorld{Age: 4242, TimeOfDay: 13000})
	write("overrides.json", []legacyOverride{
		{X: 1, Y: 2, Z: 3, StateID: 4 << 4},      // cobblestone in the terrain
		{X: 5, Y: 130, Z: 5, StateID: 54<<4 | 2}, // a chest far above it
		{X: 0, Y: 4, Z: 0, StateID: 0},           // and one block broken back to air
	})

	slots := make([]legacyChestSlot, world.ChestSlots)
	slots[0] = legacyChestSlot{ID: 1, Count: 64}
	slots[7] = legacyChestSlot{ID: 264, Count: 3, Damage: 0}
	write("chests.json", []legacyChest{{X: 5, Y: 130, Z: 5, Slots: slots}})

	return dir
}

// TestALegacyDataDirectoryLoadsToTheSameWorld is the whole safety argument for
// the migration.
func TestALegacyDataDirectoryLoadsToTheSameWorld(t *testing.T) {
	dir := writeLegacyWorld(t)
	ctx := context.Background()

	srv, _ := newStoredServer(t, dir)
	if _, err := srv.Load(ctx); err != nil {
		t.Fatalf("Load: %v", err)
	}

	assertMigratedWorld(t, srv.World())

	age, timeOfDay := srv.World().GetTime()
	if age != 4242 || timeOfDay != 13000 {
		t.Errorf("clock = %d/%d, want 4242/13000", age, timeOfDay)
	}

	// And it survives a restart, which is what says the fold reached the disk.
	restarted, _ := newStoredServer(t, dir)
	if _, err := restarted.Load(ctx); err != nil {
		t.Fatalf("Load after restart: %v", err)
	}
	assertMigratedWorld(t, restarted.World())
}

func assertMigratedWorld(t *testing.T, w *world.World) {
	t.Helper()

	cobble := w.Registry().Intern("minecraft:cobblestone", nil)
	chest := w.Registry().Intern("minecraft:chest", world.Properties{{Key: "metadata", Value: "2"}})

	for _, tc := range []struct {
		pos  world.BlockPos
		want world.State
		name string
	}{
		{world.BlockPos{X: 1, Y: 2, Z: 3}, cobble, "cobblestone"},
		{world.BlockPos{X: 5, Y: 130, Z: 5}, chest, "chest"},
		{world.BlockPos{X: 0, Y: 4, Z: 0}, w.Air(), "air"},
		{world.BlockPos{X: 2, Y: 4, Z: 2}, w.Registry().Intern("minecraft:grass", nil), "grass"},
	} {
		if got := w.Block(tc.pos); got != tc.want {
			t.Errorf("block at %v = %d, want %s", tc.pos, got, tc.name)
		}
	}

	contents := w.Chest(world.BlockPos{X: 5, Y: 130, Z: 5})
	if !contents[0].Equal(world.ItemStack{BlockID: 1, ItemCount: 64}) {
		t.Errorf("chest slot 0 = %+v, want 64 stone", contents[0])
	}
	if !contents[7].Equal(world.ItemStack{BlockID: 264, ItemCount: 3}) {
		t.Errorf("chest slot 7 = %+v, want 3 diamonds", contents[7])
	}
}

// TestMigrationRenamesRatherThanDeletes: a migration that deletes its input
// leaves nobody anything to go back to.
func TestMigrationRenamesRatherThanDeletes(t *testing.T) {
	dir := writeLegacyWorld(t)

	srv, _ := newStoredServer(t, dir)
	if _, err := srv.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}

	for _, name := range []string{"world.json", "overrides.json", "chests.json"} {
		path := filepath.Join(dir, "world", name)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s is still in place after the migration (%v)", name, err)
		}
		if _, err := os.Stat(path + ".migrated"); err != nil {
			t.Errorf("%s was deleted rather than renamed: %v", name, err)
		}
	}
}

// TestTheSecondStartDoesNotMigrate: the migration runs once, because the
// renamed files no longer match.
func TestTheSecondStartDoesNotMigrate(t *testing.T) {
	dir := writeLegacyWorld(t)
	ctx := context.Background()

	first, _ := newStoredServer(t, dir)
	if _, err := first.Load(ctx); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// A block placed after the migration must not be reverted by a second one.
	w := first.World()
	sand := w.Registry().Intern("minecraft:sand", nil)
	w.SetBlock(world.BlockPos{X: 1, Y: 2, Z: 3}, sand)
	first.SaveAll()

	second, _ := newStoredServer(t, dir)
	if _, err := second.Load(ctx); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := second.World().Block(world.BlockPos{X: 1, Y: 2, Z: 3}); got != sand {
		t.Fatalf("the block came back as %d, want sand — the migration ran twice", got)
	}
}

// TestRegionsAndOverridesThatDisagreeResolveToTheOverrides is every world the
// pre-M11.3 server wrote: the regions were never read back, so the overrides
// were the truth.
func TestRegionsAndOverridesThatDisagreeResolveToTheOverrides(t *testing.T) {
	dir := writeLegacyWorld(t)
	ctx := context.Background()

	// Put a region on disk that disagrees with overrides.json at (1,2,3).
	seed, seedStore := newStoredServer(t, dir)
	sw := seed.World()
	sw.SetBlock(world.BlockPos{X: 1, Y: 2, Z: 3}, sw.Registry().Intern("minecraft:sand", nil))
	if err := seedStore.World().SaveSnapshot(ctx, server.DefaultWorld, sw.Snapshot()); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}

	srv, _ := newStoredServer(t, dir)
	if _, err := srv.Load(ctx); err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := srv.World().Registry().Intern("minecraft:cobblestone", nil)
	if got := srv.World().Block(world.BlockPos{X: 1, Y: 2, Z: 3}); got != want {
		t.Fatalf("block = %d, want the override's cobblestone — the stale region won", got)
	}
}
