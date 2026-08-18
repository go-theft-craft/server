package server_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/go-theft-craft/server/pkg/world"
	"github.com/go-theft-craft/server/server"
)

// Reconciliation at load.
//
// A world is written down in three places — the region file, the sidecar, and
// the player files — so the three can disagree, and something has to say so
// out loud. An external edit between two runs is one of the ways a duplication
// gets into a world, so the pass that finds one records it rather than
// absorbing it.

// stackOf is an identified stack of n items with the IDs given.
func stackOf(blockID int16, n int, ids ...world.ItemID) world.ItemStack {
	return world.ItemStack{BlockID: blockID, ItemCount: int8(n), ItemDamage: 0, IDs: ids}
}

// saveChest writes a chest into a stored world and closes the run.
func saveChest(t *testing.T, dir string, pos world.BlockPos, contents world.ChestContents) {
	t.Helper()

	srv, store := newIdentifiedServer(t, dir)
	w := srv.World()
	w.SetBlock(pos, w.Registry().Intern("minecraft:chest", nil))
	w.SetChest(pos, contents)

	snap := w.Snapshot()
	if err := store.World().SaveSnapshot(context.Background(), server.DefaultWorld, snap); err != nil {
		t.Fatalf("save world: %v", err)
	}
	if err := store.Side().SaveSnapshot(context.Background(), server.DefaultWorld, snap); err != nil {
		t.Fatalf("save sidecar: %v", err)
	}
}

func TestAnItemWithNoIDIsMintedWithAReconcileRecord(t *testing.T) {
	dir := t.TempDir()
	pos := world.BlockPos{X: 3, Y: 80, Z: 3}

	var contents world.ChestContents
	for i := range contents {
		contents[i] = world.EmptyStack
	}
	// Four cobblestone and no identity at all: a chest written by a server
	// that ran without it, or by something that was not this server.
	contents[0] = stackOf(4, 4)
	saveChest(t, dir, pos, contents)

	records := &recordingStore{}
	reopened, _ := newIdentifiedServer(t, dir, server.WithProvenance(records))

	// Reading the chest is what loads the column, which is what reconciles it.
	back := reopened.World().Chest(pos)
	if got := len(back[0].IDs); got != 4 {
		t.Fatalf("the restored stack carries %d IDs for %d items, want 4",
			got, back[0].ItemCount)
	}
	if got := reopened.Reconciled().Minted; got != 4 {
		t.Errorf("reconciliation minted %d, want 4", got)
	}

	rec := records.wait(t, 1)[0]
	if rec.Reason != server.ReasonReconcile {
		t.Errorf("record reason is %q, want %q", rec.Reason, server.ReasonReconcile)
	}
	if rec.Actor.Kind != server.ActorReconcile {
		t.Errorf("record actor is %v, want the reconcile actor", rec.Actor.Kind)
	}
	if len(rec.Items) != 4 {
		t.Errorf("record names %d items, want 4", len(rec.Items))
	}

	// Every minted ID is where the pass said it was, which is what makes the
	// index usable from the first click rather than from the first move.
	for _, id := range back[0].IDs {
		at, known := reopened.ItemIndex().Where(id)
		if !known {
			t.Fatalf("item %v was minted but the index does not have it", id)
		}
		if at.Kind != server.LocationContainer || at.Block != pos {
			t.Errorf("item %v is at %v, want the chest at %v", id, at, pos)
		}
	}
}

