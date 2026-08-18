package server

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
