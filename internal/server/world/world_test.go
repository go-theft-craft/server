package world

import (
	"testing"

	"github.com/OCharnyshevich/minecraft-server/internal/server/world/gen"
)

func TestWorldBaseStateFlatGenerator(t *testing.T) {
	w := NewWorld(gen.NewFlatGenerator(0))

	// Flat generator: bedrock at y=0, stone at y=1-2, dirt at y=3, grass at y=4.
	if got := w.GetBlock(0, 0, 0); got != 7<<4 { // bedrock
		t.Errorf("GetBlock(0,0,0) = %d, want %d (bedrock)", got, 7<<4)
	}
	if got := w.GetBlock(0, 1, 0); got != 1<<4 { // stone
		t.Errorf("GetBlock(0,1,0) = %d, want %d (stone)", got, 1<<4)
	}
	if got := w.GetBlock(0, 4, 0); got != 2<<4 { // grass
		t.Errorf("GetBlock(0,4,0) = %d, want %d (grass)", got, 2<<4)
	}

	// y>4 should be air (0).
	if got := w.GetBlock(5, 64, 10); got != 0 {
		t.Errorf("GetBlock(5,64,10) = %d, want 0 (air)", got)
	}
}

func TestWorldSetBlock(t *testing.T) {
	w := NewWorld(gen.NewFlatGenerator(0))

	// Place a block at y=10 (air location).
	w.SetBlock(3, 10, 5, 4<<4) // cobblestone state
	if got := w.GetBlock(3, 10, 5); got != 4<<4 {
		t.Errorf("GetBlock(3,10,5) = %d, want %d", got, 4<<4)
	}

	// Break grass at y=4 (set to air).
	w.SetBlock(0, 4, 0, 0)
	if got := w.GetBlock(0, 4, 0); got != 0 {
		t.Errorf("GetBlock(0,4,0) after break = %d, want 0", got)
	}

	// Restore grass at y=4 (should remove override).
	w.SetBlock(0, 4, 0, 2<<4)
	if got := w.GetBlock(0, 4, 0); got != 2<<4 {
		t.Errorf("GetBlock(0,4,0) after restore = %d, want %d", got, 2<<4)
	}
}

func TestWorldSetBlockRemovesRedundantOverride(t *testing.T) {
	w := NewWorld(gen.NewFlatGenerator(0))

	// Trigger chunk generation so base state is known.
	_ = w.GetBlock(0, 10, 0)

	// Set air at y=10 (which is already air) — should not store an override.
	w.SetBlock(0, 10, 0, 0)

	w.mu.RLock()
	_, exists := w.blocks[BlockPos{0, 10, 0}]
	w.mu.RUnlock()
	if exists {
		t.Error("setting air at y=10 should not create an override")
	}
}

func TestWorldSpawnHeight(t *testing.T) {
	w := NewWorld(gen.NewFlatGenerator(0))
	// Flat: grass at y=4, HeightAt=4, SpawnHeight = 4+1 = 5
	if got := w.SpawnHeight(); got != 5 {
		t.Errorf("SpawnHeight() = %d, want 5", got)
	}
}

func TestPreGenerateRadius(t *testing.T) {
	w := NewWorld(gen.NewFlatGenerator(0))
	count := w.PreGenerateRadius(2)

	// Radius 2 → 5×5 = 25 chunks.
	if count != 25 {
		t.Errorf("PreGenerateRadius(2) returned %d, want 25", count)
	}

	// Verify all 25 chunks are cached (no generation needed on second access).
	for cx := -2; cx <= 2; cx++ {
		for cz := -2; cz <= 2; cz++ {
			pos := gen.ChunkPos{X: cx, Z: cz}
			w.mu.RLock()
			_, ok := w.chunks[pos]
			w.mu.RUnlock()
			if !ok {
				t.Errorf("chunk (%d,%d) not pre-generated", cx, cz)
			}
		}
	}
}

func TestWorldDefaultGenerator(t *testing.T) {
	w := NewWorld(gen.NewDefaultGenerator(12345))

	// Bedrock should always be at y=0.
	if got := w.GetBlock(0, 0, 0); got != 7<<4 { // bedrock
		t.Errorf("GetBlock(0,0,0) = %d, want %d (bedrock)", got, 7<<4)
	}

	// Should have some terrain above y=0.
	height := w.SpawnHeight()
	if height < 5 || height > 255 {
		t.Errorf("SpawnHeight() = %d, want between 5 and 255", height)
	}
}
