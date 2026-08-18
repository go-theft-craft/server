package conn

import (
	"testing"

	"github.com/go-theft-craft/server/pkg/world"
)

// Chunk work is attributed to the player it was done for.
//
// The label is stored once at login rather than read out of the player per
// sample, because a join at view distance 12 sends 625 chunks and each one is
// a span.

type recordedSpan struct {
	feature string
	player  string
	pos     world.ChunkPos
}

func TestAChunkSendIsAttributedToThePlayerItIsFor(t *testing.T) {
	c, _, _, _ := newTestConnWithCapture(t, "Alice")

	var spans []recordedSpan
	counted := map[string]float64{}
	c.SetMeasure(func(feature, player string, pos world.ChunkPos) func() {
		spans = append(spans, recordedSpan{feature: feature, player: player, pos: pos})

		return func() {}
	}, func(feature, _ string, n float64) {
		counted[feature] += n
	})
	c.metricsPlayer = "Alice"

	pos := world.ChunkPos{X: -3, Z: 40}
	if err := c.sendChunk(pos); err != nil {
		t.Fatalf("sendChunk: %v", err)
	}

	if len(spans) != 1 {
		t.Fatalf("one chunk sent produced %d spans, want 1", len(spans))
	}
	if spans[0].feature != world.MeasureChunkSend {
		t.Errorf("span feature is %q, want %q", spans[0].feature, world.MeasureChunkSend)
	}
	if spans[0].player != "Alice" {
		t.Errorf("span player is %q, want Alice", spans[0].player)
	}
	if spans[0].pos != pos {
		t.Errorf("span chunk is %v, want %v", spans[0].pos, pos)
	}

	// A block write is counted rather than timed, which is the rule the whole
	// accumulator exists to enforce.
	c.setBlockAt(0, 5, 0, c.states.air)
	if got := counted[world.MeasureBlockWrite]; got != 1 {
		t.Errorf("one block write counted %v, want 1", got)
	}
	if len(spans) != 1 {
		t.Errorf("a block write produced %d spans; it is counted, not timed", len(spans)-1)
	}
}

func TestAConnectionWithNoMeasureSendsChunksAnyway(t *testing.T) {
	// The default. An unobserved connection must not pay an indirect call per
	// chunk, and it must not fail to send one either.
	c, _, _, _ := newTestConnWithCapture(t, "Bob")

	if c.measure != nil {
		t.Fatal("a connection was built already measuring")
	}
	if err := c.sendChunk(world.ChunkPos{}); err != nil {
		t.Fatalf("sendChunk without a measure: %v", err)
	}
	if _, loaded := c.loadedChunks[world.ChunkPos{}]; !loaded {
		t.Error("the chunk was not tracked as sent")
	}
}
