package world

import "testing"

func TestWorldBaseState(t *testing.T) {
	w := NewWorld()

	// y=0 should be stone (blockID=1, state=1<<4=16).
	if got := w.GetBlock(0, 0, 0); got != 16 {
		t.Errorf("GetBlock(0,0,0) = %d, want 16 (stone)", got)
	}

	// y>0 should be air (0).
	if got := w.GetBlock(0, 1, 0); got != 0 {
		t.Errorf("GetBlock(0,1,0) = %d, want 0 (air)", got)
	}
	if got := w.GetBlock(5, 64, 10); got != 0 {
		t.Errorf("GetBlock(5,64,10) = %d, want 0 (air)", got)
	}
}

func TestWorldSetBlock(t *testing.T) {
	w := NewWorld()

	// Place a block at y=1.
	w.SetBlock(3, 1, 5, 4<<4) // cobblestone state
	if got := w.GetBlock(3, 1, 5); got != 4<<4 {
		t.Errorf("GetBlock(3,1,5) = %d, want %d", got, 4<<4)
	}

	// Break the stone at y=0 (set to air).
	w.SetBlock(0, 0, 0, 0)
	if got := w.GetBlock(0, 0, 0); got != 0 {
		t.Errorf("GetBlock(0,0,0) after break = %d, want 0", got)
	}

	// Restore stone at y=0 (should remove override).
	w.SetBlock(0, 0, 0, 16)
	if got := w.GetBlock(0, 0, 0); got != 16 {
		t.Errorf("GetBlock(0,0,0) after restore = %d, want 16", got)
	}
}

func TestWorldSetBlockRemovesRedundantOverride(t *testing.T) {
	w := NewWorld()

	// Set air at y=1 (which is already air) — should not store an override.
	w.SetBlock(0, 1, 0, 0)

	w.mu.RLock()
	_, exists := w.blocks[BlockPos{0, 1, 0}]
	w.mu.RUnlock()
	if exists {
		t.Error("setting air at y=1 should not create an override")
	}
}
