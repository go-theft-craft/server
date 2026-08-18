package server_test

import (
	"context"
	"testing"

	"github.com/go-theft-craft/server/config"
	"github.com/go-theft-craft/server/pkg/world"
	"github.com/go-theft-craft/server/server"
)

// Block identity across a restart.
//
// The table is in memory and the sidecar is where it survives. These are the
// two halves that have to agree: what a save writes and what the next start
// reads back when the chunk it belongs to comes off disk.

// newIdentifiedServer is a stored server with item identity, and therefore
// block identity, switched on.
func newIdentifiedServer(t *testing.T, dir string, extra ...server.Option) (*server.Server, *server.Storage) {
	t.Helper()

	settings := config.DefaultConfig()
	settings.GeneratorType = config.GeneratorFlat

	store, err := server.FileStore(dir, nil)
	if err != nil {
		t.Fatalf("FileStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	opts := append(
		store.Options(),
		server.WithSettings(settings),
		server.WithItemIdentity(server.DuplicateAllow),
	)
	opts = append(opts, extra...)

	srv, err := server.New(opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return srv, store
}

func TestBlockIdentitySurvivesARestartThroughTheSidecar(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	srv, store := newIdentifiedServer(t, dir)
	w := srv.World()
	pos := world.BlockPos{X: 5, Y: 80, Z: 9}
	w.SetBlock(pos, w.Registry().Intern("minecraft:cobblestone", nil))

	id, err := srv.Minter().Mint()
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	srv.BlockIdentity().Set(pos, id)

	snap := w.Snapshot()
	if err := store.World().SaveSnapshot(ctx, server.DefaultWorld, snap); err != nil {
		t.Fatalf("save world: %v", err)
	}
	if err := store.Side().SaveSnapshot(ctx, server.DefaultWorld, snap); err != nil {
		t.Fatalf("save sidecar: %v", err)
	}

	// A second start, reading the same directory. Loading the column is what
	// brings its identity resident: nothing else knows which chunks have any.
	reopened, _ := newIdentifiedServer(t, dir)
	if got := reopened.World().Block(pos); got != reopened.World().Registry().Intern("minecraft:cobblestone", nil) {
		t.Fatalf("the block did not come back; identity has nothing to attach to")
	}

	got, ok := reopened.BlockIdentity().At(pos)
	if !ok {
		t.Fatal("the block came back without its identity")
	}
	if got != id {
		t.Errorf("identity came back as %v, want %v", got, id)
	}
}

// A server without item identity writes the sidecar it always wrote and
// carries no table, which is the off-by-default property in the one place a
// format change could have broken it.
func TestASidecarWrittenWithoutIdentityStaysEmpty(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	srv, store := newStoredServer(t, dir)
	w := srv.World()
	w.SetBlock(world.BlockPos{X: 1, Y: 80, Z: 1}, w.Registry().Intern("minecraft:cobblestone", nil))

	if srv.BlockIdentity() != nil {
		t.Fatal("a server without item identity built a block identity table")
	}

	snap := w.Snapshot()
	if err := store.Side().SaveSnapshot(ctx, server.DefaultWorld, snap); err != nil {
		t.Fatalf("save sidecar: %v", err)
	}

	sc, found, err := store.Side().Load(ctx, server.DefaultWorld, world.ChunkPos{}, snap.Chunks[world.ChunkPos{}].Gen)
	if err != nil {
		t.Fatalf("load sidecar: %v", err)
	}
	if !found {
		t.Fatal("no sidecar was written")
	}
	if len(sc.BlockIdentity) != 0 {
		t.Errorf("sidecar holds %d identities on a server that tracks none", len(sc.BlockIdentity))
	}
}
