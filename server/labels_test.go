package server_test

import (
	"context"
	"testing"
	"time"

	protocol "github.com/go-theft-craft/minecraft-protocol"

	"github.com/go-theft-craft/server/server"
)

// The label surface.
//
// A label is read by a person and joined against something else by a machine,
// so the two things that matter about one are that it is correct and that
// there are not too many of them. The region arithmetic is the first and the
// closed set is the second.

func TestRegionOfNegativeChunkCoordinates(t *testing.T) {
	// cx/32 and cx>>5 agree for positives and disagree for everything below
	// zero, and the Anvil layout uses the shift. A metrics label built with
	// division would name r.0.0 for the chunk stored in r.-1.-1, which is
	// worse than no label at all: it reads like a fact and joins wrongly.
	cases := []struct {
		cx, cz int
		want   string
	}{
		{0, 0, "r.0.0"},
		{31, 31, "r.0.0"},
		{32, 32, "r.1.1"},
		{-1, -1, "r.-1.-1"},
		{-32, -32, "r.-1.-1"},
		{-33, -33, "r.-2.-2"},
		{-1, 33, "r.-1.1"},
	}

	for _, c := range cases {
		if got := server.RegionOf(c.cx, c.cz).String(); got != c.want {
			t.Errorf("RegionOf(%d, %d) = %q, want %q", c.cx, c.cz, got, c.want)
		}
	}

	// The zero value is "no region", which is what a process-wide sample has.
	if got := (server.RegionPos{}).String(); got != "" {
		t.Errorf("an unset region renders as %q, want empty", got)
	}
}

// countingObserver counts what it was given and can be made slow.
type countingObserver struct {
	seen  chan server.Sample
	block chan struct{}
}

func newCountingObserver(depth int) *countingObserver {
	return &countingObserver{seen: make(chan server.Sample, depth)}
}

func (o *countingObserver) Observe(s server.Sample) {
	if o.block != nil {
		<-o.block
	}
	select {
	case o.seen <- s:
	default:
	}
}

func TestTheDispatcherReportsItsOwnDrops(t *testing.T) {
	// The gap M11.1 left: the queue drops when it is full and said nothing, so
	// an observer under load saw less than it thought and had no way to know.
	obs := newCountingObserver(4096)
	obs.block = make(chan struct{})

	srv, err := server.New(server.WithObserver(obs))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// The worker is stuck inside Observe, so everything past the queue's
	// depth is dropped.
	const flood = 8192
	for range flood {
		srv.Observe(server.Sample{Kind: server.SampleCount, Value: 1})
	}

	dropped := srv.DroppedSamples()
	if dropped == 0 {
		t.Fatalf("flooded a blocked dispatcher with %d samples and it reported no drops", flood)
	}
	close(obs.block)

	// Reported, not merely counted: the number has to reach an observer or it
	// is a private variable, not a metric.
	got := srv.ReportDroppedSamples()
	if got.Kind != server.SampleDropped {
		t.Errorf("drop report is a %q sample, want %q", got.Kind, server.SampleDropped)
	}
	if got.Value < float64(dropped) {
		t.Errorf("drop report says %v, want at least the %d that were counted", got.Value, dropped)
	}
}

func TestASampleCarriesNoMapAndAllocatesNothing(t *testing.T) {
	// Labels was a map in M11.1 and emitted by nothing. The moment anything
	// fills one in, a map is an allocation per sample on a path that runs per
	// frame, which is the measurement becoming the load.
	labels := server.Labels{
		Player:  "Alice",
		Feature: server.FeatureChunkEncode,
		Region:  server.RegionOf(3, -4),
	}

	var sink server.Sample
	if got := testing.AllocsPerRun(100, func() {
		sink = server.Sample{Kind: server.SampleDuration, Value: 1, Labels: labels}
	}); got != 0 {
		t.Errorf("building a labelled sample allocated %v times, want 0", got)
	}
	_ = sink
}

func TestTheNetworkSinkLabelsDirectionAndLeavesPacketEmptyOnARawFrame(t *testing.T) {
	obs := newCountingObserver(4)
	sink := server.NetworkSink(obs)

	err := sink.Observe(context.Background(), protocol.Observation{
		Stage:       protocol.ObservationRawFrame,
		Direction:   protocol.DirectionServerbound,
		OriginalLen: 42,
		Elapsed:     time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}

	got := <-obs.seen
	if got.Kind != server.SampleNetworkIn {
		t.Errorf("a serverbound frame is a %q sample, want %q", got.Kind, server.SampleNetworkIn)
	}
	if got.Labels.Direction != server.DirectionIn {
		t.Errorf("direction label is %q, want %q", got.Labels.Direction, server.DirectionIn)
	}
	// A raw frame is bytes before anything decided what they mean, so there
	// is no name to report and none is invented.
	if got.Labels.Packet != "" {
		t.Errorf("a raw frame carries packet label %q, want none", got.Labels.Packet)
	}
	if got.Value != 42 {
		t.Errorf("frame size is %v, want 42", got.Value)
	}
}

func TestEveryFeatureIsInTheDeclaredList(t *testing.T) {
	// The list is the API. A sink pre-registering a series per feature reads
	// it here, so a feature that exists and is not listed is a series that
	// appears from nowhere at runtime.
	seen := map[server.Feature]bool{}
	for _, f := range server.Features() {
		if f == "" {
			t.Error("the feature list holds an empty name")
		}
		if seen[f] {
			t.Errorf("feature %q is listed twice", f)
		}
		seen[f] = true
	}
	// Fourteen: the design's thirteen plus block_write, which the per-tick
	// accumulator needed and the design had nowhere to put.
	if len(seen) != 14 {
		t.Errorf("the list holds %d features, want 14", len(seen))
	}
}
