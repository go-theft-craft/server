package server_test

import (
	"testing"

	"github.com/go-theft-craft/server/config"
	"github.com/go-theft-craft/server/pkg/world"
	"github.com/go-theft-craft/server/server"
)

// The off profile.
//
// Track exit criterion 9 is that turning observability off returns the server
// to the resource profile it had before this milestone. That needs a
// measurement, and a measurement in a benchmark nobody runs is a claim. The
// allocation assertion below is a *test*, so CI fails on a regression; the
// wall-time numbers are recorded by the benchmarks and asserted by nothing.
//
// What the workload is: the chunk work a join at view distance 12 does — 625
// encodes — plus a thousand block writes and a thousand inventory clicks. It
// runs against the server API rather than through a socket, because the
// question is what the instrumentation costs and a wire client would put a
// TCP stack between the measurement and the thing measured. The network sink
// is measured separately, by its own benchmark, for the same reason.

const (
	// benchViewDistance is the default, and 625 is the square it makes. That
	// number is what motivated this milestone.
	benchViewDistance = 12
	benchChunks       = (2*benchViewDistance + 1) * (2*benchViewDistance + 1)
	benchEvents       = 1000
)

// discardingObserver takes samples and drops them. It is the sink that
// measures what producing a sample costs, with nothing added for consuming it.
type discardingObserver struct{}

func (discardingObserver) Observe(server.Sample) {}

func newBenchServer(tb testing.TB, opts ...server.Option) *server.Server {
	tb.Helper()

	settings := config.DefaultConfig()
	settings.GeneratorType = config.GeneratorFlat
	settings.ViewDistance = benchViewDistance

	srv, err := server.New(append([]server.Option{server.WithSettings(settings)}, opts...)...)
	if err != nil {
		tb.Fatalf("New: %v", err)
	}

	return srv
}

// observeWorkload is the fixed workload every configuration runs.
func observeWorkload(tb testing.TB, srv *server.Server) {
	tb.Helper()

	w := srv.World()
	stone := w.Registry().Intern("minecraft:stone", nil)

	for cx := -benchViewDistance; cx <= benchViewDistance; cx++ {
		for cz := -benchViewDistance; cz <= benchViewDistance; cz++ {
			if _, err := w.EncodeChunk(world.ChunkPos{X: cx, Z: cz}); err != nil {
				tb.Fatalf("EncodeChunk: %v", err)
			}
		}
	}
	for i := range benchEvents {
		w.SetBlock(world.BlockPos{X: i % 16, Y: 70, Z: (i / 16) % 16}, stone)
		srv.CountPerTick(server.FeatureBlockWrite, "Alice", 1)
		srv.CountPerTick(server.FeatureInventory, "Alice", 1)
	}
	srv.FlushTickStats()
}

// TestOffProfileAllocatesNothingForMeasurement is the exit criterion.
//
// An unobserved server must not allocate to decide it is not measuring. Every
// span it starts returns a package-level closure and reads no clock, and every
// count returns at a branch, so the whole instrumentation surface costs
// exactly the branches.
func TestOffProfileAllocatesNothingForMeasurement(t *testing.T) {
	srv := newBenchServer(t)

	labels := server.Labels{Player: "Alice", Region: server.RegionOf(3, -4)}
	if got := testing.AllocsPerRun(1000, func() {
		srv.Measure(server.FeatureChunkEncode, labels)()
		srv.Measure(server.FeatureChunkSend, labels)()
		srv.Count(server.FeatureCommand, labels, 1)
		srv.CountPerTick(server.FeatureBlockWrite, "Alice", 1)
		srv.Gauge(server.SampleGauge, labels, 1)
	}); got != 0 {
		t.Fatalf("the instrumentation surface allocated %v times per run with no observer, want 0", got)
	}
}

// TestOffProfileHoldsAcrossTheWorkload runs the whole fixed workload and
// checks that an unobserved server produced nothing at all.
//
// It is the assertion that catches an instrumented path someone forgot to
// guard: a sample built before the observed() check would show up here as a
// sample delivered to a server that has no observer.
func TestOffProfileHoldsAcrossTheWorkload(t *testing.T) {
	srv := newBenchServer(t)
	observeWorkload(t, srv)

	if got := srv.PendingTickStats(); got != 0 {
		t.Errorf("an unobserved server accumulated %d tick-stat entries, want 0", got)
	}
	if got := srv.DroppedSamples(); got != 0 {
		t.Errorf("an unobserved server dropped %d samples, which means it produced some", got)
	}
}

// TestObservedWorkloadEmitsWhatItShould is the other side: the workload above
// is only a floor if the same workload observed actually measures something.
func TestObservedWorkloadEmitsWhatItShould(t *testing.T) {
	obs := &collector{}
	srv := newBenchServer(t, server.WithObserver(obs))
	observeWorkload(t, srv)
	srv.DrainSamples()

	if got := len(obs.byFeature(server.FeatureChunkEncode)); got != benchChunks {
		t.Errorf("the workload encoded %d chunks and reported %d, want one each", benchChunks, got)
	}
	// A thousand block writes, one sample: that is the accumulator earning
	// its place.
	if got := len(obs.byFeature(server.FeatureBlockWrite)); got != 1 {
		t.Errorf("%d block writes produced %d samples, want 1", benchEvents, got)
	}
}

// BenchmarkObserveOff is the floor: the workload with no observer. Recorded,
// not asserted.
func BenchmarkObserveOff(b *testing.B) {
	for b.Loop() {
		b.StopTimer()
		srv := newBenchServer(b)
		b.StartTimer()

		observeWorkload(b, srv)
	}
}

// BenchmarkObserveDiscarding is the cost of producing samples nobody keeps.
// The difference between this and the floor is what the instrumentation costs.
func BenchmarkObserveDiscarding(b *testing.B) {
	for b.Loop() {
		b.StopTimer()
		srv := newBenchServer(b, server.WithObserver(discardingObserver{}))
		b.StartTimer()

		observeWorkload(b, srv)
	}
}

// BenchmarkObserveRecording is the cost with a real consumer behind it.
// Recorded, not asserted: this is the sink's cost, not the server's, and a
// Prometheus client would cost something else again.
func BenchmarkObserveRecording(b *testing.B) {
	for b.Loop() {
		b.StopTimer()
		srv := newBenchServer(b, server.WithObserver(&collector{}))
		b.StartTimer()

		observeWorkload(b, srv)
		srv.DrainSamples()
	}
}

// BenchmarkMeasureSpanObserved is the per-span cost on the hot path, which is
// the number to watch: 625 of these run before a joining player can move.
func BenchmarkMeasureSpanObserved(b *testing.B) {
	srv := newBenchServer(b, server.WithObserver(discardingObserver{}))
	labels := server.Labels{Player: "Alice", Region: server.RegionOf(0, 0)}

	for b.Loop() {
		srv.Measure(server.FeatureChunkEncode, labels)()
	}
}

// BenchmarkMeasureSpanUnobserved is the same call with nobody watching: one
// branch and a shared closure.
func BenchmarkMeasureSpanUnobserved(b *testing.B) {
	srv := newBenchServer(b)
	labels := server.Labels{Player: "Alice", Region: server.RegionOf(0, 0)}

	for b.Loop() {
		srv.Measure(server.FeatureChunkEncode, labels)()
	}
}
