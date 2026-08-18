package server

import "time"

// Test hooks. This file is only compiled into the test binary, so nothing here
// is part of the package's surface.

// ReportDroppedSamples is the sample the dispatcher emits on its ten-second
// cadence, taken now. A test asserts on it rather than waiting ten seconds.
func (s *Server) ReportDroppedSamples() Sample {
	if s.dispatch == nil {
		return Sample{}
	}

	return s.dispatch.dropSample()
}

// SetClock replaces the clock Measure reads, so a test can prove the
// unobserved path never reads it at all. It restores the real one on cleanup.
func SetClock(fn func() time.Time) func() {
	previous := now
	now = fn

	return func() { now = previous }
}

// DrainSamples waits until every sample the server produced has reached the
// observer. Delivery is asynchronous by design, so a test that read the
// observer immediately would be asserting on the scheduler and a test that
// slept would be flaky on a loaded machine.
func (s *Server) DrainSamples() {
	if s.dispatch == nil {
		return
	}

	d := s.dispatch
	for d.delivered.Load()+d.dropped.Load() < d.enqueued.Load() {
		time.Sleep(100 * time.Microsecond)
	}
}
