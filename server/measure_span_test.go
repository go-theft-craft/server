package server_test

import (
	"testing"
	"time"

	"github.com/go-theft-craft/server/server"
)

// The measurement span.
//
// The file is measure_span_test.go rather than measure_test.go because that
// name was already taken by M11.3's storage measurements, which measure a
// world rather than a span.

func newObservedServer(t *testing.T, obs server.Observer) *server.Server {
	t.Helper()

	srv, err := server.New(server.WithObserver(obs))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return srv
}

func TestMeasureAllocatesNothingWhenUnobserved(t *testing.T) {
	srv, err := server.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	labels := server.Labels{Player: "Alice", Region: server.RegionOf(1, 1)}
	if got := testing.AllocsPerRun(1000, func() {
		srv.Measure(server.FeatureChunkEncode, labels)()
	}); got != 0 {
		t.Errorf("an unobserved span allocated %v times per run, want 0", got)
	}
}

func TestMeasureDoesNotReadTheClockWhenUnobserved(t *testing.T) {
	// This is the whole argument for leaving Measure inside the 625-iteration
	// loop that sends a join's chunks. If the unobserved path read the clock,
	// the loop would pay 625 clock reads to produce nothing.
	reads := 0
	restore := server.SetClock(func() time.Time {
		reads++

		return time.Now()
	})
	defer restore()

	srv, err := server.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for range 625 {
		srv.Measure(server.FeatureChunkEncode, server.Labels{})()
	}
	if reads != 0 {
		t.Errorf("an unobserved server read the clock %d times, want 0", reads)
	}
}

func TestMeasureRecordsAPlausibleDuration(t *testing.T) {
	obs := newCountingObserver(8)
	srv := newObservedServer(t, obs)

	end := srv.Measure(server.FeatureChunkEncode, server.Labels{Player: "Alice"})
	time.Sleep(2 * time.Millisecond)
	end()

	got := <-obs.seen
	if got.Kind != server.SampleDuration {
		t.Errorf("span produced a %q sample, want %q", got.Kind, server.SampleDuration)
	}
	if got.Labels.Feature != server.FeatureChunkEncode {
		t.Errorf("span labelled feature %q, want %q", got.Labels.Feature, server.FeatureChunkEncode)
	}
	if got.Labels.Player != "Alice" {
		t.Errorf("span labelled player %q, want Alice", got.Labels.Player)
	}
	// Seconds, not nanoseconds: a consumer that graphed the wrong unit would
	// be off by a factor of a billion and the graph would still look like a
	// graph.
	if got.Value < 0.001 || got.Value > 5 {
		t.Errorf("span measured %v seconds for a 2ms sleep, want something between 1ms and 5s", got.Value)
	}
}

func TestCountAndGaugeCarryTheirLabels(t *testing.T) {
	obs := newCountingObserver(8)
	srv := newObservedServer(t, obs)

	srv.Count(server.FeatureCommand, server.Labels{Player: "Bob"}, 3)
	got := <-obs.seen
	if got.Kind != server.SampleCount || got.Value != 3 {
		t.Errorf("Count produced %q = %v, want count = 3", got.Kind, got.Value)
	}
	if got.Labels.Feature != server.FeatureCommand || got.Labels.Player != "Bob" {
		t.Errorf("Count labels are %+v, want the command feature and Bob", got.Labels)
	}

	srv.Gauge(server.SampleGauge, server.Labels{World: server.DefaultWorld}, 12)
	got = <-obs.seen
	if got.Kind != server.SampleGauge || got.Value != 12 {
		t.Errorf("Gauge produced %q = %v, want gauge = 12", got.Kind, got.Value)
	}
	if got.Labels.World != server.DefaultWorld {
		t.Errorf("Gauge labelled world %q, want %q", got.Labels.World, server.DefaultWorld)
	}
	// A gauge is a level and carries no feature unless one was given: a
	// feature on a level would invite a sink to graph it as work.
	if got.Labels.Feature != "" {
		t.Errorf("Gauge invented feature %q", got.Labels.Feature)
	}
}

func TestCountAndGaugeAreFreeWhenUnobserved(t *testing.T) {
	srv, err := server.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if got := testing.AllocsPerRun(1000, func() {
		srv.Count(server.FeatureCommand, server.Labels{Player: "Alice"}, 1)
		srv.Gauge(server.SampleGauge, server.Labels{}, 1)
	}); got != 0 {
		t.Errorf("unobserved Count and Gauge allocated %v times per run, want 0", got)
	}
}
