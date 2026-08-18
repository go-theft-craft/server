package server

import (
	"sync"

	"github.com/go-theft-craft/server/pkg/world"
)

// Per-tick aggregation.
//
// A block write, an inventory click, and an entity position update happen
// thousands of times a second. A sample each would make the measurement the
// load, which is the failure mode where observing something changes it.
//
// The rule the design sets, and the one to apply when adding a call site:
// anything that can happen more than once per tick per player is counted here
// and flushed as one sample at the end of the tick; anything rarer is sampled
// directly. Deviating from it in a single call site is enough to turn
// measurement into load, because the call site that deviates is always the hot
// one — that is why it was tempting.
//
// The design describes this as a plain struct on the tick goroutine needing no
// synchronization. It is not: a block write happens on the connection's
// goroutine and the flush happens on the tick's, so the counters are behind a
// mutex. The lock is held for a map increment, against a block write that
// already does a compare-and-swap on a chunk pointer, so it is not the cost
// this file exists to avoid.

// statKey is what a count is attributed to. It is comparable, so it is a map
// key and the map is the aggregation.
type statKey struct {
	feature Feature
	player  string
}

// tickStats accumulates events between flushes.
type tickStats struct {
	mu     sync.Mutex
	counts map[statKey]float64
}

func newTickStats() *tickStats { return &tickStats{counts: make(map[statKey]float64)} }

// add records that something happened n times.
func (t *tickStats) add(f Feature, player string, n float64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.counts[statKey{feature: f, player: player}] += n
}

// flush hands every accumulated count to fn and empties the accumulator.
//
// The map is cleared rather than reallocated, so a steady state does no
// allocation per tick: the keys are a small fixed set — a feature per player —
// and they repeat every tick.
func (t *tickStats) flush(fn func(f Feature, player string, n float64)) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for key, n := range t.counts {
		fn(key.feature, key.player, n)
		delete(t.counts, key)
	}
}

// pending is how many (feature, player) pairs are waiting to be flushed.
func (t *tickStats) pending() int {
	t.mu.Lock()
	defer t.mu.Unlock()

	return len(t.counts)
}

// CountPerTick records an event that is too frequent to sample directly.
//
// It costs one branch on an unobserved server. On an observed one it costs a
// mutex and a map increment, and the sample it becomes is emitted once per
// tick per (feature, player) rather than once per event.
func (s *Server) CountPerTick(f Feature, player string, n float64) {
	if !s.observed() {
		return
	}

	s.ticks.add(f, player, n)
}

// flushTickStats emits one sample per (feature, player) that saw activity.
func (s *Server) flushTickStats() {
	if !s.observed() {
		return
	}

	s.ticks.flush(func(f Feature, player string, n float64) {
		s.Observe(Sample{
			Kind:   SampleCount,
			Value:  n,
			Labels: Labels{Feature: f, Player: player, World: DefaultWorld},
		})
	})
}

// sampleLevels emits the gauges: what the server is holding rather than what
// it is doing.
//
// They go on the ten-second cadence the resource samples use, not per tick. A
// level does not need twenty samples a second, and emitting one would put a
// hundred and twenty thousand points a day into a graph that changes when
// somebody logs in.
func (s *Server) sampleLevels() {
	if !s.observed() {
		return
	}

	base := Labels{World: DefaultWorld}
	s.Gauge(SampleGauge, base.withFeature(FeatureLogin), float64(s.players.PlayerCount()))

	resident := 0
	s.world.ForEachChunk(func(world.ChunkPos, *world.Chunk) { resident++ })
	s.Gauge(SampleGauge, base.withFeature(FeatureChunkLoad), float64(resident))

	// Item identity is the one level that is a memory risk rather than a
	// curiosity: the index holds an entry per identified item and nothing
	// evicts it, which the framework design flags as the risk no test reveals
	// early. It is zero when identity is off.
	if s.index != nil {
		s.Gauge(SampleGauge, base.withFeature(FeatureProvenance), float64(s.index.Len()))
	}
	if s.recorder != nil {
		s.Count(FeatureProvenance, Labels{World: DefaultWorld}, float64(s.recorder.Dropped()))
	}
}
