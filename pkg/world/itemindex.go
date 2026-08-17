package world

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

// The item index.
//
// The index is the *write path*, not an observer of it. Every movement of an
// identified item goes through Move, which knows where each ID currently is
// and reports one that is live somewhere other than where the move says it
// came from. That is what makes a duplication detectable at the moment it
// happens rather than inferable from a log afterwards.
//
// It lives in this package for the same reason ItemID does: a stack carries
// the IDs, and internal/server/conn — where every click path is — cannot
// import the server package.

// LocationKind is what sort of place an item is in.
type LocationKind uint8

// The places an item can be.
const (
	// LocationNowhere is the zero value: an ID the index has never seen, or
	// one that has been retired.
	LocationNowhere LocationKind = iota
	// LocationInventory is a slot of a player's own inventory or armor.
	LocationInventory
	// LocationCursor is the stack a player is holding with the mouse.
	LocationCursor
	// LocationCrafting is a cell of an open crafting grid.
	LocationCrafting
	// LocationContainer is a slot of a container in the world.
	LocationContainer
	// LocationEntity is a dropped item on the ground.
	LocationEntity
	// LocationWorld is a block that is itself the item, which is what a
	// placed block becomes.
	LocationWorld
)

func (k LocationKind) String() string {
	switch k {
	case LocationInventory:
		return "inventory"
	case LocationCursor:
		return "cursor"
	case LocationCrafting:
		return "crafting"
	case LocationContainer:
		return "container"
	case LocationEntity:
		return "entity"
	case LocationWorld:
		return "world"
	default:
		return "nowhere"
	}
}

// Location is where an item is.
//
// Which fields carry meaning depends on Kind: Player and Slot for the three
// player-side kinds, Block for a container or a placed block, Entity for a
// dropped item.
type Location struct {
	Kind   LocationKind
	Player string
	Slot   int
	Block  BlockPos
	Entity int32
}

// Nowhere is the location of an item that is not anywhere: the destination of
// a retirement and the origin of a mint.
var Nowhere = Location{}

func (l Location) String() string {
	var b strings.Builder
	b.WriteString(l.Kind.String())
	switch l.Kind {
	case LocationInventory, LocationCursor, LocationCrafting:
		fmt.Fprintf(&b, "(%s slot %d)", l.Player, l.Slot)
	case LocationContainer:
		fmt.Fprintf(&b, "(%d,%d,%d slot %d)", l.Block.X, l.Block.Y, l.Block.Z, l.Slot)
	case LocationWorld:
		fmt.Fprintf(&b, "(%d,%d,%d)", l.Block.X, l.Block.Y, l.Block.Z)
	case LocationEntity:
		fmt.Fprintf(&b, "(%d)", l.Entity)
	case LocationNowhere:
	}

	return b.String()
}

// ActorKind is who moved something.
type ActorKind uint8

// The kinds of actor.
const (
	// ActorServer is the server itself: generation, a tick, a command with no
	// player behind it.
	ActorServer ActorKind = iota
	// ActorPlayer is a connected player.
	ActorPlayer
	// ActorReconcile is the load-time pass that squares identity with what is
	// actually in the world.
	ActorReconcile
)

func (k ActorKind) String() string {
	switch k {
	case ActorPlayer:
		return "player"
	case ActorReconcile:
		return "reconcile"
	default:
		return "server"
	}
}

// Actor is who caused a movement.
type Actor struct {
	Kind ActorKind
	UUID string
	Name string
}

func (a Actor) String() string {
	if a.Name != "" {
		return a.Kind.String() + " " + a.Name
	}

	return a.Kind.String()
}

// ErrDuplicate reports an ID that was live somewhere other than where a move
// said it came from.
//
// It names both locations and the actor, because the first question about a
// duplication is which two places claimed the same item and who was doing
// something at the time.
type ErrDuplicate struct {
	ID       ItemID
	Expected Location
	Actual   Location
	By       Actor
}

func (e *ErrDuplicate) Error() string {
	return fmt.Sprintf("item %s is at %s, not at %s, where %s said it was",
		e.ID, e.Actual, e.Expected, e.By)
}

