package server

import (
	"fmt"
	"sync/atomic"
	"time"
)

// SampleKind names what a sample measures.
type SampleKind string

const (
	// SampleCPU is process CPU time consumed, in seconds.
	SampleCPU SampleKind = "cpu"
	// SampleMemory is heap bytes currently allocated.
	SampleMemory SampleKind = "memory"
	// SampleNetworkIn is bytes read from a connection, counted at the frame.
	SampleNetworkIn SampleKind = "network_in"
	// SampleNetworkOut is bytes written to a connection, counted at the frame.
	SampleNetworkOut SampleKind = "network_out"
	// SampleDuration is seconds a piece of work took, from Measure.
	SampleDuration SampleKind = "duration"
	// SampleCount is how many times something happened.
	SampleCount SampleKind = "count"
	// SampleBytes is a payload size.
	SampleBytes SampleKind = "bytes"
	// SampleGauge is a level rather than an event: players online, chunks
	// resident, identified items live.
	SampleGauge SampleKind = "gauge"
	// SampleDropped is samples the dispatcher discarded because its queue was
	// full. It closes the gap M11.1 left: without it an observer under load
	// silently sees less than it thinks, and the graph that goes quiet looks
	// like a server that went quiet.
	SampleDropped SampleKind = "dropped"
)

// Sample is one measurement.
//
// Labels is a closed struct rather than a map. M11.1 declared it as a map and
// emitted none; a map allocates per sample, and some of these are built per
// frame, so the type changed the moment anything started filling it in. See
// labels.go for what a call site may attribute a sample to and why the set is
// closed.
type Sample struct {
	Kind   SampleKind
	Value  float64
	At     time.Duration
	Labels Labels
}

// Observer receives samples.
//
// An implementation should not block, but the server does not trust it not to:
// samples are emitted from the tick loop and from stream goroutines, and a slow
// observer that applied backpressure would slow the server it was only supposed
// to be watching. Delivery is therefore asynchronous and lossy under pressure.
type Observer interface {
	Observe(s Sample)
}

// NopObserver discards every sample. It is the default, so nothing has to
// nil-check an observer on a hot path.
type NopObserver struct{}

// Observe discards the sample.
func (NopObserver) Observe(Sample) {}

// WithObserver supplies an observer. Omit the option to run without one; a nil
// observer is an error because "I passed an observer and got no samples" is
// harder to diagnose than a rejected option.
func WithObserver(obs Observer) Option {
	return func(b *builder) error {
		if obs == nil {
			return fmt.Errorf("%w: nil observer, omit WithObserver to run without one", ErrInvalidOption)
		}
		b.observer = obs

		return nil
	}
}

// observeQueueDepth is how many samples may be in flight before delivery
// starts dropping. It is large enough to absorb a burst from every connection
// in a tick and small enough that a stalled observer cannot grow without
// bound.
const observeQueueDepth = 1024

// dispatcher delivers samples to an observer without ever blocking its caller.
//
// A sample is a measurement, not a record: losing one under pressure costs a
// point on a graph, while waiting for a stuck observer costs the connection
// that was trying to report it. So the queue drops when it is full.
//
// The worker runs for the life of the server. There is no Close in M11.1, and
// a server is a long-lived object, so this is a goroutine per observed server
// rather than a leak that grows with load.
type dispatcher struct {
	observer Observer
	queue    chan Sample
	dropped  atomic.Uint64
}

func newDispatcher(obs Observer) *dispatcher {
	d := &dispatcher{
		observer: obs,
		queue:    make(chan Sample, observeQueueDepth),
	}

	go d.run()
	go d.reportDrops(dropReportInterval)

	return d
}

func (d *dispatcher) run() {
	for sample := range d.queue {
		d.observer.Observe(sample)
	}
}

// Observe enqueues a sample, dropping it when the queue is full.
func (d *dispatcher) Observe(sample Sample) {
	select {
	case d.queue <- sample:
	default:
		d.dropped.Add(1)
	}
}

// Dropped is how many samples the queue discarded over the life of the server.
func (d *dispatcher) Dropped() uint64 { return d.dropped.Load() }

// dropReportInterval is how often the drop count is emitted. It is the same
// ten seconds the resource samples use, because a level does not need to be
// reported twenty times a second and the number this reports is a level.
const dropReportInterval = 10 * time.Second

// reportDrops emits the running drop total on its own goroutine.
//
// It is a cumulative total rather than a per-interval delta, so a consumer
// that misses an interval does not lose the drops in it — and a drop report
// that was itself dropped would be the joke this whole sample exists to
// prevent. It is emitted directly to the observer rather than through the
// queue for the same reason.
func (d *dispatcher) reportDrops(every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()

	for range ticker.C {
		if d.dropped.Load() == 0 {
			continue
		}
		d.observer.Observe(d.dropSample())
	}
}

// dropSample is the running total, as a sample.
func (d *dispatcher) dropSample() Sample {
	return Sample{Kind: SampleDropped, Value: float64(d.dropped.Load())}
}

// Observe forwards a sample to the configured observer. It returns without
// waiting for the observer to handle it.
func (s *Server) Observe(sample Sample) {
	if s.dispatch == nil {
		return
	}

	s.dispatch.Observe(sample)
}

// observed reports whether the server was built with a real observer. Work
// that only exists to produce samples is skipped when it was not.
func (s *Server) observed() bool { return s.dispatch != nil }

// DroppedSamples is how many samples never reached the observer because the
// delivery queue was full.
//
// It is also emitted as a SampleDropped every ten seconds. Both exist because
// the two failure shapes are different: a consumer reading the sample learns
// about a gap in its own graph, and an operator reading this learns it without
// having a working metrics pipeline, which is the state they are usually in
// when they ask.
func (s *Server) DroppedSamples() uint64 {
	if s.dispatch == nil {
		return 0
	}

	return s.dispatch.Dropped()
}
