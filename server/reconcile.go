package server

import (
	"sync"

	"github.com/go-theft-craft/server/internal/server/player"
	"github.com/go-theft-craft/server/internal/server/storage"
	"github.com/go-theft-craft/server/pkg/world"
)

// Reconciliation, from the server's side.
//
// The pass itself is in internal/server/storage. What lives here is the two
// things it needs from this package: somewhere for its findings to become
// records, and somewhere for its counts to be added up. A chunk load happens
// on whichever goroutine asked for the chunk, so the counters are atomic and
// the report is one line rather than one per column.

// reconcileSink turns what the pass found into records.
//
// Actor kind reconcile is the point: a query that finds an item whose history
// begins at a load can tell that from one whose history begins at a craft, and
// "this appeared at startup and nobody knows where it came from" is exactly
// the thing an audit trail exists to say out loud.
type reconcileSink struct{ recorder *Recorder }

var _ storage.ReconcileSink = reconcileSink{}

func (s reconcileSink) Reconciled(minted, retired []ItemID, at Location, note string) {
	if s.recorder == nil {
		return
	}
	if len(minted) > 0 {
		s.recorder.Record(Record{
			Kind:   RecordItem,
			Reason: ReasonReconcile,
			Actor:  Actor{Kind: ActorReconcile},
			To:     at,
			Items:  minted,
			Cause:  []Reason{ReasonMint},
			Note:   note,
		})
	}
	if len(retired) > 0 {
		s.recorder.Record(Record{
			Kind:   RecordItem,
			Reason: ReasonReconcile,
			Actor:  Actor{Kind: ActorReconcile},
			From:   at,
			Items:  retired,
			Cause:  []Reason{ReasonRetire},
			Note:   note,
		})
	}
}

// reconcileCounts adds up what every chunk load reconciled.
type reconcileCounts struct {
	mu    sync.Mutex
	total storage.Result
}

func (c *reconcileCounts) add(r storage.Result) storage.Result {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.total.Chunks += r.Chunks
	c.total.Minted += r.Minted
	c.total.Retired += r.Retired
	c.total.Stale += r.Stale

	return c.total
}

func (c *reconcileCounts) read() storage.Result {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.total
}

// recordReconciliation logs a column's findings, and only a column that found
// something: a world of ten thousand chunks that all agree should say nothing.
func (s *Server) recordReconciliation(r storage.Result) {
	if r.Empty() {
		s.reconciled.add(r)

		return
	}

	total := s.reconciled.add(r)
	s.log.Info("reconciled identity at load",
		"minted", r.Minted, "retired", r.Retired,
		"total_minted", total.Minted, "total_retired", total.Retired)
}

// Reconciled is everything this run's loads squared away. It is what a test
// asserts on and what an operator asks after a restart that went badly.
func (s *Server) Reconciled() storage.Result { return s.reconciled.read() }

// reconcileInventory squares a loaded player's own slots with their identity.
//
// It runs where the data is read rather than where a click happens, which is
// the difference this task makes: before it, a restored stack got identity on
// the first click that moved it, so the index was empty until the player
// touched something, and a duplication between two saved inventories was
// invisible until then.
func (s *Server) reconcileInventory(uuid string, slots, armor []world.ItemStack) {
	if s == nil || s.reconcile == nil {
		return
	}

	// The index names a player slot by its protocol number, which is what
	// every click path names it too. Reconciling under a different numbering
	// would claim an item somewhere no later move could find it.
	protos := make([]int, 0, len(slots)+len(armor))
	for i := range slots {
		protos = append(protos, player.ProtocolSlotOf(i))
	}
	for a := range armor {
		protos = append(protos, armorProtocolSlot(a))
	}

	get := func(proto int) world.ItemStack {
		if a, ok := armorIndexOf(proto); ok {
			return armor[a]
		}

		return slots[inventoryIndexOf(proto)]
	}
	set := func(proto int, v world.ItemStack) {
		if a, ok := armorIndexOf(proto); ok {
			armor[a] = v

			return
		}
		slots[inventoryIndexOf(proto)] = v
	}

	s.recordReconciliation(s.reconcile.Inventory(uuid, protos, get, set))
}

// armorProtocolSlot maps an armor array index onto its protocol slot. The
// array runs feet first and the protocol numbers head first, which is a
// reversal it is easy to write the wrong way round by hand.
func armorProtocolSlot(index int) int { return 8 - index }

func armorIndexOf(proto int) (int, bool) {
	if proto < 5 || proto > 8 {
		return 0, false
	}

	return 8 - proto, true
}

func inventoryIndexOf(proto int) int {
	if proto >= 36 {
		return proto - 36
	}

	return proto
}
