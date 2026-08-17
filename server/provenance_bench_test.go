package server_test

import (
	"log/slog"
	"testing"
	"time"

	"github.com/go-theft-craft/server/pkg/world"
	"github.com/go-theft-craft/server/server"
)

// TestProvenanceOffAllocatesNothingExtra is the milestone's exit criterion,
// written as a test rather than a benchmark so CI fails on a regression
// instead of printing a number nobody reads.
//
// A server without provenance has a nil recorder, and every recording call
// site is a nil-receiver method that returns immediately. If that ever stops
// being true — a record built before the nil check, a slice allocated for
// items nobody will read — this is what says so.
func TestProvenanceOffAllocatesNothingExtra(t *testing.T) {
	var off *server.Recorder

	allocs := testing.AllocsPerRun(1000, func() {
		off.Record(server.Record{Kind: server.RecordItem, Reason: server.ReasonMove})
		off.RecordDuplicate(nil)
	})
	if allocs != 0 {
		t.Fatalf("recording with provenance off allocated %v times per call, want 0", allocs)
	}
}

// TestASeverWithoutProvenanceHasNoRecorder is the other half: the option is
// what creates the queue and the goroutine, and omitting it creates neither.
func TestASeverWithoutProvenanceHasNoRecorder(t *testing.T) {
	srv, err := server.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if srv.Recorder() != nil {
		t.Error("a server built without WithProvenance has a recorder")
	}
	if srv.ItemIndex() != nil {
		t.Error("a server built without WithItemIdentity has an item index")
	}
}

func TestWithItemIdentityBuildsAnIndex(t *testing.T) {
	srv, err := server.New(server.WithItemIdentity(server.DuplicateAllow))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	index := srv.ItemIndex()
	if index == nil {
		t.Fatal("WithItemIdentity built no index")
	}

	ids, err := index.Mint(3, world.Location{Kind: world.LocationInventory, Player: "alice"}, world.Actor{})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if len(ids) != 3 || index.Len() != 3 {
		t.Fatalf("minted %d IDs into an index of %d", len(ids), index.Len())
	}
}

// BenchmarkRecordWithProvenanceOff is the cost of the call sites when nobody
// asked for records: one nil check.
func BenchmarkRecordWithProvenanceOff(b *testing.B) {
	var off *server.Recorder

	for b.Loop() {
		off.Record(server.Record{Kind: server.RecordItem, Reason: server.ReasonMove})
	}
}

// BenchmarkRecordWithProvenanceOn is the cost with a store behind it: a
// marshal and a channel send, both off the tick.
func BenchmarkRecordWithProvenanceOn(b *testing.B) {
	store, err := server.FileProvenance(b.TempDir(), time.Hour, 1<<30)
	if err != nil {
		b.Fatalf("FileProvenance: %v", err)
	}
	rec := server.NewRecorder(store, slog.New(slog.DiscardHandler), server.OverflowDrop)
	b.Cleanup(func() { _ = rec.Close() })

	id := server.NewItemID(1, 1)

	for b.Loop() {
		rec.Record(server.Record{
			Kind:   server.RecordItem,
			Reason: server.ReasonMove,
			Items:  []server.ItemID{id},
			Actor:  server.Actor{Kind: server.ActorPlayer, UUID: "u", Name: "Fixture"},
		})
	}
}
