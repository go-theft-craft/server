package server

import (
	"fmt"
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
)

// Sample is one measurement.
//
// Labels carries dimensions a later milestone attributes by: a player, a
// feature, a chunk. M11.1 emits none, and the field exists now so M11.6 adds
// attribution without changing this type and every implementation of Observer
// with it.
type Sample struct {
	Kind   SampleKind
	Value  float64
	At     time.Duration
	Labels map[string]string
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
}

func newDispatcher(obs Observer) *dispatcher {
	d := &dispatcher{
		observer: obs,
		queue:    make(chan Sample, observeQueueDepth),
	}

	go d.run()

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
	}
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
