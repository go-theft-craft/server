package conn

import (
	"testing"

	"github.com/go-theft-craft/server/internal/server/packet"
	"github.com/go-theft-craft/server/internal/server/storage"
	"github.com/go-theft-craft/server/pkg/world"
)

// Block identity on the place and break paths.
//
// The join is the point: a placement record names the item that was spent and
// the block that resulted, and a break record names the same block and the
// items that came out of it. Following one chain from an inventory, through a
// wall, and back into an inventory is what block identity buys, and it is the
// only reason a position gets an ID at all.

// blockRecords collects what the connection recorded, in order.
type blockRecords struct {
	placed []blockEvent
	broken []blockEvent
}

type blockEvent struct {
	pos   world.BlockPos
	block string
	id    world.ItemID
	items []world.ItemID
}

func (r *blockRecords) RecordBlockPlace(pos world.BlockPos, block string, id world.ItemID, from []world.ItemID, _ world.Actor) {
	r.placed = append(r.placed, blockEvent{pos: pos, block: block, id: id, items: from})
}

func (r *blockRecords) RecordBlockBreak(pos world.BlockPos, block string, id world.ItemID, drops []world.ItemID, _ world.Actor) {
	r.broken = append(r.broken, blockEvent{pos: pos, block: block, id: id, items: drops})
}

// newBlockIdentityConn is an identity connection that also identifies blocks.
func newBlockIdentityConn(t *testing.T) (*Connection, *storage.BlockIdentity, *blockRecords) {
	t.Helper()

	c, _, _ := newIdentityTestConn(t)
	blocks, records := storage.NewBlockIdentity(), &blockRecords{}
	c.SetBlockIdentity(blocks, records)

	return c, blocks, records
}

func TestAPlacedBlockGetsAnIDAndAWorldgenBlockDoesNot(t *testing.T) {
	c, blocks, records := newBlockIdentityConn(t)
	c.self.SetGameMode(packet.GameModeSurvival)
	seed(t, c, int16(slotHotbarStart), dirt(10))
	c.self.Inventory.SetHeldSlot(0)

	if err := c.handleBlockPlace(placeOnTopOf(0, 3, 0, dirt(10))); err != nil {
		t.Fatalf("handleBlockPlace: %v", err)
	}

	placed := world.BlockPos{X: 0, Y: 4, Z: 0}
	id, ok := blocks.At(placed)
	if !ok || !id.Valid() {
		t.Fatalf("block at %v has identity %v, %v; want one", placed, id, ok)
	}

	// The block under it is the one the generator made, and the whole reason
	// the table is affordable is that it is not in there.
	if _, ok := blocks.At(world.BlockPos{X: 0, Y: 3, Z: 0}); ok {
		t.Error("a generated block has an identity; the table is meant to be sparse")
	}
	if got := blocks.Len(); got != 1 {
		t.Errorf("table holds %d blocks, want 1", got)
	}

	if len(records.placed) != 1 {
		t.Fatalf("recorded %d placements, want 1", len(records.placed))
	}
	rec := records.placed[0]
	if rec.id != id || rec.pos != placed {
		t.Errorf("placement record is %v at %v, want %v at %v", rec.id, rec.pos, id, placed)
	}
	if len(rec.items) != 1 {
		t.Fatalf("placement record names %d items spent, want the one dirt", len(rec.items))
	}
}

func TestBreakingAPlacedBlockLinksItsIDToTheDropsItemID(t *testing.T) {
	c, blocks, records := newBlockIdentityConn(t)
	c.self.SetGameMode(packet.GameModeSurvival)
	seed(t, c, int16(slotHotbarStart), dirt(10))
	c.self.Inventory.SetHeldSlot(0)

	if err := c.handleBlockPlace(placeOnTopOf(0, 3, 0, dirt(10))); err != nil {
		t.Fatalf("handleBlockPlace: %v", err)
	}
	placed := world.BlockPos{X: 0, Y: 4, Z: 0}
	blockID, _ := blocks.At(placed)
	spent := records.placed[0].items

	c.breakBlock(placed.X, placed.Y, placed.Z)

	if _, ok := blocks.At(placed); ok {
		t.Error("the block is gone but its identity is still in the table")
	}
	if len(records.broken) != 1 {
		t.Fatalf("recorded %d breaks, want 1", len(records.broken))
	}

	broke := records.broken[0]
	if broke.id != blockID {
		t.Errorf("break record names block %v, want %v", broke.id, blockID)
	}
	if len(broke.items) == 0 {
		t.Fatal("break record names no drop; the join to the item chain is what the record is for")
	}
	for _, id := range broke.items {
		if id == blockID {
			t.Errorf("drop %v reuses the block's own ID; the two are linked, not the same", id)
		}
		for _, was := range spent {
			if id == was {
				t.Errorf("drop %v reuses the ID of the item that was spent placing it", id)
			}
		}
	}
}