// DuplicatePolicy is what the index does when it detects one.
type DuplicatePolicy uint8

// The two policies.
const (
	// DuplicateAllow records the detection and lets the write through. It is
	// the default: refusing turns a duplication bug into item loss, and item
	// loss on a false positive is worse for the player than an extra item.
	// The detector is new code with no field history.
	DuplicateAllow DuplicatePolicy = iota
	// DuplicateRefuse rejects the write.
	DuplicateRefuse
)

// ItemIndex tracks where every identified item is.
type ItemIndex interface {
	// Mint issues n IDs and records them at loc.
	Mint(n int, loc Location, by Actor) ([]ItemID, error)
	// Move records that ids went from one place to another. It reports
	// *ErrDuplicate for an ID that was somewhere else, whether or not the
	// policy let the write through.
	Move(ids []ItemID, from, to Location, by Actor) error
	// Retire records that ids no longer exist. A retired ID is never reissued
	// to anyone.
	Retire(ids []ItemID, at Location, by Actor) error
	// Where is an ID's current location.
	Where(id ItemID) (Location, bool)
	// Len is how many IDs are live, which is the index's memory cost.
	Len() int
}

// IndexObserver is told about every detected duplication.
//
// It is how the recorder learns about one without the index depending on it.
type IndexObserver func(*ErrDuplicate)

// NewItemIndex returns an index backed by one map under one mutex.
//
// It is deliberately simple: a click moves at most 64 IDs, and a sharded map
// is an optimization to reach for once measurement says it is needed.
func NewItemIndex(m *Minter, policy DuplicatePolicy, observe IndexObserver) ItemIndex {
	return &itemIndex{minter: m, policy: policy, observe: observe, at: map[ItemID]Location{}}
}

type itemIndex struct {
	minter  *Minter
	policy  DuplicatePolicy
	observe IndexObserver

	mu sync.Mutex
	at map[ItemID]Location
}

func (i *itemIndex) Mint(n int, loc Location, _ Actor) ([]ItemID, error) {
	if n <= 0 {
		return nil, nil
	}
	if i.minter == nil {
		return nil, errors.New("world: no minter, item identity is unavailable")
	}

	ids, err := i.minter.MintN(n)
	if err != nil {
		return nil, err
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	for _, id := range ids {
		i.at[id] = loc
	}

	return ids, nil
}

func (i *itemIndex) Move(ids []ItemID, from, to Location, by Actor) error {
	if len(ids) == 0 {
		return nil
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	var first *ErrDuplicate
	for _, id := range ids {
		actual, known := i.at[id]
		if known && actual != from {
			dup := &ErrDuplicate{ID: id, Expected: from, Actual: actual, By: by}
			if i.observe != nil {
				i.observe(dup)
			}
			if first == nil {
				first = dup
			}
			if i.policy == DuplicateRefuse {
				continue
			}
		}
		i.at[id] = to
	}

	if first != nil {
		return first
	}

	return nil
}

func (i *itemIndex) Retire(ids []ItemID, at Location, by Actor) error {
	if len(ids) == 0 {
		return nil
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	var first *ErrDuplicate
	for _, id := range ids {
		actual, known := i.at[id]
		if known && actual != at {
			dup := &ErrDuplicate{ID: id, Expected: at, Actual: actual, By: by}
			if i.observe != nil {
				i.observe(dup)
			}
			if first == nil {
				first = dup
			}
			if i.policy == DuplicateRefuse {
				continue
			}
		}
		// Deleted rather than marked: a retired ID is never reissued, because
		// the minter only ever counts forward.
		delete(i.at, id)
	}

	if first != nil {
		return first
	}

	return nil
}

func (i *itemIndex) Where(id ItemID) (Location, bool) {
	i.mu.Lock()
	defer i.mu.Unlock()

	loc, ok := i.at[id]

	return loc, ok
}

func (i *itemIndex) Len() int {
	i.mu.Lock()
	defer i.mu.Unlock()

	return len(i.at)
}
