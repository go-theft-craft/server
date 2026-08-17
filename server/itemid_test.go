package server_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/go-theft-craft/server/server"
)

func TestAnIDCarriesItsEpochAndCounter(t *testing.T) {
	for _, tc := range []struct {
		epoch   uint32
		counter uint64
	}{
		{0, 1},
		{1, 1},
		{4095, 1 << 30},
		{1<<24 - 1, 1<<40 - 1},
	} {
		id := server.NewItemID(tc.epoch, tc.counter)
		if id.Epoch() != tc.epoch || id.Counter() != tc.counter {
			t.Errorf("NewItemID(%d, %d) split back to %d, %d", tc.epoch, tc.counter, id.Epoch(), id.Counter())
		}
		if !id.Valid() {
			t.Errorf("NewItemID(%d, %d) reads as invalid", tc.epoch, tc.counter)
		}
	}

	if server.NoItemID.Valid() {
		t.Error("the zero value reads as a valid ID")
	}
}

func TestMintingRunsFromOne(t *testing.T) {
	m, err := server.NewMinter(3)
	if err != nil {
		t.Fatalf("NewMinter: %v", err)
	}

	first, err := m.Mint()
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if first.Epoch() != 3 || first.Counter() != 1 {
		t.Fatalf("first ID is %s, want 3:1", first)
	}

	second, _ := m.Mint()
	if second.Counter() != 2 {
		t.Fatalf("second ID is %s, want counter 2", second)
	}

	batch, err := m.MintN(3)
	if err != nil {
		t.Fatalf("MintN: %v", err)
	}
	for i, id := range batch {
		if id.Counter() != uint64(3+i) {
			t.Errorf("batch[%d] is %s, want counter %d", i, id, 3+i)
		}
	}
}

// TestTheEpochIsPersistedRatherThanDerivedFromTheClock is the design's reason
// for the split: a clock that moves backwards mints colliding IDs, and
// collision is the one failure that makes the whole structure worthless.
func TestTheEpochIsPersistedRatherThanDerivedFromTheClock(t *testing.T) {
	// Two minters built from the same stored epoch produce the same first ID,
	// which is exactly why the stored value must advance between runs.
	a, _ := server.NewMinter(9)
	b, _ := server.NewMinter(9)

	first, _ := a.Mint()
	same, _ := b.Mint()
	if first != same {
		t.Fatal("two minters on one epoch disagreed; the epoch is not what separates runs")
	}

	next, err := server.NextEpoch(9)
	if err != nil {
		t.Fatalf("NextEpoch: %v", err)
	}
	if next != 10 {
		t.Fatalf("NextEpoch(9) = %d, want 10", next)
	}

	c, _ := server.NewMinter(next)
	fromNextRun, _ := c.Mint()
	if fromNextRun == first {
		t.Fatal("the next run reissued the previous run's first ID")
	}
}

// TestTheEpochAdvancesOncePerStart drives it through the server, which is
// where the persistence actually happens.
func TestTheEpochAdvancesOncePerStart(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	var epochs []uint32
	for range 3 {
		srv, store := newStoredServer(t, dir)
		if _, err := srv.Load(ctx); err != nil {
			t.Fatalf("Load: %v", err)
		}
		srv.SaveAll()

		level, found, err := store.World().Level(ctx, server.DefaultWorld)
		if err != nil || !found {
			t.Fatalf("Level: %v (found=%v)", err, found)
		}
		epochs = append(epochs, level.ItemEpoch)
	}

	for i := 1; i < len(epochs); i++ {
		if epochs[i] <= epochs[i-1] {
			t.Fatalf("epochs did not advance across starts: %v", epochs)
		}
	}
}

func TestEpochExhaustionRefusesAndKeepsServing(t *testing.T) {
	const last = 1<<24 - 1

	if _, err := server.NextEpoch(last); !errors.Is(err, server.ErrIDSpaceExhausted) {
		t.Fatalf("NextEpoch(%d) = %v, want ErrIDSpaceExhausted", last, err)
	}
	if _, err := server.NewMinter(last + 1); !errors.Is(err, server.ErrIDSpaceExhausted) {
		t.Fatal("a minter was built for an epoch past the last one")
	}

	// The last epoch itself still mints: refusing to advance is not refusing
	// to serve.
	m, err := server.NewMinter(last)
	if err != nil {
		t.Fatalf("NewMinter(%d): %v", last, err)
	}
	if _, err := m.Mint(); err != nil {
		t.Fatalf("the last epoch refuses to mint: %v", err)
	}
}

func TestMintingIsSafeUnderConcurrentCallers(t *testing.T) {
	m, err := server.NewMinter(2)
	if err != nil {
		t.Fatalf("NewMinter: %v", err)
	}

	const callers, each = 8, 500

	var wg sync.WaitGroup
	results := make([][]server.ItemID, callers)
	for g := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ids := make([]server.ItemID, 0, each*2)
			for range each {
				one, err := m.Mint()
				if err != nil {
					t.Errorf("Mint: %v", err)

					return
				}
				batch, err := m.MintN(2)
				if err != nil {
					t.Errorf("MintN: %v", err)

					return
				}
				ids = append(ids, one)
				ids = append(ids, batch...)
			}
			results[g] = ids
		}()
	}
	wg.Wait()

	seen := map[server.ItemID]bool{}
	for _, ids := range results {
		for _, id := range ids {
			if seen[id] {
				t.Fatalf("%s was minted twice", id)
			}
			seen[id] = true
		}
	}
	if len(seen) != callers*each*3 {
		t.Fatalf("minted %d distinct IDs, want %d", len(seen), callers*each*3)
	}
}