func TestBlockIDsLoadAndUnloadWithTheirChunk(t *testing.T) {
	blocks := storage.NewBlockIdentity()
	pos := world.BlockPos{X: 33, Y: 70, Z: -17}
	id := world.NewItemID(7, 42)

	blocks.Set(pos, id)
	stored := blocks.Chunk(pos.ChunkPos())
	if len(stored) != 1 {
		t.Fatalf("chunk holds %d entries, want 1", len(stored))
	}

	// A restart is a new table filled from what the sidecar held.
	restarted := storage.NewBlockIdentity()
	if bad := restarted.LoadChunk(pos.ChunkPos(), stored); bad != 0 {
		t.Fatalf("%d entries would not parse", bad)
	}
	got, ok := restarted.At(pos)
	if !ok || got != id {
		t.Fatalf("after load, %v holds %v, %v; want %v", pos, got, ok, id)
	}

	// A position in another chunk with the same chunk-local coordinates is a
	// different block, which is what the chunk key has to keep apart.
	other := world.BlockPos{X: pos.X + 16, Y: pos.Y, Z: pos.Z}
	if _, ok := restarted.At(other); ok {
		t.Errorf("%v answers for %v; the chunk key is not separating them", other, pos)
	}

	restarted.DropChunk(pos.ChunkPos())
	if _, ok := restarted.At(pos); ok {
		t.Error("identity outlived the chunk it belongs to")
	}
	if got := restarted.Len(); got != 0 {
		t.Errorf("table holds %d blocks after unloading the only chunk, want 0", got)
	}
}

func TestUniversalIdentityIsOffByDefault(t *testing.T) {
	// Identity on, a whole generated column read, and nothing in the table:
	// the sparse design is that generated blocks never enter it. Universal
	// identity — an ID for every block — is not implemented, and this is the
	// test that says a build cannot drift into it by accident. A 2000×2000
	// world is a billion blocks and 8.2 GB of identity, nearly all of it air
	// and stone no query would reach.
	c, blocks, records := newBlockIdentityConn(t)
	c.self.SetGameMode(packet.GameModeSurvival)

	for y := range 16 {
		c.blockAt(0, y, 0)
	}
	if got := blocks.Len(); got != 0 {
		t.Fatalf("reading %d generated blocks put %d in the table, want 0", 16, got)
	}

	// Breaking one records nothing either: there is no identity to release
	// and no chain to join.
	c.breakBlock(0, 3, 0)
	if len(records.broken) != 0 {
		t.Errorf("breaking a generated block recorded %d block events, want 0", len(records.broken))
	}
}

// A connection built without identity is the default, and the block paths have
// to be inert on it rather than merely cheap.
func TestBlockIdentityIsInertWhenIdentityIsOff(t *testing.T) {
	c := newInventoryTestConn(t)
	c.self.SetGameMode(packet.GameModeSurvival)
	c.self.Inventory.SetSlot(0, dirt(10))
	c.self.Inventory.SetHeldSlot(0)

	if err := c.handleBlockPlace(placeOnTopOf(0, 3, 0, dirt(10))); err != nil {
		t.Fatalf("handleBlockPlace: %v", err)
	}
	c.breakBlock(0, 4, 0)

	if c.blocks != nil {
		t.Error("a server without item identity built a block identity table")
	}
	if got := countItem(c, 3, 0); got != 9 {
		t.Errorf("dirt after placing one = %d, want 9; identity must not change the game", got)
	}
}
