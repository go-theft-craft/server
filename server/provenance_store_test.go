package server_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-theft-craft/server/pkg/world"
	"github.com/go-theft-craft/server/server"
)

func newProvenance(t *testing.T, dir string) server.ProvenanceStore {
	t.Helper()

	store, err := server.FileProvenance(dir, time.Hour, 1<<30)
	if err != nil {
		t.Fatalf("FileProvenance: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	return store
}

// write appends records through a recorder and waits for them to land.
func write(t *testing.T, store server.ProvenanceStore, records ...server.Record) {
	t.Helper()

	rec := server.NewRecorder(store, slog.New(slog.DiscardHandler), server.OverflowBlock)
	for _, r := range records {
		rec.Record(r)
	}
	// Close drains, but must not close the store the caller still holds, so
	// this drains through the recorder's own queue only.
	if err := rec.Close(); err != nil {
		t.Fatalf("drain: %v", err)
	}
}

func TestAtPositionFindsPlacementAndBreak(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	store := newProvenance(t, dir)

	pos := world.BlockPos{X: 12, Y: 64, Z: -7}
	elsewhere := world.BlockPos{X: 0, Y: 0, Z: 0}

	write(
		t, store,
		server.Record{Kind: server.RecordBlock, Reason: server.ReasonPlace, Block: "minecraft:stone", Pos: pos},
		server.Record{Kind: server.RecordBlock, Reason: server.ReasonPlace, Block: "minecraft:dirt", Pos: elsewhere},
		server.Record{Kind: server.RecordBlock, Reason: server.ReasonBreak, Block: "minecraft:stone", Pos: pos},
	)

	got, err := store.AtPosition(ctx, pos, time.Hour)
	if err != nil {
		t.Fatalf("AtPosition: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("AtPosition found %d records, want the placement and the break", len(got))
	}
	reasons := map[server.Reason]bool{}
	for _, r := range got {
		reasons[r.Reason] = true
	}
	if !reasons[server.ReasonPlace] || !reasons[server.ReasonBreak] {
		t.Fatalf("found %v, want a place and a break", reasons)
	}
}

func TestByActorIsWindowed(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	store := newProvenance(t, dir)

	alice := server.Actor{Kind: server.ActorPlayer, UUID: "00000000-0000-3000-8000-00000000000a", Name: "Alpha"}
	bob := server.Actor{Kind: server.ActorPlayer, UUID: "00000000-0000-3000-8000-00000000000b", Name: "Beta"}

	write(
		t, store,
		server.Record{At: time.Now().UTC(), Kind: server.RecordBlock, Reason: server.ReasonPlace, Actor: alice},
		server.Record{At: time.Now().UTC(), Kind: server.RecordBlock, Reason: server.ReasonBreak, Actor: bob},
	)

	got, err := store.ByActor(ctx, alice.UUID, time.Hour)
	if err != nil {
		t.Fatalf("ByActor: %v", err)
	}
	if len(got) != 1 || got[0].Actor.UUID != alice.UUID {
		t.Fatalf("ByActor found %d records for one actor", len(got))
	}

	// A window that ends before the records were written finds nothing, which
	// is what makes the parameter a window rather than decoration.
	none, err := store.ByActor(ctx, alice.UUID, time.Nanosecond)
	if err != nil {
		t.Fatalf("ByActor: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("a one-nanosecond window found %d records", len(none))
	}
}

func TestForItemReturnsTheChainInOrder(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	store := newProvenance(t, dir)

	id := server.NewItemID(1, 42)
	other := server.NewItemID(1, 43)

	write(
		t, store,
		server.Record{Kind: server.RecordItem, Reason: server.ReasonMint, Items: []server.ItemID{id}},
		server.Record{Kind: server.RecordItem, Reason: server.ReasonMove, Items: []server.ItemID{other}},
		server.Record{Kind: server.RecordItem, Reason: server.ReasonDrop, Items: []server.ItemID{id}},
		server.Record{Kind: server.RecordItem, Reason: server.ReasonPickup, Items: []server.ItemID{id, other}},
	)

	got, err := store.ForItem(ctx, id)
	if err != nil {
		t.Fatalf("ForItem: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("ForItem found %d records, want 3", len(got))
	}
	want := []server.Reason{server.ReasonMint, server.ReasonDrop, server.ReasonPickup}
	for i, r := range got {
		if r.Reason != want[i] {
			t.Fatalf("record %d is %q, want %q — the chain is out of order", i, r.Reason, want[i])
		}
	}
}

// TestForItemSkipsFilesTheBloomFilterExcludes is the only way to see the
// filter working: the answer is exact either way, and only the work changes.
func TestForItemSkipsFilesTheBloomFilterExcludes(t *testing.T) {
	dir := t.TempDir()
	store := newProvenance(t, dir)

	// Several rotations, each holding a different item.
	for run := range 4 {
		write(t, store, server.Record{
			At:     time.Now().UTC().AddDate(0, 0, -run),
			Kind:   server.RecordItem,
			Reason: server.ReasonMint,
			Items:  []server.ItemID{server.NewItemID(1, uint64(run+1))},
		})
	}

	skipped, total := server.ProvenanceFilesSkipped(store, server.NewItemID(1, 1))
	if total < 2 {
		t.Skipf("only %d rotations; nothing to skip", total)
	}
	if skipped == 0 {
		t.Fatalf("the bloom filter excluded none of %d files", total)
	}
}

// TestACorruptLineIsSkippedAndCounted: a crash mid-append leaves a partial
// line, and a reader that died on it would lose every record after it.
func TestACorruptLineIsSkippedAndCounted(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	store := newProvenance(t, dir)
	pos := world.BlockPos{X: 5, Y: 5, Z: 5}
	write(
		t, store,
		server.Record{Kind: server.RecordBlock, Reason: server.ReasonPlace, Block: "minecraft:stone", Pos: pos},
	)
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// A half-written line, then a whole one after it.
	path := rotationPath(t, dir)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("open rotation: %v", err)
	}
	if _, err := f.WriteString(`{"kind":"block","reason":"br`); err != nil {
		t.Fatalf("write partial: %v", err)
	}
	f.Close()

	reopened := newProvenance(t, dir)
	write(
		t, reopened,
		server.Record{Kind: server.RecordBlock, Reason: server.ReasonBreak, Block: "minecraft:stone", Pos: pos},
	)

	got, err := reopened.AtPosition(ctx, pos, time.Hour)
	if err != nil {
		t.Fatalf("AtPosition: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("AtPosition found %d records past a corrupt line, want both whole ones", len(got))
	}
	if n := server.ProvenanceCorruptLines(reopened); n == 0 {
		t.Fatal("the corrupt line was skipped but not counted")
	}
}

// TestTheChainSurvivesARestart is the milestone's exit criterion end to end:
// place a block, stop, start, break it, and one query returns the whole chain.
func TestTheChainSurvivesARestart(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	pos := world.BlockPos{X: -20, Y: 70, Z: 33}
	id := server.NewItemID(1, 7)
	alice := server.Actor{Kind: server.ActorPlayer, UUID: "00000000-0000-3000-8000-00000000000a", Name: "Alpha"}

	first := newProvenance(t, dir)
	write(
		t, first,
		server.Record{Kind: server.RecordItem, Reason: server.ReasonMint, Items: []server.ItemID{id}, Actor: alice},
		server.Record{
			Kind: server.RecordBlock, Reason: server.ReasonPlace, Block: "minecraft:chest",
			Pos: pos, Actor: alice, Items: []server.ItemID{id},
		},
	)
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// A new process, a new store over the same directory.
	second := newProvenance(t, dir)
	write(t, second, server.Record{
		Kind: server.RecordBlock, Reason: server.ReasonBreak, Block: "minecraft:chest",
		Pos: pos, Actor: server.Actor{Kind: server.ActorServer}, Items: []server.ItemID{id},
		Cause: []server.Reason{server.ReasonBreak, server.ReasonPlace},
	})

	chain, err := second.ForItem(ctx, id)
	if err != nil {
		t.Fatalf("ForItem: %v", err)
	}
	if len(chain) != 3 {
		t.Fatalf("the chain is %d records long across a restart, want 3", len(chain))
	}
	for i, want := range []server.Reason{server.ReasonMint, server.ReasonPlace, server.ReasonBreak} {
		if chain[i].Reason != want {
			t.Fatalf("chain[%d] is %q, want %q", i, chain[i].Reason, want)
		}
	}

	// And the block's own history is there too.
	atBlock, err := second.AtPosition(ctx, pos, time.Hour)
	if err != nil {
		t.Fatalf("AtPosition: %v", err)
	}
	if len(atBlock) != 2 {
		t.Fatalf("AtPosition found %d records across a restart, want the place and the break", len(atBlock))
	}
}

func rotationPath(t *testing.T, dir string) string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".ndjson") {
			return filepath.Join(dir, e.Name())
		}
	}
	t.Fatal("no rotation file was written")

	return ""
}
