package server

import (
	"time"

	"github.com/go-theft-craft/server/pkg/world"
)

// Measurement.
//
// The whole argument for leaving a Measure call inside a 625-iteration loop is
// that an unobserved server does nothing there: one predictable branch and a
// package-level closure, no allocation and no clock read. Everything in this
// file exists to keep that true, and measure_span_test.go is what says it is.
//
// An observed server reads the monotonic clock twice per span. On Linux that
// is a vDSO call of tens of nanoseconds against a chunk encode measured in
// hundreds of microseconds, which is three orders of magnitude of headroom —
// enough that the measurement is not the thing being measured.

// noopSpan is what Measure returns when nobody is watching.
//
// It is a package-level value, so the unobserved path allocates nothing and
// every call site shares one closure.
var noopSpan = func() {}

// now is the clock, replaceable in the test build so a test can prove the
// unobserved path never reads it.
var now = time.Now

// Measure starts a timing span and returns the function that ends it.
//
// The returned function records the elapsed time as a SampleDuration. It is
// normally called through defer:
//
//	defer srv.Measure(FeatureChunkEncode, Labels{Player: name, Region: r})()
//
// On a server with no observer it returns a shared no-op and reads no clock,
// which is what makes it safe on a hot path.
func (s *Server) Measure(f Feature, l Labels) func() {
	if !s.observed() {
		return noopSpan
	}

	start := now()

	return func() {
		s.Observe(Sample{
			Kind:   SampleDuration,
			Value:  time.Since(start).Seconds(),
			Labels: l.withFeature(f),
		})
	}
}

// Count records that something happened n times.
//
// It is for events too rare to be worth accumulating and too frequent to time:
// a command run, a login, a record dropped. Anything that can happen more than
// once per tick per player belongs in the per-tick accumulator instead, which
// is the rule that keeps measurement from becoming load.
func (s *Server) Count(f Feature, l Labels, n float64) {
	if !s.observed() {
		return
	}

	s.Observe(Sample{Kind: SampleCount, Value: n, Labels: l.withFeature(f)})
}

// Gauge records a level rather than an event: players online, chunks resident,
// identified items live.
//
// The kind is the caller's because a gauge and a byte count are both levels
// and a consumer graphs them differently.
func (s *Server) Gauge(kind SampleKind, l Labels, v float64) {
	if !s.observed() {
		return
	}

	s.Observe(Sample{Kind: kind, Value: v, Labels: l})
}

// withFeature returns the labels with a feature set, so a call site names the
// feature once rather than in both the argument and the label.
func (l Labels) withFeature(f Feature) Labels {
	l.Feature = f

	return l
}

// measureChunk is the seam the world and the stores report through.
//
// They cannot name a Feature or a Labels — this package imports them, not the
// other way round — so the feature crosses as a string and the labels are
// built here, which is also the one place that decides whether the chunk label
// is set. See chunkLabels.
func (s *Server) measureChunk(feature string, pos world.ChunkPos) func() {
	return s.Measure(Feature(feature), s.chunkLabels("", pos))
}

// measureConnection is the same seam for a connection, which knows which
// player its work is for.
func (s *Server) measureConnection(feature, player string, pos world.ChunkPos) func() {
	return s.Measure(Feature(feature), s.chunkLabels(player, pos))
}

// chunkLabels attributes a piece of chunk work.
//
// The region is always set and the exact chunk only under WithChunkDetail,
// which is the cardinality decision in the one place both branches are
// visible. Building the chunk string is the only formatting on this path, and
// it happens exactly when somebody asked for it.
func (s *Server) chunkLabels(player string, pos world.ChunkPos) Labels {
	l := Labels{
		Player: player,
		World:  DefaultWorld,
		Region: RegionOf(pos.X, pos.Z),
	}
	if s.chunkDetail {
		l.Chunk = ChunkPosLabel(pos.X, pos.Z)
	}

	return l
}

// countConnection is the accumulating half of the same seam: an event a
// connection reports that is too frequent to time.
func (s *Server) countConnection(feature, player string, n float64) {
	s.CountPerTick(Feature(feature), player, n)
}