func TestAnIDWithNoItemIsRetiredWithAReconcileRecord(t *testing.T) {
	dir := t.TempDir()
	pos := world.BlockPos{X: 4, Y: 80, Z: 4}

	// A block with an identity, saved, and then removed from the world behind
	// the sidecar's back.
	srv, store := newIdentifiedServer(t, dir)
	w := srv.World()
	w.SetBlock(pos, w.Registry().Intern("minecraft:cobblestone", nil))
	id, err := srv.Minter().Mint()
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	srv.BlockIdentity().Set(pos, id)

	snap := w.Snapshot()
	if err := store.Side().SaveSnapshot(context.Background(), server.DefaultWorld, snap); err != nil {
		t.Fatalf("save sidecar: %v", err)
	}
	// The world is saved *without* the block: air where the sidecar says
	// something stood, which is the shape an external edit leaves behind.
	w.SetBlock(pos, w.Air())
	if err := store.World().SaveSnapshot(context.Background(), server.DefaultWorld, w.Snapshot()); err != nil {
		t.Fatalf("save world: %v", err)
	}

	records := &recordingStore{}
	reopened, _ := newIdentifiedServer(t, dir, server.WithProvenance(records))

	reopened.World().Block(pos) // load the column
	if got := reopened.Reconciled().Retired; got != 1 {
		t.Fatalf("reconciliation retired %d identities, want 1", got)
	}
	if _, ok := reopened.BlockIdentity().At(pos); ok {
		t.Error("identity for a block that is not there survived reconciliation")
	}

	rec := records.wait(t, 1)[0]
	if rec.Reason != server.ReasonReconcile || rec.Actor.Kind != server.ActorReconcile {
		t.Errorf("record is %q by %v, want a reconcile by the reconcile actor", rec.Reason, rec.Actor.Kind)
	}
	if len(rec.Items) != 1 || rec.Items[0] != id {
		t.Errorf("record names %v, want the orphaned %v", rec.Items, id)
	}
}

func TestAnExternalEditBetweenTwoRunsIsRecordedRatherThanAbsorbed(t *testing.T) {
	dir := t.TempDir()
	pos := world.BlockPos{X: 6, Y: 80, Z: 6}

	// Run one: a chest with an identified stack in it.
	first, store := newIdentifiedServer(t, dir)
	w := first.World()
	w.SetBlock(pos, w.Registry().Intern("minecraft:chest", nil))
	ids, err := first.Minter().MintN(2)
	if err != nil {
		t.Fatalf("MintN: %v", err)
	}

	var contents world.ChestContents
	for i := range contents {
		contents[i] = world.EmptyStack
	}
	contents[0] = stackOf(4, 2, ids...)
	w.SetChest(pos, contents)

	snap := w.Snapshot()
	if err := store.World().SaveSnapshot(context.Background(), server.DefaultWorld, snap); err != nil {
		t.Fatalf("save world: %v", err)
	}
	if err := store.Side().SaveSnapshot(context.Background(), server.DefaultWorld, snap); err != nil {
		t.Fatalf("save sidecar: %v", err)
	}

	// Between the runs, something else writes the region: the same chest now
	// holds five of the item and still carries only the two IDs. That is what
	// an editor, a restored backup, or a duplication looks like from here.
	edited := contents
	edited[0] = stackOf(4, 5, ids...)
	saveChest(t, dir, pos, edited)

	// Run two.
	records := &recordingStore{}
	second, _ := newIdentifiedServer(t, dir, server.WithProvenance(records))

	back := second.World().Chest(pos)
	if got := int(back[0].ItemCount); got != 5 {
		t.Fatalf("the edited chest came back with %d items, want the 5 that were written", got)
	}
	if got := len(back[0].IDs); got != 5 {
		t.Fatalf("the edited stack carries %d IDs for 5 items; the invariant is one each", got)
	}
	if got := second.Reconciled().Minted; got != 3 {
		t.Fatalf("reconciliation minted %d for the three unaccounted items, want 3", got)
	}

	rec := records.wait(t, 1)[0]
	if rec.Note == "" {
		t.Error("the record does not say what was found; a count with no sentence is not a finding")
	}
	if len(rec.Items) != 3 {
		t.Errorf("record names %d items, want the 3 that were unaccounted for", len(rec.Items))
	}
}

