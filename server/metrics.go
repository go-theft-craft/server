package server

import (
	"context"
	"runtime"
	"syscall"
	"time"

	protocol "github.com/go-theft-craft/minecraft-protocol"
)

// networkSink turns stream observations into network samples.
//
// It counts ObservationRawFrame only. A packet record describes bytes a raw
// record already counted, so counting both would double every connection's
// traffic, and the rejected and secret stages describe no wire bytes at all.
type networkSink struct{ observer Observer }

// NetworkSink adapts an Observer to minecraft-protocol's observation sink, so
// network accounting reuses the observation points M1 already publishes rather
// than adding a second counting path that could disagree with them.
func NetworkSink(obs Observer) protocol.ObservationSink {
	if obs == nil {
		obs = NopObserver{}
	}

	return &networkSink{observer: obs}
}

// Observe records one frame's bytes.
//
// It always returns nil. Observation delivery is lossless and a sink that
// errors would fail the stream, and a metrics sink must never be able to drop
// a connection.
func (s *networkSink) Observe(_ context.Context, record protocol.Observation) error {
	if record.Stage != protocol.ObservationRawFrame {
		return nil
	}

	kind, direction := SampleNetworkOut, DirectionOut
	if record.Direction == protocol.DirectionServerbound {
		kind, direction = SampleNetworkIn, DirectionIn
	}

	// The packet name comes from the record's own metadata when it has any. A
	// raw frame does not — it is bytes before anything decided what they mean
	// — so raw frames carry direction only. Synthesizing a name the record
	// does not have would put a guess in a label that reads like a fact.
	labels := Labels{Direction: direction}
	if record.Packet != nil {
		labels.Packet = record.Packet.Name
	}

	// OriginalLen rather than len(Bytes): a redacted record drops its
	// payload but still reports the size it withheld.
	s.observer.Observe(Sample{
		Kind:   kind,
		Value:  float64(record.OriginalLen),
		At:     record.Elapsed,
		Labels: labels,
	})

	return nil
}

// SampleResources emits one CPU and one memory sample for the process.
//
// Both are process-wide rather than per-player. Attributing them to a player
// or a feature is M11.6, and it needs the world model M11.2 replaces.
func (s *Server) SampleResources() {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)

	s.Observe(Sample{Kind: SampleMemory, Value: float64(stats.HeapAlloc)})

	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		// CPU accounting is best-effort. A platform without getrusage
		// still reports memory, and a metrics gap is not a server fault.
		return
	}

	seconds := float64(usage.Utime.Sec) + float64(usage.Utime.Usec)/1e6 +
		float64(usage.Stime.Sec) + float64(usage.Stime.Usec)/1e6

	s.Observe(Sample{Kind: SampleCPU, Value: seconds})
}

// resourceSampleInterval is how often the tick loop samples process
// resources. Ten seconds is often enough to see a trend and rare enough that
// ReadMemStats, which stops the world briefly, is not itself the load.
const resourceSampleInterval = 10 * time.Second
