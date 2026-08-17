package storage

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/server/pkg/world"
	"github.com/go-theft-craft/server/pkg/world/gen"
	"github.com/go-theft-craft/server/pkg/world/v47"
)

func newWorld(t *testing.T) *world.World {
	t.Helper()

	set, err := v1_8.Data()
	if err != nil {
		t.Fatalf("v1_8.Data: %v", err)
	}
	registry, err := world.NewJavaRegistry(set)
	if err != nil {
		t.Fatalf("NewJavaRegistry: %v", err)
	}
	adapter, err := v47.New(registry, set)
	if err != nil {
		t.Fatalf("v47.New: %v", err)
	}
	w, err := world.NewWorld(world.Overworld18(), adapter, gen.NewFlatGenerator(0))
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}

	return w
}

// setBlockID and blockID name a block the way protocol 47 does, which is what
// overrides.json still holds.
func setBlockID(t *testing.T, w *world.World, x, y, z int, stateID int32) {
	t.Helper()

	state, err := w.Adapter().DecodeState(stateID)
	if err != nil {
		t.Fatalf("DecodeState(%d): %v", stateID, err)
	}
	w.SetBlock(world.BlockPos{X: x, Y: y, Z: z}, state)
}

func blockID(t *testing.T, w *world.World, x, y, z int) int32 {
	t.Helper()

	v, err := w.Adapter().EncodeState(w.Block(world.BlockPos{X: x, Y: y, Z: z}))
	if err != nil {
		t.Fatalf("EncodeState: %v", err)
	}

	return v
}

func newStorage(t *testing.T) (*Storage, string) {
	t.Helper()

	dir := t.TempDir()
	s, err := New(dir, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return s, dir
}

// TestOverridesRoundTripThroughTheChunkModel is what keeps overrides.json the
// same file it was before the override map was deleted: the world now holds
// player edits inside its chunks, and the two shims have to recover exactly
// the same set on the way out.
func TestOverridesRoundTripThroughTheChunkModel(t *testing.T) {
	s, dir := newStorage(t)
	path := filepath.Join(dir, "world", "overrides.json")

	const cobblestone = 4 << 4
	const chest = 54<<4 | 2

	first := newWorld(t)
	// A block in a section the generator filled, one in a section it left
	// empty, one in a neighbouring chunk, and one broken back to air.
	setBlockID(t, first, 1, 2, 3, cobblestone)
	setBlockID(t, first, 5, 130, 5, chest)
	setBlockID(t, first, -20, 40, 70, cobblestone)
	setBlockID(t, first, 0, 4, 0, 0)

	if err := s.SaveBlockOverrides(first); err != nil {
		t.Fatalf("SaveBlockOverrides: %v", err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read overrides: %v", err)
	}

	second := newWorld(t)
	if err := s.LoadBlockOverrides(second); err != nil {
		t.Fatalf("LoadBlockOverrides: %v", err)
	}

	for _, tc := range []struct {
		x, y, z int
		want    int32
	}{
		{1, 2, 3, cobblestone},
		{5, 130, 5, chest},
		{-20, 40, 70, cobblestone},
		{0, 4, 0, 0},
		{1, 4, 1, 2 << 4}, // untouched grass from the generator
	} {
		if got := blockID(t, second, tc.x, tc.y, tc.z); got != tc.want {
			t.Errorf("reloaded block at (%d,%d,%d) = %d, want %d", tc.x, tc.y, tc.z, got, tc.want)
		}
	}

	if err := s.SaveBlockOverrides(second); err != nil {
		t.Fatalf("SaveBlockOverrides: %v", err)
	}
	again, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read overrides: %v", err)
	}
	if string(again) != string(saved) {
		t.Fatalf("a load-and-save changed overrides.json.\n--- first ---\n%s\n--- second ---\n%s", saved, again)
	}
}

// A world nobody has edited has no overrides, which is what keeps the file
// from growing with every generated chunk.
func TestAnUneditedWorldHasNoOverrides(t *testing.T) {
	s, _ := newStorage(t)

	w := newWorld(t)
	w.PreGenerateRadius(2)

	entries, err := extractOverrides(w)
	if err != nil {
		t.Fatalf("extractOverrides: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("an unedited world produced %d overrides, first %+v", len(entries), entries[0])
	}
	if err := s.SaveBlockOverrides(w); err != nil {
		t.Fatalf("SaveBlockOverrides: %v", err)
	}
}