func TestAStaleSidecarIsDetectedByItsGenerationStamp(t *testing.T) {
	dir := t.TempDir()
	pos := world.BlockPos{X: 8, Y: 80, Z: 8}

	srv, store := newIdentifiedServer(t, dir)
	w := srv.World()
	w.SetBlock(pos, w.Registry().Intern("minecraft:cobblestone", nil))
	snap := w.Snapshot()
	if err := store.World().SaveSnapshot(context.Background(), server.DefaultWorld, snap); err != nil {
		t.Fatalf("save world: %v", err)
	}
	if err := store.Side().SaveSnapshot(context.Background(), server.DefaultWorld, snap); err != nil {
		t.Fatalf("save sidecar: %v", err)
	}

	// Every chunk a second run loads is stale by this measure, and that is the
	// finding rather than a flaw in the test. The generation is a per-run
	// counter and the world file does not carry it, so a sidecar written by a
	// previous run can never carry a stamp a fresh load will agree with. The
	// stamp is therefore a request to reconcile, not a licence to trust —
	// which is why nothing is discarded when it disagrees.
	reopened, _ := newIdentifiedServer(t, dir)
	reopened.World().Block(pos)

	got := reopened.Reconciled()
	if got.Stale == 0 {
		t.Fatalf("a sidecar from a previous run was accepted as current: %s", got)
	}
	if got.Chunks == 0 {
		t.Fatal("no column was reconciled")
	}
}

// recordingStore keeps every record the recorder wrote, so a test can assert
// on what reconciliation said rather than only on what it counted.
type recordingStore struct {
	mu  sync.Mutex
	got []server.Record
}

func (s *recordingStore) Append(_ context.Context, records []server.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, records...)

	return nil
}

func (s *recordingStore) AtPosition(context.Context, world.BlockPos, time.Duration) ([]server.Record, error) {
	return nil, nil
}

func (s *recordingStore) ByActor(context.Context, string, time.Duration) ([]server.Record, error) {
	return nil, nil
}

func (s *recordingStore) ForItem(context.Context, server.ItemID) ([]server.Record, error) {
	return nil, nil
}

func (s *recordingStore) Close() error { return nil }

// wait blocks until at least n records have arrived. Recording runs off the
// caller's goroutine by design, so a test that read immediately would be
// asserting on the scheduler.
func (s *recordingStore) wait(t *testing.T, n int) []server.Record {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for {
		s.mu.Lock()
		got := append([]server.Record(nil), s.got...)
		s.mu.Unlock()

		if len(got) >= n {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("waited for %d records, got %d", n, len(got))
		}
		time.Sleep(time.Millisecond)
	}
}

// TestAChestKeepsItsItemIdentityAcrossARestart is the property the milestone
// record had to admit it did not have: a chest's contents lost their identity
// every time the server stopped, so a chain broke at every restart. The
// vanilla format has a field for the items and none for their identity, which
// is exactly what the sidecar is for.
func TestAChestKeepsItsItemIdentityAcrossARestart(t *testing.T) {
	dir := t.TempDir()
	pos := world.BlockPos{X: 11, Y: 80, Z: 11}

	first, store := newIdentifiedServer(t, dir)
	w := first.World()
	w.SetBlock(pos, w.Registry().Intern("minecraft:chest", nil))
	ids, err := first.Minter().MintN(3)
	if err != nil {
		t.Fatalf("MintN: %v", err)
	}

	var contents world.ChestContents
	for i := range contents {
		contents[i] = world.EmptyStack
	}
	contents[4] = stackOf(4, 3, ids...)
	w.SetChest(pos, contents)

	snap := w.Snapshot()
	if err := store.World().SaveSnapshot(context.Background(), server.DefaultWorld, snap); err != nil {
		t.Fatalf("save world: %v", err)
	}
	if err := store.Side().SaveSnapshot(context.Background(), server.DefaultWorld, snap); err != nil {
		t.Fatalf("save sidecar: %v", err)
	}

	reopened, _ := newIdentifiedServer(t, dir)
	back := reopened.World().Chest(pos)

	if got := back[4].IDs; len(got) != 3 {
		t.Fatalf("the restored stack carries %v, want the three it was saved with", got)
	}
	for i, id := range back[4].IDs {
		if id != ids[i] {
			t.Errorf("slot 4 item %d came back as %v, want %v", i, id, ids[i])
		}
	}
	if got := reopened.Reconciled().Minted; got != 0 {
		t.Errorf("reconciliation minted %d for a chest that came back whole, want 0", got)
	}
}
