package server_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-theft-craft/server/pkg/world"
	"github.com/go-theft-craft/server/server"
)

// blockingStore stalls in Append, so a test can fill the recorder's queue.
type blockingStore struct {
	release chan struct{}
	mu      sync.Mutex
	got     []server.Record
}

func newBlockingStore() *blockingStore {
	return &blockingStore{release: make(chan struct{})}
}

func (s *blockingStore) Append(_ context.Context, records []server.Record) error {
	<-s.release
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, records...)

	return nil
}

func (s *blockingStore) AtPosition(context.Context, world.BlockPos, time.Duration) ([]server.Record, error) {
	return nil, nil
}

func (s *blockingStore) ByActor(context.Context, string, time.Duration) ([]server.Record, error) {
	return nil, nil
}

func (s *blockingStore) ForItem(context.Context, server.ItemID) ([]server.Record, error) {
	return nil, nil
}

func (s *blockingStore) Close() error { return nil }

func (s *blockingStore) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.got)
}

// TestRecordingNeverBlocksTheCaller is M11.1's non-blocking observer property
// applied to a second queue: the tick must not wait for a disk.
func TestRecordingNeverBlocksTheCaller(t *testing.T) {
	store := newBlockingStore()
	rec := server.NewRecorder(store, slog.New(slog.DiscardHandler), server.OverflowDrop)

	done := make(chan struct{})
	go func() {
		defer close(done)
		// Far more than the queue holds, into a store that is not draining.
		for range 50_000 {
			rec.Record(server.Record{Kind: server.RecordItem, Reason: server.ReasonMove})
		}
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("recording blocked the caller")
	}

	if rec.Dropped() == 0 {
		t.Fatal("nothing was dropped; the queue cannot have been full")
	}

	close(store.release)
	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestOverflowDropsCountsAndWarnsAtMostOncePerInterval: the condition that
// fills the queue also produces a warning per record, so an unlimited warning
// turns one problem into two.
func TestOverflowDropsCountsAndWarnsAtMostOncePerInterval(t *testing.T) {
	store := newBlockingStore()
	logged := &strings.Builder{}
	rec := server.NewRecorder(store, slog.New(slog.NewTextHandler(logged, nil)), server.OverflowDrop)

	for range 40_000 {
		rec.Record(server.Record{Kind: server.RecordItem, Reason: server.ReasonMove})
	}

	if warnings := strings.Count(logged.String(), "provenance records are being dropped"); warnings != 1 {
		t.Fatalf("%d overflow warnings for one burst, want 1", warnings)
	}
	if rec.Dropped() < 1000 {
		t.Fatalf("only %d records dropped for a 40,000-record burst", rec.Dropped())
	}

	close(store.release)
	_ = rec.Close()
}

// TestOverflowBlocksWhenAskedTo: an operator may value a complete trail over
// a world that never stutters.
func TestOverflowBlocksWhenAskedTo(t *testing.T) {
	store := newBlockingStore()
	rec := server.NewRecorder(store, slog.New(slog.DiscardHandler), server.OverflowBlock)

	blocked := make(chan struct{})
	go func() {
		defer close(blocked)
		for range 20_000 {
			rec.Record(server.Record{Kind: server.RecordItem, Reason: server.ReasonMove})
		}
	}()

	select {
	case <-blocked:
		t.Fatal("recording did not block under OverflowBlock")
	case <-time.After(200 * time.Millisecond):
	}

	close(store.release)
	<-blocked

	if rec.Dropped() != 0 {
		t.Fatalf("%d records were dropped under OverflowBlock", rec.Dropped())
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if store.count() != 20_000 {
		t.Fatalf("the store got %d of 20,000 records", store.count())
	}
}

// TestACauseChainIsBoundedInDepth: a chain that referred to itself would
// otherwise be written forever.
func TestACauseChainIsBoundedInDepth(t *testing.T) {
	store := newBlockingStore()
	close(store.release)
	rec := server.NewRecorder(store, slog.New(slog.DiscardHandler), server.OverflowBlock)

	long := make([]server.Reason, 40)
	for i := range long {
		long[i] = server.ReasonMove
	}
	rec.Record(server.Record{Kind: server.RecordItem, Reason: server.ReasonBreak, Cause: long})

	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.got) != 1 {
		t.Fatalf("the store got %d records, want 1", len(store.got))
	}
	if depth := len(store.got[0].Cause); depth > 8 {
		t.Fatalf("cause chain is %d deep, want at most 8", depth)
	}
}

// TestBlockRecordsCarryCanonicalNamesNotHandles: a handle is meaningful only
// to the process that minted it, and a record outlives the process.
func TestBlockRecordsCarryCanonicalNamesNotHandles(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	store, err := server.FileProvenance(dir, time.Hour, 1<<20)
	if err != nil {
		t.Fatalf("FileProvenance: %v", err)
	}
	rec := server.NewRecorder(store, slog.New(slog.DiscardHandler), server.OverflowBlock)

	rec.Record(server.Record{
		Kind:   server.RecordBlock,
		Reason: server.ReasonPlace,
		Block:  "minecraft:chest",
		Pos:    world.BlockPos{X: 1, Y: 2, Z: 3},
		Actor:  server.Actor{Kind: server.ActorPlayer, UUID: "u", Name: "Fixture"},
	})
	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reader, err := server.FileProvenance(dir, time.Hour, 1<<20)
	if err != nil {
		t.Fatalf("FileProvenance: %v", err)
	}
	defer reader.Close()

	got, err := reader.AtPosition(ctx, world.BlockPos{X: 1, Y: 2, Z: 3}, time.Hour)
	if err != nil {
		t.Fatalf("AtPosition: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("AtPosition found %d records, want 1", len(got))
	}
	if got[0].Block != "minecraft:chest" {
		t.Fatalf("record names block %q, want the canonical name", got[0].Block)
	}
	// The raw file must hold the name too, not a number.
	if !fileContains(t, dir, "minecraft:chest") {
		t.Fatal("the file does not hold the canonical block name")
	}
}

func fileContains(t *testing.T, dir, needle string) bool {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		if strings.Contains(string(raw), needle) {
			return true
		}
	}

	return false
}

func TestWithProvenanceRejectsNil(t *testing.T) {
	if _, err := server.New(server.WithProvenance(nil)); !errors.Is(err, server.ErrInvalidOption) {
		t.Error("WithProvenance accepted nil; omit the option to run without it")
	}
}

func TestANilRecorderIsANoOp(t *testing.T) {
	var rec *server.Recorder

	rec.Record(server.Record{Kind: server.RecordItem})
	rec.RecordDuplicate(nil)
	if rec.Dropped() != 0 {
		t.Error("a nil recorder counted a drop")
	}
	if err := rec.Close(); err != nil {
		t.Errorf("closing a nil recorder: %v", err)
	}
}
