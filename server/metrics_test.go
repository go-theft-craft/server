package server_test

import (
	"testing"

	protocol "github.com/go-theft-craft/minecraft-protocol"

	"github.com/go-theft-craft/server/server"
)

func TestTheNetworkSinkCountsRawFrameBytesInBothDirections(t *testing.T) {
	obs := &collectingObserver{}
	sink := server.NetworkSink(obs)

	inbound := protocol.Observation{
		Stage:       protocol.ObservationRawFrame,
		Direction:   protocol.DirectionServerbound,
		OriginalLen: 100,
	}
	outbound := protocol.Observation{
		Stage:       protocol.ObservationRawFrame,
		Direction:   protocol.DirectionClientbound,
		OriginalLen: 250,
	}

	if err := sink.Observe(t.Context(), inbound); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if err := sink.Observe(t.Context(), outbound); err != nil {
		t.Fatalf("Observe: %v", err)
	}

	if got := obs.count(server.SampleNetworkIn); got != 1 {
		t.Errorf("%d network-in samples, want 1", got)
	}
	if got := obs.count(server.SampleNetworkOut); got != 1 {
		t.Errorf("%d network-out samples, want 1", got)
	}
}

func TestTheNetworkSinkIgnoresEveryStageButRawFrame(t *testing.T) {
	// A packet record describes the same bytes a raw record already
	// counted, so counting both would double every connection's traffic.
	obs := &collectingObserver{}
	sink := server.NetworkSink(obs)

	for _, stage := range []protocol.ObservationStage{
		protocol.ObservationPacket,
		protocol.ObservationRejected,
		protocol.ObservationSecret,
	} {
		record := protocol.Observation{
			Stage:       stage,
			Direction:   protocol.DirectionServerbound,
			OriginalLen: 100,
		}
		if err := sink.Observe(t.Context(), record); err != nil {
			t.Fatalf("Observe %s: %v", stage, err)
		}
	}

	if got := len(obs.samples); got != 0 {
		t.Errorf("sink emitted %d samples for non-frame stages, want 0", got)
	}
}

func TestTheNetworkSinkUsesOriginalLenSoARedactedRecordStillCounts(t *testing.T) {
	obs := &collectingObserver{}
	sink := server.NetworkSink(obs)

	record := protocol.Observation{
		Stage:       protocol.ObservationRawFrame,
		Direction:   protocol.DirectionServerbound,
		OriginalLen: 512,
		Redacted:    true,
	}
	if err := sink.Observe(t.Context(), record); err != nil {
		t.Fatalf("Observe: %v", err)
	}

	obs.mu.Lock()
	defer obs.mu.Unlock()

	if len(obs.samples) != 1 {
		t.Fatalf("%d samples, want 1", len(obs.samples))
	}
	if got := obs.samples[0].Value; got != 512 {
		t.Errorf("counted %v bytes, want 512 from OriginalLen", got)
	}
}

func TestResourceSamplesReportPlausibleValues(t *testing.T) {
	obs := &collectingObserver{}

	srv, err := server.New(server.WithObserver(obs))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv.SampleResources()

	obs.await(t, server.SampleMemory, 1)

	obs.mu.Lock()
	defer obs.mu.Unlock()

	for _, s := range obs.samples {
		if s.Kind == server.SampleMemory && s.Value <= 0 {
			t.Errorf("memory sample is %v, want a positive byte count", s.Value)
		}
	}
}
