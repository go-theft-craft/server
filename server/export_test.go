package server

// Test hooks. This file is only compiled into the test binary, so nothing here
// is part of the package's surface.

// ExhaustCounterForTest walks a minter's counter to one before its last value,
// so a test can reach exhaustion without minting a trillion IDs.
func ExhaustCounterForTest(m *Minter) { m.next.Store(maxCounter - 1) }
