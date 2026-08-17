package world

import (
	"sync"
	"testing"
)

// TestConcurrentWritersAllLand is the property a naive retry loop breaks: the
// loser of a compare-and-swap has to rebuild from the winner's chunk, not from
// the one it loaded first. Run it under the race detector too — the detector
// alone would pass an implementation that silently loses writes.
func TestConcurrentWritersAllLand(t *testing.T) {
	w, _, reg := newTestWorld(t)
	stone := reg.Intern("minecraft:stone", nil)

	const writers, perWriter = 8, 500

	var wg sync.WaitGroup
	for g := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range perWriter {
				n := g*perWriter + i
				w.SetBlock(positionFor(n), stone)
			}
		}()
	}
	wg.Wait()

	for n := range writers * perWriter {
		pos := positionFor(n)
		if got := w.Block(pos); got != stone {
			t.Fatalf("write %d at %v was lost: read %d, want %d", n, pos, got, stone)
		}
	}
}

// positionFor spreads 4,000 distinct positions through one chunk, above the
// terrain the stub generator lays so nothing collides with it.
func positionFor(n int) BlockPos {
	return BlockPos{X: n % 16, Y: 16 + n/256, Z: (n / 16) % 16}
}

func TestReadersSeeAConsistentSectionUnderWrites(t *testing.T) {
	w, _, reg := newTestWorld(t)
	stone := reg.Intern("minecraft:stone", nil)
	dirt := reg.Intern("minecraft:dirt", nil)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 2000 {
			state := stone
			if i%2 == 1 {
				state = dirt
			}
			w.SetBlock(BlockPos{X: 1, Y: 100, Z: 1}, state)
		}
	}()

	for range 2000 {
		got := w.Block(BlockPos{X: 1, Y: 100, Z: 1})
		if got != stone && got != dirt && got != reg.Air() {
			t.Fatalf("read a state nobody wrote: %d", got)
		}
	}
	<-done
}

func TestASnapshotDoesNotChangeUnderLaterWrites(t *testing.T) {
	w, g, reg := newTestWorld(t)
	stone := reg.Intern("minecraft:stone", nil)

	w.PreGenerateRadius(1)
	before := w.Snapshot()
	chunk := before.Chunks[ChunkPos{}]
	if chunk == nil {
		t.Fatal("the snapshot is missing the origin chunk")
	}
	if got := chunk.At(w.Dimension(), 0, 4, 0, reg.Air()); got != g.grass {
		t.Fatalf("snapshot has %d at the surface, want grass", got)
	}

	w.SetBlock(BlockPos{0, 4, 0}, stone)

	if got := chunk.At(w.Dimension(), 0, 4, 0, reg.Air()); got != g.grass {
		t.Fatalf("the snapshot changed under a later write: %d", got)
	}
	if got := w.Block(BlockPos{0, 4, 0}); got != stone {
		t.Fatalf("the world did not take the write: %d", got)
	}
	if before.Chunks[ChunkPos{}] == w.Chunk(ChunkPos{}) {
		t.Fatal("the write did not replace the chunk pointer")
	}
}

func TestConcurrentReadersAndGeneratorsAgree(t *testing.T) {
	w, g, reg := newTestWorld(t)

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for cx := range 20 {
				if got := w.Block(BlockPos{X: cx * 16, Y: 4, Z: 0}); got != g.grass {
					t.Errorf("chunk %d has %d at the surface, want grass", cx, got)
				}
			}
		}()
	}
	wg.Wait()

	if len(w.Snapshot().Chunks) != 20 {
		t.Fatalf("%d chunks resident, want 20", len(w.Snapshot().Chunks))
	}
	_ = reg
}
