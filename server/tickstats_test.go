package server_test

import (
	"testing"

	"github.com/go-theft-craft/server/server"
)

// The per-tick accumulator.
//
// The property is not that the counts are right — a map increment is not the
// hard part. It is that a thousand events become one sample, because the
// alternative is a metrics pipeline that falls over on a busy server and takes
// the server with it.

func TestAThousandBlockWritesProduceOneSamplePerTick(t *testing.T) {
	obs := &collector{}
	srv := newObservedServer(t, obs)

	for range 1000 {
		srv.CountPerTick(server.FeatureBlockWrite, "Alice", 1)
	}

	srv.FlushTickStats()
	srv.DrainSamples()

	got := obs.byFeature(server.FeatureBlockWrite)
	if len(got) != 1 {
		t.Fatalf("1,000 block writes produced %d samples, want 1", len(got))
	}
	if got[0].Value != 1000 {
		t.Errorf("the one sample says %v writes, want 1000", got[0].Value)
	}
	if got[0].Kind != server.SampleCount {
		t.Errorf("the flush produced a %q sample, want %q", got[0].Kind, server.SampleCount)
	}
	if got[0].Labels.Player != "Alice" {
		t.Errorf("the sample names player %q, want Alice", got[0].Labels.Player)
	}
}

func TestTheAccumulatorIsFreeWhenUnobserved(t *testing.T) {
	srv, err := server.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if got := testing.AllocsPerRun(1000, func() {
		srv.CountPerTick(server.FeatureBlockWrite, "Alice", 1)
	}); got != 0 {
		t.Errorf("an unobserved count allocated %v times per run, want 0", got)
	}
	if got := srv.PendingTickStats(); got != 0 {
		t.Errorf("an unobserved server accumulated %d entries, want 0", got)
	}
}

func TestFlushEmitsOneSamplePerFeatureAndPlayer(t *testing.T) {
	obs := &collector{}
	srv := newObservedServer(t, obs)

	for range 10 {
		srv.CountPerTick(server.FeatureBlockWrite, "Alice", 1)
		srv.CountPerTick(server.FeatureBlockWrite, "Bob", 1)
		srv.CountPerTick(server.FeatureInventory, "Alice", 1)
	}
	if got := srv.PendingTickStats(); got != 3 {
		t.Fatalf("30 events made %d pairs, want 3", got)
	}

	srv.FlushTickStats()
	srv.DrainSamples()

	if got := len(obs.byFeature(server.FeatureBlockWrite)); got != 2 {
		t.Errorf("two players writing blocks produced %d samples, want one each", got)
	}
	if got := len(obs.byFeature(server.FeatureInventory)); got != 1 {
		t.Errorf("one player clicking produced %d samples, want 1", got)
	}

	// A flush empties it: a count that survived would be reported again next
	// tick, and a counter that only ever grows is a different metric from the
	// one this claims to be.
	if got := srv.PendingTickStats(); got != 0 {
		t.Errorf("%d entries survived the flush, want 0", got)
	}
}

func TestTheGaugesReportWhatTheServerIsHolding(t *testing.T) {
	obs := &collector{}
	srv := newObservedServer(t, obs)

	srv.SampleLevels()
	srv.DrainSamples()

	// Chunks resident is the one that starts nonzero on any server that has
	// generated anything, and zero here is the correct answer for a server
	// nobody has joined.
	found := false
	for _, s := range obs.byFeature(server.FeatureChunkLoad) {
		if s.Kind == server.SampleGauge {
			found = true
		}
	}
	if !found {
		t.Error("no chunks-resident gauge was emitted")
	}
	if len(obs.byFeature(server.FeatureLogin)) == 0 {
		t.Error("no players-online gauge was emitted")
	}
}
