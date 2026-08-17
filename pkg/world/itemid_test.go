package world

import (
	"errors"
	"testing"
)

// TestCounterExhaustionRefusesAndKeepsServing lives in this package because
// reaching the last counter value any other way would mean minting a trillion
// IDs.
func TestCounterExhaustionRefusesAndKeepsServing(t *testing.T) {
	m, err := NewMinter(1)
	if err != nil {
		t.Fatalf("NewMinter: %v", err)
	}

	m.next.Store(maxCounter - 1)

	id, err := m.Mint()
	if err != nil {
		t.Fatalf("the last counter value refuses to mint: %v", err)
	}
	if id.Counter() != maxCounter {
		t.Fatalf("last ID is %s, want counter %d", id, uint64(maxCounter))
	}

	if _, err := m.Mint(); !errors.Is(err, ErrIDSpaceExhausted) {
		t.Fatalf("minting past the counter gave %v, want ErrIDSpaceExhausted", err)
	}
	if _, err := m.MintN(4); !errors.Is(err, ErrIDSpaceExhausted) {
		t.Fatalf("MintN past the counter gave %v, want ErrIDSpaceExhausted", err)
	}
}

// MintN reserving in one step is what keeps two callers from interleaving a
// run of IDs; this is the boundary where that reservation overflows.
func TestMintNRefusesRatherThanWrapping(t *testing.T) {
	m, err := NewMinter(1)
	if err != nil {
		t.Fatalf("NewMinter: %v", err)
	}
	m.next.Store(maxCounter - 2)

	if _, err := m.MintN(3); !errors.Is(err, ErrIDSpaceExhausted) {
		t.Fatalf("MintN past the end gave %v, want ErrIDSpaceExhausted", err)
	}
}
